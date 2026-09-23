package scorecard

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Phase 4a (Story 05) — the per-pair disagreement tally.
//
// It lives in its own file rather than in trust_test.go for the reason Phase 2
// and Phase 3 split out trust_outcome_test.go and opportunity_test.go: the
// surface is new, self-contained, and trust_test.go is already 1600+ lines.
// The sprint plan's task 4.1 names trust_test.go; this is the same tests in the
// file layout the two preceding phases established.

// pairReviewer builds a reviewer record carrying pair signals and the current
// pair era, i.e. what the post-4.2 emitter writes.
func pairReviewer(runID, name, model string, raised, corroborated int, signals ...PairSignal) Record {
	r := reviewer_(runID, name, model, raised, corroborated)
	r.PairSignals = signals
	r.PairEra = PairEraCurrent
	return r
}

// pairRunID stamps the YYYY-MM prefix Append derives the month file from, so a
// fixture run id is a real one rather than a bare label.
func pairRunID(base string) string { return runIDAt(time.Now(), base) }

// coEligible seeds n runs on which both personas were eligible and in play,
// sharing agreedEach agreements and disagreedEach severity splits per run.
func coEligible(t *testing.T, dir string, n int, a, b string, agreedEach, disagreedEach int) {
	t.Helper()
	for i := 0; i < n; i++ {
		runID := pairRunID(fmt.Sprintf("run-%s-%s-%03d", a, b, i))
		ra := pairReviewer(runID, a, "m1", agreedEach+disagreedEach, agreedEach,
			PairSignal{Peer: b, Agreed: agreedEach, Disagreed: disagreedEach})
		rb := pairReviewer(runID, b, "m1", agreedEach+disagreedEach, agreedEach,
			PairSignal{Peer: a, Agreed: agreedEach, Disagreed: disagreedEach})
		require.NoError(t, Append(dir, ra))
		require.NoError(t, Append(dir, rb))
	}
}

// ---------------------------------------------------------------------------
// AC 05-01 — pair key normalization
// ---------------------------------------------------------------------------

func TestPairKey_OrderNormalizedAndLowercase(t *testing.T) {
	forward, ok := PairKey("Penny", "pace")
	require.True(t, ok)
	reverse, ok := PairKey("pace", "PENNY")
	require.True(t, ok)

	assert.Equal(t, "pace|penny", forward, "key must be lowercase and alphabetically ordered")
	assert.Equal(t, forward, reverse, "both input orderings must collapse to one key")
}

func TestPairKey_EmptyOrWhitespaceMemberIsRejected(t *testing.T) {
	// Error Scenario 2: a malformed reviewer name must never become half of a
	// key. Fail closed, never a "|<other>" key and never a panic.
	for _, tc := range []struct{ a, b string }{
		{"", "pace"},
		{"pace", ""},
		{"   ", "pace"},
		{"pace", "\t"},
		{"", ""},
	} {
		key, ok := PairKey(tc.a, tc.b)
		assert.False(t, ok, "PairKey(%q, %q) must be rejected", tc.a, tc.b)
		assert.Empty(t, key, "a rejected pair must return no key")
	}
}

func TestPairKey_SelfPairIsRejected(t *testing.T) {
	// A reviewer is never its own co-reviewer; a "bruce|bruce" key would be a
	// tally of a lens against itself and would always read as total agreement.
	key, ok := PairKey("Bruce", "bruce")
	assert.False(t, ok, "a persona paired with itself must be rejected")
	assert.Empty(t, key)
}

// ---------------------------------------------------------------------------
// AC 05-01 / C15 / C16 — the emit seam
// ---------------------------------------------------------------------------

func TestEmit_RecordCarriesPairSignalsForCoReviewers(t *testing.T) {
	dir := t.TempDir()
	in := EmitInput{
		RunID: pairRunID("r1"),
		Reviewers: map[string]ReviewerMeta{
			"bruce": {Model: "m1", Outcome: outcomeFindings},
			"dax":   {Model: "m2", Outcome: outcomeFindings},
		},
		Findings: []Finding{{
			File: "a.go", Line: 1, Problem: "p",
			Reviewers: []string{"bruce", "dax"},
			Category:  "correctness", Severity: "HIGH",
		}},
	}
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	byName := reviewerRecordsByName(t, dir)
	require.Contains(t, byName, "bruce")
	require.Contains(t, byName, "dax")

	assert.Equal(t, []PairSignal{{Peer: "dax", Agreed: 1}}, byName["bruce"].PairSignals)
	assert.Equal(t, []PairSignal{{Peer: "bruce", Agreed: 1}}, byName["dax"].PairSignals)
}

func TestEmit_SeveritySplitCountsAsDisagreed(t *testing.T) {
	// reconcile.Merge stamps Disagreement ("<lo> vs <hi>") when the cluster's
	// members did not agree on severity, and BuildDisagreements keys
	// KindSeveritySplit off exactly that field. The merged Severity alone is the
	// MAX and cannot reveal the split, which is why Disagreement is threaded
	// alongside it.
	dir := t.TempDir()
	in := EmitInput{
		RunID: pairRunID("r1"),
		Reviewers: map[string]ReviewerMeta{
			"bruce": {Model: "m1", Outcome: outcomeFindings},
			"dax":   {Model: "m2", Outcome: outcomeFindings},
		},
		Findings: []Finding{
			{
				File: "a.go", Line: 1, Problem: "agreed",
				Reviewers: []string{"bruce", "dax"},
				Category:  "correctness", Severity: "HIGH",
			},
			{
				File: "b.go", Line: 2, Problem: "split",
				Reviewers: []string{"bruce", "dax"},
				Category:  "correctness", Severity: "HIGH",
				Disagreement: "LOW vs HIGH",
			},
		},
	}
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	byName := reviewerRecordsByName(t, dir)
	assert.Equal(t, []PairSignal{{Peer: "dax", Agreed: 1, Disagreed: 1}}, byName["bruce"].PairSignals)
	assert.Equal(t, []PairSignal{{Peer: "bruce", Agreed: 1, Disagreed: 1}}, byName["dax"].PairSignals)
}

func TestEmit_SoloFindingCreatesNoPairSignal(t *testing.T) {
	// AC 05-01 Edge Case 2: a persona sharing no finding gets NO entry, never a
	// zero-disagreement one — the two are indistinguishable downstream and the
	// second reads as "never disagrees", which is the drop-candidate verdict.
	dir := t.TempDir()
	in := EmitInput{
		RunID: pairRunID("r1"),
		Reviewers: map[string]ReviewerMeta{
			"bruce": {Model: "m1", Outcome: outcomeFindings},
			"vera":  {Model: "m2", Outcome: outcomeClean},
		},
		Findings: []Finding{{
			File: "a.go", Line: 1, Problem: "p",
			Reviewers: []string{"bruce"},
			Category:  "correctness", Severity: "HIGH",
		}},
	}
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	byName := reviewerRecordsByName(t, dir)
	assert.Empty(t, byName["bruce"].PairSignals, "a solo finding pairs bruce with nobody")
	assert.Empty(t, byName["vera"].PairSignals, "a silent lens pairs with nobody")
}

