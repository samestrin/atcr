package reconcile

import (
	"strings"
	"testing"
)

// docs/benchmark.md's resume exception (checkpoints written before the serial lane
// existed) lets an unstamped checkpoint resume across an added serial reviewer, but
// the bullet historically stopped there — it never said what the resulting run-result
// looks like for that reviewer. The replayed cases were reviewed by the parallel lane
// only, so the newly-added serial reviewer is scored on the un-replayed remainder
// alone and publishes a leaderboard row backed by a strict subset of the suite.
// buildRunResult's per-reviewer Coverage array makes this visible, but only to
// someone who inspects it; a reader taking the bullet at face value assumes the
// resume is apples-to-apples.
//
// This guard is BIDIRECTIONAL like its siblings: the doc needles pin the sentence
// that states the caveat, the code needle pins the arm in cli/benchmark_run.go that
// makes the caveat true (the per-reviewer coverage rows buildRunResult emits). It
// lives in internal/reconcile/ per the repo's convention for doc-vs-code drift tests
// (no Go package lives under docs/).
func TestBenchmarkDoc_ResumeExceptionStatesCoverageCaveat(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")
	code := readRepoFile(t, "../../cli/benchmark_run.go")

	docNeedles := []struct {
		name    string
		needle  string
		because string
	}{
		{
			name:    "coverage spans only post-resume cases",
			needle:  "coverage spans only the cases executed after the resume",
			because: "the added serial reviewer is scored on the un-replayed remainder alone, and the bullet must say so",
		},
		{
			name:    "per-reviewer coverage array names the cases",
			needle:  "per-reviewer coverage array",
			because: "the run-result field that makes the subset visible must be named",
		},
	}
	for _, n := range docNeedles {
		if !strings.Contains(doc, n.needle) {
			t.Errorf("docs/benchmark.md resume exception must state that the added serial reviewer's %s (%s)", n.name, n.because)
		}
	}

	// Behaviour-bearing code arm: this is buildRunResult constructing the
	// per-reviewer coverage rows (CaseIDs/Outcomes per reviewer). Deleting the
	// coverage construction fails this test, which is what makes the doc claim
	// grounded rather than decorative.
	codeNeedle := "coverage = append(coverage, benchmark.ReviewerCoverage{"
	if !strings.Contains(code, codeNeedle) {
		t.Errorf("cli/benchmark_run.go buildRunResult must keep constructing the per-reviewer coverage rows the doc's caveat points at (%s)", codeNeedle)
	}
}
