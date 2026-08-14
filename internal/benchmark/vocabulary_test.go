package benchmark

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/samestrin/atcr/reconcile"
)

// recordedRun is a recorded benchmark run in SCORER-INPUT shape: the raw
// per-reviewer raised categories, before Score aggregates them away.
//
// The shape is deliberate. A fixture holding a RunResult with a pre-written
// out_of_vocabulary_rate would let a test assert a number its author typed —
// tautological, and the same inert-guard defect this epic removes from
// internal/benchmarkimport. Holding raw categories forces the test to compute the
// rate through the production path, so it exercises the membership check, the
// denominator, and the wiring.
//
// Field names mirror cli/benchmark_checkpoint.go's checkpointReviewer /
// checkpointCase, which already serialize exactly this data for --checkpoint
// resume. That makes a real V1 checkpoint hand-convertible into a fixture. The
// type is redeclared rather than imported: package cli is outside this package's
// dependency direction, and internal/benchmark stays a leaf.
//
// Four fields below are carried for that shape fidelity ALONE, and no value a
// fixture puts in them is ever asserted. Suite, SuiteVersion, and CaseID are
// never read after unmarshalling. Expected IS loaded into CaseScore.Expected,
// but the only production function these fixtures drive is OutOfVocabularyRate,
// which decides membership against the taxonomy rather than against ground
// truth and so never consults it. Verified by mutation: rewriting a fixture's
// suite, suite_version, and expected values leaves every test in this file
// green. Recall scoring is the consumer that does read CaseScore.Expected
// (score.go), and it is pinned by score_test.go and equivalence_test.go against
// their own inline cases — never through these fixtures.
type recordedRun struct {
	Suite        string         `json:"suite"`         // shape fidelity only — never read
	SuiteVersion string         `json:"suite_version"` // shape fidelity only — never read
	Cases        []recordedCase `json:"cases"`
}

type recordedCase struct {
	CaseID    string             `json:"case_id"` // shape fidelity only — never read
	Expected  []string           `json:"expected"`
	Reviewers []recordedReviewer `json:"reviewers"`
}

type recordedReviewer struct {
	Agent   string   `json:"agent"`
	Model   string   `json:"model"`
	Persona string   `json:"persona"`
	Raised  []string `json:"raised"`
}

// loadRecordedRun folds a fixture into the []ReviewerScore the scorer consumes,
// using the same per-agent accumulation cli/benchmark_run.go performs.
func loadRecordedRun(t *testing.T, name string) []ReviewerScore {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "vocabulary", name))
	require.NoError(t, err)

	var doc recordedRun
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.NotEmpty(t, doc.Cases, "%s records no cases", name)

	accs := map[string]*ReviewerScore{}
	var order []string
	for _, c := range doc.Cases {
		for _, r := range c.Reviewers {
			acc, ok := accs[r.Agent]
			if !ok {
				acc = &ReviewerScore{Model: r.Model, Persona: r.Persona}
				accs[r.Agent] = acc
				order = append(order, r.Agent)
			}
			acc.Cases = append(acc.Cases, CaseScore{Expected: c.Expected, Raised: r.Raised})
		}
	}

	sort.Strings(order)
	out := make([]ReviewerScore, 0, len(order))
	for _, name := range order {
		out = append(out, *accs[name])
	}
	return out
}

// mustRate derefs a measured rate, failing when the run reported none. Every
// fixture below raises findings, so a nil here is a defect rather than a valid
// "unmeasured" outcome.
func mustRate(t *testing.T, rate *float64) float64 {
	t.Helper()
	require.NotNil(t, rate, "this run raised findings, so it must report a rate")
	return *rate
}

// A recorded run whose reviewers stayed inside the closed vocabulary passes.
func TestOutOfVocabularyRate_CleanRunIsUnderTheCeiling(t *testing.T) {
	rate := mustRate(t, OutOfVocabularyRate(loadRecordedRun(t, "run-clean.json")))

	assert.InDelta(t, 0.0, rate, 1e-9, "every raised category is a taxonomy member")
	assert.Less(t, rate, MaxOutOfVocabularyRate)
}

// A recorded run whose reviewers ignored the enumeration FAILS. This is the
// assertion AC3 names: model drift away from the vocabulary must not be silent.
func TestOutOfVocabularyRate_DriftedRunExceedsTheCeiling(t *testing.T) {
	rate := mustRate(t, OutOfVocabularyRate(loadRecordedRun(t, "run-drifted.json")))

	assert.InDelta(t, 0.6, rate, 1e-9, "12 of 20 findings used a non-member word")
	assert.GreaterOrEqual(t, rate, MaxOutOfVocabularyRate,
		"a run this drifted must trip the guard")
}

