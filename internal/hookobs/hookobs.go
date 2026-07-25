// Package hookobs carries a caller-supplied model-invocation observer on the
// request context and decorates the LLM client so every model call is reported
// exactly once.
//
// It exists as its own low-level package for a structural reason. The public
// cli package owns the exported hook API (cli.Hooks, cli.MainWithHooks), but
// model invocations do not all originate under cli: internal/verify's skeptics,
// its fix executor, and internal/debate's seats each construct their own client
// and call providers directly. Those packages cannot import cli — cli imports
// them — so the context key and the decorator have to live below all of them.
//
// The payoff is that observation resolves from the context at the point a
// client is built, rather than being threaded through every call site as a
// parameter. A new model-invocation site is covered by calling Wrap; it cannot
// silently bypass observation by forgetting to forward an argument, which is
// the failure mode that left verify/debate uncovered in the first place.
//
// This package deliberately defines its own payload type rather than importing
// the exported one: the dependency has to point this way (cli depends on
// hookobs), and cli adapts between the two.
package hookobs

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samestrin/atcr/internal/circuitbreaker"
	"github.com/samestrin/atcr/internal/llmclient"
)

// Invocation is one observed model call. It mirrors the exported
// cli.ModelInvocation; cli converts between the two.
//
// SENSITIVE DATA: Prompt and Response hold the model exchange verbatim. This
// package applies no masking beyond stripping credentials from the endpoint.
type Invocation struct {
	// Seq is a process-wide monotonic sequence number, assigned in emit order.
	// The engine runs agents concurrently and `atcr serve` runs concurrent
	// detached reviews, so wall-clock timestamps alone do not give a consumer a
	// total order over an interleaved stream.
	Seq int64
	// RunID, AgentName, and Stage identify which unit of work this call belongs
	// to. Without them an observer receives a flat interleaved stream it cannot
	// group into a per-run record. Each is best-effort: a layer contributes what
	// it knows, so a call made outside a review may carry none of them.
	RunID     string
	AgentName string
	Stage     string

	Model    string
	Provider string
	BaseURL  string
	Prompt   string
	Messages []Message
	Response string
	// ResponseToolCalls is the tool calls the assistant requested on this turn.
	// A tool-enabled agent's "response" is frequently a tool call with no text
	// at all, so a record omitting these would misreport the exchange.
	ResponseToolCalls []ToolCall
	FinishReason      string
	Truncated         bool
	PromptTokens      int
	CompletionTokens  int
	// CostUSD is the estimated cost from the provider-reported token counts. It
	// is computed here because the rate table lives in an internal package that
	// an external consumer cannot reach. Zero when the model is not in the rate
	// table or the provider reported no usage.
	CostUSD float64
	// Temperature and MaxTokens are the sampling parameters the call was made
	// with; nil means the provider default applied. A record of a model
	// interaction that omits them cannot answer "what settings produced this".
	Temperature *float64
	MaxTokens   *int
	// Attempts is the number of HTTP round-trips the call actually took,
	// retries included. Duration spans all of them.
	Attempts  int
	StartedAt time.Time
	Duration  time.Duration
	Err       string
}

// ToolCall is one model-requested tool invocation, flattened for export.
// Arguments is the tool's argument object as JSON, normalised to the decoded
// form so it does not vary with the provider's encoding.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Message is one chat message in a multi-turn invocation, with a nil
// (content:null) assistant tool-call turn flattened to an empty string.
type Message struct {
	Role    string
	Content string
	// ToolCalls is present on an assistant turn that requested tools;
	// ToolCallID ties a role:"tool" result back to the call that produced it.
	// Without both, a multi-turn record cannot be reassembled into the exchange
	// that actually happened.
	ToolCalls  []ToolCall
	ToolCallID string
}

// Observer receives one call per model invocation. Implementations must be safe
// for concurrent use: the fan-out engine runs reviewer agents in parallel.
type Observer interface {
	OnModelInvocation(Invocation)
}

// state is what travels on the context. stderr is carried alongside the
// observer so the panic report goes to the writer the caller supplied.
type state struct {
	observer Observer
	stderr   io.Writer
}

type ctxKey struct{}

// Call identifies the unit of work an invocation belongs to. Layers contribute
// what they know — cli sets RunID and Stage, the fan-out engine sets AgentName,
// verify/debate/the fix executor set Stage — and WithCall merges rather than
// replaces so a later layer cannot erase an earlier one's contribution.
type Call struct {
	RunID     string
	AgentName string
	Stage     string
}

type callKey struct{}