func TestEmit_PairSignalsAreDeterministicallyOrdered(t *testing.T) {
	// Two byte-identical runs must serialize byte-identically, or a diff of the
	// store reports churn that is really Go's map iteration order.
	dir := t.TempDir()
	in := EmitInput{
		RunID: pairRunID("r1"),
		Reviewers: map[string]ReviewerMeta{
			"bruce": {Model: "m1", Outcome: outcomeFindings},
			"dax":   {Model: "m2", Outcome: outcomeFindings},
			"greta": {Model: "m3", Outcome: outcomeFindings},
			"sasha": {Model: "m4", Outcome: outcomeFindings},
		},
		// THREE SEPARATE TWO-REVIEWER findings, not one four-reviewer cluster.
		// A cluster that is not exactly a pair now contributes nothing at all —
		// symmetrically, so the rate's two halves come from one population — so
		// the old single-cluster fixture would produce no signals to order.
		// What this test pins is the ORDERING, which is unchanged.
		Findings: []Finding{
			{File: "a.go", Line: 1, Problem: "p", Reviewers: []string{"sasha", "bruce"},
				Category: "correctness", Severity: "HIGH"},
			{File: "b.go", Line: 2, Problem: "q", Reviewers: []string{"greta", "bruce"},
				Category: "correctness", Severity: "HIGH"},
			{File: "c.go", Line: 3, Problem: "r", Reviewers: []string{"dax", "bruce"},
				Category: "correctness", Severity: "HIGH"},
		},
	}
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	got := reviewerRecordsByName(t, dir)["bruce"].PairSignals
	assert.Equal(t, []PairSignal{
		{Peer: "dax", Agreed: 1},
		{Peer: "greta", Agreed: 1},
		{Peer: "sasha", Agreed: 1},
	}, got, "peers must be sorted, not map-ordered")
}

func TestEmit_StampsThePairEraMarker(t *testing.T) {
	// C15: an absent pair_signals key is byte-identical whether the run measured
	// no pairs or predates the field entirely. The era marker is the only thing
	// in the bytes that tells them apart, so it is stamped UNCONDITIONALLY —
	// including on a run that produced no pair at all, which is exactly the case
	// the marker has to distinguish.
	dir := t.TempDir()
	in := EmitInput{
		RunID:     pairRunID("r1"),
		Reviewers: map[string]ReviewerMeta{"bruce": {Model: "m1", Outcome: outcomeClean}},
	}
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	assert.Equal(t, PairEraCurrent, reviewerRecordsByName(t, dir)["bruce"].PairEra,
		"a measured-empty run must be distinguishable from a pre-era record")
}

func TestPairSignals_RoundTripThroughTheStore(t *testing.T) {
	dir := t.TempDir()
	want := pairReviewer(pairRunID("r1"), "bruce", "m1", 3, 2,
		PairSignal{Peer: "dax", Agreed: 2, Disagreed: 1})
	require.NoError(t, Append(dir, want))

	got := reviewerRecordsByName(t, dir)["bruce"]
	assert.Equal(t, want.PairSignals, got.PairSignals)
	assert.Equal(t, PairEraCurrent, got.PairEra)
}

func TestPairSignals_AbsentKeysOmittedFromJSON(t *testing.T) {
	// omitempty on both, so a record that measured nothing serializes exactly as
	// a pre-4a record did.
	raw, err := json.Marshal(Record{SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer})
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "pair_signals")
	assert.NotContains(t, string(raw), "pair_era")
}

func TestPairSignals_DoNotBumpTheSchemaVersion(t *testing.T) {
	// C15 closes TD-030 the D8 way: an additive omitempty field plus its own era
	// marker is NOT a schema era. The literal is deliberate — asserting against
	// the constant would contract with it and pass at any value.
	assert.Equal(t, 2, SchemaVersion,
		"PairSignals is additive; bumping SchemaVersion would reclassify every measured v2 record as unmeasured")
}

// ---------------------------------------------------------------------------
// AC 05-01 — the cross-run fold
// ---------------------------------------------------------------------------

func TestPairTallies_AggregatesAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	coEligible(t, dir, minPairCases, "penny", "pace", 3, 1)

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)

	got, ok := tallies["pace|penny"]
	require.True(t, ok, "expected a tally under the normalized key, got keys %v", keysOf(tallies))
	assert.Equal(t, minPairCases*3, got.Agreed)
	assert.Equal(t, minPairCases*1, got.Disagreed)
	assert.Equal(t, minPairCases, got.Cases)
	assert.InDelta(t, 0.25, got.DisagreementRate(), 1e-9)
}

func TestPairTallies_IsDeterministicAcrossReads(t *testing.T) {
	// AC 05-01 Scenario 3, read as the AC itself instructs: assert determinism
	// (two reads of an unchanged store agree), not the absence of recomputation.
	// trustPriorsSince re-folds on every call and holds no cache; a persisted
	// precomputed tally would be the second durable store the epic forbids.
	dir := t.TempDir()
	coEligible(t, dir, minPairCases, "penny", "pace", 3, 1)

	first, err := PairDisagreements(dir)
	require.NoError(t, err)
	second, err := PairDisagreements(dir)
	require.NoError(t, err)

	assert.Equal(t, first, second, "two reads of an unchanged store must agree exactly")
}

func TestPairTallies_BothOrderingsCollapseToOneKey(t *testing.T) {
	dir := t.TempDir()
	// Run 1 names the pair one way round, run 2 the other.
	r1, r2 := pairRunID("r1"), pairRunID("r2")
	require.NoError(t, Append(dir, pairReviewer(r1, "penny", "m1", 1, 1, PairSignal{Peer: "pace", Agreed: 1})))
	require.NoError(t, Append(dir, pairReviewer(r1, "pace", "m1", 1, 1, PairSignal{Peer: "penny", Agreed: 1})))
	require.NoError(t, Append(dir, pairReviewer(r2, "pace", "m1", 1, 1, PairSignal{Peer: "penny", Agreed: 1})))
	require.NoError(t, Append(dir, pairReviewer(r2, "penny", "m1", 1, 1, PairSignal{Peer: "pace", Agreed: 1})))

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"pace|penny"}, keysOf(tallies), "both orderings must form ONE key")
	assert.Equal(t, 2, tallies["pace|penny"].Agreed, "each run must be counted once, not twice")
}

func TestPairTallies_CountsASharedFindingOnceNotOncePerMember(t *testing.T) {
	// Both members' records carry the same run's signal. Folding both sides
	// naively doubles every count, which halves every disagreement rate's
	// denominator-relative meaning and would silently make pairs look more
	// agreeable than they are.
	dir := t.TempDir()
	runID := pairRunID("r1")
	require.NoError(t, Append(dir, pairReviewer(runID, "bruce", "m1", 2, 2,
		PairSignal{Peer: "dax", Agreed: 2, Disagreed: 1})))
	require.NoError(t, Append(dir, pairReviewer(runID, "dax", "m1", 2, 2,
		PairSignal{Peer: "bruce", Agreed: 2, Disagreed: 1})))

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	assert.Equal(t, 2, tallies["bruce|dax"].Agreed)
	assert.Equal(t, 1, tallies["bruce|dax"].Disagreed)
	assert.Equal(t, 1, tallies["bruce|dax"].Cases)
}

