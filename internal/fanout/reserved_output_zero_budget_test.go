package fanout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reserved_output_tokens was recorded only when the sizing left a positive input budget,
// which suppressed it EXACTLY on the zero-budget arm — the one case where the cap is the
// cause of the degradation rather than incidental to it.
//
// On that record status.json carries resolved_window and degradation_action: overflow
// with nothing naming the cap, and effective_budget is 0 (omitted) too. Nothing else on
// disk carries it either: payload.Manifest records max_parallel and timeout_secs "for
// post-hoc diagnosis" but not the resolved cap or the --max-tokens override, and that
// override is re-resolved from live config on a resume — so config at diagnosis time need
// not be the config the run used. An operator reading the artifact afterwards cannot tell
// whether the WINDOW or the CAP closed the budget, which is the exact question the
// zero-budget remedy asks them to answer.
//
// The old rationale held that reserving 8192 out of a 12288-token window "is a record
// that contradicts itself". It is not a contradiction — it is the CAUSE, and it is the
// only number on the record that explains the zero.
func TestBuildSlots_ZeroBudgetRecordStillNamesTheCap(t *testing.T) {
	// A window this small is fully consumed by the cap plus the fixed prompt overhead,
	// so sizing yields no input budget at all.
	cfg := declaredWindowRoster(t, 12288)

	got, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	require.Zero(t, got.EffectiveBudget,
		"premise: this window funds no input budget, so this is the arm under test")
	require.NotZero(t, got.ResolvedWindow,
		"premise: the agent WAS sized — resolved_window is the signal for that")

	assert.Equal(t, defaultMaxTokens, got.ReservedOutputTokens,
		"the cap is what closed this budget; suppressing it leaves the record unable to say "+
			"whether the window or the cap was at fault")
}

// The compatibility guarantee the old gate was protecting must survive: a bare, unsized
// agent (doctor, direct construction) records nothing, so its status.json stays
// byte-identical under omitempty. resolved_window is the "was this agent sized" signal, so
// keying on it preserves that while still naming the cap on every sized record.
func TestAgentStatus_UnsizedAgentStillRecordsNoReservation(t *testing.T) {
	st := statusFor(Result{Agent: "greta", Status: StatusOK}, findingsResult{})

	assert.Zero(t, st.ResolvedWindow, "premise: a bare Result is unsized")
	assert.Zero(t, st.ReservedOutputTokens,
		"an unsized agent reserves nothing; omitempty must keep the key absent as before")
}
