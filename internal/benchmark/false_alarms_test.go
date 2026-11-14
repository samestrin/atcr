package benchmark

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC3 (epic 113), counting half: a finding whose category satisfies no expected
// category of its case is a false alarm and is counted.
func TestFalseAlarms_CountsAKnownUnmatchedFinding(t *testing.T) {
	got := FalseAlarms([]ReviewerScore{{
		Model: "m", Persona: "p",
		Cases: []CaseScore{{
			Expected: []string{"security"},
			// `performance` matches nothing planted in this case.
			Raised: []string{"security", "performance"},
		}},
	}})

	require.Len(t, got, 1)
	assert.Equal(t, 1, got[0].Cases)
	assert.Equal(t, 2, got[0].Findings)
	assert.Equal(t, 1, got[0].Unmatched, "the known-unmatched finding must be counted")
	require.NotNil(t, got[0].UnmatchedPerCase)
	assert.InDelta(t, 1.0, *got[0].UnmatchedPerCase, 1e-9)
}

// AC3 (epic 113), not-counting half: a matched finding is never a false alarm —
// including one matched through an equivalence family rather than by exact word,
// since the false-alarm count and recall must agree on what a match is.
func TestFalseAlarms_DoesNotCountAKnownMatchedFinding(t *testing.T) {
	got := FalseAlarms([]ReviewerScore{{
		Model: "m", Persona: "p",
		Cases: []CaseScore{
			{Expected: []string{"security"}, Raised: []string{"security"}},
			// `style` satisfies an expected `maintainability` (equivalence.go).
			{Expected: []string{"maintainability"}, Raised: []string{"style"}},
		},
	}})

	require.Len(t, got, 1)
	assert.Equal(t, 2, got[0].Findings)
	assert.Equal(t, 0, got[0].Unmatched, "a matched finding must not be counted")
	require.NotNil(t, got[0].UnmatchedPerCase, "a measured zero is emitted, not omitted")
	assert.InDelta(t, 0.0, *got[0].UnmatchedPerCase, 1e-9)
}

// The figure is per case: unmatched findings over cases scored, the same
// denominator findings_raised_avg uses.
func TestFalseAlarms_PerCaseIsUnmatchedOverCases(t *testing.T) {
	got := FalseAlarms([]ReviewerScore{{
		Model: "m", Persona: "p",
		Cases: []CaseScore{
			{Expected: []string{"correctness"}, Raised: []string{"security", "performance", "correctness"}},
			{Expected: []string{"correctness"}, Raised: []string{"naming"}},
			{Expected: []string{"correctness"}, Raised: nil},
			{Expected: []string{"correctness"}, Raised: []string{"correctness"}},
		},
	}})

	require.Len(t, got, 1)
	assert.Equal(t, 4, got[0].Cases)
	assert.Equal(t, 6, got[0].Findings)
	assert.Equal(t, 3, got[0].Unmatched)
	require.NotNil(t, got[0].UnmatchedPerCase)
	assert.InDelta(t, 0.75, *got[0].UnmatchedPerCase, 1e-9)
}

// Every finding in a case that plants nothing is unmatched: there is nothing for it
// to match. That is the upper-bound property, pinned so it is a decision rather than
// an accident.
func TestFalseAlarms_CaseWithNoExpectedCategoriesCountsEveryFinding(t *testing.T) {
	got := FalseAlarms([]ReviewerScore{{
		Model: "m", Persona: "p",
		Cases: []CaseScore{{Expected: nil, Raised: []string{"security", "correctness"}}},
	}})
	require.Len(t, got, 1)
	assert.Equal(t, 2, got[0].Unmatched)
}

// An empty category matches nothing, so it counts. Otherwise a reviewer would lower
// its false alarms by dropping its labels.
func TestFalseAlarms_EmptyCategoryIsUnmatched(t *testing.T) {
	got := FalseAlarms([]ReviewerScore{{
		Model: "m", Persona: "p",
		Cases: []CaseScore{{Expected: []string{"security"}, Raised: []string{"", "  "}}},
	}})
	require.Len(t, got, 1)
	assert.Equal(t, 2, got[0].Unmatched)
}

// Matching is case- and whitespace-insensitive, exactly as it is for recall.
func TestFalseAlarms_MatchingIsNormalized(t *testing.T) {
	got := FalseAlarms([]ReviewerScore{{
		Model: "m", Persona: "p",
		Cases: []CaseScore{{Expected: []string{" Security "}, Raised: []string{"SECURITY"}}},
	}})
	require.Len(t, got, 1)
	assert.Equal(t, 0, got[0].Unmatched)
}