func TestPairTallies_RequiresBothMembersToSurviveTheChain(t *testing.T) {
	// The whole point of the eligibility gate is that a lens whose call timed
	// out did not get a fair attempt. A pair signal read off the SURVIVING
	// member's record alone would re-admit exactly that run through the pair
	// surface — scoring a relationship with a lens the gate just excluded.
	//
	// BOTH ORDERINGS ARE EXERCISED. With only the excluded-member-sorts-first
	// case, the fold's own alphabetical handling short-circuits before the
	// presence check is ever reached, and deleting that check passes the whole
	// suite — which is exactly what the 4.2 adversarial pass demonstrated.
	for _, tc := range []struct {
		name              string
		survivor, dropped string
	}{
		{"excluded member sorts first", "bruce", "archer"},
		{"survivor sorts first", "bruce", "dax"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			runID := pairRunID("r1")
			ok := pairReviewer(runID, tc.survivor, "m1", 1, 1, PairSignal{Peer: tc.dropped, Agreed: 1})
			excluded := pairReviewer(runID, tc.dropped, "m2", 1, 1, PairSignal{Peer: tc.survivor, Agreed: 1})
			// truncated: an infrastructure fault, not a judgment failure.
			excluded.Outcome = "truncated"
			require.NoError(t, Append(dir, ok))
			require.NoError(t, Append(dir, excluded))

			tallies, err := PairDisagreements(dir)
			require.NoError(t, err)
			assert.Empty(t, tallies,
				"a pair may not be scored on a run one member was excluded from")
		})
	}
}

func TestPairTallies_ExcludesRecordsWithoutThePairEraMarker(t *testing.T) {
	// AC 05-01 Edge Case 3: items predating the pair signal are excluded from
	// the validated count, never read as a measured zero.
	dir := t.TempDir()
	for i := 0; i < minPairCases; i++ {
		runID := pairRunID(fmt.Sprintf("old-%03d", i))
		a := pairReviewer(runID, "penny", "m1", 1, 1, PairSignal{Peer: "pace", Agreed: 1})
		b := pairReviewer(runID, "pace", "m1", 1, 1, PairSignal{Peer: "penny", Agreed: 1})
		// PairSignals STAYS POPULATED. Nilling it too (an earlier version did)
		// makes the fixture pass whether or not the era guard exists, so the
		// test named for the guard could not fail when the guard was deleted.
		a.PairEra, b.PairEra = 0, 0 // pre-era
		require.NoError(t, Append(dir, a))
		require.NoError(t, Append(dir, b))
	}

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	assert.Empty(t, tallies, "pre-era records carry no pair evidence and must not form a tally")
}

func TestPairTallies_NoEntryForAPersonaThatNeverSharesAFinding(t *testing.T) {
	// AC 05-01 Edge Case 2 again, at the fold rather than the emitter.
	dir := t.TempDir()
	for i := 0; i < minPairCases; i++ {
		runID := pairRunID(fmt.Sprintf("r-%03d", i))
		require.NoError(t, Append(dir, pairReviewer(runID, "bruce", "m1", 1, 1)))
		require.NoError(t, Append(dir, pairReviewer(runID, "sasha", "m2", 1, 1)))
	}

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	assert.Empty(t, tallies, "co-eligibility alone is not a pair; a shared finding is")
}

func TestPairTallies_EmptyStoreYieldsEmptyMapNoError(t *testing.T) {
	tallies, err := PairDisagreements(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, tallies)
}

func TestPairTallies_MissingStoreDegradesNotErrors(t *testing.T) {
	// Matches TrustPriors' best-effort contract: a missing store is "no data
	// yet", never an error and never a panic.
	tallies, err := PairDisagreements(filepath.Join(t.TempDir(), "absent"))
	require.NoError(t, err)
	assert.Empty(t, tallies)
}

func TestPairTallies_MalformedPeerNameIsSkipped(t *testing.T) {
	// Error Scenario 2 at the fold: a whitespace-only peer must not become
	// "bruce|" — fail closed, silent skip, matching soloItem's convention.
	dir := t.TempDir()
	runID := pairRunID("r1")
	require.NoError(t, Append(dir, pairReviewer(runID, "bruce", "m1", 1, 1,
		PairSignal{Peer: "   ", Agreed: 1})))
	require.NoError(t, Append(dir, pairReviewer(runID, "dax", "m1", 1, 1,
		PairSignal{Peer: "bruce", Agreed: 1})))

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	for k := range tallies {
		assert.NotContains(t, k, "|\"", "no malformed key may be formed")
		assert.False(t, strings.HasPrefix(k, "|") || strings.HasSuffix(k, "|"),
			"malformed key %q formed from a blank peer", k)
	}
}

// ---------------------------------------------------------------------------
// AC 05-02 — the floor and the drop-candidate threshold
// ---------------------------------------------------------------------------

func TestPairTallies_BelowFloorIsInsufficientDataNotAZeroRate(t *testing.T) {
	// AC 05-01 Edge Case 1 / D6: ONE floor for the whole pair surface. A sparse
	// pair is "insufficient data" — never a numeric 0 read as perfect agreement,
	// and never a confident non-candidate either.
	dir := t.TempDir()
	coEligible(t, dir, minPairCases-1, "penny", "pace", 4, 0)

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)

	got := tallies["pace|penny"]
	assert.False(t, got.Sufficient, "a pair below the floor must not be marked sufficient")
	assert.False(t, got.DropCandidate, "insufficient data may never produce a drop candidate")
}

func TestDropCandidate_AtOrBelowThresholdIsFlagged(t *testing.T) {
	dir := t.TempDir()
	// 0 disagreements over a sufficient sample: the penny test's pure case.
	coEligible(t, dir, minPairCases, "penny", "pace", 4, 0)

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)

	got := tallies["pace|penny"]
	assert.True(t, got.Sufficient)
	assert.True(t, got.DropCandidate, "a pair that never disagrees over a sufficient sample IS the penny test")
}

func TestDropCandidate_StrictlyAboveThresholdIsNotFlagged(t *testing.T) {
	// AC 05-02 Edge Case 1: at-or-below semantics, not "near".
	dir := t.TempDir()
	// 1 disagreement in 10 shared findings per run = 0.10, above the threshold.
	coEligible(t, dir, minPairCases, "penny", "pace", 9, 1)

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)

	got := tallies["pace|penny"]
	require.True(t, got.Sufficient)
	require.Greater(t, got.DisagreementRate(), dropCandidateMaxRate)
	assert.False(t, got.DropCandidate, "a rate above the threshold is never flagged")
}

