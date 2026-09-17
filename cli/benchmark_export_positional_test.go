package cli

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeRunResultWithPositional is writeRunResultWithVocabulary's twin for the
// positional array: the same two-reviewer shape, so a length or identity mismatch
// is expressible.
func writeRunResultWithPositional(t *testing.T, rows []benchmark.ReviewerPositionalRecall) string {
	t.Helper()
	rr := benchmark.RunResult{
		Suite:        "suite-valid",
		SuiteVersion: "1",
		GeneratedAt:  "2026-08-15T00:00:00Z",
		Reviewers: []scorecard.PublicRecord{
			{Model: "m-a", Persona: "p-a"},
			{Model: "m-b", Persona: "p-b"},
		},
		PositionalRecall: rows,
	}
	data, err := json.MarshalIndent(rr, "", "  ")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "run-result.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

// A run-result is hand-suppliable, which is the stated reason reviewer_vocabulary
// gets seven arithmetic arms and out_of_vocabulary_rate gets a range check.
// reviewer_positional_recall is the structurally identical array and got nothing —
// a file claiming 99 matched against 1 expected, recall 42.0 and
// outside_diff_recall -3.0 exported with exit 0 and no warning.
func TestBenchmarkExport_RejectsImpossiblePositionalRecall(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  benchmark.ReviewerPositionalRecall
		want string
	}{
		{
			name: "negative counts",
			row:  benchmark.ReviewerPositionalRecall{Model: "m-a", Persona: "p-a", ExpectedTotal: -1},
			want: "negative",
		},
		{
			name: "matched exceeds expected",
			row:  benchmark.ReviewerPositionalRecall{Model: "m-a", Persona: "p-a", ExpectedTotal: 1, MatchedTotal: 99},
			want: "exceeding",
		},
		{
			name: "matched exceeds expected out of diff",
			row: benchmark.ReviewerPositionalRecall{Model: "m-a", Persona: "p-a",
				ExpectedOutsideDiff: 1, MatchedOutsideDiff: 4},
			want: "exceeding",
		},
		{
			name: "rate above one",
			row: benchmark.ReviewerPositionalRecall{Model: "m-a", Persona: "p-a",
				ExpectedTotal: 2, MatchedTotal: 1, Recall: ptrFloat(42.0)},
			want: "outside [0,1]",
		},
		{
			name: "rate is NaN",
			row: benchmark.ReviewerPositionalRecall{Model: "m-a", Persona: "p-a",
				ExpectedOutsideDiff: 2, MatchedOutsideDiff: 1, OutsideDiffRecall: ptrFloat(math.NaN())},
			want: "outside [0,1]",
		},
		{
			name: "rate on a zero denominator",
			row: benchmark.ReviewerPositionalRecall{Model: "m-a", Persona: "p-a",
				WithinDiffRecall: ptrFloat(1.0)},
			want: "expected nothing",
		},
		{
			name: "rate contradicts its own counts",
			row: benchmark.ReviewerPositionalRecall{Model: "m-a", Persona: "p-a",
				ExpectedTotal: 4, MatchedTotal: 1, Recall: ptrFloat(0.9)},
			want: "does not match its own",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := execExportErr(t, writeRunResultWithPositional(t,
				[]benchmark.ReviewerPositionalRecall{tc.row, {Model: "m-b", Persona: "p-b"}}))
			require.Error(t, err, "an impossible positional row must not export")
			assert.Contains(t, err.Error(), tc.want)
			assert.Contains(t, err.Error(), "reviewer_positional_recall",
				"the error must name the field so the operator can find it")
		})
	}
}

// The documented positional join (entry i describes reviewers[i]) gets the same
// WARNING the vocabulary array gets — not a rejection. AC5 holds either way because
// BuildSubmission never copies the field, which is equally true of
// reviewer_vocabulary, and that array is checked anyway.
func TestBenchmarkExport_WarnsOnMisalignedPositionalRecall(t *testing.T) {
	t.Run("length", func(t *testing.T) {
		_, stderr, err := execExportErr(t, writeRunResultWithPositional(t,
			[]benchmark.ReviewerPositionalRecall{{Model: "m-a", Persona: "p-a"}}))
		require.NoError(t, err, "a misaligned length warns, it does not fail: %s", stderr)
		assert.Contains(t, stderr, "reviewer_positional_recall")
	})
	t.Run("identity", func(t *testing.T) {
		_, stderr, err := execExportErr(t, writeRunResultWithPositional(t,
			[]benchmark.ReviewerPositionalRecall{{Model: "m-a", Persona: "p-a"}, {Model: "wrong", Persona: "p-b"}}))
		require.NoError(t, err, "a misaligned identity warns, it does not fail: %s", stderr)
		assert.Contains(t, stderr, "reviewer_positional_recall")
	})
}

// A well-formed array must stay silent, or the warning is noise on every valid run.
func TestBenchmarkExport_AcceptsAWellFormedPositionalRecall(t *testing.T) {
	_, stderr, err := execExportErr(t, writeRunResultWithPositional(t,
		[]benchmark.ReviewerPositionalRecall{
			{Model: "m-a", Persona: "p-a", ExpectedTotal: 4, MatchedTotal: 1, Recall: ptrFloat(0.25),
				ExpectedOutsideDiff: 2, MatchedOutsideDiff: 0, OutsideDiffRecall: ptrFloat(0),
				ExpectedWithinDiff: 2, MatchedWithinDiff: 1, WithinDiffRecall: ptrFloat(0.5)},
			{Model: "m-b", Persona: "p-b"},
		}))
	require.NoError(t, err)
	assert.NotContains(t, stderr, "reviewer_positional_recall")
}
