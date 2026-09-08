package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	reclib "github.com/samestrin/atcr/reconcile"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/tools"
)

// Dispatcher executes a single tool call against the snapshot sandbox. It mirrors
// fanout's unexported toolDispatcher so verify can inject a fake in tests and pass
// the production *tools.Dispatcher in orchestration — a value satisfying this
// interface also satisfies fanout.toolDispatcher (identical method set), so it can
// be handed to fanout.WithDispatcher.
type Dispatcher interface {
	Execute(ctx context.Context, name string, args json.RawMessage) (tools.ToolResult, error)
}

// invokeSkeptic drives one skeptic through the Epic 2.0 tool loop against a single
// finding's prompt and converts the engine result into a reclib.Verification.
//
// Failure isolation is the core contract: a runtime failure — provider error,
// timeout, tripped budget, loop-hygiene halt, or malformed output — is NEVER
// returned as an error. It becomes verdict "unverifiable" with a diagnostic Notes
// field and the skeptic's name, so the caller can always record a verdict and the
// finding is never dropped. The error return is reserved strictly for programming
// errors (nil context, nil completer, nil dispatcher).
//
// The tool loop is reused unchanged: invokeSkeptic constructs a single
// tool-enabled fanout.Slot and runs it through a throwaway fanout.Engine wired to
// the supplied completer and dispatcher. Per-finding budgets (MaxTurns,
// ToolBudgetBytes, TimeoutSecs) are forwarded from the skeptic's AgentConfig.
//
// The second return is the tripped-budget slice (e.g. ["max_turns"]): the same
// budgets failureNotes folds into Notes for humans, surfaced structurally so the
// caller can populate VerificationResult.TrippedBudgets (AC1). It is non-empty
// only on a halted run; a clean verdict returns nil. A halted run does NOT always
// mean an unverifiable verdict — see tripsVoidTheVerdict, under which a trip on a
// window-DERIVED tool ceiling is reported here while the skeptic's own verdict
// survives.
//
// Read-only contract: callers must not mutate the returned tripped-budget slice.
// It aliases the fanout.Result's backing memory; mutating it corrupts the engine
// result for any subsequent inspection.
func invokeSkeptic(ctx context.Context, skeptic Skeptic, prompt string, cc fanout.ChatCompleter, disp Dispatcher, exec bool) (*reclib.Verification, []string, error) {
	if ctx == nil {
		return nil, nil, errors.New("invokeSkeptic: nil context")
	}
	if cc == nil {
		return nil, nil, errors.New("invokeSkeptic: nil ChatCompleter")
	}
	if disp == nil {
		return nil, nil, errors.New("invokeSkeptic: nil dispatcher")
	}

	logger := log.FromContext(ctx)
	// buildSkepticAgent evaluates skepticToolBudget ONCE and returns both the
	// agent carrying the enforced ceiling and that ceiling's provenance. The
	// trust classification below is therefore derived from the number actually
	// enforced — not re-derived by a second, independent call and assumed equal
	// to it (a future override, settings tier or clamp inside buildSkepticAgent
	// would otherwise desync the two with no test able to catch it).
	agent, derivedBudget := buildSkepticAgent(skeptic, prompt, exec)
	// The ceiling this lane will enforce, with its provenance, once per
	// invocation. failureNotes alone renders a 400 KB read and a 3-byte derived
	// ceiling as byte-identical class=budget_truncated lines, and the floored
	// case goes down the voiding branch below as a generic budget_tripped
	// indistinguishable from a max_turns trip — so an operator whose roster is
	// systematically starving cannot see the cause. The same package already has
	// the pattern: the executor lane names its ceiling via executor_ceiling_skip.
	capped := false
	if _, src := payload.ResolveContextWindow(skeptic.Config.Model, skeptic.Config.ContextWindowTokens); src == payload.WindowSourceDeclaration {
		capped = payload.InputRoomTokens(skeptic.Config.Model, skeptic.Config.ContextWindowTokens)/2 < reservedOutputTokens(skeptic.Config)
	}
	logger.Debug("skeptic tool ceiling",
		"skeptic", skeptic.Name,
		"budget", agent.ToolBudgetBytes,
		"derived", derivedBudget,
		"capped", capped,
		"floored", agent.ToolBudgetBytes == minSkepticToolBudget,
	)
	if agent.ToolBudgetBytes == minSkepticToolBudget {
		// The enforced ceiling is the floor: the declared window cannot fund one
		// tool result, so no trustworthy investigation is possible and the run
		// cannot be spent usefully. Driving the engine would either hand reconcile
		// a live verdict from a window that cannot hold even the prompt overhead
		// (a completer that never calls a tool) or deliver a full first tool
		// result into that window before the deferred end-of-turn trip fires (a
		// guaranteed provider-side overflow). Short-circuit instead: named,
		// structural, and no provider request is issued at all. A declared 1-byte
		// budget lands here too — the same outcome its own first trip would
		// produce, minus the doomed run.
		logger.Warn("skeptic failed", "skeptic", skeptic.Name, "class", "window_too_small")
		logger.Debug("skeptic failure detail", "skeptic", skeptic.Name, "class", "window_too_small", "detail", "window_below_prompt_overhead")
		return &reclib.Verification{Verdict: verdictUnverifiable, Notes: "window_below_prompt_overhead", Skeptic: skeptic.Name}, nil, nil
	}
	engine := fanout.NewEngine(cc, fanout.WithDispatcher(clampDispatcher(disp, agent.ToolBudgetBytes)), fanout.WithLogger(logger))
	results := engine.Run(ctx, []fanout.Slot{{Primary: agent}})
	// Engine.Run returns one Result per slot in input order, so one slot yields
	// exactly one result. Guard the index anyway: a zero-length return must not
	// panic (a panic would violate the never-propagate-runtime-error contract).
	if len(results) == 0 {
		return &reclib.Verification{Verdict: verdictUnverifiable, Notes: "engine_returned_no_result", Skeptic: skeptic.Name}, nil, nil
	}
	res := results[0]

	// A non-OK status (provider error, timeout) or a VOIDING tripped budget means
	// the skeptic could not complete a trustworthy investigation — even though the
	// tool loop returns StatusOK after a budget trip (partial-success final
	// answer), such a trip must not be read as a real verdict. Both collapse to
	// unverifiable. The tripped-budget slice is returned either way so the caller
	// records it structurally, including on the surviving-verdict path below.
	if res.Status != fanout.StatusOK || tripsVoidTheVerdict(res.TrippedBudgets, derivedBudget) {
		notes := failureNotes(res)
		logSkepticFailure(logger, skeptic.Name, failureClass(res), notes)
		return &reclib.Verification{Verdict: verdictUnverifiable, Notes: notes, Skeptic: skeptic.Name}, res.TrippedBudgets, nil
	}

	v, _ := parseVerdict(res.Content)
	v.Skeptic = skeptic.Name
	if v.Verdict == verdictUnverifiable {
		logSkepticFailure(logger, skeptic.Name, "malformed_output", v.Notes)
	}
	if len(res.TrippedBudgets) > 0 {
		// Reached only via the derived-ceiling exemption: the read was truncated
		// but the answer stands. Report the trip so the audit record says the
		// skeptic worked from a shortened view. This is NOT a failure — the verdict
		// survived — so it gets its own Info record instead of the failure helper
		// (whose Warn("skeptic failed") false-alarms every operator alerting on
		// skeptic failures, and whose detail claims a run halted that returned a
		// verdict), and its own detail text.
		// Mark the VERDICT, not just the log line and the tripped-budget slice.
		// The slice reaches reconciled/verification.json alone; this object is
		// what rides the finding into findings.json and report.md, so without the
		// marker a confirmed formed from a shortened read renders with no caveat
		// and is charged to the reviewer's durable precision score as a full read.
		v.Truncated = true
		logger.Info("skeptic truncated", "skeptic", skeptic.Name, "class", "budget_truncated")
		detail := fmt.Sprintf("skeptic run truncated (status: %s); tripped budgets: %s", res.Status, strings.Join(res.TrippedBudgets, ", "))
		logger.Debug("skeptic truncation detail", "skeptic", skeptic.Name, "class", "budget_truncated", "detail", detail)
		return v, res.TrippedBudgets, nil
	}
	return v, nil, nil
}

