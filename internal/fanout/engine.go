package fanout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/tools"
)

// Completer abstracts the LLM chat call so the engine can be driven by a fake in
// tests (deterministic concurrency/fallback assertions) while production uses
// *llmclient.Client. The engine consumes the interface; the client returns a
// concrete type.
type Completer interface {
	Complete(ctx context.Context, inv llmclient.Invocation) (string, error)
}

// UsageCompleter is the optional usage-reporting extension of Completer: it
// returns the same content plus the provider's token usage for the call. The
// single-shot path type-asserts for it (the same pattern as ChatCompleter) so a
// completer that does not implement it degrades to the plain Complete path with
// zero usage. *llmclient.Client satisfies it via CompleteWithUsage.
type UsageCompleter interface {
	CompleteWithUsage(ctx context.Context, inv llmclient.Invocation) (string, llmclient.UsageData, error)
}

// ChatCompleter is the multi-turn extension of Completer: it carries a message
// history plus tool definitions and returns the assistant turn (which may
// request tool_calls). The engine selects it via a type assertion only when an
// agent has Tools enabled; a completer that does not implement it (or a missing
// dispatcher) degrades the agent to the single-shot Complete path. *llmclient.Client
// implements both Complete and Chat, so production agents loop while the doctor
// package's separate Completer is unaffected.
type ChatCompleter interface {
	Completer
	Chat(ctx context.Context, inv llmclient.Invocation, messages []llmclient.Message, toolDefs []llmclient.ToolDef) (*llmclient.ChatResponse, error)
}

// toolDispatcher executes a single tool call against the snapshot sandbox,
// returning bounded content (never panicking — a tool failure is a *tools.ToolError
// the loop relays to the model as a role:"tool" result). *tools.Dispatcher
// satisfies it; tests inject a fake. The interface is the only coupling between
// the agent loop and the tool harness.
type toolDispatcher interface {
	Execute(ctx context.Context, name string, args json.RawMessage) (tools.ToolResult, error)
}

// Agent is a fully-resolved reviewer ready to invoke: the LLM call parameters,
// the rendered persona prompt, the payload mode it saw, and any byte-budget
// truncation recorded for its status.json.
type Agent struct {
	Name        string
	Invocation  llmclient.Invocation
	Prompt      string
	PayloadMode string
	Truncation  payload.Truncation
	// TimeoutSecs bounds this single agent's call within the global deadline; 0
	// means "use the global context deadline only".
	TimeoutSecs int

	// Tool-loop configuration (Epic 2.0). Tools enables the multi-turn agent
	// loop; when false the agent runs single-shot exactly as in 1.x. MaxTurns
	// caps Chat-with-tools turns (default 10 applied at registry load when
	// Tools is true); ToolBudgetBytes caps cumulative tool-result bytes (0 =
	// unlimited). These are threaded from the resolved AgentConfig by buildAgent.
	Tools           bool
	MaxTurns        int
	ToolBudgetBytes int64
	// SupportsFC is this agent's model's function-calling capability, threaded
	// from the registry (per-agent, not per-lane). A tools:true agent whose model
	// lacks it degrades to single-shot regardless of the harness being wired.
	SupportsFC bool

	// Review-constraint guardrails (Epic 2.2), threaded from the resolved
	// AgentConfig by buildAgent and carried onto the Result so findingsFor can
	// enforce them. Scope is applied earlier (as soft prompt injection) and is
	// not carried here. A fallback inherits these from its primary (the
	// constraint follows the slot, like the persona prompt).
	MinSeverity string
	MaxFindings *int
}

// Slot is one reviewer position in the roster: a primary agent plus its
// resolved fallback chain (each a full Agent). Serial marks slots that must run
// sequentially in the serial lane (rate-limited providers). The engine tries
// Primary, then each Fallback in order, until one succeeds.
type Slot struct {
	Primary   Agent
	Fallbacks []Agent
	Serial    bool
}

