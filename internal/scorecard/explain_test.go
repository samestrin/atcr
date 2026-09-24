package scorecard

import (
	"fmt"
	"io"
	"testing"
	"time"

	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scoped writes n reviewer records for persona, each raising raisedEach findings
// under the given CATEGORY vocabulary members, all at STRICT consensus so
// strictRuns keeps them. Each record gets its own RunID, so the opportunity
// union is per-run exactly as it is in production.
//
// It deliberately builds on reviewer_ rather than a private literal: a fixture
// that constructs a Record by hand drifts from what the emitter actually writes,
// and every gate this file exercises reads fields the emitter sets.
func scoped(t *testing.T, dir string, n int, persona string, raisedEach, corroboratedEach int, cats ...string) {
	t.Helper()
	for i := 0; i < n; i++ {
		rec := reviewer_(runIDAt(time.Now(), fmt.Sprintf("x-%s-%03d", persona, i)), persona, "m1", raisedEach, corroboratedEach)
		rec.CategoriesRaised = append([]string(nil), cats...)
		require.NoError(t, Append(dir, rec))
	}
}

// scopedOutcome is scoped with the record's Outcome overridden, for exercising
// the eligibility gate. reviewer_ picks a self-consistent outcome from the
// raised count, so overriding it is the only way to build an ineligible record.
func scopedOutcome(t *testing.T, dir string, n int, persona, outcome string, raisedEach int, cats ...string) {
	t.Helper()
	for i := 0; i < n; i++ {
		rec := reviewer_(runIDAt(time.Now(), fmt.Sprintf("o-%s-%03d", persona, i)), persona, "m1", raisedEach, 0)
		rec.Outcome = outcome
		rec.CategoriesRaised = append([]string(nil), cats...)
		require.NoError(t, Append(dir, rec))
	}
}

// outcomeTruncatedLiteral is the ineligible outcome these tests exercise. It is
// a plain literal because internal/scorecard cannot import internal/benchmark
// (that edge already runs the other way and would cycle — C5), so the eligible
// set in trust.go is spelled as literals too and only these four are named
// constants. The literal's agreement with the benchmark vocabulary is pinned by
// the cli/ drift test, which is a legal importer of both packages.
const outcomeTruncatedLiteral = "truncated"

func TestScoreReasons_IsAClosedSixMemberVocabulary(t *testing.T) {
	// C24's golden pin, grown 3→5 by the TD-041 clarification (2026-09-22) and
	// 5→6 by the record.go:476 TD row (epic acceptance criterion 7): the
	// not-opportunity-scoped statement the five registry-only lenses' records
	// needed. AC 06-05 requires the reason labels be drawn from a closed,
	// finite set so cli/personas.go's renderer can rely on them; this is the
	// test that makes growing the set a deliberate act rather than a silent
	// one. A seventh member must update this test FIRST.
	assert.Equal(t, []string{
		"outcome-ineligible",
		"consensus-not-strict",
		"superseded-era",
		"category-not-in-opportunity-set",
		"no-recognized-category",
		"not opportunity-scoped: no in-repo persona definition",
	}, ScoreReasons())

	// The split matters as much as the membership: four labels name a DROPPED
	// record and two name KEPT ones, and a renderer that sums all six into
	// an "excluded" column reports a lens as less-measured than it is.
	assert.True(t, ReasonExcludes(ReasonOutcomeIneligible))
	assert.True(t, ReasonExcludes(ReasonConsensusNotStrict))
	assert.True(t, ReasonExcludes(ReasonSupersededEra))
	assert.True(t, ReasonExcludes(ReasonNotInOpportunitySet))
	assert.False(t, ReasonExcludes(ReasonNoRecognizedCategory),
		"TD-032's label annotates a record that was kept and charged, not one that was dropped")
	assert.False(t, ReasonExcludes(ReasonNotOpportunityScoped),
		"the unmapped statement annotates a kept record — a scope decision, not a drop")
	assert.False(t, ReasonExcludes("not-a-member"))
}

// TestExplainTrustPriors_NonStrictRunIsAttributed pins the consensus boundary's
// per-record attribution: a reviewer record whose consensus level is not strict
// is noted consensus-not-strict, the cause the three-member vocabulary could not
// name (TD-041).
func TestExplainTrustPriors_NonStrictRunIsAttributed(t *testing.T) {
	dir := t.TempDir()
	// A strict run too: applyExplainFloor keeps exactly the lenses TrustPriors
	// keys, so a lens whose EVERY record was non-strict has no row at all (the
	// adjudicated no-row rule) and its note would be unreachable. The boundary
	// is observable on a lens with mixed history.
	appendN(t, dir, 2, "sasha", "opus", 1, 1)
	runID := runIDAt(time.Now(), "nonstrict")
	rec := reviewer(runID, "sasha", "opus", 1, 0, 0, 0)
	rec.ConsensusLevel = "off" // not strict: the consensus gate drops it
	require.NoError(t, Append(dir, rec))

	details, err := ExplainTrustPriors(dir, 0)
	require.NoError(t, err)
	d := details["sasha"]
	require.NotNil(t, d)
	assert.Equal(t, 1, d.Reasons[ReasonConsensusNotStrict],
		"a non-strict run must be attributed to the consensus boundary, not vanish unexplained")
}

// TestExplainTrustPriors_SupersededEraIsAttributed pins the era boundary's
// per-record attribution: a reviewer record computed under a definition older
// than the reviewer's newest is noted superseded-era (TD-041).
func TestExplainTrustPriors_SupersededEraIsAttributed(t *testing.T) {
	dir := t.TempDir()
	oldRun := reviewer(runIDAt(time.Now(), "old"), "bruce", "opus", 1, 0, 0, 0)
	oldRun.RaisedDenominator = 1
	require.NoError(t, Append(dir, oldRun))
	newRun := reviewer(runIDAt(time.Now(), "new"), "bruce", "opus", 1, 0, 0, 0)
	newRun.RaisedDenominator = RaisedDenominatorCurrent
	require.NoError(t, Append(dir, newRun))

	details, err := ExplainTrustPriors(dir, 0)
	require.NoError(t, err)
	d := details["bruce"]
	require.NotNil(t, d)
	assert.Equal(t, 1, d.Reasons[ReasonSupersededEra],
		"the older half of an era-spanning reviewer must be attributed to the era boundary")
}

func TestExplainTrustPriors_CountsTheCasesBehindALensRate(t *testing.T) {
	// Epic acceptance criterion 7, happy path: every record eligible, every run
	// in remit, nothing excluded.
	dir := t.TempDir()
	scoped(t, dir, 20, "Dax", 1, 1, reclib.CategoryTesting)

	detail, err := ExplainTrustPriors(dir, 10)
	require.NoError(t, err)
	require.Contains(t, detail, "dax")
	assert.Equal(t, 20, detail["dax"].Counted)
	assert.Equal(t, 0, detail["dax"].Excluded)
	assert.Empty(t, detail["dax"].Reasons,
		"a label with no records must be absent, not present at zero")
}

func TestExplainTrustPriors_NamesTheOutcomeGateAsTheExclusionReason(t *testing.T) {
	// Epic acceptance criterion 2, made explainable: this is the surface that
	// tells a maintainer archer's missing cases were a hosting failure and not a
	// judgment failure.
	dir := t.TempDir()
	scoped(t, dir, 20, "Dax", 1, 1, reclib.CategoryTesting)
	scopedOutcome(t, dir, 5, "Dax", outcomeTruncatedLiteral, 1, reclib.CategoryTesting)

	detail, err := ExplainTrustPriors(dir, 10)
	require.NoError(t, err)
	require.Contains(t, detail, "dax")
	assert.Equal(t, 20, detail["dax"].Counted)
	assert.Equal(t, 5, detail["dax"].Excluded)
	assert.Equal(t, 5, detail["dax"].Reasons[ReasonOutcomeIneligible])
}

func TestExplainTrustPriors_NamesTheOpportunityGateAsTheExclusionReason(t *testing.T) {
	// Epic acceptance criterion 1, made explainable: dax is scored on testing and
	// error-handling, so a run whose only discriminating topic is performance is
	// out of its remit. Being correctly silent there must read as "not my case",
	// never as a weak result.
	dir := t.TempDir()
	scoped(t, dir, 20, "Dax", 1, 1, reclib.CategoryTesting)
	// A genuinely SILENT dax on a run whose union is purely out of its remit.
	// The union has to come from ANOTHER reviewer: a clean record carrying its
	// own categories is a record no emitter writes, so the old single-record
	// shortcut asserted the gate's behaviour on a fixture that cannot occur.
	for i := 0; i < 4; i++ {
		runID := runIDAt(time.Now(), fmt.Sprintf("offremit-%03d", i))
		quiet := reviewer_(runID, "Dax", "m1", 0, 0)
		require.NoError(t, Append(dir, quiet))
		other := reviewer_(runID, "Pace", "m1", 1, 0)
		other.CategoriesRaised = []string{reclib.CategoryPerformance}
		require.NoError(t, Append(dir, other))
	}

	detail, err := ExplainTrustPriors(dir, 10)
	require.NoError(t, err)
	require.Contains(t, detail, "dax")
	assert.Equal(t, 20, detail["dax"].Counted)
	assert.Equal(t, 4, detail["dax"].Excluded)
	assert.Equal(t, 4, detail["dax"].Reasons[ReasonNotInOpportunitySet])
}

// TestExplainTrustPriors_PreEraMappedRecordCountsUnannotated pins the pre-era
// branch: a record written before CategoriesRaised existed cannot be judged on
// its absence, so a mapped lens's silent v1 record on an out-of-remit run is
// counted with no reason, where the same record at v2 would be excluded.
func TestExplainTrustPriors_PreEraMappedRecordCountsUnannotated(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 4; i++ {
		runID := runIDAt(time.Now(), fmt.Sprintf("preera-%03d", i))
		quiet := reviewer_(runID, "Dax", "m1", 0, 0)
		quiet.SchemaVersion = categoriesRaisedSinceSchema - 1
		require.NoError(t, Append(dir, quiet))
		other := reviewer_(runID, "Pace", "m1", 1, 0)
		other.CategoriesRaised = []string{reclib.CategoryPerformance}
		require.NoError(t, Append(dir, other))
	}

	detail, err := ExplainTrustPriors(dir, 0)
	require.NoError(t, err)
	require.Contains(t, detail, "dax")
	assert.Equal(t, 4, detail["dax"].Counted)
	assert.Zero(t, detail["dax"].Excluded)
	assert.Empty(t, detail["dax"].Reasons, "a pre-era record counts unannotated")
}

// TestExplainTrustPriors_AggregateRecordsAreNeverExplained pins the four
// RecordType skips in detailsFromRecords. Each aggregate below is shaped to
// trip exactly one gate's note if it were read as a reviewer record, and it
// carries dax's name so a leak is visible on dax's own detail.
func TestExplainTrustPriors_AggregateRecordsAreNeverExplained(t *testing.T) {
	seed := func(dir string) {
		scoped(t, dir, 20, "Dax", 1, 1, reclib.CategoryTesting)
	}
	agg := func(base string) Record {
		r := reviewer_(runIDAt(time.Now(), base), "Dax", "m1", 1, 0)
		r.RecordType = RecordTypeAggregate
		return r
	}

	baseDir := t.TempDir()
	seed(baseDir)
	want, err := ExplainTrustPriors(baseDir, 0)
	require.NoError(t, err)
	require.Contains(t, want, "dax")

	dir := t.TempDir()
	seed(dir)
	nonStrict := agg("agg-nonstrict")
	nonStrict.ConsensusLevel = "off" // consensus gate
	ineligible := agg("agg-failed")
	ineligible.Outcome = "failed" // outcome gate
	futureEra := agg("agg-era")
	futureEra.RaisedDenominator = RaisedDenominatorCurrent + 1 // era gate
	outOfRemit := agg("agg-remit")
	outOfRemit.FindingsRaised = 0
	outOfRemit.Outcome = outcomeClean
	other := reviewer_(outOfRemit.RunID, "Pace", "m1", 1, 0)
	other.CategoriesRaised = []string{reclib.CategoryPerformance} // opportunity gate
	blank := agg("agg-blank")
	blank.Reviewer = ""
	for _, r := range []Record{nonStrict, ineligible, futureEra, outOfRemit, other, blank} {
		require.NoError(t, Append(dir, r))
	}

	got, err := ExplainTrustPriors(dir, 0)
	require.NoError(t, err)
	assert.Equal(t, want["dax"], got["dax"], "aggregate records add no count, exclusion or reason")
	assert.NotContains(t, got, "", "an aggregate is never explained under a blank reviewer")
}

func TestExplainTrustPriors_ExcludedEqualsTheExcludingReasonsOnly(t *testing.T) {
	// The invariant PersonaScoreDetail.Excluded documents. Both a real exclusion
	// and a TD-032 annotation are present, so a fold that naively sums every
	// Reasons entry produces 9 instead of 5 and fails here.
	dir := t.TempDir()
	scoped(t, dir, 20, "Dax", 1, 1, reclib.CategoryTesting)
	scopedOutcome(t, dir, 5, "Dax", outcomeTruncatedLiteral, 1, reclib.CategoryTesting)
	unlabelled(t, dir, 4, "Dax")

	detail, err := ExplainTrustPriors(dir, 10)
	require.NoError(t, err)
	d := detail["dax"]

	sum := 0
	for reason, n := range d.Reasons {
		require.Contains(t, ScoreReasons(), reason, "every key must be a vocabulary member")
		if ReasonExcludes(reason) {
			sum += n
		}
	}
	assert.Equal(t, d.Excluded, sum,
		"Excluded must equal the EXCLUDING reasons' sum, never the sum of all reasons")
	assert.Equal(t, 4, d.Reasons[ReasonNoRecognizedCategory])
}

func TestExplainTrustPriors_JunkLabelledRaiserIsCountedNotExcluded(t *testing.T) {
	// TD-032. dax raises findings on every run, but on four of them every
	// CATEGORY it typed is outside reclib.Categories(), so it contributes nothing
	// to those runs' unions. Another lens raised a discriminating out-of-remit
	// topic on the same runs, which before TD-032's fix deleted dax's record and
	// let a junk-labelled phantom-raiser escape demoteByTrust entirely.
	dir := t.TempDir()
	scoped(t, dir, 20, "Dax", 1, 1, reclib.CategoryTesting)
	for i := 0; i < 4; i++ {
		runID := runIDAt(time.Now(), fmt.Sprintf("junk-%03d", i))
		// CategoriesRaised is EMPTY, not junk-filled: reviewerCategories drops a
		// value outside reclib.Categories() at emit time (scorecard.go), so a
		// record whose every label was unrecognised reaches the store carrying
		// none. A fixture that stuffs "wibble" in here tests a record no emitter
		// could write — and would smuggle that word INTO the union, inverting
		// the very condition under test.
		junk := reviewer_(runID, "Dax", "m1", 3, 0)
		require.NoError(t, Append(dir, junk))
		// A different lens puts a real, discriminating, out-of-dax's-remit topic
		// on the run, so the union is non-empty and the remit test would fail.
		other := reviewer_(runID, "Pace", "m1", 1, 0)
		other.CategoriesRaised = []string{reclib.CategoryPerformance}
		require.NoError(t, Append(dir, other))
	}

	detail, err := ExplainTrustPriors(dir, 10)
	require.NoError(t, err)
	d := detail["dax"]
	assert.Equal(t, 24, d.Counted,
		"a record with unreadable labels must be excluded from SCOPING, not from the tally")
	assert.Equal(t, 0, d.Excluded)
	assert.Equal(t, 4, d.Reasons[ReasonNoRecognizedCategory])
}

func TestExplainTrustPriors_NonDiscriminatingLabelsGetTheSameAnnotation(t *testing.T) {
	// The second provenance ReasonNoRecognizedCategory covers. `invariant` IS a
	// real vocabulary member and IS in every remit (C14), so a lens labelling
	// everything `invariant` contributes nothing to the union either and would
	// otherwise buy the same free pass as an out-of-vocabulary word.
	dir := t.TempDir()
	scoped(t, dir, 20, "Dax", 1, 1, reclib.CategoryTesting)
	for i := 0; i < 3; i++ {
		runID := runIDAt(time.Now(), fmt.Sprintf("inv-%03d", i))
		rec := reviewer_(runID, "Dax", "m1", 2, 0)
		rec.CategoriesRaised = []string{reclib.CategoryInvariant, reclib.CategoryOther}
		require.NoError(t, Append(dir, rec))
		other := reviewer_(runID, "Pace", "m1", 1, 0)
		other.CategoriesRaised = []string{reclib.CategoryPerformance}
		require.NoError(t, Append(dir, other))
	}

	detail, err := ExplainTrustPriors(dir, 10)
	require.NoError(t, err)
	assert.Equal(t, 3, detail["dax"].Reasons[ReasonNoRecognizedCategory])
	assert.Equal(t, 0, detail["dax"].Excluded)
}

func TestExplainTrustPriors_SilentSpecialistIsNeverAnnotatedAsUnreadable(t *testing.T) {
	// The boundary TD-032's fix must not cross. A lens that raised NOTHING on an
	// out-of-remit run also contributes no category — but that is the correct
	// silence epic acceptance criterion 1 protects, and it must keep taking the
	// opportunity-gate path rather than being charged as an unreadable raiser.
	dir := t.TempDir()
	scoped(t, dir, 20, "Dax", 1, 1, reclib.CategoryTesting)
	for i := 0; i < 6; i++ {
		runID := runIDAt(time.Now(), fmt.Sprintf("silent-%03d", i))
		quiet := reviewer_(runID, "Dax", "m1", 0, 0) // clean: raised nothing
		require.NoError(t, Append(dir, quiet))
		other := reviewer_(runID, "Pace", "m1", 1, 0)
		other.CategoriesRaised = []string{reclib.CategoryPerformance}
		require.NoError(t, Append(dir, other))
	}

	detail, err := ExplainTrustPriors(dir, 10)
	require.NoError(t, err)
	d := detail["dax"]
	assert.Equal(t, 6, d.Reasons[ReasonNotInOpportunitySet],
		"correct silence is an opportunity-set exclusion, not an unreadable-label annotation")
	assert.Equal(t, 0, d.Reasons[ReasonNoRecognizedCategory])
	assert.Equal(t, 20, d.Counted)
}

func TestExplainTrustPriors_MembershipMatchesTrustPriorsExactly(t *testing.T) {
	// AC 06-03's absence-not-zero rule, applied to BOTH maps. cli/personas.go
	// joins them by key, so a rate without a detail (or the reverse) would render
	// a half-populated row.
	dir := t.TempDir()
	scoped(t, dir, 25, "Dax", 1, 1, reclib.CategoryTesting)
	scoped(t, dir, 3, "Otto", 1, 1, reclib.CategoryStyle)

	rates, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	detail, err := ExplainTrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)

	assert.Equal(t, len(rates), len(detail))
	for k := range rates {
		assert.Contains(t, detail, k)
	}
	_, ratePresent := rates["otto"]
	_, detailPresent := detail["otto"]
	assert.False(t, ratePresent, "a below-floor lens is omitted from the priors map")
	assert.False(t, detailPresent,
		"a below-floor lens must be omitted from the detail map too, never present at a fabricated zero")
}