func TestDropCandidateMaxRate_NotNarrowedWithoutRemeasurement(t *testing.T) {
	// Mirrors TestDefaultTrustWindow_NotNarrowedWithoutRemeasurement. The
	// literal is deliberate: derived from the constant, this test would contract
	// with it and pass at any value.
	//
	// The constant is PROVISIONAL — there is no scorecard store to measure it
	// against. Moving it requires the measurement its doc comment names, and
	// updating this literal in the same commit.
	assert.Equal(t, 0.05, dropCandidateMaxRate,
		"redo the live-store measurement in dropCandidateMaxRate's doc comment before moving this")
}

func TestMinPairCases_NotNarrowedWithoutRemeasurement(t *testing.T) {
	// D6: this is the ONE floor for the whole pair surface. AC 04-05's Edge
	// Case 2 defers to it; Story 4 must not introduce a second.
	assert.Equal(t, 20, minPairCases,
		"redo the live-store measurement in minPairCases' doc comment before moving this")
}

func TestPairEraCurrent_NotBumpedWithoutAMixingRule(t *testing.T) {
	// pairTallies excludes ABOVE-CURRENT PairEra records outright rather than
	// clamping them. Bumping this literal mixes era-N evidence with era-1
	// evidence unless the mixing rule at the pairTallies gate is re-decided
	// first — the same convention unresolvedEraRuns applies to
	// RaisedDenominator. Mirrors
	// TestMinPairCases_NotNarrowedWithoutRemeasurement.
	assert.Equal(t, 1, PairEraCurrent,
		"decide the era-mixing rule at pairTallies' gate before bumping this")
}

// ---------------------------------------------------------------------------
// AC 05-03 — specialist protection
// ---------------------------------------------------------------------------

func TestDropCandidate_HighDisagreementSpecialistPairIsNeverFlagged(t *testing.T) {
	// AC 05-03 Scenario 1, with C11's substitution applied: vera has no in-repo
	// persona file, so the worked example uses two in-repo specialists with
	// genuinely distinct remits instead — sasha (security) and otto (style).
	dir := t.TempDir()
	coEligible(t, dir, minPairCases*2, "sasha", "otto", 1, 9)

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)

	got := tallies["otto|sasha"]
	require.True(t, got.Sufficient)
	assert.False(t, got.DropCandidate,
		"distinct remits disagree often; frequent disagreement is evidence of independence, never redundancy")
}

func TestPairTallies_DoNotAlterIndividualTrustRates(t *testing.T) {
	// AC 05-03 Scenario 2 / Error Scenario 1: the pair surface is READ-ONLY
	// against the individual-rate pipeline. If a future change wires the tally
	// into trustPriorsSince, this fails.
	dir := t.TempDir()
	coEligible(t, dir, minPairCases, "sasha", "otto", 1, 9)

	before, err := TrustPriors(dir, 0)
	require.NoError(t, err)

	_, err = PairDisagreements(dir)
	require.NoError(t, err)

	after, err := TrustPriors(dir, 0)
	require.NoError(t, err)
	assert.Equal(t, before, after, "computing pair tallies must not move any individual rate")
}

func TestDropCandidate_GeneralistPairVolumeIsNotItselfASignal(t *testing.T) {
	// AC 05-03 Edge Case 1: bruce pairs with every specialist, so it appears in
	// the most pairs by construction. Pair COUNT must never be the signal; only
	// the per-pair RATE is.
	dir := t.TempDir()
	specialists := []string{"dax", "greta", "ingrid", "kai", "mira", "otto", "penny", "sasha"}
	for _, s := range specialists {
		coEligible(t, dir, minPairCases, "bruce", s, 6, 4) // rate 0.4, well above threshold
	}

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	require.Len(t, tallies, len(specialists), "bruce should appear in one pair per specialist")

	for key, got := range tallies {
		assert.False(t, got.DropCandidate,
			"pair %s flagged on volume alone; only the per-pair rate may flag", key)
	}
}

func TestPairTallies_OutOfRemitSilenceIsNotTacitAgreement(t *testing.T) {
	// AC 05-03 Edge Case 2: a narrow lens raising nothing on an out-of-remit
	// case must not have that silence counted as agreeing with everyone. Read
	// as agreement it would drive the pair rate to zero and flag a correctly
	// silent specialist as redundant — the epic's headline failure mode.
	dir := t.TempDir()
	for i := 0; i < minPairCases; i++ {
		runID := pairRunID(fmt.Sprintf("r-%03d", i))
		// bruce raises; sasha is eligible but silent and shares no finding.
		require.NoError(t, Append(dir, pairReviewer(runID, "bruce", "m1", 1, 0)))
		silent := pairReviewer(runID, "sasha", "m2", 0, 0)
		silent.Outcome = outcomeClean
		require.NoError(t, Append(dir, silent))
	}

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	assert.NotContains(t, tallies, "bruce|sasha",
		"silence is not agreement; it forms no pair evidence at all")
}

// ---------------------------------------------------------------------------
// AC 05-04 — reporting-only safety boundary
// ---------------------------------------------------------------------------

func TestPairSurface_DoesNotImportInternalRegistry(t *testing.T) {
	// AC 05-04 Error Scenario 1. internal/boundaries_test.go already pins this
	// package's allowed internal imports and omits registry, so the guard exists
	// repo-wide; a third copy of that AST scan is the very smell TD-009 files.
	// This test pins the fact LOCALLY, where a reader of the pair surface will
	// see it, by asserting the package source names no registry import.
	// The needle is ASSEMBLED rather than written as one literal, and _test.go
	// files are skipped. Without both, this test scans its own source, finds the
	// path it is searching for, and fails on itself — which it did on the first
	// run of the RED pass.
	needle := `"` + "github.com/samestrin/atcr/internal" + "/registry" + `"`

	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		scanned++
		assert.NotContains(t, string(src), needle,
			"%s imports internal/registry; the pair surface reports, it never touches the registry", name)
	}
	require.NotZero(t, scanned, "the scan found no package source to check")
}

func TestPairDisagreements_PerformsNoWrites(t *testing.T) {
	// AC 05-04 Scenario 1: flagging a drop candidate writes nothing, anywhere.
	dir := t.TempDir()
	coEligible(t, dir, minPairCases, "penny", "pace", 4, 0)

	before := snapshotDir(t, dir)
	_, err := PairDisagreements(dir)
	require.NoError(t, err)
	assert.Equal(t, before, snapshotDir(t, dir), "the pair surface is read-only")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func reviewerRecordsByName(t *testing.T, dir string) map[string]Record {
	t.Helper()
	recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	out := map[string]Record{}
	for _, r := range recs {
		if r.RecordType == RecordTypeReviewer {
			out[r.Reviewer] = r
		}
	}
	return out
}

func keysOf(m map[string]PairTally) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// snapshotDir records every file's name, size and modification time so a write
// of any kind shows up as a difference.
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		out[path] = fmt.Sprintf("%d|%s", info.Size(), info.ModTime().Format(time.RFC3339Nano))
		return nil
	})
	require.NoError(t, err)
	return out
}

