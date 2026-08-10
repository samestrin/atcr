package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
type recordedRun struct {
	Suite        string         `json:"suite"`
	SuiteVersion string         `json:"suite_version"`
	Cases        []recordedCase `json:"cases"`
}

type recordedCase struct {
	CaseID    string             `json:"case_id"`
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

// A recorded run whose reviewers stayed inside the closed vocabulary passes.
func TestOutOfVocabularyRate_CleanRunIsUnderTheCeiling(t *testing.T) {
	rate := OutOfVocabularyRate(loadRecordedRun(t, "run-clean.json"))

	assert.InDelta(t, 0.0, rate, 1e-9, "every raised category is a taxonomy member")
	assert.Less(t, rate, MaxOutOfVocabularyRate)
}

// A recorded run whose reviewers ignored the enumeration FAILS. This is the
// assertion AC3 names: model drift away from the vocabulary must not be silent.
func TestOutOfVocabularyRate_DriftedRunExceedsTheCeiling(t *testing.T) {
	rate := OutOfVocabularyRate(loadRecordedRun(t, "run-drifted.json"))

	assert.InDelta(t, 0.6, rate, 1e-9, "12 of 20 findings used a non-member word")
	assert.GreaterOrEqual(t, rate, MaxOutOfVocabularyRate,
		"a run this drifted must trip the guard")
}

// The boundary pair. These two fixtures differ by a single in-vocabulary finding
// (4/21 vs 4/20), so an inverted or misread comparison cannot satisfy both — which
// two far-apart extremes would.
func TestOutOfVocabularyRate_BoundaryPair(t *testing.T) {
	under := OutOfVocabularyRate(loadRecordedRun(t, "run-boundary-under.json"))
	at := OutOfVocabularyRate(loadRecordedRun(t, "run-boundary-at.json"))

	assert.InDelta(t, 4.0/21.0, under, 1e-9)
	assert.Less(t, under, MaxOutOfVocabularyRate, "just under the ceiling passes")

	assert.InDelta(t, 0.20, at, 1e-9)
	assert.GreaterOrEqual(t, at, MaxOutOfVocabularyRate,
		"the ceiling is exclusive: a run AT the threshold must trip the guard")
}

// The denominator is FINDINGS, not distinct categories — matching CaseScore.Raised's
// documented one-entry-per-finding semantics. A distinct-category denominator would
// let one prolific in-vocabulary category mask many drifted findings.
func TestOutOfVocabularyRate_DenominatorCountsFindingsNotCategories(t *testing.T) {
	rate := OutOfVocabularyRate([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		Cases: []CaseScore{{
			Expected: []string{"correctness"},
			// 2 distinct categories, 5 findings, 1 of which is out of vocabulary.
			Raised: []string{"correctness", "correctness", "correctness", "correctness", "bug"},
		}},
	}})

	assert.InDelta(t, 1.0/5.0, rate, 1e-9, "1 drifted finding of 5, not 1 of 2 distinct categories")
}

// A finding with no category at all is drift. Excluding it would produce a rate
// that IMPROVES when a reviewer stops labelling entirely.
func TestOutOfVocabularyRate_EmptyCategoryCountsAsDrift(t *testing.T) {
	rate := OutOfVocabularyRate([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		Cases:   []CaseScore{{Expected: []string{"correctness"}, Raised: []string{"correctness", "", "   "}}},
	}})

	assert.InDelta(t, 2.0/3.0, rate, 1e-9, "blank and whitespace-only categories are out of vocabulary")
}

// Membership is decided against the live taxonomy after the scorer's own
// normalize, so case and padding do not create phantom drift.
func TestOutOfVocabularyRate_MembershipIsNormalized(t *testing.T) {
	rate := OutOfVocabularyRate([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		Cases:   []CaseScore{{Expected: []string{"security"}, Raised: []string{"  SECURITY ", "Input-Validation"}}},
	}})

	assert.InDelta(t, 0.0, rate, 1e-9, "normalized members are in vocabulary")
}

// Every member of the taxonomy is in vocabulary by definition — including the two
// routing values, which are legal emissions even though they satisfy no expected
// category. A reviewer that reaches for `other` read its prompt; that is not drift.
func TestOutOfVocabularyRate_EveryTaxonomyMemberIsInVocabulary(t *testing.T) {
	rate := OutOfVocabularyRate([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		Cases:   []CaseScore{{Expected: []string{"correctness"}, Raised: reconcile.Categories()}},
	}})

	assert.InDelta(t, 0.0, rate, 1e-9, "no member of the closed vocabulary may count as drift")
}

// A run that raised nothing has no rate to report; 0 is the only value that does
// not divide by zero, and it must not read as a failure.
func TestOutOfVocabularyRate_NoFindingsIsZeroNotNaN(t *testing.T) {
	assert.InDelta(t, 0.0, OutOfVocabularyRate(nil), 1e-9)
	assert.InDelta(t, 0.0, OutOfVocabularyRate([]ReviewerScore{{Model: "m", Persona: "p"}}), 1e-9)
	assert.InDelta(t, 0.0, OutOfVocabularyRate([]ReviewerScore{{
		Cases: []CaseScore{{Expected: []string{"correctness"}, Raised: nil}},
	}}), 1e-9)
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