// The boundary pair. These two fixtures differ by a single in-vocabulary finding
// (1/21 vs 1/20), so an inverted or misread comparison cannot satisfy both — which
// two far-apart extremes would.
//
// Both were re-derived when epic 35.16.6.1 tightened the ceiling from 0.20 to 0.05:
// they straddled the OLD number by construction (4/21 vs 4/20), so leaving them alone
// was not an option — the "under" fixture measures 0.190476 and would sit above the new
// ceiling. The pair's structure is deliberately unchanged; only the ratio moved. Rates
// are asserted as LITERALS rather than against the constant, so a future change to the
// ceiling cannot quietly redefine what these fixtures measure.
func TestOutOfVocabularyRate_BoundaryPair(t *testing.T) {
	under := mustRate(t, OutOfVocabularyRate(loadRecordedRun(t, "run-boundary-under.json")))
	at := mustRate(t, OutOfVocabularyRate(loadRecordedRun(t, "run-boundary-at.json")))

	assert.InDelta(t, 1.0/21.0, under, 1e-9)
	assert.Less(t, under, MaxOutOfVocabularyRate, "just under the ceiling passes")

	assert.InDelta(t, 0.05, at, 1e-9)
	assert.GreaterOrEqual(t, at, MaxOutOfVocabularyRate,
		"the ceiling is exclusive: a run AT the threshold must trip the guard")

	// The pair is a NEIGHBOUR pair, and that is the property that makes it a boundary
	// test rather than two arbitrary points: the two fixtures differ by exactly one
	// in-vocabulary finding. Pinned so a future re-derivation cannot widen the gap and
	// leave a test that any sloppy comparison would satisfy.
	assert.InDelta(t, 1.0/20.0-1.0/21.0, at-under, 1e-9,
		"one in-vocabulary finding apart — the whole point of a boundary pair")
}

// AC5. The ceiling is derived from V1's measured output, not from a fixture guess, and
// the relationship it encodes is asserted rather than left to the doc comment: V1
// measured 0.0100 (2 drifted of 201 findings) on the only valid run in existence, and
// the ceiling sits a full order of magnitude above it.
//
// The direction of travel is also pinned. vocabulary.go is explicit that this number
// moves ONE WAY — tightened once a real measurement exists, never loosened when a run
// fails — so a future edit that raises it to accommodate a bad run has to delete an
// assertion that says so out loud, rather than just editing a constant.
func TestMaxOutOfVocabularyRate_IsDerivedFromTheV1Measurement(t *testing.T) {
	const v1Measured = 2.0 / 201.0 // 0.00995..., the V1 validation run's Run A

	assert.InDelta(t, 0.05, MaxOutOfVocabularyRate, 1e-9,
		"the ceiling is V1-derived; changing it is a deliberate act, not a refactor")
	assert.Greater(t, MaxOutOfVocabularyRate, v1Measured,
		"a ceiling at or below the only valid measurement would fail a run that behaved")
	assert.Less(t, MaxOutOfVocabularyRate, 0.20,
		"0.20 was the pre-V1 fixture guard, justified by merge-table words V1 never emitted; "+
			"this number may be tightened further but must never climb back")
	assert.Greater(t, MaxOutOfVocabularyRate, v1Measured*4,
		"n=1: variance under this metric is unmeasured, so the ceiling keeps real headroom "+
			"over the single observation rather than hugging it")
}

// The rate is MICRO-averaged across the whole run — one pooled numerator over one
// pooled denominator — and NOT macro-averaged per reviewer. vocabulary.go:111-113
// states that choice as load-bearing, so it needs a test that can tell the two
// apart rather than one that happens to agree with both.
//
// The four recorded fixtures cannot: computed both ways they give 0.0/0.0
// (run-clean), 0.6/0.6 (run-drifted) and 0.05/0.05 (run-boundary-at) — identical.
// Only run-boundary-under separates them at all, at micro 1/21 = 0.047619 vs macro
// 0.045455 — and both land on the SAME side of the ceiling, so no assertion in this
// package changes its verdict between the two averagings. A documented decision that
// no fixture can falsify is not pinned.
//
// (Those figures are for the fixtures as re-derived when epic 35.16.6.1 tightened the
// ceiling to 0.05. Before that they read 0.20/0.20 and micro 4/21 = 0.190476 vs macro
// 0.190909 — different numbers, same conclusion, which is why this case exists.)
//
// So this case is deliberately lopsided: one reviewer raising 2 findings that both
// drifted, one raising 80 that are all clean. Micro pools them to 2/82 ≈ 0.024 —
// under the ceiling, a run that passes. Macro averages the per-reviewer rates to
// (1.0 + 0.0)/2 = 0.5 — over the ceiling, a run that trips. The two answers now
// straddle MaxOutOfVocabularyRate, so a macro implementation cannot satisfy this
// test by rounding.
func TestOutOfVocabularyRate_IsMicroAveragedNotMacroAveraged(t *testing.T) {
	twoDrifted := []string{"bug", "clarity"} // neither is a taxonomy member
	eightyClean := make([]string, 80)
	for i := range eightyClean {
		eightyClean[i] = reconcile.CategoryCorrectness
	}

	rate := mustRate(t, OutOfVocabularyRate([]ReviewerScore{
		{Model: "sparse", Persona: "p", Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness}, Raised: twoDrifted,
		}}},
		{Model: "prolific", Persona: "p", Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness}, Raised: eightyClean,
		}}},
	}))

	const micro, macro = 2.0 / 82.0, 0.5
	assert.InDelta(t, micro, rate, 1e-9,
		"the rate pools findings run-wide; a 2-finding reviewer must not weigh as much as an 80-finding one")
	assert.Greater(t, math.Abs(rate-macro), 1e-6,
		"a per-reviewer average would report 0.5 here — that is the implementation this test exists to reject")

	// The two averagings land on opposite sides of the ceiling, so the choice is
	// not academic: it decides whether this run passes.
	assert.False(t, ExceedsVocabularyCeiling(&rate), "micro-averaged, this run is under the ceiling")
	assert.True(t, ExceedsVocabularyCeiling(ptr(macro)), "macro-averaged, the same run would trip it")
}