// ---------------------------------------------------------------------------
// C16 — the reconcile seam threads Severity and Disagreement
// ---------------------------------------------------------------------------

func TestEmitForReconcile_ThreadsDisagreementIntoPairSignals(t *testing.T) {
	// The gap C16 closes end to end: reconcile.Merge records a severity split in
	// Merged.Disagreement, and without threading it the emitter sees only the
	// cluster's MAX severity and reads every split as plain agreement.
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{
				File: "a.go", Line: 1, Problem: "agreed",
				Reviewers: []string{"bruce", "greta"},
				Severity:  "HIGH",
			}},
			{Finding: reconcile.Finding{
				File: "b.go", Line: 2, Problem: "split",
				Reviewers:    []string{"bruce", "greta"},
				Severity:     "HIGH",
				Disagreement: "LOW vs HIGH",
			}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}

	EmitForReconcile(reviewDir, res, EmitOpts{})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)

	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce)
	assert.Equal(t, []PairSignal{{Peer: "greta", Agreed: 1, Disagreed: 1}}, bruce.PairSignals,
		"the severity split must survive the reconcile seam, not just a direct Emit call")
	assert.Equal(t, PairEraCurrent, bruce.PairEra)
}

// ---------------------------------------------------------------------------
// Regressions from the 4.2 adversarial pass
// ---------------------------------------------------------------------------

func TestReviewerPairSignals_ThreeWaySplitIsNotChargedToEveryPair(t *testing.T) {
	// THE 4.2 HIGH FINDING. reconcile.MergeSeverity sets Disagreement when the
	// GROUP's severities span more than one value, so on a cluster where bruce
	// and greta both said HIGH and otto said LOW, charging every pair a split
	// invents two disagreements and deletes one real agreement. Inflating every
	// multi-reviewer pair's rate systematically SUPPRESSES the drop-candidate
	// flag this surface exists to raise.
	findings := []Finding{{
		File: "a.go", Line: 1, Problem: "three-way",
		Reviewers:    []string{"bruce", "greta", "otto"},
		Severity:     "HIGH",
		Disagreement: "LOW vs HIGH",
	}}

	for _, name := range []string{"bruce", "greta", "otto"} {
		assert.Nil(t, reviewerPairSignals(name, findings, nil),
			"%s: a 3+-reviewer split names no pair and must contribute nothing", name)
	}
}

func TestReviewerPairSignals_TwoWaySplitIsAttributable(t *testing.T) {
	// The complement of the test above, and the reason the rule is "exactly
	// two" rather than "never": with two reviewers on the cluster, the split
	// provably IS between them.
	findings := []Finding{{
		File: "a.go", Line: 1, Problem: "two-way",
		Reviewers:    []string{"bruce", "greta"},
		Severity:     "HIGH",
		Disagreement: "LOW vs HIGH",
	}}
	assert.Equal(t, []PairSignal{{Peer: "greta", Disagreed: 1}},
		reviewerPairSignals("bruce", findings, nil))
}

func TestReviewerPairSignals_PeerNamesAreNormalizedAndDeduped(t *testing.T) {
	// Emit is exported, so its reviewer list is untrusted input — the same
	// reasoning distinctCount documents at this layer. Undeduped, " dax" and
	// "dax" become two entries that the fold collapses into one key with the
	// counts SUMMED, inflating Agreed for a single shared finding. An inflated
	// Agreed drives the rate DOWN, toward a false drop-candidate flag.
	findings := []Finding{{
		File: "a.go", Line: 1, Problem: "p",
		Reviewers: []string{"bruce", "dax", " dax", "DAX", "  "},
		Severity:  "HIGH",
	}}
	assert.Equal(t, []PairSignal{{Peer: "dax", Agreed: 1}},
		reviewerPairSignals("Bruce", findings, nil),
		"one shared finding is one agreement, whatever the cell's whitespace and casing")
}

func TestPairTallies_DuplicateRecordsInOneRunDoNotDoubleTheEvidence(t *testing.T) {
	// A reviewer can hold more than one record for a run (two models), and
	// Append is a blind append with no (RunID, Reviewer) dedupe. Summing the
	// mirrored copies made the result depend on which member sorted first.
	dir := t.TempDir()
	runID := pairRunID("r1")
	sig := PairSignal{Peer: "dax", Agreed: 4, Disagreed: 1}
	require.NoError(t, Append(dir, pairReviewer(runID, "bruce", "m1", 5, 4, sig)))
	require.NoError(t, Append(dir, pairReviewer(runID, "bruce", "m2", 5, 4, sig)))
	require.NoError(t, Append(dir, pairReviewer(runID, "dax", "m1", 5, 4,
		PairSignal{Peer: "bruce", Agreed: 4, Disagreed: 1})))

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	got := tallies["bruce|dax"]
	assert.Equal(t, 4, got.Agreed, "one run's evidence is counted once, not once per record")
	assert.Equal(t, 1, got.Disagreed)
	assert.Equal(t, 1, got.Cases)
}

func TestPairTallies_EvidenceSurvivesAOneSidedSignal(t *testing.T) {
	// Taking the alphabetically-first member's copy discarded the pair's whole
	// evidence whenever that member's record carried none. Max across both
	// copies recovers it.
	dir := t.TempDir()
	runID := pairRunID("r1")
	require.NoError(t, Append(dir, pairReviewer(runID, "bruce", "m1", 1, 1))) // no signal
	require.NoError(t, Append(dir, pairReviewer(runID, "dax", "m1", 1, 1,
		PairSignal{Peer: "bruce", Disagreed: 1})))

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	assert.Equal(t, 1, tallies["bruce|dax"].Disagreed,
		"a recorded split must not be lost because the other member's copy is missing")
}

func TestPairTallies_CasesCountCoEligibilityNotSharedFindings(t *testing.T) {
	// PairTally.Cases is the pair's OPPORTUNITY, which is what makes
	// minPairCases mean for a pair what DefaultTrustMinRuns means for a lens.
	// Counted from shared findings instead, Cases would be a far harsher
	// quantity than the analogy it is adopted from describes.
	//
	// The floor is now applied to the evidence axis as WELL — see minPairCases —
	// but that is a second application to a second quantity. This test pins what
	// Cases itself counts, which is unchanged.
	dir := t.TempDir()
	for i := 0; i < minPairCases; i++ {
		runID := pairRunID(fmt.Sprintf("r-%03d", i))
		var aSig, bSig []PairSignal
		if i < 5 { // only 5 of the 20 co-eligible runs share a finding
			aSig = []PairSignal{{Peer: "pace", Agreed: 1}}
			bSig = []PairSignal{{Peer: "penny", Agreed: 1}}
		}
		require.NoError(t, Append(dir, pairReviewer(runID, "penny", "m1", 1, 1, aSig...)))
		require.NoError(t, Append(dir, pairReviewer(runID, "pace", "m1", 1, 1, bSig...)))
	}

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	got := tallies["pace|penny"]
	assert.Equal(t, minPairCases, got.Cases, "Cases counts co-eligible runs, not shared findings")
	assert.Equal(t, 5, got.Agreed)
	// Cases clears the floor and the EVIDENCE does not, which is the whole point
	// of flooring both axes: five shared findings across twenty co-eligible runs
	// is ample opportunity and a sample far too thin to call a pair redundant on.
	assert.False(t, got.Sufficient,
		"opportunity alone must not make a pair sufficient — the rate needs evidence too")
}

