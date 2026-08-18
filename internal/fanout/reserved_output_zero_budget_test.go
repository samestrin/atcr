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
	require.Zero(t, got.ReservedOutputTokens,
		"premise: nothing was funded, so the RESERVATION is correctly 0 — that field keeps "+
			"its meaning, and this test is not asking it to lie")

	assert.Equal(t, defaultMaxTokens, got.ResolvedMaxTokens,
		"the cap is what closed this budget, so the record has to name it — otherwise nothing "+
			"on disk says whether the window or the cap was at fault")
}

// The same on the fallback lane, which has its own re-derived budget and window and so
// reaches this arm independently.
func TestBuildFallbackAgent_ZeroBudgetRecordStillNamesTheCap(t *testing.T) {
	cfg := declaredWindowRoster(t, 512000)
	kai := cfg.Registry.Agents["kai"]
	kai.Model = "unlisted-backup-model"
	tiny := 1
	kai.ContextWindowTokens = &tiny
	cfg.Registry.Agents["kai"] = kai

	primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	var fb Agent
	_ = captureStderr(t, func() { fb, _, err = buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{}) })
	require.NoError(t, err)
	require.Zero(t, fb.EffectiveBudget, "premise: a 1-token window funds no input budget")
	require.Zero(t, fb.ReservedOutputTokens, "premise: nothing was funded")

	assert.Equal(t, defaultMaxTokens, fb.ResolvedMaxTokens,
		"a fallback that could not fund its cap must still record what that cap was")
}

// A funded agent records both, and they agree — equal is the readable signal for
// "the reservation was funded".
func TestBuildOneAgent_FundedRecordAgreesOnCapAndReservation(t *testing.T) {
	cfg := declaredWindowRoster(t, 128000)

	got, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	require.NotZero(t, got.EffectiveBudget, "premise: this window funds a budget")

	assert.Equal(t, got.ReservedOutputTokens, got.ResolvedMaxTokens,
		"when the budget funds the cap the two are the same number; a reader compares them "+
			"to tell a funded reservation from an unfunded one")
}

// A bare, unsized agent (doctor, direct construction) records nothing, so its status.json
// stays byte-identical under omitempty — the same guarantee its sibling sizing fields make.
func TestAgentStatus_UnsizedAgentRecordsNoCap(t *testing.T) {
	st := statusFor(Result{Agent: "greta", Status: StatusOK}, findingsResult{})

	assert.Zero(t, st.ResolvedWindow, "premise: a bare Result is unsized")
	assert.Zero(t, st.ReservedOutputTokens)
	assert.Zero(t, st.ResolvedMaxTokens,
		"an unsized agent resolved no cap; omitempty must keep the key absent as before")
}