// The denominator is FINDINGS, not distinct categories — matching CaseScore.Raised's
// documented one-entry-per-finding semantics. A distinct-category denominator would
// let one prolific in-vocabulary category mask many drifted findings.
func TestOutOfVocabularyRate_DenominatorCountsFindingsNotCategories(t *testing.T) {
	rate := mustRate(t, OutOfVocabularyRate([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		Cases: []CaseScore{{
			Expected: []string{"correctness"},
			// 2 distinct categories, 5 findings, 1 of which is out of vocabulary.
			Raised: []string{"correctness", "correctness", "correctness", "correctness", "bug"},
		}},
	}}))

	assert.InDelta(t, 1.0/5.0, rate, 1e-9, "1 drifted finding of 5, not 1 of 2 distinct categories")
}

// A finding with no category at all is drift. Excluding it would produce a rate
// that IMPROVES when a reviewer stops labelling entirely.
func TestOutOfVocabularyRate_EmptyCategoryCountsAsDrift(t *testing.T) {
	rate := mustRate(t, OutOfVocabularyRate([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		Cases:   []CaseScore{{Expected: []string{"correctness"}, Raised: []string{"correctness", "", "   "}}},
	}}))

	assert.InDelta(t, 2.0/3.0, rate, 1e-9, "blank and whitespace-only categories are out of vocabulary")
}

// Membership is decided against the live taxonomy after the scorer's own
// normalize, so case and padding do not create phantom drift.
func TestOutOfVocabularyRate_MembershipIsNormalized(t *testing.T) {
	rate := mustRate(t, OutOfVocabularyRate([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		Cases:   []CaseScore{{Expected: []string{"security"}, Raised: []string{"  SECURITY ", "Input-Validation"}}},
	}}))

	assert.InDelta(t, 0.0, rate, 1e-9, "normalized members are in vocabulary")
}

// Every member of the taxonomy is in vocabulary by definition — including the two
// routing values, which are legal emissions even though they satisfy no expected
// category. A reviewer that reaches for `other` read its prompt; that is not drift.
func TestOutOfVocabularyRate_EveryTaxonomyMemberIsInVocabulary(t *testing.T) {
	rate := mustRate(t, OutOfVocabularyRate([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		Cases:   []CaseScore{{Expected: []string{"correctness"}, Raised: reconcile.Categories()}},
	}}))

	assert.InDelta(t, 0.0, rate, 1e-9, "no member of the closed vocabulary may count as drift")
}

// A run that raised NOTHING is unmeasured, not clean. This is the sharp edge: a
// run in which every reviewer errored is the most drifted outcome possible, and
// reporting it as 0.0 would publish it as flawless vocabulary agreement — exactly
// the collapse the pointer field exists to prevent. It must also not divide by
// zero or emit NaN, which would neither serialize nor compare.
func TestOutOfVocabularyRate_NoFindingsIsUnmeasuredNotClean(t *testing.T) {
	assert.Nil(t, OutOfVocabularyRate(nil), "no reviewers -> nothing measured")
	assert.Nil(t, OutOfVocabularyRate([]ReviewerScore{{Model: "m", Persona: "p"}}),
		"a reviewer with no cases -> nothing measured")
	assert.Nil(t, OutOfVocabularyRate([]ReviewerScore{{
		Cases: []CaseScore{{Expected: []string{"correctness"}, Raised: nil}},
	}}), "a total-failure run that raised nothing must not read as perfect agreement")

	// ...while a run that raised findings and drifted on none IS measured, and
	// reports an explicit 0. The two outcomes must stay distinguishable.
	clean := OutOfVocabularyRate([]ReviewerScore{{
		Cases: []CaseScore{{Expected: []string{"correctness"}, Raised: []string{"correctness"}}},
	}})
	require.NotNil(t, clean, "a run with findings is measured")
	assert.InDelta(t, 0.0, *clean, 1e-9)
}