func TestExplainTrustPriors_MissingStoreIsEmptyAndNotAnError(t *testing.T) {
	// Best-effort on the same terms as TrustPriors — cli/personas.go discards the
	// error and renders an all-n/a table with a footer.
	detail, err := ExplainTrustPriors(t.TempDir(), 0)
	require.NoError(t, err)
	assert.Empty(t, detail)
}

func TestExplainTrustPriors_DoesNotChangeWhatTrustPriorsReturns(t *testing.T) {
	// AC 06-01's DoD and D3/D4's whole point: the companion is additive, so
	// reconcile/consensus.go's trustExempt and demoteByTrust see exactly the
	// numbers they saw before this phase.
	dir := t.TempDir()
	scoped(t, dir, 20, "Dax", 4, 3, reclib.CategoryTesting)

	before, err := TrustPriors(dir, 10)
	require.NoError(t, err)
	_, err = ExplainTrustPriors(dir, 10)
	require.NoError(t, err)
	after, err := TrustPriors(dir, 10)
	require.NoError(t, err)

	assert.Equal(t, before, after)
	assert.InDelta(t, 0.75, after["dax"], 1e-9)
}

// unlabelled writes n runs on which persona raised findings the emitter could
// attribute to no topic (CategoriesRaised empty, exactly as reviewerCategories
// leaves it) while ANOTHER lens raised a real, discriminating, out-of-remit
// topic. That second lens is what makes the run's union non-empty, which is the
// only condition under which TD-032's escape was reachable: an empty union
// already takes opportunitySetRuns' "refuse to guess" pass-through.
func unlabelled(t *testing.T, dir string, n int, persona string) {
	t.Helper()
	for i := 0; i < n; i++ {
		runID := runIDAt(time.Now(), fmt.Sprintf("u-%s-%03d", persona, i))
		rec := reviewer_(runID, persona, "m1", 3, 0)
		require.NoError(t, Append(dir, rec))
		other := reviewer_(runID, "Pace", "m1", 1, 0)
		other.CategoriesRaised = []string{reclib.CategoryPerformance}
		require.NoError(t, Append(dir, other))
	}
}

