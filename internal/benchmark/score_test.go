package benchmark

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Score folds each reviewer's per-case category outcomes into the public reviewer
// schema: CorroborationRate carries macro-averaged category recall, FindingsRaisedAvg
// the mean findings per case, and Runs the number of cases scored.
func TestScore_RecallAndVolume(t *testing.T) {
	got := Score([]ReviewerScore{
		{
			Model:   "claude-sonnet-4-6",
			Persona: "bruce",
			Cases: []CaseScore{
				// case A: all expected categories surfaced -> recall 1.0; 2 findings.
				{Expected: []string{"correctness"}, Raised: []string{"correctness", "security"}},
				// case B: 1 of 2 expected categories surfaced -> recall 0.5; 1 finding.
				{Expected: []string{"security", "correctness"}, Raised: []string{"security"}},
			},
		},
	})

	require.Len(t, got, 1)
	r := got[0]
	assert.Equal(t, "claude-sonnet-4-6", r.Model)
	assert.Equal(t, "bruce", r.Persona)
	assert.Equal(t, 2, r.Runs, "runs == number of cases scored")
	assert.InDelta(t, 1.5, r.FindingsRaisedAvg, 1e-9, "mean findings per case == 3/2")
	assert.InDelta(t, 0.75, r.CorroborationRate, 1e-9, "macro-avg recall == (1.0 + 0.5) / 2")
	require.NotNil(t, r.CostPerCorroboratedFindingUSD, "matched findings exist -> key present even at 0 cost")
	assert.InDelta(t, 0.0, *r.CostPerCorroboratedFindingUSD, 1e-9, "no usage -> real 0 cost, not omitted")
	assert.Equal(t, int64(0), r.LatencyP50MS)
	assert.Nil(t, r.SurvivedSkepticRate, "no skeptic verification in the benchmark path")
}

// Category matching is case-insensitive and whitespace-trimmed on both sides,
// matching reconcile.ModalCategory normalization.
func TestScore_CategoryMatchNormalized(t *testing.T) {
	got := Score([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		Cases:   []CaseScore{{Expected: []string{"Security"}, Raised: []string{"  security  "}}},
	}})
	require.Len(t, got, 1)
	assert.InDelta(t, 1.0, got[0].CorroborationRate, 1e-9, "normalized categories match")
}

// Duplicate expected categories are deduplicated so recall denominator counts
// DISTINCT expected categories; duplicate raised findings still inflate volume.
func TestScore_DistinctExpectedDuplicateRaised(t *testing.T) {
	got := Score([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		Cases:   []CaseScore{{Expected: []string{"correctness", "correctness"}, Raised: []string{"correctness", "correctness", "correctness"}}},
	}})
	require.Len(t, got, 1)
	assert.InDelta(t, 1.0, got[0].CorroborationRate, 1e-9, "1 distinct expected, surfaced -> recall 1.0")
	assert.InDelta(t, 3.0, got[0].FindingsRaisedAvg, 1e-9, "3 findings over 1 case")
}

// Cost-per-corroborated divides recorded cost by the number of findings whose
// category matched an expected (planted) category; 0 when nothing matched.
func TestScore_CostPerCorroborated(t *testing.T) {
	got := Score([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		CostUSD: 0.12,
		// 2 matched findings (correctness, security both expected), 1 unmatched (perf).
		Cases: []CaseScore{{Expected: []string{"correctness", "security"}, Raised: []string{"correctness", "security", "perf"}}},
	}})
	require.Len(t, got, 1)
	require.NotNil(t, got[0].CostPerCorroboratedFindingUSD)
	assert.InDelta(t, 0.06, *got[0].CostPerCorroboratedFindingUSD, 1e-9, "0.12 / 2 matched findings")
}

// A priced reviewer that matches zero planted categories must leave the field
// nil (key omitted) — the exact ambiguity this epic exists to fix: paid-but-
// uncorroborated must not read identically to genuinely free.
func TestScore_CostPerCorroboratedNilWhenPaidButUnmatched(t *testing.T) {
	got := Score([]ReviewerScore{{
		Model:   "m",
		Persona: "p",
		CostUSD: 0.5,
		Cases:   []CaseScore{{Expected: []string{"correctness"}, Raised: []string{"perf"}}},
	}})
	require.Len(t, got, 1)
	assert.Nil(t, got[0].CostPerCorroboratedFindingUSD, "paid but zero matched findings -> key must be absent, not 0.0")
}

