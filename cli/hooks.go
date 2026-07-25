package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/samestrin/atcr/internal/circuitbreaker"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
)

// ModelInvocation is one observed model call: the request that went out, the
// response that came back, and the telemetry surrounding it.
//
// SENSITIVE DATA. Prompt and Response carry the model exchange verbatim, which
// for atcr means the diff hunks, file paths, and source lines under review — and
// anything a reviewer persona template interpolated into the prompt. This type
// applies no masking, redaction, or truncation of its own, deliberately: the
// public core stays generic and cannot know which values a given deployment
// considers secret. A consumer that persists, forwards, or logs these fields is
// responsible for redacting them first, for the durability and access controls
// of wherever they land, and for any retention obligations that follow. If that
// is not something you want to own, do not register a hook.
//
// Every field is a plain type. Nothing here exposes an internal/ package,
// because the intended consumer is a separate Go module (the private
// atcr-enterprise wrapper) for which internal/ types are unreachable by
// construction.
type ModelInvocation struct {
	// Model is the provider-qualified model identifier as configured, e.g.
	// "anthropic/claude-sonnet-4.5".
	Model string
	// Provider is the logical provider name the fan-out engine attached to the
	// call (e.g. "openrouter"). It falls back to BaseURL when the call runs
	// outside the engine and no provider was attached.
	Provider string
	// BaseURL is the provider endpoint the request was sent to, exactly as
	// configured. Any userinfo credentials in a configured URL are stripped by
	// the client before the request is built, but BaseURL here is the
	// unmodified configured value — treat it as sensitive if your config
	// embeds credentials in the URL.
	BaseURL string
	// Prompt is the single-shot prompt text. On a multi-turn tool-loop turn it
	// is the invocation's base prompt; Messages carries that turn's history.
	Prompt string
	// Messages is the conversation history sent on a multi-turn (tool-loop)
	// turn, in wire order. It is nil for single-shot invocations.
	Messages []ModelMessage
	// Response is the assistant's content for this call. It is empty when the
	// provider returned only tool calls (an assistant turn with content:null),
	// and when the invocation failed before producing content.
	Response string
	// FinishReason is the provider's stop reason for the call ("stop",
	// "length", "tool_calls", ...). It is empty when the provider omitted it or
	// the call failed before a response was decoded.
	FinishReason string
	// Truncated reports that the provider stopped on finish_reason "length" —
	// the token budget was exhausted and Response may be partial.
	Truncated bool
	// PromptTokens and CompletionTokens are the provider-reported token counts
	// for this call. Both are zero when the provider omitted the usage block,
	// which is graceful degradation rather than an error, and on failure paths.
	PromptTokens     int
	CompletionTokens int
	// StartedAt is when the call was dispatched, in UTC.
	StartedAt time.Time
	// Duration is the wall-clock time the call took, including retries.
	Duration time.Duration
	// Err is the invocation's error message, empty on success. A failed call is
	// still reported exactly once, with whatever partial data was recovered, so
	// a consumer building an audit trail can account for it.
	Err string
}

// ModelMessage is one chat message in a multi-turn invocation. Content is
// flattened to a string: a nil (content:null) assistant tool-call turn is
// reported as an empty string.
type ModelMessage struct {
	Role    string
	Content string
}

// ModelInvocationObserver receives one call per model invocation.
//
// The fan-out engine runs reviewer agents concurrently, so implementations MUST
// be safe for concurrent use. Implementations should also return promptly:
// OnModelInvocation runs inline on the invocation path, so time spent here is
// time added to the review. A panic is recovered at the seam, reported on
// stderr, and the run continues unaffected — but do not rely on that as an
// error-handling strategy.
type ModelInvocationObserver interface {
	OnModelInvocation(ModelInvocation)
}

// Hooks is the set of observers an embedding binary can register. The zero
// value registers nothing and is exactly equivalent to not using hooks at all:
// MainWithHooks(ctx, stdout, stderr, Hooks{}) behaves identically to Main.
//
// It is a struct rather than a bare interface so future observation points can
// be added as new fields without breaking existing consumers.
type Hooks struct {
	// ModelInvocation observes every model call that routes through the review
	// fan-out engine — which covers the review, resume, benchmark, and serve
	// paths. It does NOT cover `atcr doctor`, whose self-test uses its own
	// separate completer and never enters the fan-out engine.
	ModelInvocation ModelInvocationObserver
}

