package cli

import (
	"strconv"
	"testing"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// caseFailureExitGate folds slot_failures[] into --fail-on-case-failure and
// --max-case-failures on the premise its own comment states: "A slot failure IS an
// infrastructure failure". SlotFailureUnmeasuredOK breaks that premise — its doc
// says "the call succeeded" — so an agent that habitually answers on its reasoning
// channel produces one entry per case and makes a HEALTHY panel exit non-zero in
// CI, under a message asserting infrastructure failures that did not occur.
//
// This is the only finding in its set that changes an exit code, which is why the
// gate has to learn the difference rather than the message being softened
// (TD cli/benchmark.go:311).
func TestCaseFailureExitGate_UnmeasuredOKSlotsAreNotInfrastructureFailures(t *testing.T) {
	run := func(reason string, n int) *benchmark.RunResult {
		rr := &benchmark.RunResult{}
		for i := 1; i <= 10; i++ {
			rr.SuiteCaseIDs = append(rr.SuiteCaseIDs, "case-"+strconv.Itoa(i))
		}
		for i := 0; i < n; i++ {
			rr.SlotFailures = append(rr.SlotFailures, benchmark.SlotFailure{
				Model: "m-primary", Persona: "brad",
				CaseID: "case-" + strconv.Itoa(i+1), Reason: reason,
			})
		}
		return rr
	}

	// Every slot "failure" is an OK call that contributed nothing. Nothing was lost
	// to infrastructure, so neither flag may fail the run.
	assert.NoError(t, caseFailureExitGate(run(benchmark.SlotFailureUnmeasuredOK, 10), true, -1),
		"--fail-on-case-failure must not fire on a panel where every call succeeded")
	assert.NoError(t, caseFailureExitGate(run(benchmark.SlotFailureUnmeasuredOK, 10), false, 0),
		"--max-case-failures=0 counts infrastructure losses, and there were none")

	// The gate must still fire on the real thing — the regression direction that
	// matters, since an over-narrow predicate would let a dead provider exit 0.
	require.Error(t, caseFailureExitGate(run(benchmark.SlotFailureCall, 1), true, -1),
		"a genuinely failed call is still an infrastructure failure")

	// A MIXED run is counted by its infrastructure half only: one lost call trips a
	// threshold of zero, while nine unmeasured-ok slots beside it add nothing.
	mixed := run(benchmark.SlotFailureUnmeasuredOK, 9)
	mixed.SlotFailures = append(mixed.SlotFailures, benchmark.SlotFailure{
		Model: "m-primary", Persona: "brad", CaseID: "case-10", Reason: benchmark.SlotFailureTimeout,
	})
	require.Error(t, caseFailureExitGate(mixed, false, 0),
		"the one timed-out slot is a real loss and must still trip a zero threshold")
	assert.NoError(t, caseFailureExitGate(mixed, false, 1),
		"but only ONE loss may be counted: the nine unmeasured-ok slots must not consume the allowance")
}
