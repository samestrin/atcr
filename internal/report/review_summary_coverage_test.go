package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewSummaryAXI_CoverageColumns pins the Epic 35.16.13 T5 additive
// columns: present (in order, after the base columns) only when coverage was
// measured, and absent otherwise so a bulk or diff run's payload keeps its shape.
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

	// The legacy pipe encoder carries the same columns in the same order.
	var p bytes.Buffer
	require.NoError(t, RenderReviewSummaryPipe(&p, measured))
	head, row, _ := strings.Cut(p.String(), "\n")
	assert.True(t, strings.HasSuffix(head, "|bins_reviewed|bins_planned|files_covered|files_in_scope}:"), head)
	assert.True(t, strings.HasSuffix(strings.TrimSpace(row), "|68|128|64|128"), row)

	p.Reset()
	require.NoError(t, RenderReviewSummaryPipe(&p, unmeasured))
	assert.NotContains(t, p.String(), "bins_reviewed")
}