// hooksState is what travels on the context: the registered hooks plus the
// stderr writer the seam reports consumer-hook panics to. stderr is carried
// alongside rather than looked up globally so the observing decorator stays
// consistent with the writer the caller handed to MainWithHooks (tests included).
type hooksState struct {
	hooks  Hooks
	stderr io.Writer
}

type hooksKey struct{}

// withHooks attaches hooks to ctx so the completer construction sites can find
// them. Unexported: registration is via MainWithHooks, not by hand-building a
// context, which keeps the public surface to one entry point.
func withHooks(ctx context.Context, hooks Hooks, stderr io.Writer) context.Context {
	return context.WithValue(ctx, hooksKey{}, hooksState{hooks: hooks, stderr: stderr})
}

// hooksFrom returns the hooks attached to ctx, if any.
func hooksFrom(ctx context.Context) (hooksState, bool) {
	s, ok := ctx.Value(hooksKey{}).(hooksState)
	return s, ok
}

// MainWithHooks is Main with caller-supplied observers. It runs the identical
// lifecycle — signal handling, telemetry drain, exit-code mapping — because it
// IS the same code path; Main is defined as MainWithHooks with an empty Hooks.
//
// With a zero Hooks the behaviour is byte-for-byte identical to Main: no
// decorator is installed, so the fan-out engine receives the same
// *llmclient.Client it always has and every capability type-assertion it makes
// resolves exactly as before.
//
// The intended caller is a wrapper binary that needs to observe model traffic
// without vendoring the command tree:
//
//	func main() {
//		hooks := cli.Hooks{ModelInvocation: myAuditGateway{}}
//		os.Exit(cli.MainWithHooks(context.Background(), os.Stdout, os.Stderr, hooks))
//	}
//
// See ModelInvocation for the sensitive-data handling that registering an
// observer makes the caller responsible for.
func MainWithHooks(ctx context.Context, stdout, stderr io.Writer, hooks Hooks) int {
	return runMain(withHooks(ctx, hooks, stderr), stdout, stderr)
}

// newCompleter builds the completer the fan-out engine drives, wrapping it in
// the observing decorator when a hook is registered. Every cli call site that
// hands a completer to internal/fanout must go through here — a bare
// llmclient.New() at such a site is an invisible audit gap, which
// TestHooks_FanoutCallSitesUseObservedCompleter guards against.
func newCompleter(ctx context.Context) fanout.Completer {
	return observedCompleter(ctx, llmclient.New())
}

// observedCompleter returns client unchanged when no observer is registered, so
// the default path adds no wrapper, no allocation, and no behavioural
// difference whatsoever (AC 01-03).
func observedCompleter(ctx context.Context, client *llmclient.Client) fanout.Completer {
	state, ok := hooksFrom(ctx)
	if !ok || state.hooks.ModelInvocation == nil {
		return client
	}
	return &observingCompleter{
		inner:    client,
		observer: state.hooks.ModelInvocation,
		stderr:   state.stderr,
	}
}

// observingCompleter decorates the real client, reporting each call to the
// registered observer exactly once — on the success and failure paths alike.
//
// It implements the full completer family (Completer, UsageCompleter,
// MetaCompleter, ChatCompleter) deliberately. The engine selects its call path
// by type assertion, preferring the richest interface available; a decorator
// implementing only Complete would silently downgrade every agent to the
// no-usage, no-truncation path, producing observations that look valid but
// carry zero token counts.
//
// Each method delegates to the corresponding method on the inner client — never
// to a sibling method on the decorator — so exactly one observation is emitted
// per model call regardless of which entry point the engine chose.
type observingCompleter struct {
	inner    *llmclient.Client
	observer ModelInvocationObserver
	stderr   io.Writer
}

// Compile-time proof of the capability set described above. A missing method
// here is a silent data-quality regression rather than a build failure at the
// call sites, so it is asserted explicitly.
var (
	_ fanout.Completer      = (*observingCompleter)(nil)
	_ fanout.UsageCompleter = (*observingCompleter)(nil)
	_ fanout.MetaCompleter  = (*observingCompleter)(nil)
	_ fanout.ChatCompleter  = (*observingCompleter)(nil)
)