// The rate reaches `atcr benchmark run` consumers through the run result.
func TestRunResult_CarriesOutOfVocabularyRate(t *testing.T) {
	measured := 0.125
	data, err := json.Marshal(RunResult{
		Suite:               "standard-v1",
		SuiteVersion:        "1.0.0",
		GeneratedAt:         "2026-08-10T00:00:00Z",
		OutOfVocabularyRate: &measured,
	})
	require.NoError(t, err)

	var back map[string]any
	require.NoError(t, json.Unmarshal(data, &back))
	assert.Equal(t, 0.125, back["out_of_vocabulary_rate"])
}

// A clean run emits an explicit 0, and a run-result that never carried the field
// stays absent. Those two must not collapse into one another: a producer that
// measured perfect vocabulary agreement is saying something a file predating this
// epic is not.
func TestRunResult_ZeroRateIsEmittedButAnUnmeasuredRunIsNot(t *testing.T) {
	clean := 0.0
	data, err := json.Marshal(RunResult{Suite: "s", SuiteVersion: "1.0.0", OutOfVocabularyRate: &clean})
	require.NoError(t, err)

	var withRate map[string]any
	require.NoError(t, json.Unmarshal(data, &withRate))
	require.Contains(t, withRate, "out_of_vocabulary_rate", "a measured 0.0 must still be published")
	assert.Equal(t, 0.0, withRate["out_of_vocabulary_rate"])

	data, err = json.Marshal(RunResult{Suite: "s", SuiteVersion: "1.0.0"})
	require.NoError(t, err)

	var withoutRate map[string]any
	require.NoError(t, json.Unmarshal(data, &withoutRate))
	assert.NotContains(t, withoutRate, "out_of_vocabulary_rate", "an unmeasured run must not read as clean")

	// And the read direction: a legacy file round-trips to nil, not to 0.
	var legacy RunResult
	require.NoError(t, json.Unmarshal([]byte(`{"suite":"s","suite_version":"1.0.0"}`), &legacy))
	assert.Nil(t, legacy.OutOfVocabularyRate, "an absent key means unmeasured, never clean")
}

// ...but it must NOT reach the public submission envelope. Submission is the
// frozen public contract; adding a diagnostic column there is 35.16.6's decision
// to make, not a side effect of this one.
func TestBuildSubmission_DoesNotPublishOutOfVocabularyRate(t *testing.T) {
	drifted := 0.42
	data, err := json.Marshal(BuildSubmission(RunResult{
		Suite:               "standard-v1",
		SuiteVersion:        "1.0.0",
		OutOfVocabularyRate: &drifted,
	}, time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)))
	require.NoError(t, err)

	var back map[string]any
	require.NoError(t, json.Unmarshal(data, &back))
	assert.NotContains(t, back, "out_of_vocabulary_rate",
		"the public submission schema must not gain a column from this epic")
}

// The ceiling's EXCLUSIVE semantics ("a run sitting exactly on it trips the guard")
// must live in production code, not in whichever operator a test happens to pick.
func TestExceedsVocabularyCeiling(t *testing.T) {
	for _, tc := range []struct {
		name string
		rate *float64
		want bool
	}{
		{name: "unmeasured is not a breach", rate: nil, want: false},
		{name: "clean run", rate: ptr(0.0), want: false},
		{name: "under the ceiling", rate: ptr(MaxOutOfVocabularyRate - 0.01), want: false},
		{name: "exactly on the ceiling trips it", rate: ptr(MaxOutOfVocabularyRate), want: true},
		{name: "over the ceiling", rate: ptr(0.72), want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ExceedsVocabularyCeiling(tc.rate))
		})
	}
}

func ptr(f float64) *float64 { return &f }