// boundedDispatcher wraps a Dispatcher so the bytes it hands the tool loop can
// never carry a skeptic past the ceiling this lane derived for it.
//
// It exists because internal/fanout/loop.go's byte-budget check is a DEFERRED
// end-of-turn trip: a turn's results are appended to the message list in full
// and the ceiling is consulted only once they are already in hand, after which
// requestFinalAnswer re-sends that message list to the provider. A single result
// is capped independently at tools.DefaultMaxResultBytes (64 KiB), which for
// every window below roughly 31000 tokens is larger than the whole derived
// ceiling — so ONE read_file could walk a small window past its own budget
// before anything tripped, and the clamp only ever stopped the SECOND turn.
//
// The wrapper is per-invocation, not per-dispatcher: buildDispatcher builds ONE
// dispatcher for the whole verify run while each skeptic derives its own
// ceiling, so clamping tools.Limits centrally would impose the smallest roster
// window on every agent. Wrapping here keeps each skeptic bounded by its own
// number and leaves the shared dispatcher untouched.
type boundedDispatcher struct {
	inner     Dispatcher
	remaining int64
}

// clampDispatcher returns disp bounded to at most budget+1 bytes of cumulative
// tool content, or disp unchanged when budget is the engine's UNLIMITED
// sentinel (<= 0) and there is nothing to bound against.
//
// The allowance is budget+1, not budget, and the extra byte is load-bearing:
// loop.go trips on `ToolBytes > ToolBudgetBytes`, so a wrapper that delivered at
// most the budget exactly would leave the comparison false forever. The loop
// would never trip, the tripped-budget slice would stay empty, and the skeptic
// would spend every remaining turn receiving empty results instead of being sent
// to its final answer. One byte over is the smallest overrun that still lets the
// existing trip semantics — including the derived-ceiling exemption in
// tripsVoidTheVerdict — fire exactly as they did before.
func clampDispatcher(disp Dispatcher, budget int64) Dispatcher {
	if budget <= 0 {
		return disp
	}
	return &boundedDispatcher{inner: disp, remaining: budget + 1}
}

