package benchmark

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ValidCaseFailureReason is the EXPORT trust boundary for the per-case failure
// channel, the same role ValidOutcome plays for the per-slot outcome vocabulary. It
// admits exactly the CaseFailure* wire values and nothing else.
//
// The empty string is REJECTED here, which is the one place this vocabulary
// deliberately parts company with ValidOutcome. OutcomeUnknown is the empty string
// because a checkpoint written before that field existed must decode into a
// representable absence; case_failures is a new array whose every element is written
// by executeRepoStateBenchmarkRun with a reason, so an element carrying no reason
// cannot have come from the producer at all.
func TestValidCaseFailureReason(t *testing.T) {
	for _, v := range []string{
		CaseFailureWorkDir, CaseFailureMaterialize, CaseFailurePrepare,
		CaseFailureExecute, CaseFailurePoolSummary, CaseFailureReadFindings,
	} {
		assert.True(t, ValidCaseFailureReason(v), "wire value %q must be valid", v)
	}
	for _, v := range []string{
		"",        // a failure record always carries a reason; absence is not representable
		"unknown", // no tally label exists for this vocabulary
		"MATERIALIZE", "materialize ", "fabricated", "failed",
	} {
		assert.False(t, ValidCaseFailureReason(v), "%q is not a storable failure reason", v)
	}
}

// The failure vocabulary must not collide with the OUTCOME vocabulary. Both appear
// in the same run-result document, one describing a CASE and the other describing a
// reviewer SLOT, and a shared spelling would invite a reader to fold two axes that
// mean different things — the exact conflation Strategy item 1 rejected when it
// declined to add a new Outcome* value for this.
func TestCaseFailureReasonsDoNotCollideWithOutcomes(t *testing.T) {
	for _, r := range []string{
		CaseFailureWorkDir, CaseFailureMaterialize, CaseFailurePrepare,
		CaseFailureExecute, CaseFailurePoolSummary, CaseFailureReadFindings,
	} {
		assert.False(t, ValidOutcome(r), "failure reason %q must not also be an outcome value", r)
		assert.NotEqual(t, OutcomeUnknownLabel, r, "failure reason %q must not be the outcome tally label", r)
	}
}

// The channel serializes under its own key, and an unfailed run omits it entirely —
// so a clean run-result is byte-identical to one written before the field existed,
// the same omitempty contract SuiteCaseIDs and Vocabulary carry.
func TestRunResultCaseFailuresRoundTrip(t *testing.T) {
	clean, err := json.Marshal(RunResult{Suite: "s", SuiteVersion: "1"})
	require.NoError(t, err)
	assert.NotContains(t, string(clean), "case_failures", "a run with no failures must omit the key")

	partial, err := json.Marshal(RunResult{
		Suite:        "s",
		SuiteVersion: "1",
		CaseFailures: []CaseFailure{{CaseID: "case-02", Reason: CaseFailurePrepare}},
	})
	require.NoError(t, err)
	assert.Contains(t, string(partial), `"case_failures":[{"case_id":"case-02","reason":"prepare"}]`)

	var back RunResult
	require.NoError(t, json.Unmarshal(partial, &back))
	require.Len(t, back.CaseFailures, 1)
	assert.Equal(t, "case-02", back.CaseFailures[0].CaseID)
	assert.Equal(t, CaseFailurePrepare, back.CaseFailures[0].Reason)
}
