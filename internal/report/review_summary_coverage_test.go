package report

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewSummaryAXI_CoverageColumns pins the Epic 35.16.13 T5 additive
// columns: present (in order, after the base columns) only when coverage was
// measured, and absent otherwise so a bulk or diff run's payload keeps its shape.
// The frozen legacy pipe format never carries them.
func TestReviewSummaryAXI_CoverageColumns(t *testing.T) {
	measured := ReviewSummaryAXI{
		ID: "2026-10-12_x", Dir: "d", AgentsTotal: 2,
		CoverageMeasured: true, BinsReviewed: 68, BinsPlanned: 128, FilesCovered: 64, FilesInScope: 128,
	}
	var b bytes.Buffer
	require.NoError(t, RenderReviewSummaryAXI(&b, measured))
	doc := decodeAXI(t, b.Bytes())
	assert.Equal(t, append(append([]string(nil), reviewSummaryAXIHeader...), reviewSummaryCoverageHeader...), doc.Fields)
	require.Len(t, doc.Rows, 1)
	assert.Equal(t, "68", doc.Rows[0]["bins_reviewed"])
	assert.Equal(t, "128", doc.Rows[0]["bins_planned"])
	assert.Equal(t, "64", doc.Rows[0]["files_covered"])
	assert.Equal(t, "128", doc.Rows[0]["files_in_scope"])

	unmeasured := measured
	unmeasured.CoverageMeasured = false
	b.Reset()
	require.NoError(t, RenderReviewSummaryAXI(&b, unmeasured))
	assert.Equal(t, reviewSummaryAXIHeader, decodeAXI(t, b.Bytes()).Fields,
		"unmeasured coverage declares no coverage columns, even with stray counts set")

	// The legacy pipe format is frozen: coverage never reaches it, so a measured
	// summary renders byte-identically to an unmeasured one (EST-937).
	var measuredPipe, unmeasuredPipe bytes.Buffer
	require.NoError(t, RenderReviewSummaryPipe(&measuredPipe, measured))
	require.NoError(t, RenderReviewSummaryPipe(&unmeasuredPipe, unmeasured))
	assert.Equal(t, unmeasuredPipe.String(), measuredPipe.String())
	assert.Equal(t, "review_summary[1|]{id|dir|agents_succeeded|agents_total|agents_failed|agents_timed_out|api_calls|findings_total|findings_critical|findings_high|findings_medium|findings_low}:\n"+
		"  2026-10-12_x|d|0|2|0|0|0|0|0|0|0|0\n", measuredPipe.String())
}