// Result is the outcome of one slot after fallback resolution. Status is one of
// StatusOK/StatusFailed/StatusTimeout. FallbackUsed/FallbackFrom record which
// agent actually answered when a fallback took over. Agent names the agent that
// produced the result (the primary's name — attribution follows the slot, not
// the substitute), while FallbackFrom records the primary when a fallback ran.
type Result struct {
	Agent        string
	Content      string
	Status       string
	Err          error
	DurationMS   int64
	FallbackUsed bool
	FallbackFrom string
	PayloadMode  string
	Truncation   payload.Truncation

	// Review-constraint guardrails (Epic 2.2), threaded from the resolved
	// AgentConfig by buildAgent so findingsFor can enforce them per source.
	// MinSeverity drops findings below the floor; MaxFindings caps the count
	// (severity-sorted). Both empty/nil mean "no constraint".
	MinSeverity string
	MaxFindings *int

	// Tool-loop accounting (Epic 2.0). Tools records that this was a tool-enabled
	// agent (so status.json emits explicit zero counters even on the degrade
	// path, while pure single-shot agents keep them absent). Turns/ToolCalls/
	// ToolBytes are the actual loop usage; ToolsDegraded marks a tool agent that
	// fell back to single-shot; TrippedBudgets names every budget that halted the
	// loop ("max_turns", "tool_budget_bytes", "timeout_secs"). ToolsRequested
	// preserves the original tools:true intent even when the agent degraded, so
	// status.json can show what was asked for versus what ran.
	Tools          bool
	Turns          int
	ToolCalls      int
	ToolBytes      int64
	ToolsDegraded  bool
	ToolsRequested bool
	TrippedBudgets []string

	// Per-agent usage accounting (Epic 3.3 scorecard). Model is the configured
	// model id; TokensIn/TokensOut are the provider-reported token counts,
	// accumulated across every Chat() turn on the tool-loop path and taken from
	// the single CompleteWithUsage() call on the single-shot path. They are
	// persisted into status.json (AgentStatus) so the reconcile-time scorecard
	// emitter — which runs in a separate process from the review — can source
	// per-reviewer model/tokens and derive cost. Zero when the completer does not
	// report usage (graceful degradation, never an error).
	Model     string
	TokensIn  int
	TokensOut int
}

// Engine fans a review out to a roster across a parallel lane (default) and a
// serial lane (rate-limited providers), both running concurrently.
type Engine struct {
	completer Completer
	// maxParallel bounds concurrent parallel-lane agent calls; 0 (the default)
	// means unbounded, preserving the original goroutine-per-slot behavior.
	maxParallel int
	// dispatcher executes tool calls for tool-enabled agents. nil (the default)
	// means no tool harness is wired, so a tool-enabled agent degrades to
	// single-shot. It is shared across all agents in a review (one snapshot).
	dispatcher toolDispatcher
	// transcript builds a per-agent transcript writer for the tool loop. nil (the
	// default) disables transcript recording — the loop runs exactly as before.
	// Each agent gets its own writer (concurrent loops, separate files); the loop
	// closes it when the agent finishes (Epic 2.0, AC 05-01/05-02).
	transcript func(agentName string) *tools.Transcript
	// log is the engine's diagnostic logger. nil (the default) falls back to a
	// no-op discard logger via logger(). ExecuteReview injects the review_id-
	// correlated context logger via WithLogger, so invokeAgent's per-agent
	// WithAgent scoping produces lines carrying both review_id and agent_name.
	log *slog.Logger
}

// EngineOption configures an Engine at construction.
type EngineOption func(*Engine)

// WithMaxParallel bounds concurrent parallel-lane agent calls to n. n <= 0
// leaves the lane unbounded (the documented escape hatch). The serial lane is
// unaffected — it is already sequential.
func WithMaxParallel(n int) EngineOption {
	return func(e *Engine) { e.maxParallel = n }
}

// WithDispatcher wires the tool harness into the engine so tool-enabled agents
// run the multi-turn loop. Without it (or with a completer that does not
// implement ChatCompleter), a tool-enabled agent degrades to single-shot and
// records tools_degraded. The dispatcher is bound to one snapshot and is shared,
// read-only, across the run's agents.
func WithDispatcher(d toolDispatcher) EngineOption {
	return func(e *Engine) { e.dispatcher = d }
}