// Invalid CostUSD values (negative, NaN, +Inf) must not propagate into the
// public record; the field is omitted instead of emitting garbage that breaks
// JSON export.
func TestScore_CostUSDInvalid(t *testing.T) {
	cases := []struct {
		name    string
		costUSD float64
	}{
		{name: "negative", costUSD: -1.0},
		{name: "NaN", costUSD: math.NaN()},
		{name: "positive infinity", costUSD: math.Inf(1)},
		{name: "negative infinity", costUSD: math.Inf(-1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Score([]ReviewerScore{{
				Model:   "m",
				Persona: "p",
				CostUSD: tc.costUSD,
				Cases:   []CaseScore{{Expected: []string{"correctness"}, Raised: []string{"correctness"}}},
			}})
			require.Len(t, got, 1)
			assert.Nil(t, got[0].CostPerCorroboratedFindingUSD, "invalid cost -> field omitted")
		})
	}
}

// A reviewer that raised nothing scores recall 0 without dividing by zero, and a
// reviewer with no cases is fully zeroed (Runs 0, no NaN).
func TestScore_EmptyInputsNoPanic(t *testing.T) {
	got := Score([]ReviewerScore{
		{Model: "a", Persona: "p", Cases: []CaseScore{{Expected: []string{"correctness"}, Raised: nil}}},
		{Model: "b", Persona: "p", Cases: nil},
	})
	require.Len(t, got, 2)
	// sorted by (model, persona): "a" then "b".
	assert.Equal(t, "a", got[0].Model)
	assert.InDelta(t, 0.0, got[0].CorroborationRate, 1e-9)
	assert.InDelta(t, 0.0, got[0].FindingsRaisedAvg, 1e-9)
	assert.Equal(t, "b", got[1].Model)
	assert.Equal(t, 0, got[1].Runs)
	assert.InDelta(t, 0.0, got[1].FindingsRaisedAvg, 1e-9, "no cases -> 0, not NaN")
}

// Two reviewers sharing the same (model, persona) preserve the input order
// across repeated Score calls — a stable sort keeps the output byte-identical
// even on an identity tie (the reproducibility AC).
func TestScore_StableOnIdentityTie(t *testing.T) {
	in := []ReviewerScore{
		{Model: "m", Persona: "p", Cases: []CaseScore{{Expected: []string{"a"}, Raised: []string{"a"}}}},
		{Model: "m", Persona: "p", Cases: []CaseScore{{Expected: []string{"a"}, Raised: nil}}},
	}
	first := Score(in)
	second := Score(in)
	require.Len(t, first, 2)
	// run-1 (recall 1.0) stays ahead of run-2 (recall 0.0), every time.
	assert.InDelta(t, 1.0, first[0].CorroborationRate, 1e-9)
	assert.InDelta(t, 0.0, first[1].CorroborationRate, 1e-9)
	assert.Equal(t, first, second, "repeated scoring is byte-identical")
}

// Records are sorted deterministically by (model, persona) so the same input
// always yields byte-identical output, and identity fields are re-scrubbed via
// scorecard.ScrubPublicRecord (PII in an identity string is removed).
func TestScore_SortedAndScrubbed(t *testing.T) {
	got := Score([]ReviewerScore{
		{Model: "zeta", Persona: "p", Cases: []CaseScore{{Expected: []string{"x"}, Raised: []string{"x"}}}},
		{Model: "alpha", Persona: "p", Cases: []CaseScore{{Expected: []string{"x"}, Raised: []string{"x"}}}},
		// PII embedded in the persona must be scrubbed on the way out.
		{Model: "alpha", Persona: "bruce edward.estrin@gmail.com", Cases: []CaseScore{{Expected: []string{"x"}, Raised: []string{"x"}}}},
	})
	require.Len(t, got, 3)
	assert.Equal(t, "alpha", got[0].Model)
	assert.Equal(t, "alpha", got[1].Model)
	assert.Equal(t, "zeta", got[2].Model)
	// the email is gone; "bruce" survives.
	assert.Equal(t, "bruce", got[0].Persona)
}
