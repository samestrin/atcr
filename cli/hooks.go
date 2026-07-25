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
// SENSITIVE DATA. Four fields carry the model exchange verbatim, and the
// exposure is wider than "the diff under review":
//
//   - Prompt and Response hold the single-shot exchange: diff hunks, file
//     paths, and source lines, plus anything a reviewer persona template
//     interpolated into the prompt.
//   - Messages holds the multi-turn history for tool-enabled agents, which
//     includes role:"tool" results. Those are the raw output of the agent's
//     tools — whole file contents from anywhere in the repository (not only the
//     reviewed diff), directory listings, grep hits, and for script execution
//     the command's stdout, which can contain environment-derived secrets.
//   - ResponseToolCalls holds the arguments the model asked those tools to run
//     with, revealing the paths and patterns it probed.
//
// Bounding your redaction to "the diff" will therefore under-redact. This type
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
	// Seq is a process-wide monotonic sequence number assigned when the call
	// completes. Reviewer agents run concurrently and `atcr serve` runs
	// concurrent detached reviews, so timestamps alone do not give a total
	// order over the stream.
	//
	// Delivery order may differ from Seq order: two concurrent calls can take
	// sequence numbers and then reach the observer in the opposite order. Sort
	// by Seq; do not treat the order records arrive in (or are appended to a
	// ledger in) as the sequence.
	Seq int64
	// RunID identifies the review these invocations belong to (e.g.
	// "2026-07-25_feat-x"). Empty for a call made outside a review.
	//
	// It is the review id when the invoking layer knows it, and the review
	// directory's basename otherwise. Those are the same value on the default
	// layout, but they diverge under --output-dir, where the directory is the
	// operator's path and the id stays the derived ReviewID. Within one process
	// the outermost layer wins, so a review that chains into --verify/--debate/
	// --auto-fix keeps one id across every stage. Across processes it cannot:
	// `atcr review --output-dir out/foo` followed by a separate `atcr verify
	// out/foo` reports the id for the first and "foo" for the second, because
	// the second process has no way to recover the id. Group on RunID, but do
	// not assume two stages of the same review always share one when
	// --output-dir is in play.
	RunID string
	// AgentName is the reviewer persona that made the call, empty when the call
	// did not originate from the fan-out engine.
	AgentName string
	// Stage is which part of the pipeline made the call: "review", "resume",
	// "verify", "autofix", "debate", or "benchmark". Empty when not
	// attributable. A review that chains into --verify/--debate/--auto-fix
	// reports the inner stage for those calls while keeping the review's RunID,
	// so one run's records stay correlated across stages.
	Stage string
	// Model is the provider-qualified model identifier as configured, e.g.
	// "anthropic/claude-sonnet-4.5".
	Model string
	// Provider is the logical provider name the fan-out engine attached to the
	// call (e.g. "openrouter"). It falls back to BaseURL when the call runs
	// outside the engine and no provider was attached.
	Provider string
	// BaseURL is the configured provider base URL. The request itself goes to
	// this value plus the chat-completions path, which the client appends. Any
	// userinfo credentials embedded in the configured URL are stripped before
	// the value reaches this field, matching what the client does before
	// building the request — so a config of https://user:pass@host/v1 is
	// reported as https://host/v1.
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
	// ResponseToolCalls is the tool calls the assistant requested on this turn.
	// A tool-enabled agent's response is frequently a tool call with no text at
	// all, so Response alone would misreport the exchange.
	ResponseToolCalls []ModelToolCall
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
	// CostUSD is the estimated cost derived from the token counts above, using
	// the same rate table atcr uses internally. It is computed here because that
	// table is not importable from another module. Zero when the model is not in
	// the table or the provider reported no usage.
	CostUSD float64
	// Temperature and MaxTokens are the sampling parameters the call was made
	// with; nil means the provider's own default applied.
	Temperature *float64
	MaxTokens   *int
	// Attempts is the number of HTTP round-trips the call took, retries
	// included. Duration spans all of them.
	Attempts int
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
	// ToolCalls is present on an assistant turn that requested tools;
	// ToolCallID ties a role:"tool" result back to the call that produced it.
	ToolCalls  []ModelToolCall
	ToolCallID string
}