// The defect this epic exists to close: micro-averaging is not merely silent about
// WHICH reviewer drifted, it is actively concealing. One reviewer raises 12 findings
// and drifts on every one; its prolific clean peer dilutes them to a pooled rate that
// clears the ceiling, and the run reports clean while one of two models ignored the
// enumeration entirely.
//
// The 300:12 ratio is what the tightened ceiling costs an attacker of this metric — the
// plan's original 80:12 illustration pooled to 0.130 and cleared the OLD 0.20 guard, but
// not 0.05. Tightening narrows the concealment window; it does not close it, which is
// exactly why the breakdown is needed alongside the tighter number rather than instead
// of it.
//
// The run-level scalar and the breakdown are asserted TOGETHER here on purpose: the
// point is not that the breakdown reports 1.0 somewhere, it is that it reports 1.0 for
// a run whose published scalar says everything is fine.
func TestPerReviewerVocabulary_NamesTheReviewerTheRunLevelRateConceals(t *testing.T) {
	threeHundredClean := make([]string, 300)
	for i := range threeHundredClean {
		threeHundredClean[i] = reconcile.CategoryCorrectness
	}
	twelveDrifted := make([]string, 12)
	for i := range twelveDrifted {
		twelveDrifted[i] = "bug" // a merge-table word, not a taxonomy member
	}

	reviewers := []ReviewerScore{
		{Model: "clean-model", Persona: "a", Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness}, Raised: threeHundredClean,
		}}},
		{Model: "drifted-model", Persona: "b", Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness}, Raised: twelveDrifted,
		}}},
	}

	run := mustRate(t, OutOfVocabularyRate(reviewers))
	require.InDelta(t, 12.0/312.0, run, 1e-9, "12 drifted of 312 pooled findings")
	require.False(t, ExceedsVocabularyCeiling(&run),
		"precondition: this run passes the guard — that is what makes the breakdown necessary")

	got := PerReviewerVocabulary(reviewers)
	require.Len(t, got, 2)

	assert.Equal(t, "clean-model", got[0].Model)
	assert.Equal(t, 300, got[0].Findings)
	assert.Equal(t, 0, got[0].Drifted)
	require.NotNil(t, got[0].Rate)
	assert.InDelta(t, 0.0, *got[0].Rate, 1e-9)

	assert.Equal(t, "drifted-model", got[1].Model)
	assert.Equal(t, "b", got[1].Persona)
	assert.Equal(t, 12, got[1].Findings)
	assert.Equal(t, 12, got[1].Drifted)
	require.NotNil(t, got[1].Rate)
	assert.InDelta(t, 1.0, *got[1].Rate, 1e-9,
		"the reviewer the run-level rate hides is reported at its own true rate")
}

// Each entry's rate is that reviewer's OWN pooled share — computed exactly as the
// run-level function computes the run's, over that reviewer's findings alone. It is
// NOT a per-case macro-average: a reviewer's 2-finding case must not weigh as much as
// its 80-finding one, for the same reason the run-level rate pools across reviewers.
func TestPerReviewerVocabulary_EntryRateIsMicroAveragedAcrossThatReviewersCases(t *testing.T) {
	eightyClean := make([]string, 80)
	for i := range eightyClean {
		eightyClean[i] = reconcile.CategoryCorrectness
	}

	got := PerReviewerVocabulary([]ReviewerScore{{
		Model: "m", Persona: "p",
		Cases: []CaseScore{
			{Expected: []string{reconcile.CategoryCorrectness}, Raised: []string{"bug", "clarity"}},
			{Expected: []string{reconcile.CategoryCorrectness}, Raised: eightyClean},
		},
	}})

	require.Len(t, got, 1)
	require.NotNil(t, got[0].Rate)
	assert.InDelta(t, 2.0/82.0, *got[0].Rate, 1e-9,
		"the entry pools this reviewer's findings; a per-case average would report 0.5")
	assert.Greater(t, math.Abs(*got[0].Rate-0.5), 1e-6,
		"0.5 is the per-case macro-average this test exists to reject")
}

// AC3. The all-`other` signature is drift 0.0 paired with recall 0.0 — indistinguishable
// from a genuinely clean reviewer on the rate alone, which is exactly the recorded blind
// spot. routing_values is the discriminator: it separates the two WITHOUT redefining
// OutOfVocabularyRate, whose contract (every taxonomy member, `other` included, is in
// vocabulary) is asserted here to be untouched.
func TestPerReviewerVocabulary_RoutingValueCountDistinguishesAllOtherFromClean(t *testing.T) {
	reviewers := []ReviewerScore{
		{Model: "all-other", Persona: "p", Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness},
			Raised: []string{
				reconcile.CategoryOther, reconcile.CategoryOther, reconcile.CategoryOutOfScope,
			},
		}}},
		{Model: "genuinely-clean", Persona: "p", Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness},
			Raised: []string{
				reconcile.CategoryCorrectness, reconcile.CategoryCorrectness, reconcile.CategorySecurity,
			},
		}}},
	}

	got := PerReviewerVocabulary(reviewers)
	require.Len(t, got, 2)

	allOther, clean := got[0], got[1]
	require.Equal(t, "all-other", allOther.Model)
	require.Equal(t, "genuinely-clean", clean.Model)

	// The rate CANNOT tell them apart — that is the blind spot, restated at reviewer level.
	require.NotNil(t, allOther.Rate)
	require.NotNil(t, clean.Rate)
	assert.InDelta(t, 0.0, *allOther.Rate, 1e-9,
		"`other`/`out-of-scope` are taxonomy members: still zero drift, by design")
	assert.InDelta(t, *clean.Rate, *allOther.Rate, 1e-9,
		"identical rates is the premise — the discriminator must come from elsewhere")

	// routing_values does.
	assert.Equal(t, 3, allOther.RoutingValues,
		"every finding routed rather than categorized")
	assert.Equal(t, 0, clean.RoutingValues)
	assert.Equal(t, allOther.Findings, allOther.RoutingValues,
		"routing_values == findings alongside rate 0.0 IS the all-`other` signature")

	// And the run-level function's own definition is unchanged by this epic: an
	// all-`other` reviewer must still report 0.0 from OutOfVocabularyRate specifically.
	only := mustRate(t, OutOfVocabularyRate(reviewers[:1]))
	assert.InDelta(t, 0.0, only, 1e-9,
		"AC3 is closed by the routing count, never by excluding routing values from the vocabulary")
}

