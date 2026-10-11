package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resemblanceOnlyCases are the shipped repo-state-v1 cases authored for epic
// 35.16.12's measurement gate. Each plants its defect in a SIBLING of the changed
// code: a second handler that needs the guard the diff adds to the first, or a
// parallel path implementing the same invariant by hand. sibling names the
// function the expected finding settles in.
//
// The gate counts a miss as evidence for semantic retrieval only when the sibling
// was absent from the payload and reachable by resemblance alone. If 35.16.8's
// reference retrieval could reach the sibling, a miss would say nothing about
// semantic retrieval, so the case would measure the wrong thing.
var resemblanceOnlyCases = map[string]string{
	"archived-guard-misses-transfer":    "transfer_project",
	"traversal-check-misses-thumbnails": "thumbnail_path",
	"page-size-cap-misses-orders":       "list_orders",
	"half-up-rounding-misses-credit":    "downgrade_credit",
}

// reviewedRange materializes c and builds every payload mode over its range on
// one RangeBuilder, which is how internal/fanout builds a review: the pre-fetch
// section is computed by the first build and grounding then reads its spans.
func reviewedRange(t *testing.T, c benchmark.RepoStateCase) (bodies string, cl payload.ChangedLines, status payload.PrefetchStatus) {
	t.Helper()
	ctx := context.Background()
	mc, err := benchmark.MaterializeCase(ctx, c, t.TempDir())
	require.NoError(t, err)
	rb := payload.NewRangeBuilder(ctx, mc.Root, mc.BaseSHA, mc.HeadSHA)
	var all strings.Builder
	for _, mode := range []payload.PayloadMode{payload.ModeDiff, payload.ModeBlocks, payload.ModeFiles} {
		entries, err := rb.BuildEntries(mode)
		require.NoError(t, err, "mode %s", mode)
		for _, e := range entries {
			all.WriteString(e.Path)
			all.WriteString("\n")
			all.WriteString(e.Body)
			all.WriteString("\n")
		}
	}
	cl, err = rb.BuildChangedLines()
	require.NoError(t, err)
	return all.String(), cl, rb.PrefetchStatus()
}

func shippedRepoStateCases(t *testing.T) map[string]benchmark.RepoStateCase {
	t.Helper()
	m, err := benchmark.LoadRepoState("../benchmarks/repo-state-v1")
	require.NoError(t, err)
	byID := make(map[string]benchmark.RepoStateCase, len(m.Cases))
	for _, c := range m.Cases {
		byID[c.ID] = c
	}
	return byID
}

// The control for the test below. Without it, "the sibling is unreachable" could
// pass simply because pre-fetching never ran in this harness. incomplete-predicate-
// guard's router/intake.py calls the changed symbol dispatch(), so reference
// retrieval must reach it here.
func TestShippedRepoState_ReferenceRetrievalReachesAConsumer(t *testing.T) {
	c, ok := shippedRepoStateCases(t)["incomplete-predicate-guard"]
	require.True(t, ok)

	bodies, cl, status := reviewedRange(t, c)

	assert.True(t, status.Present, "pre-fetching must run in this harness")
	fc, ok := cl["router/intake.py"]
	require.True(t, ok, "the consumer of dispatch() must be groundable through a pre-fetched span")
	assert.True(t, fc.PrefetchOnly)
	assert.Contains(t, bodies, "router/intake.py")
}

// Each resemblance-only case's sibling is unreachable by 35.16.8 reference
// retrieval, checked against the real retrieval code rather than argued:
//   - the sibling's file is not in the grounding map, so pre-fetching retrieved no
//     span of it (a finding there is dropped by the Epic 14.1 gate);
//   - no payload mode names the sibling's file or function, so nothing in what a
//     reviewer is shown, the commit-message claim ledger included, points at it;
//   - the diff and the commit message do not name it either.
func TestShippedResemblanceCases_SiblingIsUnreachableByReferenceRetrieval(t *testing.T) {
	cases := shippedRepoStateCases(t)
	for id, sibling := range resemblanceOnlyCases {
		t.Run(id, func(t *testing.T) {
			c, ok := cases[id]
			require.True(t, ok, "case %q must be registered in suite.json", id)
			require.Len(t, c.ExpectedFindings, 1)
			f := c.ExpectedFindings[0]
			require.True(t, f.OutsideDiff != nil && *f.OutsideDiff, "the sibling is untouched code")

			bodies, cl, _ := reviewedRange(t, c)

			_, reached := cl[f.File]
			assert.False(t, reached, "%s is groundable, so reference retrieval or the diff reaches it", f.File)
			// strings.Contains rather than assert.NotContains: the latter prints the
			// whole payload, every mode of it, on failure.
			assert.False(t, strings.Contains(bodies, f.File), "the payload names the sibling's file %s", f.File)
			assert.False(t, strings.Contains(bodies, sibling), "the payload names the sibling's function %s", sibling)

			for _, name := range []string{c.Diff, c.CommitMessage} {
				raw, err := os.ReadFile(filepath.Join(c.Dir, name))
				require.NoError(t, err)
				assert.NotContains(t, string(raw), f.File, "%s names the sibling's file", name)
				assert.NotContains(t, string(raw), sibling, "%s names the sibling's function", name)
			}
		})
	}
}