// Execute forwards to the wrapped dispatcher and truncates the result's Content
// to whatever allowance is left, marking it Truncated and preserving
// OriginalBytes so the transcript records what the tool actually produced.
//
// Not safe for concurrent use, and it does not need to be: each wrapper serves
// exactly one invokeSkeptic call, and dispatchTurn executes a turn's calls
// sequentially.
func (d *boundedDispatcher) Execute(ctx context.Context, name string, args json.RawMessage) (tools.ToolResult, error) {
	out, err := d.inner.Execute(ctx, name, args)
	if err != nil {
		return out, err
	}
	if int64(len(out.Content)) > d.remaining {
		if out.OriginalBytes == 0 {
			out.OriginalBytes = len(out.Content)
		}
		out.Content = safeRuneCut(out.Content, int(d.remaining))
		out.Truncated = true
	}
	d.remaining -= int64(len(out.Content))
	return out, nil
}

// safeRuneCut returns s truncated to at most n bytes without splitting a
// multi-byte UTF-8 rune, so the result is always valid UTF-8. It mirrors the
// unexported helper internal/tools uses for its own caps — the tool content is
// serialised into a JSON request body, and a raw byte slice through the middle
// of a rune produces the replacement character (or a provider-side reject)
// rather than a clean short read.
func safeRuneCut(s string, n int) string {
	if n >= len(s) {
		return s
	}
	if n <= 0 {
		return ""
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// budgetToolBytes is fanout's tripped-budget marker for the tool-output ceiling.
// fanout keeps its own copy unexported, so the string is duplicated here rather
// than imported; TestInvokeSkeptic_DerivedToolBudgetTripDoesNotVoidTheVerdict
// drives a real engine run and asserts on this literal, so the two copies cannot
// drift apart silently.
const budgetToolBytes = "tool_budget_bytes"

// tripsVoidTheVerdict reports whether a halted run's tripped budgets should
// discard the model's answer and substitute "unverifiable".
//
// Every budget voids the verdict EXCEPT a tool-bytes trip against a ceiling this
// lane DERIVED from the agent's declared context window. That exception exists
// because the derived ceiling is not an operator's instruction: in the shipped
// roster every agent declares context_window_tokens and none declares
// tool_budget_bytes, so before the clamp the engine enforced nothing here and a
// skeptic could read as much as it liked. Treating the derived ceiling as a
// declared one would mean a skeptic that reads a few large files and correctly
// REFUTES a false positive gets its answer rewritten to "unverifiable" — and
// reconcile.IsFailing excludes only "refuted" from the CI gate, so the rewrite
// turns a passing run into a failing one on a budget nobody configured.
//
// A DECLARED tool_budget_bytes keeps the old semantics in full: an operator who
// states a ceiling is stating that overrunning it makes the run untrustworthy.
// The derived ceiling only ever means "stop reading", never "you were wrong".
func tripsVoidTheVerdict(tripped []string, derivedBudget bool) bool {
	if !derivedBudget {
		return len(tripped) > 0
	}
	for _, b := range tripped {
		if b != budgetToolBytes {
			return true
		}
	}
	return false
}

// buildSkepticAgent assembles the tool-enabled fanout.Agent for a skeptic. Tools
// is forced true (a skeptic is meant to investigate via the tool loop), but
// SupportsFC is forwarded from the AgentConfig so a model that genuinely lacks
// function calling degrades to single-shot in the engine rather than failing every
// call — mirroring fanout.renderAgent. Per-finding budgets are forwarded from the
// AgentConfig; a nil budget pointer becomes 0, which the engine reads as "use the
// default" (MaxTurns→10), "unlimited" (ToolBudgetBytes→0), or "parent deadline
// only" (TimeoutSecs→0). The provider's BaseURL/APIKeyEnv are threaded onto the
// Invocation so llmclient.Chat can route the call (without them a production
// skeptic would hit an empty endpoint with no key).
//
// It returns (agent, derived) from a SINGLE skepticToolBudget evaluation: the
// bool is the provenance of the very budget installed on the agent, so callers
// never re-derive the ceiling and assume the two agree.
func buildSkepticAgent(skeptic Skeptic, prompt string, exec bool) (agent fanout.Agent, derived bool) {
	c := skeptic.Config
	budget, derived := skepticToolBudget(c)
	return fanout.Agent{
		Name:        skeptic.Name,
		Provider:    c.Provider,
		Prompt:      prompt,
		TimeoutSecs: derefInt(c.TimeoutSecs),
		Tools:       true,
		// Exec (Epic 11.0): in an --exec run, the skeptic is offered the
		// run_tests/run_script tools so it can reproduce a finding by executing
		// code. False keeps the read-only tool set (the default).
		Exec:            exec,
		SupportsFC:      c.SupportsFC,
		MaxTurns:        derefInt(c.MaxTurns),
		ToolBudgetBytes: budget,
		// Retry/backoff (Epic 4.6): forward the skeptic's per-agent budget the same
		// way as the other per-finding budgets. A nil pointer becomes 0; the engine
		// applies the override only when InitialBackoffMs > 0, so an unset budget
		// keeps the shared client's own default (consistent with TimeoutSecs → 0
		// meaning "parent deadline only").
		MaxRetries:       derefInt(c.MaxRetries),
		InitialBackoffMs: derefInt(c.InitialBackoffMs),
		Invocation: llmclient.Invocation{
			BaseURL:     skeptic.Provider.BaseURL,
			APIKeyEnv:   skeptic.Provider.APIKeyEnv,
			Model:       c.Model,
			Temperature: c.Temperature,
			Prompt:      prompt,
			// Output cap (max_tokens): forwarded like every other per-agent
			// budget above. Omitting it left the provider default in force and a
			// declaration silently inert, which matters most for the model class
			// llmclient.Invocation.MaxTokens' own doc warns about — a reasoning
			// model spends the budget on chain-of-thought before emitting visible
			// content, so under a low default the skeptic finishes mid-reasoning
			// and the engine records "unverifiable" while the run reports success.
			//
			// The DECLARATION only. The review fan-out resolves three tiers
			// (--max-tokens flag > declaration > payload.DefaultOutputTokens), but
			// this lane has no flag to read and imposing the built-in default here
			// would newly cap every UNDECLARED skeptic at a value nothing measured
			// — a separate decision on separate evidence. A nil pointer keeps
			// today's behaviour exactly.
			MaxTokens: c.MaxTokens,
		},
	}, derived
}

// failureNotes builds a diagnostic note for a halted skeptic run, naming the
// engine status, every tripped budget (e.g. max_turns, tool_budget_bytes,
// timeout_secs), and the underlying error when present — enough for an operator to
// see why the verdict is unverifiable without opening the transcript.
func failureNotes(res fanout.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "skeptic run halted (status: %s)", res.Status)
	if len(res.TrippedBudgets) > 0 {
		b.WriteString("; tripped budgets: " + strings.Join(res.TrippedBudgets, ", "))
	}
	if res.Err != nil {
		b.WriteString("; error: " + res.Err.Error())
	}
	return b.String()
}

// failureClass classifies a halted run for the structured log line.
func failureClass(res fanout.Result) string {
	switch res.Status {
	case fanout.StatusTimeout:
		return "timeout"
	case fanout.StatusFailed:
		return "provider_error"
	default:
		if len(res.TrippedBudgets) > 0 {
			return "budget_tripped"
		}
		return "unknown"
	}
}

// logSkepticFailure emits a structured log line so a skeptic failure is visible
// even though it is intentionally not propagated as an error. The skeptic name
// and failure class go to Warn (visible at the default level); the diagnostic
// detail — which can carry provider error bodies and path-bearing context — is
// held to Debug so it does not leak at the default level (mirrors the path-at-
// debug discipline used across the engine wiring).
func logSkepticFailure(logger *slog.Logger, skeptic, class, detail string) {
	detail = strings.ReplaceAll(detail, "\n", " ")
	logger.Warn("skeptic failed", "skeptic", skeptic, "class", class)
	logger.Debug("skeptic failure detail", "skeptic", skeptic, "class", class, "detail", detail)
}

// minSkepticToolBudget is the floor a DECLARED window falls back to when it has
// no input room to derive a ceiling from and the operator declared no budget of
// their own. It exists only to stay off the engine's UNLIMITED sentinel: one byte
// trips on the first tool result, which is the correct outcome for a window that
// cannot hold one. skepticToolBudget returns it with derived = false, so the trip
// collapses the run to unverifiable and the operator sees a named failure rather
// than either a silently unbounded read or a verdict formed from one byte.
const minSkepticToolBudget int64 = 1

// minTrustworthyCeilingBytes is the smallest derived ceiling this lane will
// treat as a real investigation: one tool result, sourced from the dispatcher's
// own per-result cap rather than restated, so the two cannot drift.
//
// Below it, a skeptic cannot have read enough for its answer to mean anything —
// the ceiling truncates the FIRST result it is handed, so whatever the model
// concluded, it concluded from a fragment. `derived = true` would let that
// answer through tripsVoidTheVerdict and into reconcile's CI gate as a live
// confirmed/refuted, which is the failure the floor already exists to prevent
// at one byte. The only difference between one byte and sixty-five thousand is
// where the line sits, and the line belongs at one result, not at one byte.
//
// Scope, measured when the threshold was chosen (2026-09-07): every agent in
// the live registry declares 98304 tokens or more and derives 301056 bytes, so
// no shipped agent's classification changes. Windows below 31013 tokens do
// change — they now take the floor and their skeptic short-circuits to
// unverifiable without a provider call — and `atcr doctor` warns for that whole
// band so the operator hears it before a run, not after.
const minTrustworthyCeilingBytes int64 = payload.MinUsableReadBytes

// skepticToolBudget resolves the skeptic's tool-output ceiling, clamping the flat
// per-agent tool_budget_bytes to what the agent's DECLARED context window can
// actually hold.
//
// A skeptic reads real files through the tool loop, so its input grows with tool
// output — but tool_budget_bytes is a flat number with no relation to the model
// behind it, so an agent declared at 32768 tokens and one declared at 512000 got
// the same budget and the small one could be walked past its window by a few large
// reads. context_window_tokens was inert in this lane; this is the one budget here
// it can bound.
//
// What the ceiling bounds is the CUMULATIVE tool content delivered across the
// whole run, and that is true only because invokeSkeptic wraps the dispatcher in
// boundedDispatcher. The engine's own check (internal/fanout/loop.go) is a
// deferred end-of-turn trip: a turn's results land in the message list in full
// and the ceiling is read afterwards, so the engine alone bounds the SECOND turn
// and never the first. A single result is capped separately at
// tools.DefaultMaxResultBytes (64 KiB), which is larger than the whole ceiling
// derived for any window below roughly 31000 tokens — so without the wrapper one
// read_file would deliver ~18700 tokens into a 12288-token window whose ceiling
// is ~4096, and requestFinalAnswer would then re-send that oversized message
// list to the provider. Read the two together: this function decides the number,
// boundedDispatcher is what makes the number real on turn one.
//
// The derivation is payload.EffectiveByteBudget, the same one the review fan-out
// sizes payloads with, so the window resolution chain (declaration → static model
// table → conservative default) and the output reservation have exactly one
// definition. The output cap passed is reservedOutputTokens — the agent's own
// max_tokens declaration, else payload.DefaultOutputTokens — capped at HALF the
// window's input room. Tokens promised to the response are not available to tool
// output, and an agent that declares no cap is still promised the provider's own
// default, so it reserves the same conservative number rather than reserving
// nothing.
//
// The half-room cap is what makes the derivation CONTINUOUS. The reservation is a
// claim on the window, never a veto over it: rather than switching formulas when
// the full cap no longer fits, the claim is always at most half the input room —
// so the read and the reply divide a small window instead of one of them taking
// all of it. This lane previously did switch —
// full reservation above the reservation's own threshold, NOTHING reserved below
// it — which made the ceiling non-monotonic in both operands (window 12288
// derived 28672 bytes, 12289 derived 3) and left the lower band with a ceiling
// equal to 100% of its input room, i.e. no room for the reply the reservation
// exists to protect. Halving rather than claiming all but one token is
// deliberate: an all-but-one-token clamp is monotonic too, but derives a 1-token
// (3-byte) ceiling across the whole band, and a trip on a DERIVED ceiling does
// not void the verdict — so the skeptic would answer from a 3-byte view without
// signalling it. The cap binds while half the input room is smaller than the
// reservation, i.e. below 2*reserved + prompt overhead (20480 tokens at the
// built-in default); every larger window derives exactly what it always did.
//
// Be precise about what the cap does and does not buy, because part of that band
// can fund the full reservation and is capped anyway. At window 16384 the room is
// 12288 tokens: the full 8192 WOULD fit, leaving 4096 for the read, but the cap
// reserves 6144 and leaves 6144. So the reservation is no longer an upper bound
// on the reply — a reply that actually spends its whole max_tokens can still
// overshoot a window in this band. That is the deliberate trade: below
// 2*reserved + overhead the window cannot host both a full-length reply and a
// usable read, and the alternative (reserve the full cap regardless) is what
// derived a 1-token ceiling at window 12289. The cap splits the shortfall
// instead of assigning all of it to the read. Every window at or above the
// boundary reserves the full RESOLVED cap, which bounds overshoot only for
// agents that DECLARE max_tokens: for an undeclared agent the wire carries no
// output cap at all (buildSkepticAgent forwards a nil verbatim and the provider
// applies its own default), so the built-in 8192 is an estimate against an
// unknown cap, not a guarantee. This repo ships no registry, so no claim is
// made here about where any shipped roster sits.
//
// Only a DECLARED window clamps, and only downward:
//
//   - No declaration — or a window value the resolution chain rejects (<= 0 or
//     above the cap, reachable only through a programmatically built config) —
//     → today's value, unchanged. Deriving from the table's conservative default
//     would silently shrink every unsized roster, and would harden one
//     construction path while trusting the same path's bogus window — a separate
//     decision on separate evidence. The one exception: a negative incoming
//     value gets the same floor as a starved window, never the engine's
//     UNLIMITED sentinel.
//   - A declared budget SMALLER than the ceiling wins. This is a ceiling, never a
//     floor: an operator asking for less still gets less.
//   - A ZERO budget (the engine's "unlimited") is clamped like any other, because
//     unlimited is exactly the state a declared window contradicts.
//
// A declared window never ends in an unbounded loop. The engine reads 0 as
// UNLIMITED (internal/fanout/loop.go guards on `> 0`), so where a window has no
// input room at all — at or below the prompt overhead, where no reservation makes
// it fit — there is no ceiling to derive and the declared value stands only when
// the operator actually declared one. A zero (or negative — load-time validation
// rejects a negative, so only a programmatically built AgentConfig can carry one
// here) declaration gets minSkepticToolBudget instead:
// bounding the loop at one byte is the honest reading of a window that cannot
// hold a tool result, and it is not the unlimited state the declaration
// contradicts.
//
// That floor returns derived = FALSE, which is the one place this function
// reports "not derived" for a number the operator did not declare. It is
// deliberate: a 1-byte ceiling trips on the first tool result by construction, so
// treating the trip as a derived one would let a skeptic that read ONE BYTE hand
// reconcile's gate a live confirmed/refuted — the same "answers from a starved
// view without signalling it" failure that ruled out reserving all but one token.
// A window at or below the prompt overhead cannot fund a trustworthy
// investigation, so its trip must say so: unverifiable.
//
// The second return therefore means "a trip on this number must NOT void the
// verdict", which is true only for a real window-derived ceiling. invokeSkeptic
// needs the distinction because the two carry different authority — see
// tripsVoidTheVerdict. Note what that means for a
// declaration at or ABOVE the derived ceiling: it is not the number enforced (the
// smaller derived one is), so a trip is a DERIVED trip and truncates the read
// without voiding the verdict. Voiding it would blame the operator for a bound
// this lane chose. An operator who wants a trip to mean "untrustworthy" must
// declare a ceiling BELOW the derived one, which is also the only declaration the
// engine will actually enforce.
//
// skepticToolBudget returns the tool ceiling and whether a trip on it truncates
// (true) or voids (false) the verdict.
func skepticToolBudget(c registry.AgentConfig) (budget int64, derived bool) {
	incoming := derefInt64(c.ToolBudgetBytes)
	declared := incoming
	if declared < 0 {
		// Normalisation, not a behaviour change: internal/fanout/loop.go guards on
		// `> 0`, so a negative and a 0 are already the SAME unlimited state, and a
		// negative loses every `declared > 0` test below either way. Nothing here
		// propagates a value the engine has no defined reading for: on the
		// no-declared-window path a NEGATIVE incoming value returns the floor
		// (below), and everywhere else the `declared > 0` guards reject it before
		// it can be returned. Load-time validation rejects a negative
		// (internal/registry/config.go), but a programmatically built AgentConfig
		// never passes through it, so without this handling the sentinel the
		// caller receives depends on which construction path built the config.
		declared = 0
	}
	if c.ContextWindowTokens == nil {
		if incoming < 0 {
			// A negative declaration must not reach the engine as UNLIMITED on this
			// path either. Normalising it to 0 forwards exactly the engine's own
			// 0-as-UNLIMITED sentinel — the leak the declared-window path closes
			// with the floor — so the same floor applies here: the loop is bounded,
			// the trip (derived = false) voids the verdict, and a config that never
			// passed load validation never buys an unbounded read.
			return minSkepticToolBudget, false
		}
		return declared, false
	}
	// Gate on the resolution TIER, not on pointer non-nilness: ResolveContextWindow
	// discards any declaration <= 0 or above the cap and falls through to the table
	// or the conservative default, so a non-nil out-of-range value is NOT a
	// declaration. Deriving a ceiling from the table default for one would harden
	// the negative-budget construction path (above) while accepting the same
	// path's bogus window as truth — inconsistent on one threat model. A
	// non-declaration behaves like no declaration: the value forwards untouched.
	if _, src := payload.ResolveContextWindow(c.Model, c.ContextWindowTokens); src != payload.WindowSourceDeclaration {
		if incoming < 0 {
			return minSkepticToolBudget, false
		}
		return declared, false
	}
	// Reserve what the window can AFFORD: the resolved output cap, never more than
	// half the input room. See the half-room paragraph above for why half.
	reserved := min(reservedOutputTokens(c), payload.InputRoomTokens(c.Model, c.ContextWindowTokens)/2)
	ceiling := payload.EffectiveByteBudget(c.Model, c.ContextWindowTokens, reserved)
	if ceiling < minTrustworthyCeilingBytes {
		// The window cannot fund one real tool result. That covers the old
		// no-input-room case (ceiling 0, the prompt overhead alone exhausts the
		// window) and every window up to 31012 tokens, whose ceiling is positive
		// but too small to read anything conclusive from.
		//
		// The declaration does NOT get an escape hatch here, and removing that
		// hatch is half of this branch's point. It used to return the operator's
		// full tool_budget_bytes on the no-room path, on the reasoning that a real
		// operator bound still bounds the loop — but it bounds it at a number the
		// window cannot hold either way, so it bought no protection while making
		// the enforced ceiling swing ~350000x across one token of window (window
		// 4096 with 1<<20 declared returned 1048576 with derived = false; window
		// 4097 returned 3 with derived = true, flipping the trip from
		// verdict-voiding to verdict-preserving at the same time).
		//
		// derived = false: see the floor paragraph above. The floor trips on the
		// first result, and that trip must void the verdict rather than pass a
		// fragment-derived answer to the gate. invokeSkeptic reads the floor and
		// short-circuits before the engine, so no provider request is spent on a
		// run whose answer could not be trusted anyway.
		return minSkepticToolBudget, false
	}
	if declared > 0 && declared < ceiling {
		return declared, false
	}
	return ceiling, true
}

// reservedOutputTokens resolves the output-token cap this lane STARTS from when
// deriving the tool ceiling: the agent's own max_tokens declaration, else
// payload.DefaultOutputTokens. It is the reservation the caller ASKS for, not
// necessarily the one it takes — skepticToolBudget caps it at half the window's
// input room, so below 2*reserved + prompt overhead the number actually
// subtracted is smaller than this one.
//
// The DEFAULT is the whole point — and it is a default, not a floor: a declared
// max_tokens of 100 reserves 100, not the built-in. The review lane resolves the
// same chain through fanout.resolveMaxTokens, which defaults to the same constant
// the same way, so the two lanes ask for the same number for the same agent (this
// lane may then cap it to fit a small window) —
// which is what
// skepticToolBudget's doc has always CLAIMED ("exactly one definition") and did
// not deliver. Reserving derefInt(c.MaxTokens) meant reserving ZERO for the 23 of
// 29 window-declaring roster agents that declare no cap, i.e. exactly the case
// where the reservation matters most: with no declaration, buildSkepticAgent
// forwards a nil MaxTokens and llmclient omits the field, so the PROVIDER applies
// its own non-zero default. Reserving nothing against an unknown-but-positive
// output budget is the one reading of the window that cannot be right.
//
// This is deliberately NOT the same decision as what to SEND. buildSkepticAgent
// still forwards the declaration alone (a nil stays nil), because imposing a
// built-in cap on the wire would newly truncate every undeclared skeptic at a
// value nothing measured. Reserving conservatively costs a slice of tool budget;
// sending a cap changes what the model is allowed to say.
func reservedOutputTokens(c registry.AgentConfig) int {
	if c.MaxTokens != nil && *c.MaxTokens > 0 {
		return *c.MaxTokens
	}
	return payload.DefaultOutputTokens
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