func TestPairTallies_ExcludesAboveCurrentPairEras(t *testing.T) {
	// unresolvedEraRuns' rule, applied to this era marker: a record measured
	// under a rule this binary does not implement is EXCLUDED, never blended.
	// A future era-2 record still carries schema_version 2, so the store's read
	// gate admits it and nothing else would stop it.
	dir := t.TempDir()
	for i := 0; i < minPairCases; i++ {
		runID := pairRunID(fmt.Sprintf("r-%03d", i))
		a := pairReviewer(runID, "penny", "m1", 1, 1, PairSignal{Peer: "pace", Agreed: 1})
		b := pairReviewer(runID, "pace", "m1", 1, 1, PairSignal{Peer: "penny", Agreed: 1})
		a.PairEra, b.PairEra = PairEraCurrent+1, PairEraCurrent+1
		require.NoError(t, Append(dir, a))
		require.NoError(t, Append(dir, b))
	}

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	assert.Empty(t, tallies, "an above-current era must not blend with era-1 evidence")
}

func TestDropCandidate_ExactlyAtThresholdIsFlagged(t *testing.T) {
	// AC 05-02 spends a criterion on at-or-below semantics, and nothing pinned
	// the boundary itself: flipping <= to < passed the whole suite.
	dir := t.TempDir()
	coEligible(t, dir, minPairCases, "penny", "pace", 19, 1) // exactly 0.05

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	got := tallies["pace|penny"]
	require.InDelta(t, dropCandidateMaxRate, got.DisagreementRate(), 1e-9,
		"fixture must sit exactly ON the threshold or it pins nothing")
	assert.True(t, got.DropCandidate, "at-or-below means AT is flagged")
}

func TestPairKey_DelimiterBearingMemberIsRejected(t *testing.T) {
	// Unrejected, PairKey("a|b","c") and PairKey("a","b|c") both yield "a|b|c",
	// so two distinct pairs sum their evidence under one key naming neither.
	for _, tc := range []struct{ a, b string }{
		{"a|b", "c"},
		{"a", "b|c"},
	} {
		key, ok := PairKey(tc.a, tc.b)
		assert.False(t, ok, "PairKey(%q, %q) must be rejected", tc.a, tc.b)
		assert.Empty(t, key)
	}
}

func TestPairTallies_ExcludesANegativePairEra(t *testing.T) {
	// The store is plain user-writable JSONL, so the marker can carry a value
	// this binary never wrote. A "== 0" test would read -7 as "measured under the
	// current rule" and fold a corrupt record's signals into a durable tally.
	dir := t.TempDir()
	runID := pairRunID("neg-era")
	for _, name := range []string{"bruce", "dax"} {
		peer := "dax"
		if name == "dax" {
			peer = "bruce"
		}
		r := pairReviewer(runID, name, "m1", 4, 4, PairSignal{Peer: peer, Agreed: 4})
		r.PairEra = -7
		require.NoError(t, Append(dir, r))
	}

	assert.Empty(t, pairTalliesFromDir(t, dir),
		"a record carrying a negative era marker must not reach the tally")
}

// pairTalliesFromDir reads dir and folds it, so a test can assert on the fold
// without reaching past PairDisagreements' own read contract.
func pairTalliesFromDir(t *testing.T, dir string) map[string]PairTally {
	t.Helper()
	got, err := PairDisagreements(dir)
	require.NoError(t, err)
	return got
}

func TestReviewerPairSignals_ClusterSizeDiscardIsSymmetric(t *testing.T) {
	// THE BIAS THIS PINS INVERTED THE VERDICT. An earlier version dropped a
	// 3+-reviewer SPLIT as unattributable while still counting a 3+-reviewer
	// AGREEMENT, so a pair that agreed ten times and split ten times — all
	// inside three-reviewer clusters — reported a disagreement rate of 0.00 and
	// was flagged as a drop candidate. The published claim is the opposite:
	// discarded evidence makes the flag go un-raised, never wrongly raised.
	three := func(file, disagreement string) Finding {
		return Finding{
			File: file, Line: 1, Problem: "p",
			Reviewers:    []string{"bruce", "greta", "otto"},
			Category:     "correctness",
			Severity:     "HIGH",
			Disagreement: disagreement,
		}
	}
	findings := []Finding{three("a.go", ""), three("b.go", "LOW vs HIGH")}

	assert.Nil(t, reviewerPairSignals("bruce", findings, nil),
		"a cluster that is not exactly a pair must contribute neither an agreement nor a split")

	// The two-reviewer complement still counts both, so the fix narrows the
	// input rather than disabling the surface.
	pair := []Finding{
		{File: "c.go", Line: 1, Problem: "p", Reviewers: []string{"bruce", "greta"}},
		{File: "d.go", Line: 2, Problem: "q", Reviewers: []string{"bruce", "greta"}, Disagreement: "LOW vs HIGH"},
	}
	assert.Equal(t, []PairSignal{{Peer: "greta", Agreed: 1, Disagreed: 1}},
		reviewerPairSignals("bruce", pair, nil))
}

func TestPairTallies_ThinEvidenceIsNeverADropCandidate(t *testing.T) {
	// The maximally INDEPENDENT pair used to be the one most likely to be
	// deleted: co-eligible on many runs, connected on almost nothing, so its
	// rate was 0.00 over a sample of one — and Sufficient keyed only on the
	// opportunity axis, so the verdict shipped as confident.
	dir := t.TempDir()
	for i := 0; i < minPairCases*2; i++ {
		runID := pairRunID(fmt.Sprintf("indep-%03d", i))
		var aSig, bSig []PairSignal
		if i == 0 { // exactly ONE shared finding across the whole history
			aSig = []PairSignal{{Peer: "sasha", Agreed: 1}}
			bSig = []PairSignal{{Peer: "dax", Agreed: 1}}
		}
		require.NoError(t, Append(dir, pairReviewer(runID, "dax", "m1", 1, 1, aSig...)))
		require.NoError(t, Append(dir, pairReviewer(runID, "sasha", "m1", 1, 1, bSig...)))
	}

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	got := tallies["dax|sasha"]
	require.GreaterOrEqual(t, got.Cases, minPairCases, "the fixture must clear the opportunity floor")
	assert.Equal(t, 1, got.Agreed+got.Disagreed)
	assert.False(t, got.Sufficient, "one shared finding is not a sample")
	assert.False(t, got.DropCandidate,
		"the most independent pair in the store must never be reported as redundant")
}