// WithCall merges c into any Call already on ctx. Empty fields do not overwrite
// values already present, so ordering between layers does not matter.
func WithCall(ctx context.Context, c Call) context.Context {
	if ctx == nil {
		return ctx
	}
	merged := callFrom(ctx)
	if c.RunID != "" {
		merged.RunID = c.RunID
	}
	if c.AgentName != "" {
		merged.AgentName = c.AgentName
	}
	if c.Stage != "" {
		merged.Stage = c.Stage
	}
	return context.WithValue(ctx, callKey{}, merged)
}

// callFrom returns the Call attached to ctx, or its zero value.
func callFrom(ctx context.Context) Call {
	if ctx == nil {
		return Call{}
	}
	c, _ := ctx.Value(callKey{}).(Call)
	return c
}

// seq assigns each observation a process-wide monotonic sequence number.
var seq atomic.Int64

// NewContext returns ctx carrying obs, to be picked up by Wrap wherever a
// client is constructed. A nil observer is stored as-is and Wrap treats it as
// "no observation", so callers need not branch.
func NewContext(ctx context.Context, obs Observer, stderr io.Writer) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, state{observer: obs, stderr: stderr})
}

// fromContext returns the observer attached to ctx, if any.
func fromContext(ctx context.Context) (state, bool) {
	if ctx == nil {
		return state{}, false
	}
	s, ok := ctx.Value(ctxKey{}).(state)
	return s, ok
}

// Client is the completer capability set the fan-out engine selects between by
// type assertion. Wrap returns this interface so the no-observer path can hand
// back the untouched *llmclient.Client: every method is present, so a value of
// this type still satisfies fanout.Completer, UsageCompleter, MetaCompleter,
// and ChatCompleter at the call sites.
type Client interface {
	Complete(ctx context.Context, inv llmclient.Invocation) (string, error)
	CompleteWithUsage(ctx context.Context, inv llmclient.Invocation) (string, llmclient.UsageData, []llmclient.CallRecord, error)
	CompleteWithMeta(ctx context.Context, inv llmclient.Invocation) (llmclient.Completion, error)
	Chat(ctx context.Context, inv llmclient.Invocation, messages []llmclient.Message, toolDefs []llmclient.ToolDef) (*llmclient.ChatResponse, error)
}

// Wrap returns client unchanged when no observer is registered, so the default
// path adds no wrapper, no allocation, and no behavioural difference at all —
// the engine's capability type-assertions resolve exactly as they did before
// this package existed.
func Wrap(ctx context.Context, client *llmclient.Client) Client {
	s, ok := fromContext(ctx)
	if !ok || s.observer == nil {
		return client
	}
	return &observingClient{inner: client, observer: s.observer, stderr: s.stderr}
}

// observingClient decorates the real client, reporting each call exactly once —
// on the success and failure paths alike.
//
// It implements the full capability set deliberately. The engine prefers the
// richest interface available (MetaCompleter, then UsageCompleter, then plain
// Complete), so a partial implementation would silently downgrade every agent
// to the no-usage, no-truncation path and emit observations that look valid but
// carry zero token counts.
//
// Each method delegates to the corresponding method on the inner client — never
// to a sibling method on the decorator — so exactly one observation is emitted
// per model call whichever entry point the engine chose.
type observingClient struct {
	inner    *llmclient.Client
	observer Observer
	stderr   io.Writer
	// mu serializes only the panic-path stderr write (see emit). It is never
	// taken on the normal invocation path, so it adds no contention.
	mu sync.Mutex
}

var _ Client = (*observingClient)(nil)

// base seeds the observation with everything known before the call is made, so
// a failure path still reports full request identity.
func (o *observingClient) base(ctx context.Context, inv llmclient.Invocation, start time.Time) Invocation {
	endpoint := scrubUserinfo(inv.BaseURL)
	provider := circuitbreaker.ProviderFromContext(ctx)
	if provider == "" {
		provider = endpoint
	}
	c := callFrom(ctx)
	return Invocation{
		RunID:       c.RunID,
		AgentName:   c.AgentName,
		Stage:       c.Stage,
		Model:       inv.Model,
		Provider:    provider,
		BaseURL:     endpoint,
		Prompt:      inv.Prompt,
		Temperature: inv.Temperature,
		MaxTokens:   inv.MaxTokens,
		StartedAt:   start.UTC(),
	}
}

// scrubUserinfo strips any user:password embedded in a configured base URL.
// llmclient does the same before building a request so credentials cannot
// surface in transport errors; an observation that reported the raw value would
// undo that, and would do it into a durable audit ledger. A URL that will not
// parse is returned unchanged: losing provider identity is worse than an odd
// string, and an unparseable value has no recoverable userinfo to strip.
func scrubUserinfo(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil {
		return rawURL
	}
	u.User = nil
	return u.String()
}

