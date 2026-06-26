package verify

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	reclib "github.com/samestrin/atcr/reconcile"

	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// execGateResolver is a minimal jail (exec tools take no path args).
type execGateResolver struct{}

func (execGateResolver) Resolve(string) (string, error) { return "/snap", nil }
func (execGateResolver) Root() string                   { return "/snap" }

// execGateBackend returns a deterministic non-zero result and counts runs, so a
// test can prove the determinism re-runs actually reached the sandbox.
type execGateBackend struct{ runs int }

func (b *execGateBackend) Name() string                    { return "exec-gate" }
func (b *execGateBackend) Preflight(context.Context) error { return nil }
func (b *execGateBackend) Run(context.Context, sandbox.RunSpec) (sandbox.RunResult, error) {
	b.runs++
	return sandbox.RunResult{Command: "go test ./calc", ExitCode: 1, Output: "assertion failed"}, nil
}

// flakyExecGateBackend returns a different non-zero exit code on its second run,
// so a test can prove divergent determinism re-runs yield an unverifiable verdict.
type flakyExecGateBackend struct{ runs int }

func (b *flakyExecGateBackend) Name() string                    { return "flaky-exec-gate" }
func (b *flakyExecGateBackend) Preflight(context.Context) error { return nil }
func (b *flakyExecGateBackend) Run(context.Context, sandbox.RunSpec) (sandbox.RunResult, error) {
	b.runs++
	exitCode := 1
	if b.runs%2 == 0 {
		exitCode = 2
	}
	return sandbox.RunResult{Command: "go test ./calc", ExitCode: exitCode, Output: "assertion failed"}, nil
}

// TestReproduceAgain_GrantsExecEligibility proves the T3 determinism re-run path
// carries exec eligibility past the structural gate (Epic 11.1). reproduceAgain
// calls the dispatcher DIRECTLY (not through the tool loop that would otherwise
// grant eligibility), and is handed a bare, non-eligible context — yet because it
// only runs in an --exec flow it must grant eligibility itself, so the re-runs
// reach the backend and the deterministic failure confirms. Without the internal
// grant, both re-runs would be refused and the verdict would be unverifiable.
func TestReproduceAgain_GrantsExecEligibility(t *testing.T) {
	t.Parallel()
	b := &execGateBackend{}
	disp := tools.NewDispatcher(execGateResolver{}, tools.DefaultLimits())
	disp.EnableExecution(b, []string{"go", "test", "./..."}, 30*time.Second)

	rec := &execEvidenceRecorder{
		inner:    disp,
		lastName: "run_script",
		lastArgs: json.RawMessage(`{"content":"./repro.sh\n"}`),
	}

	// Bare context: NOT exec-eligible. With exec=true reproduceAgain grants
	// eligibility itself from the flag (never from the incoming ctx).
	verdict, ev := rec.reproduceAgain(context.Background(), true)

	require.Equal(t, reclib.VerdictConfirmed, verdict, "deterministic non-zero re-runs must confirm")
	require.NotNil(t, ev, "a confirmed deterministic repro must surface evidence")
	assert.Equal(t, 1, ev.ExitCode)
	assert.Equal(t, 2, b.runs, "both determinism re-runs must reach the backend (not be refused at the gate)")
}

// TestReproduceAgain_NoLastNameReturnsUnverifiable proves reproduceAgain refuses
// to grant eligibility when the skeptic made no prior exec tool call. Without a
// lastName there is nothing to reproduce, so the backend must never be invoked.
func TestReproduceAgain_NoLastNameReturnsUnverifiable(t *testing.T) {
	t.Parallel()
	b := &execGateBackend{}
	disp := tools.NewDispatcher(execGateResolver{}, tools.DefaultLimits())
	disp.EnableExecution(b, []string{"go", "test", "./..."}, 30*time.Second)

	rec := &execEvidenceRecorder{inner: disp}

	verdict, ev := rec.reproduceAgain(context.Background(), true)

	require.Equal(t, reclib.VerdictUnverifiable, verdict, "no prior exec call must be unverifiable")
	require.Nil(t, ev, "no reproduction must surface no evidence")
	assert.Equal(t, 0, b.runs, "backend must not be reached when there is nothing to reproduce")
}

// TestReproduceAgain_NonExecFlowRefusesGrant proves the structural boundary: when
// the caller is NOT in an --exec flow (exec=false), reproduceAgain refuses to mint
// eligibility regardless of the incoming context — the re-runs never reach the
// backend, so a non-exec run cannot escalate to execution through this path.
func TestReproduceAgain_NonExecFlowRefusesGrant(t *testing.T) {
	t.Parallel()
	b := &execGateBackend{}
	disp := tools.NewDispatcher(execGateResolver{}, tools.DefaultLimits())
	disp.EnableExecution(b, []string{"go", "test", "./..."}, 30*time.Second)

	rec := &execEvidenceRecorder{
		inner:    disp,
		lastName: "run_script",
		lastArgs: json.RawMessage(`{"content":"./repro.sh\n"}`),
	}

	verdict, ev := rec.reproduceAgain(context.Background(), false)

	require.Equal(t, reclib.VerdictUnverifiable, verdict, "a non-exec flow must not confirm via the determinism path")
	require.Nil(t, ev, "a non-exec flow must surface no evidence")
	assert.Equal(t, 0, b.runs, "a non-exec flow must never reach the backend")
}

// TestReproduceAgain_DivergentRunsReturnUnverifiable proves two determinism
// re-runs that disagree on exit code produce an unverifiable verdict and no
// evidence, keeping flaky reproductions from earning a badge.
func TestReproduceAgain_DivergentRunsReturnUnverifiable(t *testing.T) {
	t.Parallel()
	b := &flakyExecGateBackend{}
	disp := tools.NewDispatcher(execGateResolver{}, tools.DefaultLimits())
	disp.EnableExecution(b, []string{"go", "test", "./..."}, 30*time.Second)

	rec := &execEvidenceRecorder{
		inner:    disp,
		lastName: "run_script",
		lastArgs: json.RawMessage(`{"content":"./repro.sh\n"}`),
	}

	verdict, ev := rec.reproduceAgain(context.Background(), true)

	require.Equal(t, reclib.VerdictUnverifiable, verdict, "divergent exit codes must be unverifiable")
	require.Nil(t, ev, "flaky reproduction must not surface evidence")
	assert.Equal(t, 2, b.runs, "both determinism re-runs must reach the backend")
}