// The paired half of the signature. Recall is NOT duplicated into the breakdown — it
// already sits on Reviewers[i].CorroborationRate, produced from the same []ReviewerScore
// at the same call site — so a consumer reads the pairing by correlating the two arrays
// positionally. This pins that the correlation actually holds.
func TestPerReviewerVocabulary_IsPositionallyAlignedWithScore(t *testing.T) {
	reviewers := []ReviewerScore{
		// Deliberately supplied out of sorted order: Score sorts its output, so an
		// implementation that walks the INPUT order would misalign here.
		{Model: "zeta", Persona: "p", Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness},
			Raised:   []string{reconcile.CategoryOther, reconcile.CategoryOther},
		}}},
		{Model: "alpha", Persona: "p", Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness},
			Raised:   []string{reconcile.CategoryCorrectness},
		}}},
	}

	scored := Score(reviewers)
	vocab := PerReviewerVocabulary(reviewers)
	require.Len(t, vocab, len(scored))

	for i := range scored {
		assert.Equal(t, scored[i].Model, vocab[i].Model, "row %d model", i)
		assert.Equal(t, scored[i].Persona, vocab[i].Persona, "row %d persona", i)
	}

	// The all-`other` reviewer is "zeta", which sorts second. Its paired zeros are
	// readable only because the two arrays line up.
	assert.Equal(t, "zeta", vocab[1].Model)
	assert.Equal(t, 2, vocab[1].RoutingValues)
	assert.InDelta(t, 0.0, scored[1].CorroborationRate, 1e-9,
		"drift 0.0 with recall 0.0 — the signature, read across the two aligned arrays")
}

// The breakdown and the scalar must never be two differently-defined numbers wearing
// the same name. Summing the entries reproduces the run-level numerator and
// denominator exactly — so the published scalar is precisely these rows divided,
// and any future edit that changes membership, normalization, or the empty-category
// rule in one function without the other fails here.
func TestPerReviewerVocabulary_TotalsReproduceTheRunLevelRate(t *testing.T) {
	reviewers := []ReviewerScore{
		{Model: "a", Persona: "p", Cases: []CaseScore{
			{Expected: []string{reconcile.CategoryCorrectness}, Raised: []string{"bug", reconcile.CategoryCorrectness, ""}},
			{Expected: []string{reconcile.CategorySecurity}, Raised: []string{"  SECURITY ", "clarity"}},
		}},
		{Model: "b", Persona: "p", Cases: []CaseScore{
			{Expected: []string{reconcile.CategoryCorrectness}, Raised: []string{reconcile.CategoryOther, "input"}},
		}},
		{Model: "c", Persona: "p"}, // raised nothing — contributes to neither total
	}

	var findings, drifted int
	for _, e := range PerReviewerVocabulary(reviewers) {
		findings += e.Findings
		drifted += e.Drifted
	}
	require.Equal(t, 7, findings, "every raised finding is counted exactly once, blanks included")
	require.Equal(t, 4, drifted, "bug, blank, clarity, input")

	assert.InDelta(t, float64(drifted)/float64(findings),
		mustRate(t, OutOfVocabularyRate(reviewers)), 1e-9,
		"the scalar is the summed rows divided; the two definitions must not diverge")
}

// Two reviewers sharing a (model, persona) pair is the case the alignment most needs
// to survive, and the case a key join CANNOT: the join has no way to tell the rows
// apart. Score's sort is stable for exactly this reason, and this array mirrors that
// sort rather than re-deriving the pairing, so the tied rows stay in the caller's
// order on both sides and entry i still describes Reviewers[i].
func TestPerReviewerVocabulary_IdentityTieKeepsAlignmentWithScore(t *testing.T) {
	tied := []ReviewerScore{
		// Same (model, persona) twice, distinguishable ONLY by position: the first
		// drifted entirely, the second is clean.
		{Model: "twin", Persona: "p", Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness},
			Raised:   []string{"bug", "clarity"},
		}}},
		{Model: "twin", Persona: "p", Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness},
			Raised:   []string{reconcile.CategoryCorrectness, reconcile.CategoryCorrectness, reconcile.CategoryCorrectness, reconcile.CategoryCorrectness},
		}}},
	}

	scored := Score(tied)
	vocab := PerReviewerVocabulary(tied)
	require.Len(t, vocab, 2)
	require.Len(t, scored, 2)

	// Score's own rows are distinguishable by volume; the breakdown's must line up
	// with the same row, not merely with the same identity.
	require.InDelta(t, 2.0, scored[0].FindingsRaisedAvg, 1e-9, "input order preserved on the Score side")
	require.InDelta(t, 4.0, scored[1].FindingsRaisedAvg, 1e-9)

	require.NotNil(t, vocab[0].Rate)
	assert.InDelta(t, 1.0, *vocab[0].Rate, 1e-9, "the drifted twin is entry 0, matching Reviewers[0]")
	assert.Equal(t, 2, vocab[0].Findings)

	require.NotNil(t, vocab[1].Rate)
	assert.InDelta(t, 0.0, *vocab[1].Rate, 1e-9, "the clean twin is entry 1")
	assert.Equal(t, 4, vocab[1].Findings)
}

