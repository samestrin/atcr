package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/reconcile"
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
// finding's prompt and converts the engine result into a reconcile.Verification.
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
// only on a halted run; a clean verdict returns nil.
//
// Read-only contract: callers must not mutate the returned tripped-budget slice.
// It aliases the fanout.Result's backing memory; mutating it corrupts the engine
// result for any subsequent inspection.
func invokeSkeptic(ctx context.Context, skeptic Skeptic, prompt string, cc fanout.ChatCompleter, disp Dispatcher) (*reconcile.Verification, []string, error) {
	if ctx == nil {
		return nil, nil, errors.New("invokeSkeptic: nil context")
	}
	if cc == nil {
		return nil, nil, errors.New("invokeSkeptic: nil ChatCompleter")
	}
	if disp == nil {
		return nil, nil, errors.New("invokeSkeptic: nil dispatcher")
	}

	agent := buildSkepticAgent(skeptic, prompt)
	engine := fanout.NewEngine(cc, fanout.WithDispatcher(disp))
	results := engine.Run(ctx, []fanout.Slot{{Primary: agent}})
	// Engine.Run returns one Result per slot in input order, so one slot yields
	// exactly one result. Guard the index anyway: a zero-length return must not
	// panic (a panic would violate the never-propagate-runtime-error contract).
	if len(results) == 0 {
		return &reconcile.Verification{Verdict: verdictUnverifiable, Notes: "engine_returned_no_result", Skeptic: skeptic.Name}, nil, nil
	}
	res := results[0]

	// A non-OK status (provider error, timeout) or ANY tripped budget means the
	// skeptic could not complete a trustworthy investigation — even though the tool
	// loop returns StatusOK after a budget trip (partial-success final answer), a
	// trip must not be read as a real verdict. Both collapse to unverifiable. The
	// tripped-budget slice is returned so the caller records it structurally.
	if res.Status != fanout.StatusOK || len(res.TrippedBudgets) > 0 {
		notes := failureNotes(res)
		logSkepticFailure(skeptic.Name, failureClass(res), notes)
		return &reconcile.Verification{Verdict: verdictUnverifiable, Notes: notes, Skeptic: skeptic.Name}, res.TrippedBudgets, nil
	}

	v, _ := parseVerdict(res.Content)
	v.Skeptic = skeptic.Name
	if v.Verdict == verdictUnverifiable {
		logSkepticFailure(skeptic.Name, "malformed_output", v.Notes)
	}
	return v, nil, nil
}

// buildSkepticAgent assembles the tool-enabled fanout.Agent for a skeptic. Tools
// is forced true (a skeptic is meant to investigate via the tool loop), but
// SupportsFC is forwarded from the AgentConfig so a model that genuinely lacks
// function calling degrades to single-shot in the engine rather than failing every
// call — mirroring fanout.buildAgent. Per-finding budgets are forwarded from the
// AgentConfig; a nil budget pointer becomes 0, which the engine reads as "use the
// default" (MaxTurns→10), "unlimited" (ToolBudgetBytes→0), or "parent deadline
// only" (TimeoutSecs→0). The provider's BaseURL/APIKeyEnv are threaded onto the
// Invocation so llmclient.Chat can route the call (without them a production
// skeptic would hit an empty endpoint with no key).
func buildSkepticAgent(skeptic Skeptic, prompt string) fanout.Agent {
	c := skeptic.Config
	return fanout.Agent{
		Name:            skeptic.Name,
		Prompt:          prompt,
		TimeoutSecs:     derefInt(c.TimeoutSecs),
		Tools:           true,
		SupportsFC:      c.SupportsFC,
		MaxTurns:        derefInt(c.MaxTurns),
		ToolBudgetBytes: derefInt64(c.ToolBudgetBytes),
		Invocation: llmclient.Invocation{
			BaseURL:     skeptic.Provider.BaseURL,
			APIKeyEnv:   skeptic.Provider.APIKeyEnv,
			Model:       c.Model,
			Temperature: c.Temperature,
			Prompt:      prompt,
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

// logSkepticFailure emits a single structured stderr line so a skeptic failure is
// visible in logs even though it is intentionally not propagated as an error.
func logSkepticFailure(skeptic, class, detail string) {
	detail = strings.ReplaceAll(detail, "\n", " ")
	fmt.Fprintf(os.Stderr, "atcr: verify: skeptic=%s class=%s: %s\n", skeptic, class, detail)
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
