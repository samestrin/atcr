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
// only on a halted run; a clean verdict returns nil.
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
	engine := fanout.NewEngine(cc, fanout.WithDispatcher(disp), fanout.WithLogger(logger))
	results := engine.Run(ctx, []fanout.Slot{{Primary: agent}})
	// Engine.Run returns one Result per slot in input order, so one slot yields
	// exactly one result. Guard the index anyway: a zero-length return must not
	// panic (a panic would violate the never-propagate-runtime-error contract).
	if len(results) == 0 {
		return &reclib.Verification{Verdict: verdictUnverifiable, Notes: "engine_returned_no_result", Skeptic: skeptic.Name}, nil, nil
	}
	res := results[0]

	// A non-OK status (provider error, timeout) or ANY tripped budget means the
	// skeptic could not complete a trustworthy investigation — even though the tool
	// loop returns StatusOK after a budget trip (partial-success final answer), a
	// trip must not be read as a real verdict. Both collapse to unverifiable. The
	// tripped-budget slice is returned so the caller records it structurally.
	if res.Status != fanout.StatusOK || len(res.TrippedBudgets) > 0 {
		notes := failureNotes(res)
		logSkepticFailure(logger, skeptic.Name, failureClass(res), notes)
		return &reclib.Verification{Verdict: verdictUnverifiable, Notes: notes, Skeptic: skeptic.Name}, res.TrippedBudgets, nil
	}

	v, _ := parseVerdict(res.Content)
	v.Skeptic = skeptic.Name
	if v.Verdict == verdictUnverifiable {
		logSkepticFailure(logger, skeptic.Name, "malformed_output", v.Notes)
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
func buildSkepticAgent(skeptic Skeptic, prompt string, exec bool) fanout.Agent {
	c := skeptic.Config
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
		ToolBudgetBytes: derefInt64(c.ToolBudgetBytes),
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
