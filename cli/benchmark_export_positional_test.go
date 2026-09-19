package cli

import (
	"encoding/json"
	"io"
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

// A NaN rate cannot arrive through the file path — encoding/json rejects the JSON
// spellings that would produce one — so the guard is exercised directly, exactly as
// its reviewer_vocabulary twin is. It is kept because NaN compares false against
// every bound: the quotient check alone lets it through.
func TestValidateReviewerPositionalRecall_RejectsNaNRate(t *testing.T) {
	nan := math.NaN()
	err := validateReviewerPositionalRecall(io.Discard, benchmark.RunResult{
		Reviewers: []scorecard.PublicRecord{{Model: "m-a", Persona: "p-a"}},
		PositionalRecall: []benchmark.ReviewerPositionalRecall{
			{Model: "m-a", Persona: "p-a", ExpectedOutsideDiff: 2, MatchedOutsideDiff: 1, OutsideDiffRecall: &nan},
		},
	}, "rr.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reviewer_positional_recall")
	assert.Contains(t, err.Error(), "outside [0,1]")
}

// A reviewer whose slot was skipped on a case has a SMALLER denominator: the slot
// skip removes that case's expected findings from the skipped reviewer's row, so
// reviewer_positional_recall rows no longer share a denominator — contradicting the
// field doc's "cover every expected finding in the suite". Each row is internally
// consistent (its rate matches its own counts), so the per-row arms all pass, and
// nothing told a consumer comparing the rows side by side that they are not
// comparable. Warns rather than fails: a differing denominator describes a real
// run — only an impossible number fails.
func TestBenchmarkExport_WarnsWhenPositionalRecallDenominatorsDiffer(t *testing.T) {
	_, stderr, err := execExportErr(t, writeRunResultWithPositional(t,
		[]benchmark.ReviewerPositionalRecall{
			{Model: "m-a", Persona: "p-a", ExpectedTotal: 6, ExpectedOutsideDiff: 2, ExpectedWithinDiff: 4},
			{Model: "m-b", Persona: "p-b", ExpectedTotal: 5, ExpectedOutsideDiff: 2, ExpectedWithinDiff: 3},
		}))
	require.NoError(t, err, "a differing denominator describes a real run — warn, do not fail: %s", stderr)
	assert.Contains(t, stderr, "reviewer_positional_recall")
	assert.Contains(t, stderr, "not comparable",
		"the warning must say why equal-looking rates cannot be compared across these rows")

	// Rows that DO share a denominator stay silent — the warning is a comparability
	// caveat, not noise to attach to every export.
	_, stderr2, err2 := execExportErr(t, writeRunResultWithPositional(t,
		[]benchmark.ReviewerPositionalRecall{
			{Model: "m-a", Persona: "p-a", ExpectedTotal: 6, ExpectedOutsideDiff: 2, ExpectedWithinDiff: 4},
			{Model: "m-b", Persona: "p-b", ExpectedTotal: 6, ExpectedOutsideDiff: 2, ExpectedWithinDiff: 4},
		}))
	require.NoError(t, err2, "equal denominators are the common case: %s", stderr2)
	assert.NotContains(t, stderr2, "not comparable")
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
