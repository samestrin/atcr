package fanout

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrepareReview_GroundingChangedMatchesStandalone pins the
// grounding-semantics-unchanged guarantee at the fan-out threading layer (Epic
// 22.4). The payload package already proves RangeBuilder.BuildChangedLines
// matches standalone payload.BuildChangedLines (TestRangeBuilder_ChangedLinesMatchesStandalone);
// this test asserts the END-TO-END threading that the payload test cannot see —
// buildPayloads returning the shared RangeBuilder, finalizePreparedReview passing
// it into computeGroundingData, and the result landing on PreparedReview.Changed —
// produces the same grounding data as a pre-epic standalone BuildChangedLines call.
// A regression that threads a wrong/nil rb on the git-range path would compile and
// the payload tests would stay green, but this test would fail because prep.Changed
// would no longer equal the standalone output.
func TestPrepareReview_GroundingChangedMatchesStandalone(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := twoAgentConfig("http://unused") // PrepareReview does not call the completer
	prep, err := PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	require.NotNil(t, prep)

	standalone, err := payload.BuildChangedLines(context.Background(), repo, base, head)
	require.NoError(t, err)

	assert.Equal(t, standalone, prep.Changed,
		"PreparedReview.Changed must equal standalone payload.BuildChangedLines — the grounding-semantics-unchanged guarantee for the git-range threading")
}

// TestPrepareReviewFromDiff_GroundingDisabledRangeLess pins the diff-ingestion
// path of the same threading: a range-less request (PrepareReviewFromDiff, no live
// base/head) carries no RangeBuilder, so computeGroundingData's range-less early
// return must disable grounding and record the human-readable reason. This guards
// the nil-rb / range-less arm that the git-range test above does not exercise.
func TestPrepareReviewFromDiff_GroundingDisabledRangeLess(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	out := filepath.Join(t.TempDir(), "ext-review")
	prep, err := PrepareReviewFromDiff(context.Background(), cfg, diffReq(t.TempDir(), out), looseDiff)
	require.NoError(t, err)
	require.NotNil(t, prep)

	assert.Nil(t, prep.Changed, "a range-less diff ingestion has no grounding data")
	assert.Contains(t, prep.GroundingDisabledReason, "range-less",
		"diff ingestion must record a range-less grounding-disable reason so the skip is auditable")
}