// emit reports one observation, containing any consumer panic. A broken hook
// must never change the outcome of the run it is observing.
func (o *observingClient) emit(mi Invocation) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		// Nil-guarded and serialized deliberately. This runs inside the
		// deferred recover, so a nil-writer panic here would replace the panic
		// just contained and kill the process — the exact outcome the recovery
		// exists to prevent. The engine also calls this from N agent
		// goroutines, so a caller-supplied non-file writer would otherwise be
		// written unsynchronized.
		if o.stderr == nil {
			return
		}
		o.mu.Lock()
		defer o.mu.Unlock()
		_, _ = fmt.Fprintf(o.stderr, "atcr: audit hook panicked, continuing without it: %v\n", r)
	}()
	o.observer.OnModelInvocation(mi)
}

// finish stamps duration and error onto the observation and emits it.
func (o *observingClient) finish(mi Invocation, start time.Time, err error) {
	mi.Duration = time.Since(start)
	mi.Seq = seq.Add(1)
	// Computed here rather than left to the consumer: the rate table lives in
	// internal/llmclient, which a separate module cannot import, so a downstream
	// ledger would otherwise have to fork and maintain a duplicate of it.
	mi.CostUSD = llmclient.ComputeCostUSD(mi.Model, mi.PromptTokens, mi.CompletionTokens)
	if err != nil {
		mi.Err = err.Error()
	}
	o.emit(mi)
}

// Complete observes the plain single-shot path.
func (o *observingClient) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	start := time.Now()
	mi := o.base(ctx, inv, start)

	content, err := o.inner.Complete(ctx, inv)

	mi.Response = content
	o.finish(mi, start, err)
	return content, err
}

// CompleteWithUsage observes the usage-reporting single-shot path.
func (o *observingClient) CompleteWithUsage(ctx context.Context, inv llmclient.Invocation) (
	string, llmclient.UsageData, []llmclient.CallRecord, error) {
	start := time.Now()
	mi := o.base(ctx, inv, start)

	content, usage, records, err := o.inner.CompleteWithUsage(ctx, inv)

	mi.Response = content
	mi.PromptTokens = usage.PromptTokens
	mi.CompletionTokens = usage.CompletionTokens
	mi.Attempts = len(records)
	o.finish(mi, start, err)
	return content, usage, records, err
}

// CompleteWithMeta observes the truncation-aware single-shot path, the one the
// engine prefers.
func (o *observingClient) CompleteWithMeta(ctx context.Context, inv llmclient.Invocation) (
	llmclient.Completion, error) {
	start := time.Now()
	mi := o.base(ctx, inv, start)

	comp, err := o.inner.CompleteWithMeta(ctx, inv)

	mi.Response = comp.Content
	mi.PromptTokens = comp.Usage.PromptTokens
	mi.CompletionTokens = comp.Usage.CompletionTokens
	mi.Attempts = len(comp.CallRecords)
	mi.Truncated = comp.Truncated
	if comp.Truncated {
		mi.FinishReason = "length"
	}
	o.finish(mi, start, err)
	return comp, err
}

// Chat observes the multi-turn tool-loop path. The loop calls Chat once per
// turn and each turn is a distinct model invocation, so each is observed.
func (o *observingClient) Chat(ctx context.Context, inv llmclient.Invocation,
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
		mi.ResponseToolCalls = flattenToolCalls(resp.Message.ToolCalls)
		mi.FinishReason = resp.FinishReason
		mi.Truncated = resp.Truncated
		mi.PromptTokens = resp.Usage.PromptTokens
		mi.CompletionTokens = resp.Usage.CompletionTokens
	}
	o.finish(mi, start, err)
	return resp, err
}

// flattenMessages converts the wire message history into the pointer-free
// shape, normalising content:null to an empty string.
func flattenMessages(messages []llmclient.Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]Message, len(messages))
	for i, m := range messages {
		out[i] = Message{
			Role:       m.Role,
			ToolCalls:  flattenToolCalls(m.ToolCalls),
			ToolCallID: m.ToolCallID,
		}
		if m.Content != nil {
			out[i].Content = *m.Content
		}
	}
	return out
}

// flattenToolCalls converts wire tool calls into the export shape. Arguments
// are normalised through llmclient.ToolCallArguments because providers disagree
// on the encoding — OpenAI-compatible ones send a JSON-encoded string, some
// local ones a bare object. Passing the raw form through would make the ledger
// column provider-dependent, so a consumer always receives the decoded JSON
// object, which is also what the tool harness itself acts on.
func flattenToolCalls(calls []llmclient.ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, len(calls))
	for i, c := range calls {
		out[i] = ToolCall{ID: c.ID, Name: c.Function.Name, Arguments: string(llmclient.ToolCallArguments(c))}
	}
	return out
}
