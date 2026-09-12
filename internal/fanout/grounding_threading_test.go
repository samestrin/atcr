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

// TestFinalizePreparedReview_PrefetchPayloadsWithoutBuilderDisablesGrounding
// pins the last leg of the same threading: the standalone grounding fallback
// (payload.BuildChangedLines inside computeGroundingData) never merges prefetch
// spans — only rb.BuildChangedLines does, via withPrefetchedSpans. Payloads that
// carry Context Definitions can ground findings on spans no diff ever touched;
// grounding such payloads through the fallback would silently drop every finding
// on shown spans as "file not in the patch". Every git-range caller passes the
// same builder that built the payloads, so this pairing is unreachable today;
// the guard at the finalizePreparedReview call site must keep it that way
// audibly rather than let a future caller ground incompletely.
func TestFinalizePreparedReview_PrefetchPayloadsWithoutBuilderDisablesGrounding(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := twoAgentConfig("http://unused")
	payloads := map[string]modePayload{
		"blocks": {Entries: []payload.FileEntry{{Path: payload.PrefetchContextPath, Body: "context"}}},
	}
	prep, err := finalizePreparedReview(context.Background(), cfg,
		reviewReq(repo, repo, base, head), payloads, map[string]string{}, nil, "blocks", nil, false)
	require.NoError(t, err)
	require.NotNil(t, prep)

	assert.Nil(t, prep.Changed,
		"prefetch-carrying payloads grounded without a RangeBuilder must disable grounding rather than silently drop every shown-span finding")
	assert.Contains(t, prep.GroundingDisabledReason, "prefetch",
		"the disable reason must name the prefetch-span loss so the broken builder pairing is auditable")
}

// TestComputeGroundingData_RangeBuilderRangeMismatch pins the invariant that a
// RangeBuilder passed to computeGroundingData must have been constructed from
// the same req.Range it is grounding. computeGroundingData gates on req.Range
// emptiness but, when rb != nil, computes changed lines from rb's OWN base/head,
// ignoring req.Range. Every current caller builds rb from the same req.Range in
// the same function, so they agree today — but the invariant is implicit. This
// test builds rb for base..head and grounds a request whose range is base..base
// (a different range), asserting the guard catches the mismatch and disables
// grounding with an audible reason rather than silently anchoring to the rb's
// range. Against the pre-guard code this fails: computeGroundingData would call
// rb.BuildChangedLines() (rb's base..head) and return non-nil grounding data.
func TestComputeGroundingData_RangeBuilderRangeMismatch(t *testing.T) {
	repo, base, head := initRepo(t)
	// rb is built for base..head; the request grounds base..base — a different
	// range, standing in for a future edit that threads a mismatched pair.
	rb := payload.NewRangeBuilder(context.Background(), repo, base, head)
	req := reviewReq(repo, repo, base, base)

	cl, reason := computeGroundingData(context.Background(), req, rb)
	assert.Nil(t, cl, "a mismatched rb/req range must disable grounding rather than anchor to the rb's range")
	assert.Contains(t, reason, "mismatch",
		"the range mismatch must be recorded as the grounding-disable reason so the silent-wrong-anchor class is impossible")
}
