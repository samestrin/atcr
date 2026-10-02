package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/samestrin/atcr/internal/fanout"
)

// docs/benchmark.md's `incomplete` row enumerates the routes by which a reviewer
// can end up with only a FRACTION of the diff. That enumeration is not prose the
// doc author owns: internal/fanout/revieweroutcome.go maps each route to the value
// with `UnreviewedChunks > 0 || Truncated || Salvaged`, so the row claims to
// describe a predicate it does not contain. A route added to that predicate with
// no doc edit leaves the published enumeration silently short — which is exactly
// what happened when the salvage arm landed (TD docs/benchmark.md:812).
//
// The guard is BIDIRECTIONAL. The doc half asserts the route is named; the code
// half asserts the arm the route depends on is still in the predicate. Reverting
// either side — the doc edit, or the salvage arm itself — turns this red, so a
// doc-only revert cannot pass while the code no longer refuses a salvaged slot.
//
// It lives in internal/reconcile/ per the repo's convention for doc-vs-code drift
// tests (see justification_record_boundary_test.go).
func TestBenchmarkDoc_IncompleteRoutesMatchTheClassifier(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")
	code := readRepoFile(t, "../../internal/fanout/revieweroutcome.go")

	t.Run("the published incomplete row names every route the classifier folds in", func(t *testing.T) {
		row := docLineContaining(t, doc, "`incomplete` | The reviewer saw only a fraction of the diff")
		for _, want := range []string{
			"chunked slot whose bins failed while it still reported ok",
			"payload shed to fit a byte budget",
			"salvage",
		} {
			assert.Contains(t, row, want,
				"the published `incomplete` row must name every route that maps to it: missing %q", want)
		}
	})

	t.Run("the classifier really does fold all three routes into incomplete", func(t *testing.T) {
		assert.Contains(t, code, "a.UnreviewedChunks > 0 || a.Truncated || a.Salvaged",
			"docs/benchmark.md's `incomplete` row describes this predicate; a changed predicate must re-open the row")

		// And the three routes reach that arm at all: each must map to incomplete,
		// so a route whose signal stopped being carried would fail here rather than
		// only in the doc's wording.
		for _, tc := range []struct {
			name string
			st   fanout.AgentStatus
			why  string
		}{
			{"chunked bins failed", fanout.AgentStatus{Status: fanout.StatusOK, UnreviewedChunks: 1}, "the chunked route"},
			{"payload shed", fanout.AgentStatus{Status: fanout.StatusOK, Truncated: true}, "the byte-budget route"},
			{"salvaged reply", fanout.AgentStatus{Status: fanout.StatusOK, Salvaged: true}, "the salvage route"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, "incomplete", fanout.ReviewerOutcome(tc.st, 0), tc.why)
			})
		}
	})
}
