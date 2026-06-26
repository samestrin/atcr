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

	// Bare context: NOT exec-eligible. reproduceAgain must grant eligibility itself.
	verdict, ev := rec.reproduceAgain(context.Background())

	require.Equal(t, reclib.VerdictConfirmed, verdict, "deterministic non-zero re-runs must confirm")
	require.NotNil(t, ev, "a confirmed deterministic repro must surface evidence")
	assert.Equal(t, 1, ev.ExitCode)
	assert.Equal(t, 2, b.runs, "both determinism re-runs must reach the backend (not be refused at the gate)")
}