func TestExplainTrustPriors_CountedMatchesTheRecordsBehindTheRate(t *testing.T) {
	// The cross-check that makes Counted meaningful rather than decorative: it
	// must equal the number of reviewer records keptForTrust actually kept for
	// that persona, which is the set trustPriorsSince aggregates.
	dir := t.TempDir()
	scoped(t, dir, 20, "Dax", 1, 1, reclib.CategoryTesting)
	scopedOutcome(t, dir, 5, "Dax", outcomeTruncatedLiteral, 1, reclib.CategoryTesting)
	unlabelled(t, dir, 4, "Dax")

	detail, err := ExplainTrustPriors(dir, 10)
	require.NoError(t, err)

	records, err := ReadSince(dir, 0, time.Now(), ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	kept := 0
	for _, r := range keptForTrust(records) {
		if r.RecordType == RecordTypeReviewer && normalizeReviewerName(r.Reviewer) == "dax" {
			kept++
		}
	}
	assert.Equal(t, kept, detail["dax"].Counted)
}

func TestExplainTrustPriors_CountedExcludesEveryLinkTheChainDrops(t *testing.T) {
	// Replaces TestExplainTrustPriors_WalksExactlyTheProductionChain, which the
	// 5.2.A review proved was a tautology: it re-spelled the chain expression
	// inside the test body and compared THAT against keptForTrust, so explain.go
	// could drop a link and stay green. The mutant that proved it — replacing
	// strictRuns(records) with records in ExplainTrustPriors' read path — passed the
	// entire suite.
	//
	// This pins the walk against keptForTrust's own output instead, over a store
	// that exercises all four dropping links, so removing any one of them makes
	// the counted total disagree.
	dir := t.TempDir()
	// era stamps a record at the CURRENT raised-denominator definition. reviewer_
	// leaves the field at its zero value, which reads as era 1 — so without this
	// the "superseded era" arm below shares an era with everything else and the
	// era link drops nothing.
	era := func(r Record) Record {
		r.RaisedIncludesUnresolved = true
		r.RaisedDenominator = RaisedDenominatorCurrent
		return r
	}
	good := func(n int, tag string) {
		for i := 0; i < n; i++ {
			r := era(reviewer_(runIDAt(time.Now(), fmt.Sprintf("%s-%03d", tag, i)), "Dax", "m1", 1, 1))
			r.CategoriesRaised = []string{reclib.CategoryTesting}
			require.NoError(t, Append(dir, r))
		}
	}
	good(12, "good")

	// strictRuns: measured at --consensus off, never trusted.
	for i := 0; i < 3; i++ {
		r := era(reviewer_(runIDAt(time.Now(), fmt.Sprintf("lenient-%03d", i)), "Dax", "m1", 1, 1))
		r.CategoriesRaised = []string{reclib.CategoryTesting}
		r.ConsensusLevel = reclib.ConsensusOff
		require.NoError(t, Append(dir, r))
	}
	// eligibleOutcomeRuns: a hosting failure, not a judgment.
	for i := 0; i < 4; i++ {
		r := era(reviewer_(runIDAt(time.Now(), fmt.Sprintf("trunc-%03d", i)), "Dax", "m1", 1, 0))
		r.CategoriesRaised = []string{reclib.CategoryTesting}
		r.Outcome = outcomeTruncatedLiteral
		require.NoError(t, Append(dir, r))
	}
	// unresolvedEraRuns: an older raised_denominator than dax's newest. Left
	// UNSTAMPED on purpose — an absent field is exactly what era 1 means.
	for i := 0; i < 5; i++ {
		r := reviewer_(runIDAt(time.Now(), fmt.Sprintf("olderaz-%03d", i)), "Dax", "m1", 1, 1)
		r.CategoriesRaised = []string{reclib.CategoryTesting}
		require.NoError(t, Append(dir, r))
	}
	// opportunitySetRuns: correct silence on an out-of-remit case.
	for i := 0; i < 2; i++ {
		runID := runIDAt(time.Now(), fmt.Sprintf("offremit-%03d", i))
		quiet := era(reviewer_(runID, "Dax", "m1", 0, 0))
		require.NoError(t, Append(dir, quiet))
		other := era(reviewer_(runID, "Pace", "m1", 1, 0))
		other.CategoriesRaised = []string{reclib.CategoryPerformance}
		require.NoError(t, Append(dir, other))
	}

	detail, err := ExplainTrustPriors(dir, 0)
	require.NoError(t, err)

	records, err := ReadSince(dir, 0, time.Now(), ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	want := 0
	for _, r := range keptForTrust(records) {
		if r.RecordType == RecordTypeReviewer && normalizeReviewerName(r.Reviewer) == "dax" {
			want++
		}
	}
	require.Equal(t, 12, want, "the fixture must leave exactly the twelve good runs standing")
	assert.Equal(t, want, detail["dax"].Counted,
		"Counted must equal what keptForTrust actually kept; a dropped link makes these disagree")
	assert.Equal(t, 4, detail["dax"].Reasons[ReasonOutcomeIneligible])
	assert.Equal(t, 2, detail["dax"].Reasons[ReasonNotInOpportunitySet])
}

// Raised is the rate's denominator, so it must sum FindingsRaised over exactly
// the records Counted covers: a dropped record's findings are not in the rate.
func TestExplainTrustPriors_RaisedSumsOnlyTheCountedRecords(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		r := reviewer_(runIDAt(time.Now(), fmt.Sprintf("kept-%03d", i)), "Dax", "m1", 2, 1)
		r.CategoriesRaised = []string{reclib.CategoryTesting}
		require.NoError(t, Append(dir, r))
	}
	lenient := reviewer_(runIDAt(time.Now(), "lenient"), "Dax", "m1", 7, 0)
	lenient.CategoriesRaised = []string{reclib.CategoryTesting}
	lenient.ConsensusLevel = reclib.ConsensusOff
	require.NoError(t, Append(dir, lenient))

	detail, err := ExplainTrustPriors(dir, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, detail["dax"].Counted)
	assert.Equal(t, 6, detail["dax"].Raised, "the non-strict record's 7 findings never reach the rate")
}

func TestExplainTrustPriors_MembershipMatchesTrustPriorsWithNoFloor(t *testing.T) {
	// The 5.2.A review's first HIGH, and it was reachable on the ONLY production
	// call: cli/personas.go asks for ExplainTrustPriors(dir, 0). A persona whose
	// every record the chain dropped used to survive the (absent) floor at
	// Counted 0, giving a non-nil Detail beside a nil Rate and rendering the
	// literal "0 counted" that formatScoreDetail's own contract forbids.
	dir := t.TempDir()
	// sasha is silent on a run whose only topic is out of its remit, so every
	// sasha record leaves the tally.
	runID := runIDAt(time.Now(), "solo-run")
	quiet := reviewer_(runID, "Sasha", "m1", 0, 0)
	require.NoError(t, Append(dir, quiet))
	penny := reviewer_(runID, "Penny", "m1", 1, 0)
	penny.CategoriesRaised = []string{reclib.CategoryPerformance}
	require.NoError(t, Append(dir, penny))

	rates, err := TrustPriors(dir, 0)
	require.NoError(t, err)
	detail, err := ExplainTrustPriors(dir, 0)
	require.NoError(t, err)

	require.NotContains(t, rates, "sasha", "the control: TrustPriors omits a fully-dropped persona")
	assert.NotContains(t, detail, "sasha",
		"ExplainTrustPriors must omit it too, never publish it at Counted 0")
	assert.Equal(t, len(rates), len(detail))
	for k := range rates {
		assert.Contains(t, detail, k)
	}
}

func TestExplainTrustPriors_AllInfrastructureFailureLensIsOmittedNotZeroed(t *testing.T) {
	// The same HIGH via the outcome gate, which is the shape a real archer or
	// vera takes: every run truncated or timed out. TD-042 records the diagnostic
	// this costs; what it must NOT do is render a row under the "no scorecard
	// data" footer.
	dir := t.TempDir()
	scopedOutcome(t, dir, 6, "Archer", outcomeTruncatedLiteral, 1, reclib.CategoryTesting)

	rates, err := TrustPriors(dir, 0)
	require.NoError(t, err)
	detail, err := ExplainTrustPriors(dir, 0)
	require.NoError(t, err)

	assert.Empty(t, rates)
	assert.Empty(t, detail,
		"a lens with no usable measurement reads n/a; a 0-counted row beside an empty rate map is worse than silence")
}

func TestExplainTrustPriors_TwoRecordsOneRunBothGetTheirReason(t *testing.T) {
	// The 5.2.A review's recordKey finding. The outcome gate used to be
	// attributed by diffing key sets built from RunID + reviewer name; with two
	// records for one reviewer on one run, the eligible one's key masked the
	// ineligible one and the exclusion was silently lost. Reachable because
	// reconcile.go derives a run id from a timestamp plus a directory basename.
	dir := t.TempDir()
	scoped(t, dir, 20, "Dax", 1, 1, reclib.CategoryTesting)

	runID := runIDAt(time.Now(), "collide")
	good := reviewer_(runID, "Dax", "m1", 1, 1)
	good.CategoriesRaised = []string{reclib.CategoryTesting}
	require.NoError(t, Append(dir, good))
	bad := reviewer_(runID, "Dax", "m1", 1, 0)
	bad.CategoriesRaised = []string{reclib.CategoryTesting}
	bad.Outcome = outcomeTruncatedLiteral
	require.NoError(t, Append(dir, bad))

	detail, err := ExplainTrustPriors(dir, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, detail["dax"].Reasons[ReasonOutcomeIneligible],
		"the truncated record shares a run and a name with an eligible one; its exclusion must still be reported")
	assert.Equal(t, 1, detail["dax"].Excluded)
}

func TestExplainTrustPriors_UnmappedLensIsNeverAnnotatedUnlabelled(t *testing.T) {
	// The 5.2.A review's LOW. vera and the four other registry-only lenses are
	// never opportunity-scoped (C11), so reporting "(N unlabelled)" against one
	// would name a reason the chain did not act on.
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		runID := runIDAt(time.Now(), fmt.Sprintf("vr-%03d", i))
		vera := reviewer_(runID, "Vera", "m1", 2, 1) // raised, no categories
		require.NoError(t, Append(dir, vera))
		other := reviewer_(runID, "Pace", "m1", 1, 0)
		other.CategoriesRaised = []string{reclib.CategoryPerformance}
		require.NoError(t, Append(dir, other))
	}

	detail, err := ExplainTrustPriors(dir, 10)
	require.NoError(t, err)
	require.Contains(t, detail, "vera")
	assert.Equal(t, 20, detail["vera"].Counted)
	assert.Zero(t, detail["vera"].Reasons[ReasonNoRecognizedCategory],
		"a lens the opportunity gate never judges must carry no opportunity-flavoured reason")
	// Grown from Empty by the record.go:476 TD row (epic AC 7): vera now
	// carries exactly ONE reason — the scope statement saying WHY the gate
	// never judges it — and no judgment-flavoured label.
	assert.Equal(t, map[string]int{ReasonNotOpportunityScoped: 20}, detail["vera"].Reasons,
		"the unmapped lens must state its scope status and carry nothing else")
}