// Alignment survives the identity scrub. Score re-scrubs every row it emits, so an
// identity the scrub REWRITES sorts by its post-scrub value there; a breakdown that
// sorted the raw value would land the rows in a different order and silently
// mis-attribute the drift. It also must not republish the pre-scrub string.
func TestPerReviewerVocabulary_ScrubbedIdentityStillAlignsWithScore(t *testing.T) {
	// The pair is chosen so the scrub INVERTS the order: raw, "~/tmp/zzz alpha" sorts
	// AFTER "beta" ('~' is 0x7E), but it scrubs to "alpha", which sorts BEFORE it. A
	// breakdown sorted on the raw identity therefore lands the drifted reviewer at the
	// opposite index from Score's — a fixture whose raw and scrubbed values happen to
	// sort alike could not detect that.
	reviewers := []ReviewerScore{
		{Model: "~/tmp/zzz alpha", Persona: "p", Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness}, Raised: []string{"bug"},
		}}},
		{Model: "beta", Persona: "p", Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness}, Raised: []string{reconcile.CategoryCorrectness},
		}}},
	}

	scored := Score(reviewers)
	vocab := PerReviewerVocabulary(reviewers)
	require.Len(t, vocab, 2)

	for i := range scored {
		assert.Equal(t, scored[i].Model, vocab[i].Model, "row %d must carry Score's post-scrub identity", i)
	}
	assert.Equal(t, "alpha", vocab[0].Model, "the scrubbed identity, in its post-scrub position")
	require.NotNil(t, vocab[0].Rate)
	assert.InDelta(t, 1.0, *vocab[0].Rate, 1e-9,
		"the drifted reviewer's own rate follows it to its post-scrub position")

	for _, e := range vocab {
		assert.NotContains(t, e.Model, "~/tmp",
			"the diagnostic array must not republish an identity the public rows scrubbed")
	}
}

// A reviewer that raised nothing is UNMEASURED, not clean — the same nil-vs-zero
// distinction the run-level pointer carries, applied per row. A failed reviewer
// reporting rate 0.0 would publish the most drifted possible row as flawless.
func TestPerReviewerVocabulary_ReviewerWithNoFindingsIsUnmeasured(t *testing.T) {
	got := PerReviewerVocabulary([]ReviewerScore{
		{Model: "failed", Persona: "p", Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness}, Raised: nil,
		}}},
		{Model: "no-cases", Persona: "p"},
	})

	require.Len(t, got, 2, "a reviewer that raised nothing still gets a row — absence is not a row")
	for _, e := range got {
		assert.Nil(t, e.Rate, "%s raised nothing, so its rate is unmeasured", e.Model)
		assert.Equal(t, 0, e.Findings)
		assert.Equal(t, 0, e.Drifted)
	}

	// ...and the marshalled row omits the key rather than emitting 0.0.
	data, err := json.Marshal(got[0])
	require.NoError(t, err)
	var back map[string]any
	require.NoError(t, json.Unmarshal(data, &back))
	assert.NotContains(t, back, "rate", "an unmeasured row must not read as clean")
	assert.Contains(t, back, "routing_values",
		"a real zero routing count is a measurement and must stay explicit")
}

// No reviewers at all means no array, not an empty one: omitempty drops the key so a
// run-result predating this epic and a run with nothing to report read alike.
func TestPerReviewerVocabulary_EmptyRunEmitsNoKey(t *testing.T) {
	assert.Empty(t, PerReviewerVocabulary(nil))

	data, err := json.Marshal(RunResult{Suite: "s", SuiteVersion: "1.0.0"})
	require.NoError(t, err)
	var back map[string]any
	require.NoError(t, json.Unmarshal(data, &back))
	assert.NotContains(t, back, "reviewer_vocabulary")
}

