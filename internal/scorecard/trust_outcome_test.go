package scorecard

import (
	"testing"
	"time"

	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Outcome values are plain literals here for the import-cycle reason recorded in
// outcome_schema_test.go and sprint 36.0 clarification C5.

// outcomeRec builds a strict, current-era reviewer record carrying an outcome.
// Strict + current era keeps the other two chain links out of the way, so a
// failure in this file is about eligibility and nothing else.
func outcomeRec(name, outcome string, raised, corroborated int) Record {
	return Record{
		SchemaVersion:            SchemaVersion,
		RecordType:               RecordTypeReviewer,
		Reviewer:                 name,
		Model:                    "opus",
		Role:                     "reviewer",
		ConsensusLevel:           string(reclib.ConsensusStrict),
		RaisedIncludesUnresolved: true,
		RaisedDenominator:        RaisedDenominatorCurrent,
		Outcome:                  outcome,
		FindingsRaised:           raised,
		FindingsCorroborated:     corroborated,
		FindingsSolo:             raised - corroborated,
		CorroborationRate:        ratio(corroborated, raised),
	}
}

func repeatRec(n int, r Record) []Record {
	out := make([]Record, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, r)
	}
	return out
}

// TestEligibleOutcomeRuns_AllowlistIsExactlyTheFourEligibleOutcomes is the core
// of AC 02-04. ungrounded and filtered sit on the ELIGIBLE side deliberately:
// both are downstream of a complete, parseable response whose findings were
// discarded for cause, which is a judgment result, not a hosting fault.
func TestEligibleOutcomeRuns_AllowlistIsExactlyTheFourEligibleOutcomes(t *testing.T) {
	eligible := []string{testOutcomeFindings, testOutcomeClean, testOutcomeUngrounded, testOutcomeFiltered}
	ineligible := []string{
		testOutcomeUnparseable, testOutcomeTruncated, testOutcomeIncomplete,
		testOutcomeFailed, testOutcomeUnknown,
	}
	require.Len(t, append(append([]string{}, eligible...), ineligible...), 9,
		"the vocabulary is nine values; a tenth needs an explicit decision here")

	for _, o := range eligible {
		t.Run("eligible/"+o, func(t *testing.T) {
			got := eligibleOutcomeRuns([]Record{outcomeRec("vera", o, 1, 1)})
			assert.Len(t, got, 1)
		})
	}
	for _, o := range ineligible {
		t.Run("ineligible/"+o, func(t *testing.T) {
			got := eligibleOutcomeRuns([]Record{outcomeRec("vera", o, 1, 1)})
			assert.Empty(t, got)
		})
	}
}

// TestEligibleOutcomeRuns_UnrecognizedOutcomeFailsClosed pins the allowlist as
// an allowlist. A denylist would admit a future tenth outcome value by default,
// which is the wrong direction for a durable score.
func TestEligibleOutcomeRuns_UnrecognizedOutcomeFailsClosed(t *testing.T) {
	got := eligibleOutcomeRuns([]Record{
		outcomeRec("vera", "banana", 1, 1),
		outcomeRec("vera", "FINDINGS", 1, 1),
		outcomeRec("vera", "findings ", 1, 1),
	})
	assert.Empty(t, got, "anything outside the four eligible constants is excluded")
}

// TestEligibleOutcomeRuns_DoesNotMutateInput matches the contract mergeRoutedEras
// states in its own doc comment: "The input slice is never mutated."
func TestEligibleOutcomeRuns_DoesNotMutateInput(t *testing.T) {
	in := []Record{
		outcomeRec("vera", testOutcomeFindings, 1, 1),
		outcomeRec("vera", testOutcomeFailed, 9, 0),
		outcomeRec("vera", testOutcomeClean, 2, 2),
	}
	before := append([]Record{}, in...)

	got := eligibleOutcomeRuns(in)

	assert.Equal(t, before, in, "input slice was mutated")
	assert.Len(t, got, 2)
}

// TestEligibleOutcomeRuns_PassesAggregatesThrough follows the precedent
// unresolvedEraRuns sets at trust.go ("aggregates pass through untouched"). An
// aggregate record is not a reviewer and carries no outcome, so judging it on
// one would make this the first link in the chain to drop aggregates over a
// property that does not apply to them.
func TestEligibleOutcomeRuns_PassesAggregatesThrough(t *testing.T) {
	agg := Record{
		SchemaVersion:     SchemaVersion,
		RecordType:        RecordTypeAggregate,
		ConsensusLevel:    string(reclib.ConsensusStrict),
		RaisedDenominator: RaisedDenominatorCurrent,
	}
	got := eligibleOutcomeRuns([]Record{agg, outcomeRec("vera", testOutcomeFailed, 1, 0)})

	require.Len(t, got, 1)
	assert.Equal(t, RecordTypeAggregate, got[0].RecordType)
}