// WithTranscript wires a per-agent transcript factory: for each tool-enabled
// agent the loop calls f(agentName) to obtain a writer, records every turn's
// tool_calls/tool_results and the final message, and closes it when the agent
// finishes. The factory is called concurrently — once per agent, possibly
// simultaneously — and must be goroutine-safe: it must not mutate unsynchronized
// shared state (maps, counters, pools) without its own synchronization. Without
// it (the default), no transcript is recorded. Recording is best-effort: a
// writer that fails to open or write logs and continues, never failing the
// review.
func WithTranscript(f func(agentName string) *tools.Transcript) EngineOption {
	return func(e *Engine) { e.transcript = f }
}

// WithLogger injects the engine's diagnostic logger. Pass the review_id-
// correlated logger from the request context (log.FromContext) so every agent
// log line is greppable by review. Without it, the engine logs to a no-op
// discard sink (logger()), preserving the original silent behavior in tests
// that construct an Engine without a logger.
func WithLogger(l *slog.Logger) EngineOption {
	return func(e *Engine) { e.log = l }
}

// logger returns the engine's logger, or a no-op discard logger when none was
// injected (mirrors internal/mcp/handlers.go's nil-safe guard) so direct
// construction and WithLogger-less tests never nil-panic or reach the global
// slog default.
func (e *Engine) logger() *slog.Logger {
	if e.log == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return e.log
}

// NewEngine builds an Engine over the given completer. A nil completer is a
// programming error and panics at construction rather than nil-panicking deep
// inside the first agent invocation. Options tune optional behavior (e.g.
// WithMaxParallel); the zero-option call preserves the original unbounded lane.
func NewEngine(c Completer, opts ...EngineOption) *Engine {
	if c == nil {
		panic("fanout: NewEngine called with nil Completer")
	}
	e := &Engine{completer: c}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// resultFromPanic builds a failed result for a slot whose goroutine recovered
// from a panic. It preserves the slot's identity/provenance and stamps the
// wall-clock elapsed since Run started.
func resultFromPanic(s Slot, start time.Time, r any) Result {
	return Result{
		Agent:       s.Primary.Name,
		Status:      StatusFailed,
		Err:         fmt.Errorf("panic: %v", r),
		DurationMS:  time.Since(start).Milliseconds(),
		PayloadMode: s.Primary.PayloadMode,
		Truncation:  s.Primary.Truncation,
	}
}

// Run executes every slot and returns one Result per slot in input order.
// Parallel-lane slots run concurrently via a WaitGroup; serial-lane slots run
// sequentially in a single goroutine (ctx checked before each invocation),
// concurrent with the parallel lane. The WaitGroup always drains — even when
// ctx is cancelled mid-flight — so no goroutine is leaked. A cancelled context
// surfaces as StatusTimeout for the affected slots; other slots still complete.
func (e *Engine) Run(ctx context.Context, slots []Slot) []Result {
	start := time.Now()
	results := make([]Result, len(slots))
	var wg sync.WaitGroup

	// A buffered semaphore bounds how many parallel-lane agent slots run
	// concurrently. maxParallel <= 0 leaves sem nil (unbounded). Zero-size
	// elements keep the buffer cheap regardless of cap. Each goroutine still
	// spawns; the token is acquired before the slot starts and held for the full
	// slot, including any multi-turn tool loop, so the cap bounds concurrent
	// tool-agent loops rather than individual provider calls.
	var sem chan struct{}
	if e.maxParallel > 0 {
		sem = make(chan struct{}, e.maxParallel)
	}

	// Serial slots share one goroutine so they never overlap; parallel slots
	// each get their own. Both lanes start together and the WaitGroup joins them.
	var serialIdx []int
	for i, s := range slots {
		if s.Serial {
			serialIdx = append(serialIdx, i)
			continue
		}
		wg.Add(1)
		go func(i int, s Slot) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					results[i] = resultFromPanic(s, start, r)
				}
			}()
			// Acquire a slot before invoking. The acquire is ctx-aware so a
			// cancelled run never blocks the WaitGroup drain waiting for a slot:
			// on cancellation, invokeSlot short-circuits to a timeout result
			// without a provider call. A won acquire is always released.
			if sem != nil {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					// Context cancelled before a semaphore slot was available.
					// No token was acquired, so none is released; cap enforcement
					// is correct because invokeSlot will short-circuit to a
					// timeout result without making a provider call.
					results[i] = e.invokeSlot(ctx, s)
					return
				}
			}
			results[i] = e.invokeSlot(ctx, s)
		}(i, slots[i])
	}

	if len(serialIdx) > 0 {
		wg.Add(1)
		go func(slots []Slot, serialIdx []int) {
			defer wg.Done()
			for _, i := range serialIdx {
				func(i int) {
					defer func() {
						if r := recover(); r != nil {
							results[i] = resultFromPanic(slots[i], start, r)
						}
					}()
					s := slots[i]
					// Honor cancellation before starting each serial invocation so a
					// cancelled run does not keep firing requests down the lane. The
					// short-circuited slot records the wall-clock elapsed since Run
					// started, not 0 — real time passed before the cancellation.
					if err := ctx.Err(); err != nil {
						results[i] = Result{
							Agent:       s.Primary.Name,
							Status:      classifyStatus(err),
							Err:         err,
							DurationMS:  time.Since(start).Milliseconds(),
							PayloadMode: s.Primary.PayloadMode,
							Truncation:  s.Primary.Truncation,
							MinSeverity: s.Primary.MinSeverity,
							MaxFindings: s.Primary.MaxFindings,
						}
						return
					}
					results[i] = e.invokeSlot(ctx, s)
				}(i)
			}
		}(slots, serialIdx)
	}

	wg.Wait()
	return results
}