// base seeds the observation with everything known before the call is made, so
// a failure path still reports full request identity.
func (o *observingCompleter) base(ctx context.Context, inv llmclient.Invocation, start time.Time) ModelInvocation {
	provider := circuitbreaker.ProviderFromContext(ctx)
	if provider == "" {
		provider = inv.BaseURL
	}
	return ModelInvocation{
		Model:     inv.Model,
		Provider:  provider,
		BaseURL:   inv.BaseURL,
		Prompt:    inv.Prompt,
		StartedAt: start.UTC(),
	}
}

// emit reports one observation, containing any consumer panic. A broken hook
// must never change the outcome of the run it is observing, so the panic is
// recovered here, noted on stderr, and execution continues.
func (o *observingCompleter) emit(mi ModelInvocation) {
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(o.stderr, "atcr: audit hook panicked, continuing without it: %v\n", r)
		}
	}()
	o.observer.OnModelInvocation(mi)
}

// finish stamps duration and error onto the observation and emits it.
func (o *observingCompleter) finish(mi ModelInvocation, start time.Time, err error) {
	mi.Duration = time.Since(start)
	if err != nil {
		mi.Err = err.Error()
	}
	o.emit(mi)
}

// Complete observes the plain single-shot path.
func (o *observingCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	start := time.Now()
	mi := o.base(ctx, inv, start)

	content, err := o.inner.Complete(ctx, inv)

	mi.Response = content
	o.finish(mi, start, err)
	return content, err
}

// CompleteWithUsage observes the usage-reporting single-shot path.
func (o *observingCompleter) CompleteWithUsage(ctx context.Context, inv llmclient.Invocation) (
	string, llmclient.UsageData, []llmclient.CallRecord, error) {
	start := time.Now()
	mi := o.base(ctx, inv, start)

	content, usage, records, err := o.inner.CompleteWithUsage(ctx, inv)

	mi.Response = content
	mi.PromptTokens = usage.PromptTokens
	mi.CompletionTokens = usage.CompletionTokens
	o.finish(mi, start, err)
	return content, usage, records, err
}

// CompleteWithMeta observes the truncation-aware single-shot path, the one the
// engine prefers.
func (o *observingCompleter) CompleteWithMeta(ctx context.Context, inv llmclient.Invocation) (
	llmclient.Completion, error) {
	start := time.Now()
	mi := o.base(ctx, inv, start)

	comp, err := o.inner.CompleteWithMeta(ctx, inv)

	mi.Response = comp.Content
	mi.PromptTokens = comp.Usage.PromptTokens
	mi.CompletionTokens = comp.Usage.CompletionTokens
	mi.Truncated = comp.Truncated
	if comp.Truncated {
		mi.FinishReason = "length"
	}
	o.finish(mi, start, err)
	return comp, err
}

// Chat observes the multi-turn tool-loop path. The loop calls Chat once per
// turn and each turn is a distinct model invocation, so each produces its own
// observation.
func (o *observingCompleter) Chat(ctx context.Context, inv llmclient.Invocation,
	messages []llmclient.Message, toolDefs []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	start := time.Now()
	mi := o.base(ctx, inv, start)
	mi.Messages = flattenMessages(messages)

	resp, err := o.inner.Chat(ctx, inv, messages, toolDefs)

	if resp != nil {
		// An assistant turn requesting tools carries content:null; report it as
		// an empty response rather than dereferencing a nil pointer.
		if resp.Message.Content != nil {
			mi.Response = *resp.Message.Content
		}
		mi.FinishReason = resp.FinishReason
		mi.Truncated = resp.Truncated
		mi.PromptTokens = resp.Usage.PromptTokens
		mi.CompletionTokens = resp.Usage.CompletionTokens
	}
	o.finish(mi, start, err)
	return resp, err
}

// flattenMessages converts the wire message history into the exported,
// pointer-free shape, normalising content:null to an empty string.
func flattenMessages(messages []llmclient.Message) []ModelMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]ModelMessage, len(messages))
	for i, m := range messages {
		out[i] = ModelMessage{Role: m.Role}
		if m.Content != nil {
			out[i].Content = *m.Content
		}
	}
	return out
}