// Unmatched and the cost denominator are complements over the same findings: the
// two must never disagree about which finding matched. A $6 reviewer with 4 findings
// of which 1 is unmatched pays $2 per corroborated finding.
func TestFalseAlarms_ComplementsTheCostDenominator(t *testing.T) {
	reviewers := []ReviewerScore{{
		Model: "m", Persona: "p", CostUSD: 6,
		Cases: []CaseScore{
			{Expected: []string{"maintainability"}, Raised: []string{"style", "naming", "security"}},
			{Expected: []string{"correctness"}, Raised: []string{"correctness"}},
		},
	}}
	fa := FalseAlarms(reviewers)
	sc := Score(reviewers)
	require.Len(t, fa, 1)
	require.Len(t, sc, 1)
	require.NotNil(t, sc[0].CostPerCorroboratedFindingUSD)

	matched := fa[0].Findings - fa[0].Unmatched
	assert.Equal(t, 1, fa[0].Unmatched)
	assert.InDelta(t, 6.0/float64(matched), *sc[0].CostPerCorroboratedFindingUSD, 1e-9)
}

// A reviewer with no cases has no per-case figure: nil, not a 0.0 that would read
// as "no false alarms".
func TestFalseAlarms_NoCasesIsUnmeasured(t *testing.T) {
	got := FalseAlarms([]ReviewerScore{{Model: "m", Persona: "p"}})
	require.Len(t, got, 1)
	assert.Nil(t, got[0].UnmatchedPerCase)
	assert.Nil(t, FalseAlarms(nil), "no reviewers -> no array, so the key is omitted")
}

// Rows are in Score's (model, persona) order, on the scrubbed identity, so
// reviewer_false_alarms[i] describes reviewers[i].
func TestFalseAlarms_IsPositionallyAlignedWithScore(t *testing.T) {
	reviewers := []ReviewerScore{
		{Model: "zeta", Persona: "p", Cases: []CaseScore{{Expected: []string{"security"}, Raised: []string{"naming"}}}},
		{Model: "bedrock@us-east-1/claude", Persona: "p", Cases: []CaseScore{{Expected: []string{"security"}}}},
		{Model: "alpha", Persona: "q", Cases: []CaseScore{{Expected: []string{"security"}}}},
		{Model: "alpha", Persona: "a", Cases: []CaseScore{{Expected: []string{"security"}}}},
	}
	fa := FalseAlarms(reviewers)
	sc := Score(reviewers)
	require.Len(t, fa, len(sc))
	for i := range sc {
		assert.Equal(t, sc[i].Model, fa[i].Model, "row %d", i)
		assert.Equal(t, sc[i].Persona, fa[i].Persona, "row %d", i)
	}
}

func TestRunResult_CarriesFalseAlarms(t *testing.T) {
	perCase := 0.5
	data, err := json.Marshal(RunResult{
		Suite: "standard-v1", SuiteVersion: "1.0.0",
		FalseAlarms: []ReviewerFalseAlarms{{
			Model: "m", Persona: "p", Cases: 2, Findings: 3, Unmatched: 1, UnmatchedPerCase: &perCase,
		}},
	})
	require.NoError(t, err)

	var back map[string]any
	require.NoError(t, json.Unmarshal(data, &back))
	rows, ok := back["reviewer_false_alarms"].([]any)
	require.True(t, ok, "the run result must carry the figure under its own key")
	require.Len(t, rows, 1)
	row, ok := rows[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "m", row["model"])
	assert.Equal(t, "p", row["persona"])
	assert.Equal(t, float64(2), row["cases"])
	assert.Equal(t, float64(3), row["findings"])
	assert.Equal(t, float64(1), row["unmatched"])
	assert.Equal(t, 0.5, row["unmatched_per_case"])

	empty, err := json.Marshal(RunResult{Suite: "standard-v1", SuiteVersion: "1.0.0"})
	require.NoError(t, err)
	assert.NotContains(t, string(empty), "reviewer_false_alarms", "an unmeasured run omits the key")
}

// Owner answer Q7 (epic 113): a benchmark-only figure. It must not reach the public
// submission; TestPublicRecord_JSONKeySetIsUnchangedByThisEpic pins the row schema.
func TestBuildSubmission_DoesNotPublishFalseAlarms(t *testing.T) {
	data, err := json.Marshal(BuildSubmission(RunResult{
		Suite: "standard-v1", SuiteVersion: "1.0.0",
		FalseAlarms: []ReviewerFalseAlarms{{Model: "m", Persona: "p", Cases: 1, Findings: 1, Unmatched: 1}},
	}, time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "false_alarms")
	assert.NotContains(t, string(data), "unmatched")
}