// AC1, publish half: the breakdown reaches `atcr benchmark run` consumers through the
// run result under its own top-level key.
func TestRunResult_CarriesPerReviewerVocabulary(t *testing.T) {
	rate := 1.0
	data, err := json.Marshal(RunResult{
		Suite:        "standard-v1",
		SuiteVersion: "1.0.0",
		GeneratedAt:  "2026-08-14T00:00:00Z",
		Vocabulary: []ReviewerVocabulary{{
			Model: "drifted-model", Persona: "b",
			Findings: 12, Drifted: 12, Rate: &rate, RoutingValues: 0,
		}},
	})
	require.NoError(t, err)

	var back map[string]any
	require.NoError(t, json.Unmarshal(data, &back))
	rows, ok := back["reviewer_vocabulary"].([]any)
	require.True(t, ok, "the run result must carry the breakdown under its own key")
	require.Len(t, rows, 1)

	row, ok := rows[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "drifted-model", row["model"])
	assert.Equal(t, "b", row["persona"])
	assert.Equal(t, float64(12), row["findings"])
	assert.Equal(t, float64(12), row["drifted"])
	assert.Equal(t, 1.0, row["rate"])
	assert.Equal(t, float64(0), row["routing_values"])
}

// AC1, exclusion half. BuildSubmission constructs Submission field-by-field, so the new
// key is excluded BY CONSTRUCTION — this test pins that it stays excluded rather than
// making it so. A diagnostic array is not a public leaderboard column.
func TestBuildSubmission_DoesNotPublishPerReviewerVocabulary(t *testing.T) {
	rate := 0.42
	data, err := json.Marshal(BuildSubmission(RunResult{
		Suite:        "standard-v1",
		SuiteVersion: "1.0.0",
		Vocabulary: []ReviewerVocabulary{{
			Model: "m", Persona: "p", Findings: 10, Drifted: 4, Rate: &rate,
		}},
	}, time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)))
	require.NoError(t, err)

	var back map[string]any
	require.NoError(t, json.Unmarshal(data, &back))
	assert.NotContains(t, back, "reviewer_vocabulary",
		"the public submission schema must not gain a diagnostic array from this epic")
}

// AC1, frozen-schema half. scorecard.PublicRecord is the schema shared byte-for-byte
// between `benchmark export` and production `leaderboard --export`; this epic adds a
// per-reviewer diagnostic and must not have reached for that type to carry it. The key
// set is enumerated literally so ANY addition — not just this epic's — fails here and
// has to be a deliberate edit.
func TestPublicRecord_JSONKeySetIsUnchangedByThisEpic(t *testing.T) {
	survived, cost := 0.9, 1.25
	data, err := json.Marshal(scorecard.PublicRecord{
		Model: "m", Persona: "p", Runs: 3,
		FindingsRaisedAvg: 2, CorroborationRate: 0.5,
		SurvivedSkepticRate:           &survived,
		CostPerCorroboratedFindingUSD: &cost,
		LatencyP50MS:                  120,
	})
	require.NoError(t, err)

	var back map[string]any
	require.NoError(t, json.Unmarshal(data, &back))

	keys := make([]string, 0, len(back))
	for k := range back {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	assert.Equal(t, []string{
		"corroboration_rate",
		"cost_per_corroborated_finding_usd",
		"findings_raised_avg",
		"latency_p50_ms",
		"model",
		"persona",
		"runs",
		"survived_skeptic_rate",
	}, keys, "the frozen public reviewer schema gained or lost a column")
}

// A reviewer that labels EVERY finding `other` reports perfect vocabulary
// agreement while conveying no categorical information — `other` is a taxonomy
// member, so it is in vocabulary by construction.
//
// This test does NOT assert the desirable behaviour; it pins the KNOWN one, so the
// blind spot is a recorded decision rather than an accident, and so a future change
// to how routing values are treated shows up here as a deliberate edit. The paired
// zeros are the signature: drift 0.0 alongside recall 0.0 means "categorized
// nothing", not "categorized everything correctly".
func TestOutOfVocabularyRate_AllOtherIsAKnownBlindSpot(t *testing.T) {
	reviewers := []ReviewerScore{{
		Model: "m", Persona: "p",
		Cases: []CaseScore{{
			Expected: []string{reconcile.CategoryCorrectness},
			Raised: []string{
				reconcile.CategoryOther, reconcile.CategoryOther, reconcile.CategoryOther,
			},
		}},
	}}

	rate := OutOfVocabularyRate(reviewers)
	require.NotNil(t, rate)
	assert.InDelta(t, 0.0, *rate, 1e-9,
		"`other` is a taxonomy member, so an all-`other` run currently reads as zero drift")

	got := Score(reviewers)
	require.Len(t, got, 1)
	assert.InDelta(t, 0.0, got[0].CorroborationRate, 1e-9,
		"`other` is hard-excluded from every family, so the same run detects nothing — "+
			"drift 0.0 with recall 0.0 is the all-`other` signature, not a clean run")
}
