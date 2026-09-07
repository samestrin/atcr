package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	reclib "github.com/samestrin/atcr/reconcile"
	"log/slog"
	"strings"

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
	agent := buildSkepticAgent(skeptic, prompt, exec)
	// Whether the tool ceiling the loop enforces came from the operator or was
	// derived from the declared window decides what a trip on it MEANS below.
	_, derivedBudget := skepticToolBudget(skeptic.Config)
	engine := fanout.NewEngine(cc, fanout.WithDispatcher(disp), fanout.WithLogger(logger))
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
		// skeptic worked from a shortened view, and log it so an operator whose
		// roster is systematically hitting the derived ceiling can see it.
		logSkepticFailure(logger, skeptic.Name, "budget_truncated", failureNotes(res))
		return v, res.TrippedBudgets, nil
	}
	return v, nil, nil
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
func buildSkepticAgent(skeptic Skeptic, prompt string, exec bool) fanout.Agent {
	c := skeptic.Config
	budget, _ := skepticToolBudget(c)
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
	}
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
// claim on the window, never a veto over it: a window too small to fund the full
// cap must still be bounded, so the claim shrinks to what the window can fund
// instead of switching to a second formula. This lane previously did switch —
// full reservation above the reservation's own threshold, NOTHING reserved below
// it — which made the ceiling non-monotonic in both operands (window 12288
// derived 28672 bytes, 12289 derived 3) and left the lower band with a ceiling
// equal to 100% of its input room, i.e. no room for the reply the reservation
// exists to protect. Halving rather than claiming all but one token is
// deliberate: an all-but-one-token clamp is monotonic too, but derives a 1-token
// (3-byte) ceiling across the whole band, and a trip on a DERIVED ceiling does
// not void the verdict — so the skeptic would answer from a 3-byte view without
// signalling it. The cap binds only while half the input room is smaller than the
// reservation, i.e. only below 2*reserved + prompt overhead (20480 tokens at the
// built-in default); every larger window derives exactly what it always did.
//
// Only a DECLARED window clamps, and only downward:
//
//   - No declaration → today's value, unchanged. Deriving from the table's
//     conservative default would silently shrink every unsized roster, which is a
//     separate decision on separate evidence.
//   - A declared budget SMALLER than the ceiling wins. This is a ceiling, never a
//     floor: an operator asking for less still gets less.
//   - A ZERO budget (the engine's "unlimited") is clamped like any other, because
//     unlimited is exactly the state a declared window contradicts.
//
// A declared window never ends in an unbounded loop. The engine reads 0 as
// UNLIMITED (internal/fanout/loop.go guards on `> 0`), so where a window has no
// input room at all — at or below the prompt overhead, where no reservation makes
// it fit — there is no ceiling to derive and the declared value stands only when
// the operator actually declared one. A zero (or negative, which no load-time
// validation guarantees here) declaration gets minSkepticToolBudget instead:
// bounding the loop at one byte is the honest reading of a window that cannot
// hold a tool result, and it trips visibly (logged as budget_truncated) rather
// than silently widening into the unlimited state the declaration contradicts.
//
// The second return says which of the two the caller got: true only when the
// returned number is the window-derived ceiling rather than the operator's own
// declaration. invokeSkeptic needs the distinction because the two carry
// different authority — see tripsVoidTheVerdict. Note what that means for a
// declaration at or ABOVE the derived ceiling: it is not the number enforced (the
// smaller derived one is), so a trip is a DERIVED trip and truncates the read
// without voiding the verdict. Voiding it would blame the operator for a bound
// this lane chose. An operator who wants a trip to mean "untrustworthy" must
// declare a ceiling BELOW the derived one, which is also the only declaration the
// engine will actually enforce.
func skepticToolBudget(c registry.AgentConfig) (budget int64, derived bool) {
	declared := derefInt64(c.ToolBudgetBytes)
	if declared < 0 {
		// Load-time validation rejects a negative budget, but a programmatically
		// built AgentConfig never passes through it — and a negative survives the
		// `declared > 0` test below to reach the engine's `> 0` guard as UNLIMITED,
		// the exact inversion this function exists to prevent.
		declared = 0
	}
	if c.ContextWindowTokens == nil {
		return declared, false
	}
	// Reserve what the window can AFFORD: the resolved output cap, never more than
	// half the input room. See the half-room paragraph above for why half.
	reserved := min(reservedOutputTokens(c), payload.InputRoomTokens(c.Model, c.ContextWindowTokens)/2)
	ceiling := payload.EffectiveByteBudget(c.Model, c.ContextWindowTokens, reserved)
	if ceiling <= 0 {
		// No input room at all — the prompt overhead alone exhausts the window, so
		// the reservation is not what cost the ceiling (it is already 0 here).
		if declared > 0 {
			// A real operator bound. It is not the derived ceiling, but it does bound
			// the loop, which is the property that must not be lost.
			return declared, false
		}
		return minSkepticToolBudget, true
	}
	if declared > 0 && declared < ceiling {
		return declared, false
	}
	return ceiling, true
}

// minSkepticToolBudget is the floor a DECLARED window falls back to when it has
// no input room to derive a ceiling from and the operator declared no budget of
// their own. It exists only to stay off the engine's UNLIMITED sentinel: one byte
// trips on the first tool result, which is the correct outcome for a window that
// cannot hold one, and the trip is reported (budget_truncated) so an operator
// sees the cause rather than a silently unbounded read.
const minSkepticToolBudget int64 = 1

// reservedOutputTokens resolves the output-token cap this lane must SUBTRACT from
// the window when deriving the tool ceiling: the agent's own max_tokens
// declaration, else payload.DefaultOutputTokens.
//
// The floor is the whole point. The review lane resolves the same chain through
// fanout.resolveMaxTokens, which also floors at that constant, so the two lanes
// now reserve the same number for the same agent — which is what
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