func TestPairDisagreements_ReadsTheWholeStore(t *testing.T) {
	// PairDisagreements reads all history. A windowed variant returns only when
	// a surface needs it beside ResolveTrustPriors' window (TD-040).
	dir := t.TempDir()
	coEligible(t, dir, minPairCases, "bruce", "dax", 1, 0)

	all, err := PairDisagreements(dir)
	require.NoError(t, err)
	assert.Contains(t, all, "bruce|dax")
}

func TestNormalizeReviewerName_IsTheOneIdentityRuleBothSurfacesUse(t *testing.T) {
	// The two helpers had drifted: distinctCount trimmed without folding case,
	// distinctPeers did both. ["Bruce","bruce"] therefore counted as TWO
	// distinct corroborators on the persisted credit path — a lens corroborating
	// itself — while the pair path saw one lens and rejected the self-pair.
	assert.Equal(t, 1, distinctCount([]string{"Bruce", "bruce", " BRUCE "}),
		"one lens named three ways is one corroborator")
	assert.Equal(t, map[string]bool{"bruce": true}, distinctPeers([]string{"Bruce", "bruce", " BRUCE "}))

	_, corroborated, credit := reviewerCounts("Bruce", []Finding{{Reviewers: []string{"Bruce", "bruce"}}})
	assert.InDelta(t, 1.0, credit, 1e-9,
		"a lens cannot corroborate itself into a halved isolation credit")
	assert.Equal(t, 0, corroborated,
		"the persisted FindingsCorroborated half of the same change: one lens named twice is not corroboration")
}

func TestPairTallies_RunsTheSharedTrustChainNotACopyOfIt(t *testing.T) {
	// The pair fold reads keptForTrust, not its own inlined chain. Nothing in
	// this file previously produced a record any link would reject, so a
	// re-inlined copy dropping a link survived the suite — the exact drift the
	// shared helper exists to prevent, invisible from the pair surface.
	//
	// ONE TAINT PER LINK, with one PROVED exception and two links covered by
	// their own tests below because a uniform taint func cannot express them.
	//
	// The table here covers strictRuns, eligibleOutcomeRuns and
	// unresolvedEraRuns. It cannot cover:
	//
	//   - scrubForgedCredit — the ONE proved exception. It writes only
	//     WeightedCredit and CreditEra, and neither pairtally.go nor any link
	//     downstream of it reads either field, so inlining the chain without it
	//     changes no pair result. Unobservable from this surface, not merely
	//     untested.
	//   - mergeRoutedEras — needs a MIXED-era fixture (era 2 and era 3 records
	//     for one reviewer), which a taint applied uniformly to every record
	//     cannot build. See TestPairTallies_MergeRoutedErasIsInTheSharedChain.
	//   - opportunitySetRuns — needs the tainted pair to contribute NOTHING while
	//     a THIRD reviewer supplies the union, which a two-reviewer taint cannot
	//     build either. See TestPairTallies_OpportunitySetRunsIsInTheSharedChain.
	//
	// HISTORY, because this guard has been wrong twice. An early version covered
	// only strictRuns and eligibleOutcomeRuns. The opportunitySetRuns taint was
	// then DELETED on a false claim that the link could no longer reject any
	// record this surface can build; a gate re-review disproved that by
	// construction. The mergeRoutedEras gap was found by the round after that —
	// witness: a 20-era-3 + 20-era-2 penny/sasha history yields Cases=40 on the
	// real chain and Cases=20 without the link, half the pair history silently
	// deleted, whole suite green. Do not remove a case here on a reachability
	// argument without a failing mutant to back it.
	//
	// sasha (security) and penny (performance) are used throughout because both
	// are MAPPED personas, so the unmapped pass-through cannot rescue them from
	// the opportunity gate. Untainted runs carry no CategoriesRaised, so the
	// per-run union is empty and every other case reaches the fold — proved by
	// the control test below, without which every subtest here could be passing
	// because the fixture never arrived at all.
	for name, taint := range map[string]func(*Record){
		"non-strict run (strictRuns)":           func(r *Record) { r.ConsensusLevel = "off" },
		"truncated run (eligibleOutcomeRuns)":   func(r *Record) { r.Outcome = "truncated" },
		"failed run (eligibleOutcomeRuns)":      func(r *Record) { r.Outcome = "failed" },
		"unparseable run (eligibleOutcomeRuns)": func(r *Record) { r.Outcome = "unparseable" },
		"above-current denominator (unresolvedEraRuns)": func(r *Record) {
			r.RaisedDenominator = RaisedDenominatorCurrent + 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			seedPennySasha(t, dir, "tainted", taint)

			tallies, err := PairDisagreements(dir)
			require.NoError(t, err)
			assert.Empty(t, tallies,
				"a run the trust chain excludes must not produce pair evidence either")
		})
	}
}

func TestPairTallies_UntaintedControlActuallyReachesTheFold(t *testing.T) {
	// The control for the table above.
	dir := t.TempDir()
	seedPennySasha(t, dir, "clean", func(*Record) {})

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	assert.Contains(t, tallies, "penny|sasha", "the untainted control must reach the fold")
}

// seedPennySasha writes a co-eligible penny/sasha history well clear of both
// minPairCases axes, applying taint to every record before it is appended.
func seedPennySasha(t *testing.T, dir, label string, taint func(*Record)) {
	t.Helper()
	for i := 0; i < minPairCases*2; i++ {
		runID := pairRunID(fmt.Sprintf("%s-%03d", label, i))
		for _, pair := range [][2]string{{"sasha", "penny"}, {"penny", "sasha"}} {
			r := pairReviewer(runID, pair[0], "m1", 2, 2, PairSignal{Peer: pair[1], Agreed: 2})
			taint(&r)
			require.NoError(t, Append(dir, r))
		}
	}
}

