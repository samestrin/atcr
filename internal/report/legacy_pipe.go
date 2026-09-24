package report

import (
	"io"

	"github.com/samestrin/atcr/internal/reconcile"
)

// renderPipeAXI is the legacy pipe-delimited AXI findings encoder
// (findings[N|]{...}:), reachable only through the deprecated --format pipe /
// --legacy-pipe / ATCR_LEGACY_PIPE=1 fallback. Its bytes are frozen by the
// testdata/legacy_pipe goldens.
func renderPipeAXI(w io.Writer, findings []reconcile.JSONFinding) error {
	return renderAXI(w, findings)
}

// RenderPipeAXIPaginated is the legacy pipe-delimited analogue of
// RenderAXIPaginated: it keeps the pre-migration contract (header N is the true
// total even when fewer rows are emitted, followed by `truncated: <bool>`).
func RenderPipeAXIPaginated(w io.Writer, findings []reconcile.JSONFinding, maxLines int) error {
	return RenderAXIPaginated(w, findings, maxLines)
}

// RenderReviewSummaryPipe is the legacy pipe-delimited review-summary payload
// (review_summary[1|]{...}:).
func RenderReviewSummaryPipe(w io.Writer, s ReviewSummaryAXI) error {
	return RenderReviewSummaryAXI(w, s)
}

// RenderHomeViewPipe is the legacy pipe-delimited home-view payload
// (home[1|]{...}:).
func RenderHomeViewPipe(w io.Writer, s HomeViewAXI) error {
	return RenderHomeViewAXI(w, s)
}