func TestExplainTrustPriors_ReasonsMapIsNotAliasedToTheInternalFold(t *testing.T) {
	// A caller mutating the returned map must not corrupt the next read.
	dir := t.TempDir()
	scoped(t, dir, 20, "Dax", 1, 1, reclib.CategoryTesting)
	scopedOutcome(t, dir, 3, "Dax", outcomeTruncatedLiteral, 1, reclib.CategoryTesting)

	first, err := ExplainTrustPriors(dir, 10)
	require.NoError(t, err)
	require.Equal(t, 3, first["dax"].Reasons[ReasonOutcomeIneligible])
	first["dax"].Reasons[ReasonOutcomeIneligible] = 999

	second, err := ExplainTrustPriors(dir, 10)
	require.NoError(t, err)
	assert.Equal(t, 3, second["dax"].Reasons[ReasonOutcomeIneligible])
}

func TestExplainTrustPriors_UnmappedLensSaysWhyItIsNeverOpportunityScoped(t *testing.T) {
	// Epic acceptance criterion 7, via the record.go:476 TD row: the five
	// registry-only lenses are never opportunity-scoped (C9/C11), and until now
	// the explain surface left them silently different — vera's Reasons map was
	// empty while dax's named its opportunity dispositions. The per-lens
	// explanation is the place that must say WHY.
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		runID := runIDAt(time.Now(), fmt.Sprintf("vo-%03d", i))
		vera := reviewer_(runID, "Vera", "m1", 2, 1)
		vera.CategoriesRaised = []string{reclib.CategoryTesting}
		require.NoError(t, Append(dir, vera))
		dax := reviewer_(runID, "Dax", "m1", 1, 1)
		dax.CategoriesRaised = []string{reclib.CategoryTesting}
		require.NoError(t, Append(dir, dax))
	}

	detail, err := ExplainTrustPriors(dir, 10)
	require.NoError(t, err)
	require.Contains(t, detail, "vera")
	assert.Equal(t, 20, detail["vera"].Counted,
		"unmapped means not scoped, never dropped — the count must be untouched")
	assert.Equal(t, 20, detail["vera"].Reasons[ReasonNotOpportunityScoped],
		"the per-lens explanation must state that the lens is not opportunity-scoped and why")
	assert.Zero(t, detail["dax"].Reasons[ReasonNotOpportunityScoped],
		"a mapped lens is opportunity-scoped and must not carry the unmapped annotation")
	assert.False(t, ReasonExcludes(ReasonNotOpportunityScoped),
		"the label describes a scope decision, not a drop — it must never inflate Excluded")
}