// TestEmit_WritesPairSignalsOnAZeroRaisedRecord constructs the record shape a
// previous version of this file asserted no emitter could produce, and is the
// evidence behind the opportunitySetRuns taint above.
//
// THE CAUSE IS AN ASYMMETRY BETWEEN TWO NAME MATCHERS over the same
// Finding.Reviewers cell. reviewerCounts tests membership with contains, an
// exact string compare; reviewerPairSignals tests it with distinctPeers, which
// folds case through normalizeReviewerName. So a reviewer keyed "Penny" in the
// pool summary whose findings carry "penny" matches for pair signals and does
// NOT match for the raised count — one finding raised, FindingsRaised 0, pair
// signals present.
//
// That asymmetry was TD-047, filed against the exact-match contains() in
// reviewerCounts and reviewerCategories. It is closed now: all three folds
// route membership through the normalized participates() predicate, so the
// mixed-case reviewer counts its own finding, attributes its own category, AND
// carries the pair signal — one consistent identity across the record.
func TestEmit_WritesPairSignalsOnAZeroRaisedRecord(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Emit(EmitInput{
		RunID: pairRunID("case-divergence"),
		// The reviewers map is keyed with the pool summary's casing.
		Reviewers: map[string]ReviewerMeta{
			"Penny": {Model: "m1", Outcome: outcomeFindings},
			"sasha": {Model: "m1", Outcome: outcomeFindings},
		},
		// The finding cell carries the lowercase form, as reconcile writes it.
		Findings: []Finding{{
			Reviewers: []string{"penny", "sasha"},
			Category:  "performance",
			Severity:  "MEDIUM",
		}},
	}, EmitOpts{Dir: dir}))

	records, err := ReadSince(dir, 0, time.Now(), ReadOpts{Writer: io.Discard})
	require.NoError(t, err)

	var penny *Record
	for i := range records {
		if records[i].RecordType == RecordTypeReviewer && records[i].Reviewer == "Penny" {
			penny = &records[i]
		}
	}
	require.NotNil(t, penny, "the emitter must have written a record for the mixed-case reviewer")
	assert.Equal(t, 1, penny.FindingsRaised,
		"membership is normalized now, so the mixed-case reviewer counts its own finding")
	assert.NotEmpty(t, penny.PairSignals,
		"distinctPeers() folds case, so the SAME finding still yields a pair signal")

	// With identity consistent across the record, the categories fold also
	// attributes the finding — and the opportunity gate sees the raised,
	// out-of-remit category rather than an empty set.
	assert.Equal(t, []string{"performance"}, penny.CategoriesRaised,
		"the categories fold normalizes membership too")
	// With TD-047 closed the gate can no longer misread a working lens as
	// "raised nothing": a record with findings is never dropped for remit, so
	// the same record the gate used to reject is now counted.
	assert.Equal(t, dispCounted,
		opportunityDisposition(*penny, map[string]struct{}{"testing": {}}),
		"a real emitted record, carrying findings, that the opportunity gate counts")
}

// TestPairTallies_MergeRoutedErasIsInTheSharedChain covers the link the taint
// table above cannot: mergeRoutedEras needs a reviewer whose records span TWO
// eras, and the table applies one taint uniformly to every record.
//
// The link rewrites era-3 records into their era-2 equivalent so
// unresolvedEraRuns sees ONE routed era instead of two. Drop it and the era-3
// half of a reviewer's history is discarded as an older definition, halving the
// pair evidence on any store that merely spans the 35.16.6.8 upgrade.
func TestPairTallies_MergeRoutedErasIsInTheSharedChain(t *testing.T) {
	dir := t.TempDir()
	era := func(label string, denom int) {
		for i := 0; i < minPairCases; i++ {
			runID := pairRunID(fmt.Sprintf("%s-%03d", label, i))
			for _, pair := range [][2]string{{"sasha", "penny"}, {"penny", "sasha"}} {
				r := pairReviewer(runID, pair[0], "m1", 2, 2, PairSignal{Peer: pair[1], Agreed: 2})
				r.RaisedIncludesUnresolved = true
				r.RaisedDenominator = denom
				require.NoError(t, Append(dir, r))
			}
		}
	}
	era("era3", raisedDenominatorRoutedExShield)
	era("era2", raisedDenominatorAllRouted)

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	require.NotEmpty(t, tallies, "the fixture must reach the fold at all")

	var cases int
	for _, tl := range tallies {
		cases += tl.Cases
	}
	assert.Equal(t, minPairCases*2, cases,
		"both eras must survive as one; dropping mergeRoutedEras halves this and the rest of the suite stays green")
}

// TestPairTallies_OpportunitySetRunsIsInTheSharedChain covers the other link the
// taint table cannot express. The link drops a record whose lens raised nothing
// on a run where its remit was not in play, so the tainted pair has to be silent
// while a THIRD reviewer supplies the discriminating out-of-remit union — three
// reviewers on one run, which a two-reviewer taint cannot build.
//
// The zero-raised-with-pair-signals shape is real, not contrived: see
// TestEmit_WritesPairSignalsOnAZeroRaisedRecord, which constructs it through the
// production Emit.
func TestPairTallies_OpportunitySetRunsIsInTheSharedChain(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < minPairCases*2; i++ {
		runID := pairRunID(fmt.Sprintf("offremit-%03d", i))
		for _, pair := range [][2]string{{"sasha", "penny"}, {"penny", "sasha"}} {
			r := pairReviewer(runID, pair[0], "m1", 2, 2, PairSignal{Peer: pair[1], Agreed: 2})
			// Silent and attributing nothing — the shape the exact-match name
			// mismatch really produces.
			r.FindingsRaised = 0
			r.CategoriesRaised = nil
			require.NoError(t, Append(dir, r))
		}
		// A third lens puts a real, discriminating topic on the run that is in
		// neither sasha's (security) nor penny's (performance) remit.
		other := pairReviewer(runID, "dax", "m1", 1, 0)
		other.CategoriesRaised = []string{"testing"}
		require.NoError(t, Append(dir, other))
	}

	tallies, err := PairDisagreements(dir)
	require.NoError(t, err)
	assert.Empty(t, tallies,
		"a run the trust chain excludes must not produce pair evidence either")
}

// TestEmit_GrayZoneClusterChargesOneDisagreementToItsPair pins the AC 05-01
// gray-zone charge rule (2026-09-22 clarification): each TWO-reviewer ambiguous
// cluster contributes ONE disagreement evidence item to that pair — cluster-
// shaped, not per-finding — so two clusters between the same pair charge twice,
// and a gray-zone item never touches FindingsRaised or CategoriesRaised (the
// pair surface only).
func TestEmit_GrayZoneClusterChargesOneDisagreementToItsPair(t *testing.T) {
	dir := t.TempDir()
	key, ok := PairKey("otto", "sasha")
	require.True(t, ok)
	require.NoError(t, Emit(EmitInput{
		RunID: pairRunID("gray-zone-charge"),
		Reviewers: map[string]ReviewerMeta{
			"otto":  {Model: "m1", Outcome: outcomeFindings},
			"sasha": {Model: "m1", Outcome: outcomeFindings},
		},
		// TWO gray-zone clusters between the same pair: the charge is per
		// cluster, so the pair's Disagreed is 2, not deduped to 1.
		GrayZonePairs: []string{key, key},
	}, EmitOpts{Dir: dir}))

	records, err := ReadSince(dir, 0, time.Now(), ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	byName := map[string]Record{}
	for _, r := range records {
		if r.RecordType == RecordTypeReviewer {
			byName[r.Reviewer] = r
		}
	}
	require.Len(t, byName, 2)
	otto, sok := byName["otto"]
	require.True(t, sok)
	sasha, ook := byName["sasha"]
	require.True(t, ook)
	require.Len(t, otto.PairSignals, 1)
	assert.Equal(t, 2, otto.PairSignals[0].Disagreed, "one disagreement item per gray-zone cluster")
	assert.Zero(t, otto.PairSignals[0].Agreed, "a gray-zone cluster is never an agreement")
	require.Len(t, sasha.PairSignals, 1)
	assert.Equal(t, 2, sasha.PairSignals[0].Disagreed, "the mirrored copy charges the same pair")
	assert.Zero(t, otto.FindingsRaised, "the pair surface only: gray-zone items move no finding count")
}
