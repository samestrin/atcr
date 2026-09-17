package benchmark

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// match builds one settled-or-missed outcome without going through MatchFindings,
// so these tests exercise the FOLD and not the matcher.
func match(id string, outside, matched bool) FindingMatch {
	o := outside
	idx := -1
	if matched {
		idx = 0
	}
	return FindingMatch{
		Expected:      ExpectedFinding{ID: id, OutsideDiff: &o, Category: "correctness"},
		Matched:       matched,
		ReportedIndex: idx,
	}
}

// AC4's headline: out-of-diff recall is reported as a DISTINCT metric. A reviewer
// that finds every in-diff defect and no out-of-diff one must be visibly at 0.0 on
// the metric this tier exists to produce, not averaged up to 0.5.
func TestScorePositional_SeparatesOutsideDiffRecallFromOverall(t *testing.T) {
	got := ScorePositional([]RepoStateReviewerScore{{
		Model: "m", Persona: "p",
		Cases: []RepoStateCaseScore{{CaseID: "c1", Matches: []FindingMatch{
			match("in-1", false, true),
			match("in-2", false, true),
			match("out-1", true, false),
			match("out-2", true, false),
		}}},
	}})
	require.Len(t, got, 1)
	r := got[0]

	assert.Equal(t, 4, r.ExpectedTotal)
	assert.Equal(t, 2, r.MatchedTotal)
	require.NotNil(t, r.Recall)
	assert.InDelta(t, 0.5, *r.Recall, 1e-9)

	assert.Equal(t, 2, r.ExpectedOutsideDiff)
	assert.Equal(t, 0, r.MatchedOutsideDiff)
	require.NotNil(t, r.OutsideDiffRecall)
	assert.InDelta(t, 0.0, *r.OutsideDiffRecall, 1e-9, "the gap this tier measures must read as zero, not as a blended 0.5")

	assert.Equal(t, 2, r.ExpectedWithinDiff)
	assert.Equal(t, 2, r.MatchedWithinDiff)
	require.NotNil(t, r.WithinDiffRecall)
	assert.InDelta(t, 1.0, *r.WithinDiffRecall, 1e-9)
}

// The rate is a MICRO-average over findings, so a case planting four expectations
// weighs four times a case planting one. A macro-average over cases would have to
// invent a value for a case that plants no out-of-diff finding at all.
func TestScorePositional_MicroAveragesOverFindingsNotCases(t *testing.T) {
	got := ScorePositional([]RepoStateReviewerScore{{
		Model: "m", Persona: "p",
		Cases: []RepoStateCaseScore{
			{CaseID: "big", Matches: []FindingMatch{
				match("a", false, true), match("b", false, true),
				match("c", false, true), match("d", false, false),
			}},
			{CaseID: "small", Matches: []FindingMatch{match("e", false, false)}},
		},
	}})
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Recall)
	// Micro: 3 of 5. A macro-average over cases would be (0.75 + 0.0) / 2 = 0.375.
	assert.InDelta(t, 0.6, *got[0].Recall, 1e-9)
}

// An UNMEASURED rate must not read as a measured zero. A suite that plants no
// out-of-diff finding has no out-of-diff recall, and publishing 0.0 would claim
// the reviewer missed findings that were never there — the same
// unmeasured-versus-zero rule CostPerCorroboratedFindingUSD already follows.
func TestScorePositional_AbsentDenominatorIsNilNotZero(t *testing.T) {
	got := ScorePositional([]RepoStateReviewerScore{{
		Model: "m", Persona: "p",
		Cases: []RepoStateCaseScore{{CaseID: "c1", Matches: []FindingMatch{match("in", false, true)}}},
	}})
	require.Len(t, got, 1)
	assert.Nil(t, got[0].OutsideDiffRecall, "no out-of-diff finding was planted, so there is no rate")
	assert.Equal(t, 0, got[0].ExpectedOutsideDiff)
	require.NotNil(t, got[0].WithinDiffRecall)
}

func TestScorePositional_ReviewerWithNoCasesHasNoRates(t *testing.T) {
	got := ScorePositional([]RepoStateReviewerScore{{Model: "m", Persona: "p"}})
	require.Len(t, got, 1)
	assert.Nil(t, got[0].Recall)
	assert.Nil(t, got[0].OutsideDiffRecall)
	assert.Nil(t, got[0].WithinDiffRecall)
	assert.Equal(t, 0, got[0].ExpectedTotal)
}

// Sorted by the SAME (model, persona) comparator Score uses, so a consumer can
// join reviewers[i] to reviewer_positional_recall[i] positionally — the property
// Coverage and Vocabulary already rely on.
func TestScorePositional_SortsByModelPersonaLikeScore(t *testing.T) {
	got := ScorePositional([]RepoStateReviewerScore{
		{Model: "zeta", Persona: "b"},
		{Model: "alpha", Persona: "z"},
		{Model: "alpha", Persona: "a"},
	})
	require.Len(t, got, 3)
	assert.Equal(t, []string{"alpha/a", "alpha/z", "zeta/b"}, []string{
		got[0].Model + "/" + got[0].Persona,
		got[1].Model + "/" + got[1].Persona,
		got[2].Model + "/" + got[2].Persona,
	})
}

// ---- AC5: the frozen public record is untouched ----

// Where the metric lives: on the run-result, and nowhere in the published
// submission. This checks the WIRE rather than the Go type, because the submission
// envelope is the document the public board actually reads. PublicRecord is shared
// byte-for-byte with production `leaderboard --export`, so a benchmark-only column
// on it would appear on production rows that can never populate it.
func TestRunResult_PositionalRecallIsRunResultOnly(t *testing.T) {
	half := 0.5
	rr := RunResult{
		Suite:        FormatRepoStateV1,
		SuiteVersion: "1.0.0",
		PositionalRecall: []ReviewerPositionalRecall{{
			Model: "m", Persona: "p", ExpectedTotal: 2, MatchedTotal: 1, Recall: &half,
		}},
	}

	runJSON, err := json.Marshal(rr)
	require.NoError(t, err)
	assert.Contains(t, string(runJSON), "reviewer_positional_recall",
		"the run-result is where the metric is reported")

	subJSON, err := json.Marshal(BuildSubmission(rr, time.Unix(0, 0).UTC()))
	require.NoError(t, err)
	assert.NotContains(t, string(subJSON), "positional_recall",
		"the public submission envelope must not carry the benchmark-only metric")
	assert.NotContains(t, string(subJSON), "outside_diff_recall")
}

// A run-result written before this field existed must unmarshal to nil and report
// as unmeasured, exactly as an absent OutOfVocabularyRate does.
func TestRunResult_AbsentPositionalRecallUnmarshalsToNil(t *testing.T) {
	var rr RunResult
	require.NoError(t, json.Unmarshal([]byte(`{"suite":"repo-state-v1","suite_version":"1.0.0"}`), &rr))
	assert.Nil(t, rr.PositionalRecall)

	out, err := json.Marshal(rr)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "reviewer_positional_recall",
		"an unmeasured run omits the key rather than publishing an empty array")
}