// invokeSlot tries the primary agent, then each fallback in order, until one
// succeeds. The first success wins; if all fail, the last real failure is
// reported with the slot marked failed (or timeout when ctx expired before any
// attempt ran). Attribution stays with the primary agent name;
// FallbackUsed/FallbackFrom record the substitution. On failure the primary's
// payload provenance is always recorded so status.json reflects the slot, not a
// substitute that may have seen a different payload. DurationMS covers the
// whole chain — a failed primary's wall time counts toward the slot, so a slow
// primary plus fast fallback is not misreported as a fast slot.
func (e *Engine) invokeSlot(ctx context.Context, s Slot) Result {
	start := time.Now()
	chain := append([]Agent{s.Primary}, s.Fallbacks...)
	var last Result
	for i, a := range chain {
		// Stop descending the chain once the context is done — further attempts
		// would only fail the same way and waste the remaining budget. Preserve a
		// prior real failure's diagnostics; only synthesize a timeout/cancel
		// result when no attempt has run yet.
		if err := ctx.Err(); err != nil {
			if last.Err == nil {
				last = Result{Agent: s.Primary.Name, Status: classifyStatus(err), Err: err}
			}
			break
		}
		r := e.invokeAgent(ctx, a)
		if i > 0 {
			r.FallbackUsed = true
			r.FallbackFrom = s.Primary.Name
			r.Agent = s.Primary.Name // attribution follows the slot, not the substitute
		}
		if r.Status == StatusOK {
			r.DurationMS = time.Since(start).Milliseconds()
			return r
		}
		last = r
	}
	// Slot failed: stamp the primary's identity and payload provenance.
	last.Agent = s.Primary.Name
	last.PayloadMode = s.Primary.PayloadMode
	last.Truncation = s.Primary.Truncation
	last.MinSeverity = s.Primary.MinSeverity
	last.MaxFindings = s.Primary.MaxFindings
	last.DurationMS = time.Since(start).Milliseconds()
	return last
}