// TestEligibleOutcomeRuns_RunsBeforeTheEraLinks pins D5's chain POSITION, and it
// is the reason that position is a decision rather than a detail.
//
// unresolvedEraRuns is prefer-newest per reviewer: it keeps only the records at
// the newest era that reviewer has and drops the rest. If the eligibility filter
// ran AFTER it, a single failed run at a newer era would set vera's newest era
// to that one, and vera's twenty real pre-epic runs would be discarded as "the
// older half" — before the eligibility filter ever got to discard the failed
// record itself. vera would vanish from the map because of one timeout. Running
// first, the failed record never reaches the era pass at all.
//
// The two eras have to be 1 and 2+ for this to be observable: mergeRoutedEras
// runs ahead of unresolvedEraRuns and rewrites every era-3 record into its era-2
// equivalent, so a 2-vs-3 fixture cannot tell the two orderings apart.
func TestEligibleOutcomeRuns_RunsBeforeTheEraLinks(t *testing.T) {
	dir := t.TempDir()

	// Twenty genuine runs at the PRE-EPIC era (denominator 1): no routed flag,
	// no explicit denominator, which is exactly how a pre-35.16.6.5 record reads.
	older := outcomeRec("vera", testOutcomeFindings, 2, 1)
	older.RaisedIncludesUnresolved = false
	older.RaisedDenominator = 0
	for i, r := range repeatRec(20, older) {
		r.RunID = runIDAt(time.Now(), "vera-older-"+string(rune('a'+i)))
		require.NoError(t, Append(dir, r))
	}
	// One infrastructure failure at the CURRENT era, which mergeRoutedEras
	// rewrites to era 2 — still newer than the twenty above.
	fail := outcomeRec("vera", testOutcomeFailed, 0, 0)
	fail.RunID = runIDAt(time.Now(), "vera-timeout")
	require.NoError(t, Append(dir, fail))

	rates, err := TrustPriors(dir, 20)
	require.NoError(t, err)

	require.Contains(t, rates, "vera",
		"a single failed run must not strand twenty real runs in a superseded era")
	assert.InDelta(t, 20.0/40.0, rates["vera"], 1e-9)
}

// TestTrustPriors_TimeoutHistoryDoesNotMoveTheRate is AC 02-04 Scenario 2, the
// epic's first live example: vera hung on a LiteLLM 1200s timeout x 3 retries —
// 98% of one run's wall clock, zero findings. The rate must be identical to the
// one computed from the surviving runs alone.
func TestTrustPriors_TimeoutHistoryDoesNotMoveTheRate(t *testing.T) {
	withTimeouts := t.TempDir()
	clean := t.TempDir()

	real := outcomeRec("vera", testOutcomeFindings, 2, 1)
	for i := 0; i < 20; i++ {
		r := real
		r.RunID = runIDAt(time.Now(), "real"+string(rune('a'+i)))
		require.NoError(t, Append(withTimeouts, r))
		require.NoError(t, Append(clean, r))
	}
	// Only the first store carries the timeouts. They are a MIX on purpose: a
	// hard failure contributes no findings at all, so on its own it could never
	// move a ratio and the test would pass vacuously whether or not the filter
	// exists. The retry that came back cut off DID emit uncorroborated junk
	// before dying, and truncated outranks findings in the classifier — so those
	// records carry counts that drag the rate down if they are ever scored.
	for i := 0; i < 15; i++ {
		r := outcomeRec("vera", testOutcomeFailed, 0, 0)
		r.RunID = runIDAt(time.Now(), "timeout"+string(rune('a'+i)))
		require.NoError(t, Append(withTimeouts, r))

		cut := outcomeRec("vera", testOutcomeTruncated, 4, 0)
		cut.RunID = runIDAt(time.Now(), "cutoff"+string(rune('a'+i)))
		require.NoError(t, Append(withTimeouts, cut))
	}

	got, err := TrustPriors(withTimeouts, 20)
	require.NoError(t, err)
	want, err := TrustPriors(clean, 20)
	require.NoError(t, err)

	require.Contains(t, got, "vera")
	assert.Equal(t, want["vera"], got["vera"],
		"a timeout history must leave the rate exactly where the real runs put it")
}

