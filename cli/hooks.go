package cli

import (
	"context"
	"io"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/hookobs"
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
	// BaseURL is the provider endpoint the request was sent to. Any userinfo
	// credentials embedded in the configured URL are stripped before the value
	// reaches this field, matching what the client does before building the
	// request — so a config of https://user:pass@host/v1 is reported as
	// https://host/v1.
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
	// "length", "tool_calls", ...).
	//
	// It is fully reported only on multi-turn (tool-loop) invocations. The
	// single-shot path does not surface the raw reason — the underlying client
	// decodes it into a truncation flag and discards the string — so there it
	// is synthesized: "length" when truncated, empty otherwise. An empty value
	// on a single-shot invocation therefore means "not truncated", NOT "the
	// provider omitted a stop reason". Use Truncated as the reliable signal.
	FinishReason string
	// Truncated reports that the provider stopped on finish_reason "length" —
	// the token budget was exhausted and Response may be partial.
	//
	// It is reported on the truncation-aware single-shot path (the one the
	// fan-out engine prefers) and on multi-turn turns. It is always false on
	// the plain Complete and CompleteWithUsage paths, which the engine selects
	// only for a completer that cannot report truncation at all.
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
	// ModelInvocation observes model calls. Coverage is every client built
	// through internal/hookobs.Wrap: the review and resume fan-out, benchmark,
	// the MCP server, verify's skeptics, verify's fix executor (--auto-fix),
	// and debate's seats. That spans `atcr review` including its --verify,
	// --debate, and --auto-fix stages, plus the standalone `atcr verify` and
	// `atcr debate` commands and their MCP tool equivalents.
	//
	// It does NOT cover `atcr doctor`, whose provider self-test builds its own
	// client through a separate interface in internal/doctor and never reaches
	// this seam. A doctor run therefore produces no observations.
	ModelInvocation ModelInvocationObserver
}

// hooksKey/withHooks are gone: the context carrier now lives in
// internal/hookobs, because internal/verify and internal/debate also construct
// clients and cannot import cli (cli imports them). See that package's doc.

// observerAdapter converts the low-level observation into the exported shape
// and forwards it to the consumer's observer. It is the only place the two
// payload types meet, so the exported surface can evolve independently of the
// internal one.
type observerAdapter struct {
	observer ModelInvocationObserver
}

func (a observerAdapter) OnModelInvocation(in hookobs.Invocation) {
	mi := ModelInvocation{
		Model:            in.Model,
		Provider:         in.Provider,
		BaseURL:          in.BaseURL,
		Prompt:           in.Prompt,
		Response:         in.Response,
		FinishReason:     in.FinishReason,
		Truncated:        in.Truncated,
		PromptTokens:     in.PromptTokens,
		CompletionTokens: in.CompletionTokens,
		StartedAt:        in.StartedAt,
		Duration:         in.Duration,
		Err:              in.Err,
	}
	if len(in.Messages) > 0 {
		mi.Messages = make([]ModelMessage, len(in.Messages))
		for i, m := range in.Messages {
			mi.Messages[i] = ModelMessage{Role: m.Role, Content: m.Content}
		}
	}
	a.observer.OnModelInvocation(mi)
}

// withHooks attaches hooks to ctx so every client-construction site can find
// them. Unexported: registration is via MainWithHooks, not by hand-building a
// context, which keeps the public surface to one entry point. A zero Hooks
// attaches a nil observer, which hookobs.Wrap treats as no observation.
func withHooks(ctx context.Context, hooks Hooks, stderr io.Writer) context.Context {
	if hooks.ModelInvocation == nil {
		return hookobs.NewContext(ctx, nil, stderr)
	}
	return hookobs.NewContext(ctx, observerAdapter{observer: hooks.ModelInvocation}, stderr)
}

// MainWithHooks is Main with caller-supplied observers. It runs the identical
// lifecycle — signal handling, telemetry drain, exit-code mapping — because it
// IS the same code path: both entry points delegate to runMain, neither wraps
// the other. Main attaches no hooks context at all; MainWithHooks attaches one
// and is otherwise identical.
//
// With a zero Hooks the behaviour is byte-for-byte identical to Main: no
// decorator is installed, so the fan-out engine receives the same
// *llmclient.Client it always has and every capability type-assertion it makes
// resolves exactly as before.
//
// Coverage: the observer fires for `atcr review` — including its --verify,
// --debate, and --auto-fix stages — plus `atcr review --resume`, `atcr verify`,
// `atcr debate`, `atcr benchmark run`, and the equivalent MCP tools served by
// `atcr serve`. It does NOT cover `atcr doctor`, whose provider self-test uses
// a separate completer interface and never reaches this seam, so a doctor run
// produces no observations.
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
// TestHooks_ModelClientsAreObserved guards against.
func newCompleter(ctx context.Context) fanout.Completer {
	return hookobs.Wrap(ctx, llmclient.New())
}
