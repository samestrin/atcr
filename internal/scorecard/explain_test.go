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

func TestScoreReasons_IsAClosedThreeMemberVocabulary(t *testing.T) {
	// C24's golden pin. AC 06-05 requires the reason labels be drawn from a
	// closed, finite set so cli/personas.go's renderer can rely on them; this is
	// the test that makes growing the set a deliberate act rather than a silent
	// one. A fourth member must update this test FIRST.
	assert.Equal(t, []string{
		"outcome-ineligible",
		"category-not-in-opportunity-set",
		"no-recognized-category",
	}, ScoreReasons())

	// The split matters as much as the membership: two labels name a DROPPED
	// record and one names a KEPT one, and a renderer that sums all three into
	// an "excluded" column reports a lens as less-measured than it is.
	assert.True(t, ReasonExcludes(ReasonOutcomeIneligible))
	assert.True(t, ReasonExcludes(ReasonNotInOpportunitySet))
	assert.False(t, ReasonExcludes(ReasonNoRecognizedCategory),
		"TD-032's label annotates a record that was kept and charged, not one that was dropped")
	assert.False(t, ReasonExcludes("not-a-member"))
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
	// A clean record on a run whose union is purely out of dax's remit. The
	// union comes from this same record because it is the only one on that run,
	// which is enough — opportunityUnions reads CategoriesRaised, not authorship.
	scopedOutcome(t, dir, 4, "Dax", outcomeClean, 0, reclib.CategoryPerformance)

	detail, err := ExplainTrustPriors(dir, 10)
	require.NoError(t, err)
	require.Contains(t, detail, "dax")
	assert.Equal(t, 20, detail["dax"].Counted)
	assert.Equal(t, 4, detail["dax"].Excluded)
	assert.Equal(t, 4, detail["dax"].Reasons[ReasonNotInOpportunitySet])
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

func TestExplainTrustPriors_WalksExactlyTheProductionChain(t *testing.T) {
	// The claim explainTrustPriorsSince' own comment makes. It re-walks the
	// chain link by link to learn WHICH link dropped a record, and that
	// re-walking is only honest while its survivor set is keptForTrust's.
	//
	// Mutating the walk — dropping a link, reordering two — makes Counted
	// disagree with the records that actually fed the rate, and the explanation
	// would then describe a pipeline the binary does not run.
	records := []Record{}
	add := func(runID, name string, raised, corr int, outcome string, cats ...string) {
		r := reviewer_(runID, name, "m1", raised, corr)
		r.Outcome = outcome
		r.CategoriesRaised = append([]string(nil), cats...)
		records = append(records, r)
	}
	add("r1", "Dax", 1, 1, outcomeFindings, reclib.CategoryTesting)
	add("r1", "Pace", 1, 0, outcomeFindings, reclib.CategoryPerformance)
	add("r2", "Dax", 1, 0, outcomeTruncatedLiteral, reclib.CategoryTesting)
	add("r3", "Dax", 0, 0, outcomeClean)
	add("r3", "Pace", 1, 0, outcomeFindings, reclib.CategoryPerformance)
	add("r4", "Dax", 2, 0, outcomeFindings) // raised, unlabelled
	add("r4", "Pace", 1, 0, outcomeFindings, reclib.CategoryPerformance)

	unions := opportunityUnions(records)
	walked := opportunitySetRuns(
		unresolvedEraRuns(mergeRoutedEras(scrubForgedCredit(eligibleOutcomeRuns(strictRuns(records))))),
		unions)

	assert.Equal(t, keptForTrust(records), walked,
		"the explain walk must compose the identical chain keptForTrust does")
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