// TestTrustPriors_TruncationHistoryDoesNotMoveTheRate is AC 02-04 Edge Case 1,
// the epic's second live example: archer returned "truncated with zero findings"
// on every run because ai-jr.lan silently capped prompts at 16,384 tokens while
// returning HTTP 200. Nothing about that is a judgment failure.
func TestTrustPriors_TruncationHistoryDoesNotMoveTheRate(t *testing.T) {
	withTruncation := t.TempDir()
	clean := t.TempDir()

	real := outcomeRec("archer", testOutcomeFindings, 4, 3)
	for i := 0; i < 20; i++ {
		r := real
		r.RunID = runIDAt(time.Now(), "real"+string(rune('a'+i)))
		require.NoError(t, Append(withTruncation, r))
		require.NoError(t, Append(clean, r))
	}
	// Non-zero counts, or the assertion below is vacuous: a truncated run with
	// no findings cannot move a ratio by arithmetic alone. A 16,384-token cap
	// still lets the model emit a few uncorroborated findings before the prompt
	// runs out, and truncated outranks findings in the classifier, so these are
	// exactly the records the gate has to catch.
	for i := 0; i < 25; i++ {
		r := outcomeRec("archer", testOutcomeTruncated, 3, 0)
		r.RunID = runIDAt(time.Now(), "trunc"+string(rune('a'+i)))
		require.NoError(t, Append(withTruncation, r))
	}

	got, err := TrustPriors(withTruncation, 20)
	require.NoError(t, err)
	want, err := TrustPriors(clean, 20)
	require.NoError(t, err)

	require.Contains(t, got, "archer")
	assert.Equal(t, want["archer"], got["archer"])
	assert.InDelta(t, 60.0/80.0, got["archer"], 1e-9)
}

// TestTrustPriors_AuthFailureOnlyHistoryIsOmittedNotZeroed is AC 02-04 Edge Case
// 2 joined to AC 02-05 Scenario 1, the epic's third live example: kai-backup is
// auth_failed on a Moonshot billing cap. Absent means the neutral 1/N baseline
// in reconcile/consensus.go; present at 0.0 would be a durable punishment for a
// billing problem.
func TestTrustPriors_AuthFailureOnlyHistoryIsOmittedNotZeroed(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 40; i++ {
		r := outcomeRec("kai-backup", testOutcomeFailed, 0, 0)
		r.RunID = runIDAt(time.Now(), "auth"+string(rune('a'+i)))
		require.NoError(t, Append(dir, r))
	}

	rates, err := TrustPriors(dir, 20)
	require.NoError(t, err)

	v, present := rates["kai-backup"]
	assert.False(t, present,
		"a fully excluded reviewer is absent (neutral), never present at %v (punitive)", v)
}

// TestTrustPriors_StraddlesMinRunsOnlyBecauseOfIneligibleRecords is AC 02-05
// Edge Case 1. The floor is checked against the POST-filter count, so 25 total
// runs of which 15 are eligible is below a floor of 20 — a different route to
// omission than Scenario 1's zero-eligible case, and one that would silently
// pass if the floor were checked before the filter.
func TestTrustPriors_StraddlesMinRunsOnlyBecauseOfIneligibleRecords(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 15; i++ {
		r := outcomeRec("pace", testOutcomeFindings, 2, 2)
		r.RunID = runIDAt(time.Now(), "ok"+string(rune('a'+i)))
		require.NoError(t, Append(dir, r))
	}
	for i := 0; i < 10; i++ {
		r := outcomeRec("pace", testOutcomeUnparseable, 0, 0)
		r.RunID = runIDAt(time.Now(), "bad"+string(rune('a'+i)))
		require.NoError(t, Append(dir, r))
	}

	rates, err := TrustPriors(dir, 20)
	require.NoError(t, err)

	_, present := rates["pace"]
	assert.False(t, present, "25 total but only 15 eligible is below a floor of 20")
}

// TestTrustPriors_GenuinelyLowRateSurvivesTheFilter is AC 02-05 Edge Case 2, and
// it is the test that stops this whole phase becoming a loophole. Excluding runs
// a lens never got a fair shot at must not turn into suppressing a deserved low
// score.
func TestTrustPriors_GenuinelyLowRateSurvivesTheFilter(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 25; i++ {
		// Ten raised, one corroborated: eligible, complete, and genuinely bad.
		r := outcomeRec("otto", testOutcomeFindings, 10, 1)
		r.RunID = runIDAt(time.Now(), "solo"+string(rune('a'+i)))
		require.NoError(t, Append(dir, r))
	}

	rates, err := TrustPriors(dir, 20)
	require.NoError(t, err)

	require.Contains(t, rates, "otto", "a genuinely untrustworthy lens stays demotable")
	assert.InDelta(t, 0.1, rates["otto"], 1e-9)
}

// TestTrustPriors_UnknownOutcomeHistoryIsExcluded pins the schema-era half of the
// gate. Every record written before this sprint has no outcome, and an absent
// outcome must not be read as clean — that would credit a full trust rate to
// runs nobody ever classified.
func TestTrustPriors_UnknownOutcomeHistoryIsExcluded(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 40; i++ {
		r := outcomeRec("greta", testOutcomeUnknown, 4, 4)
		r.RunID = runIDAt(time.Now(), "v1"+string(rune('a'+i)))
		require.NoError(t, Append(dir, r))
	}

	rates, err := TrustPriors(dir, 20)
	require.NoError(t, err)

	_, present := rates["greta"]
	assert.False(t, present, "unclassified history is not evidence of trustworthiness")
}