// ModelToolCall is one model-requested tool invocation. Arguments is the tool's
// argument object as JSON, normalised to the decoded form so it does not vary
// with the provider's wire encoding (some send a JSON-encoded string, others a
// bare object).
type ModelToolCall struct {
	ID        string
	Name      string
	Arguments string
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
// be added as new fields without breaking existing consumers. The unexported
// field enforces that: it makes a positional literal (cli.Hooks{obs}) a compile
// error, so every consumer must use keyed fields and a later field addition
// cannot break them.
type Hooks struct {
	_ struct{}

	// ModelInvocation observes model calls. Coverage is every client built
	// through internal/hookobs.Wrap: the review and resume fan-out, benchmark,
	// the MCP server, verify's skeptics, verify's fix executor (--auto-fix),
	// and debate's seats. That spans `atcr review` including its --verify,
	// --debate, and --auto-fix stages, plus the standalone `atcr verify` and
	// `atcr debate` commands and their MCP tool equivalents.
	//
	// Two documented gaps:
	//
	// `atcr doctor` is not covered — its provider self-test builds its own
	// client through a separate interface in internal/doctor and never reaches
	// this seam, so a doctor run produces no observations.
	//
	// A diff-cache hit produces no observation either, because no model call is
	// made: the engine replays a stored reviewer output for an unchanged
	// payload+model+persona. That content still reaches the deliverable, so a
	// ledger built from these observations will have no entry for a replayed
	// agent. Run with --no-cache when a complete per-run record matters.
	ModelInvocation ModelInvocationObserver
}

// The context carrier itself lives in internal/hookobs, not here, because
// internal/verify, internal/debate, internal/fanout and internal/mcp also need
// it and cannot import cli (cli imports them). See that package's doc.

// observerAdapter converts the low-level observation into the exported shape
// and forwards it to the consumer's observer. It is the only place the two
// payload types meet, so the exported surface can evolve independently of the
// internal one.
type observerAdapter struct {
	observer ModelInvocationObserver
}

func (a observerAdapter) OnModelInvocation(in hookobs.Invocation) {
	mi := ModelInvocation{
		Seq:              in.Seq,
		RunID:            in.RunID,
		AgentName:        in.AgentName,
		Stage:            in.Stage,
		Model:            in.Model,
		Provider:         in.Provider,
		BaseURL:          in.BaseURL,
		Prompt:           in.Prompt,
		Response:         in.Response,
		FinishReason:     in.FinishReason,
		Truncated:        in.Truncated,
		PromptTokens:     in.PromptTokens,
		CompletionTokens: in.CompletionTokens,
		CostUSD:          in.CostUSD,
		Temperature:      in.Temperature,
		MaxTokens:        in.MaxTokens,
		Attempts:         in.Attempts,
		StartedAt:        in.StartedAt,
		Duration:         in.Duration,
		Err:              in.Err,
	}
	mi.ResponseToolCalls = adaptToolCalls(in.ResponseToolCalls)
	if len(in.Messages) > 0 {
		mi.Messages = make([]ModelMessage, len(in.Messages))
		for i, m := range in.Messages {
			mi.Messages[i] = ModelMessage{
				Role:       m.Role,
				Content:    m.Content,
				ToolCalls:  adaptToolCalls(m.ToolCalls),
				ToolCallID: m.ToolCallID,
			}
		}
	}
	a.observer.OnModelInvocation(mi)
}

// adaptToolCalls converts the internal tool-call shape into the exported one.
func adaptToolCalls(calls []hookobs.ToolCall) []ModelToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ModelToolCall, len(calls))
	for i, c := range calls {
		out[i] = ModelToolCall{ID: c.ID, Name: c.Name, Arguments: c.Arguments}
	}
	return out
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