// invokeAgent performs a single agent's LLM call and classifies the outcome on
// the actual error returned (not ambient ctx state): a context deadline/cancel
// surfaces as StatusTimeout, any other error as StatusFailed. Classifying on the
// error itself avoids mislabeling a genuine failure (auth, malformed response)
// as a timeout just because a sibling slot's deadline happened to fire. The raw
// assistant content is returned on success for the artifact layer to persist.
func (e *Engine) invokeAgent(ctx context.Context, a Agent) Result {
	// A per-agent timeout further bounds this call within the global deadline;
	// when it fires only this agent times out — siblings keep running. The tool
	// loop runs under this same deadline, so timeout_secs covers the whole loop.
	if a.TimeoutSecs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(a.TimeoutSecs)*time.Second)
		defer cancel()
	}
	// Scope a per-agent logger so every line emitted while this agent runs carries
	// agent_name (AC10). The engine logger already carries review_id (seeded by
	// ExecuteReview), so agent lines carry both. Thread it through ctx so downstream
	// calls (tool loop, llmclient) inherit the agent scope via log.FromContext.
	agentLogger := log.WithAgent(e.logger(), a.Name)
	ctx = log.NewContext(ctx, agentLogger)
	agentLogger.Debug("invoking agent", "tools", a.Tools, "model", a.Invocation.Model)
	// Tool-enabled agents run the multi-turn loop only when the model is declared
	// function-calling-capable AND both a ChatCompleter and a dispatcher are
	// wired; otherwise they degrade to single-shot. The capability check is the
	// registry (per-agent), consulted before the loop starts (AC 04-01/04-02) —
	// an incapable model degrades even when the harness is fully wired. Non-tool
	// agents take the unchanged 1.x path.
	if a.Tools {
		if a.SupportsFC {
			cc, ok := e.completer.(ChatCompleter)
			if ok && e.dispatcher != nil {
				return e.invokeToolLoop(ctx, a, cc, e.dispatcher)
			}
		}
		return e.invokeDegraded(ctx, a)
	}
	return e.invokeSingleShot(ctx, a)
}

// invokeSingleShot performs one chat completion and classifies the outcome on
// the actual error returned (a context deadline/cancel → StatusTimeout, any
// other error → StatusFailed). This is the unchanged 1.x path.
func (e *Engine) invokeSingleShot(ctx context.Context, a Agent) Result {
	start := time.Now()
	// Capture usage when the completer reports it (CompleteWithUsage); otherwise
	// take the plain content-only path with zero usage. Same optional-interface
	// pattern as the tool-loop ChatCompleter.
	var (
		content string
		usage   llmclient.UsageData
		err     error
	)
	if uc, ok := e.completer.(UsageCompleter); ok {
		content, usage, err = uc.CompleteWithUsage(ctx, a.Invocation)
	} else {
		content, err = e.completer.Complete(ctx, a.Invocation)
	}
	r := Result{
		Agent:       a.Name,
		DurationMS:  time.Since(start).Milliseconds(),
		PayloadMode: a.PayloadMode,
		Truncation:  a.Truncation,
		MinSeverity: a.MinSeverity,
		MaxFindings: a.MaxFindings,
		// Preserve the original tool request even on the single-shot path so a
		// degraded tool agent (invokeDegraded reuses this) reports tools_requested.
		ToolsRequested: a.Tools,
		Model:          a.Invocation.Model,
		TokensIn:       usage.PromptTokens,
		TokensOut:      usage.CompletionTokens,
	}
	if err != nil {
		r.Err = err
		r.Status = classifyStatus(err)
		return r
	}
	r.Content = content
	r.Status = StatusOK
	return r
}

// invokeDegraded runs a tool-enabled agent through the single-shot path because
// the harness is unavailable (the completer is not a ChatCompleter, or no
// dispatcher was wired). The result is marked Tools+ToolsDegraded so status.json
// records the degrade with explicit zero counters (AC 01-05, AC 02-04 EC3).
func (e *Engine) invokeDegraded(ctx context.Context, a Agent) Result {
	r := e.invokeSingleShot(ctx, a)
	r.Tools = true
	r.ToolsDegraded = true
	return r
}

// addUsage accumulates one turn's provider-reported token usage onto the result.
// The tool-loop path calls it after every Chat() (each turn plus the final-answer
// call) so a multi-turn agent's tokens are the sum across turns, not just the
// last turn's (TD-002). Negative counts are already clamped to zero at the
// llmclient decode boundary, so the sum is always non-negative.
func (r *Result) addUsage(u llmclient.UsageData) {
	r.TokensIn += u.PromptTokens
	r.TokensOut += u.CompletionTokens
}

// addTripped records a tripped budget name on the result, de-duplicating so a
// budget is never listed twice when more than one halt path records it.
func (r *Result) addTripped(name string) {
	for _, b := range r.TrippedBudgets {
		if b == name {
			return
		}
	}
	r.TrippedBudgets = append(r.TrippedBudgets, name)
}

// classifyStatus maps an error to a status code. A context deadline or
// cancellation (anywhere in the wrap chain) is a timeout; everything else is a
// genuine failure.
func classifyStatus(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return StatusTimeout
	}
	return StatusFailed
}
