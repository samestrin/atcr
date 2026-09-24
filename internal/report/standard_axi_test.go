package report

import (
	"bytes"
	"io"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
)

// TestRenderAXI_SanitizesViaGoAxi pins that the standard TOON path cleans text
// with go-axi rather than the legacy hand-rolled stripper: go-axi drops an
// invalid UTF-8 byte, where the legacy path writes U+FFFD.
func TestRenderAXI_SanitizesViaGoAxi(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Confidence: "MEDIUM", Problem: "bad \xff byte"},
	}
	var b bytes.Buffer
	require.NoError(t, Render(&b, findings, FormatAXI))
	assert.Contains(t, b.String(), goaxi.SanitizeString("bad \xff byte"))
	assert.NotContains(t, b.String(), "�", "the standard path must not use the legacy U+FFFD replacement")
}

// decodeAXI decodes a standard TOON payload with go-axi's stock decoder — no
// custom delimiter option — failing the test if it does not parse or does not
// use the default comma delimiter.
func decodeAXI(t *testing.T, payload []byte) *goaxi.Document {
	t.Helper()
	doc, err := goaxi.DecodeTabular(bytes.NewReader(payload))
	require.NoErrorf(t, err, "standard TOON payload must decode:\n%s", payload)
	_, err = goaxi.Decode(bytes.NewReader(payload))
	require.NoErrorf(t, err, "standard TOON payload must decode with the generic decoder:\n%s", payload)
	assert.Equal(t, ',', doc.Delimiter, "standard TOON uses the default comma delimiter")
	return doc
}

// TestRenderAXI_StandardTOONRoundTrip is AC1/AC2: the default --axi findings
// payload is standard comma-delimited TOON, and code strings carrying pipes,
// quotes and newlines survive a stock decode verbatim.
func TestRenderAXI_StandardTOONRoundTrip(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "cmd/a|b.go", Line: 3,
			Problem: "a || b and x | y: \"quoted\"\nnext", Fix: "use regexp `foo|bar`, [x]{y}",
			Category: "true", EstMinutes: 30, Reviewers: []string{"greta", "otto"}, Confidence: "42"},
		{Severity: "LOW", File: "u.go", Line: 7, Problem: "-1", Category: "style", Confidence: "LOW"},
	}
	var b bytes.Buffer
	require.NoError(t, Render(&b, findings, FormatAXI))
	doc := decodeAXI(t, b.Bytes())
	assert.Equal(t, "findings", doc.Name)
	assert.NotContains(t, strings.SplitN(b.String(), "\n", 2)[0], "|", "the header carries no pipe delimiter marker")
	require.Len(t, doc.Rows, 2)
	assert.Equal(t, findings[0].Problem, doc.Rows[0]["problem"])
	assert.Equal(t, findings[0].Fix, doc.Rows[0]["fix"])
	assert.Equal(t, "cmd/a|b.go:3", doc.Rows[0]["file:line"])
	assert.Equal(t, "true", doc.Rows[0]["category"])
	assert.Equal(t, "42", doc.Rows[0]["confidence"])
	assert.Equal(t, "greta,otto", doc.Rows[0]["reviewers"])
	assert.Equal(t, "-1", doc.Rows[1]["problem"])
}

// TestRenderAXIPaginated_HeaderNEqualsEmittedRows is AC8 (amending Sprint 31.0
// AC 03-02 Edge Case 1 for the standard path): a truncated payload declares
// header N equal to the rows emitted and carries the true count as `total: N`
// beside `truncated: true`. Uncut payloads carry both keys too.
func TestRenderAXIPaginated_HeaderNEqualsEmittedRows(t *testing.T) {
	many := make([]reconcile.JSONFinding, 1200)
	for i := range many {
		many[i] = reconcile.JSONFinding{Severity: "LOW", File: "a.go", Line: i, Problem: "p", Confidence: "LOW"}
	}
	var b bytes.Buffer
	require.NoError(t, RenderAXIPaginated(&b, many, AXIMaxLinesDefault))
	doc := decodeAXI(t, b.Bytes())
	assert.Equal(t, AXIMaxLinesDefault-1, doc.Declared, "header N counts the emitted rows, not the true total")
	assert.Len(t, doc.Rows, doc.Declared)
	assert.Equal(t, "1200", doc.Meta["total"])
	assert.Equal(t, "true", doc.Meta["truncated"])

	var u bytes.Buffer
	require.NoError(t, RenderAXIPaginated(&u, sample(), AXIMaxLinesDefault))
	ud := decodeAXI(t, u.Bytes())
	assert.Equal(t, 2, ud.Declared)
	assert.Equal(t, "2", ud.Meta["total"])
	assert.Equal(t, "false", ud.Meta["truncated"])
}

// TestRenderSingleRowAXI_StandardTOON covers the review-summary and home
// payloads: single-row standard TOON arrays a stock decoder reads.
func TestRenderSingleRowAXI_StandardTOON(t *testing.T) {
	var rs bytes.Buffer
	require.NoError(t, RenderReviewSummaryAXI(&rs, ReviewSummaryAXI{ID: "2026-06-10_x", Dir: "/tmp/a|b", AgentsTotal: 4, FindingsHigh: 2}))
	doc := decodeAXI(t, rs.Bytes())
	assert.Equal(t, "review_summary", doc.Name)
	assert.Equal(t, reviewSummaryAXIHeader, doc.Fields)
	require.Len(t, doc.Rows, 1)
	assert.Equal(t, "/tmp/a|b", doc.Rows[0]["dir"])
	assert.Equal(t, "4", doc.Rows[0]["agents_total"])

	var h bytes.Buffer
	require.NoError(t, RenderHomeViewAXI(&h, HomeViewAXI{ExecPath: "~/go/bin/atcr", Description: "d, e", ReviewStatus: "none"}))
	hd := decodeAXI(t, h.Bytes())
	assert.Equal(t, "home", hd.Name)
	assert.Equal(t, homeViewAXIHeader, hd.Fields)
	require.Len(t, hd.Rows, 1)
	assert.Equal(t, "", hd.Rows[0]["review_id"])
	assert.Equal(t, "d, e", hd.Rows[0]["description"])
}

// TestEncodeAXI_SanitizeErrorsSurfaceViaErrorsAs pins that go-axi's typed
// sanitize failures (key collision, cycle) reach the caller unwrapped enough for
// errors.As.
func TestEncodeAXI_SanitizeErrorsSurfaceViaErrorsAs(t *testing.T) {
	err := encodeAXI(io.Discard, map[string]any{"a": 1, "a\x1b": 2})
	var kc *goaxi.KeyCollisionError
	require.ErrorAs(t, err, &kc)

	type node struct{ Next *node }
	n := &node{}
	n.Next = n
	err = encodeAXI(io.Discard, n)
	var ce *goaxi.CycleError
	require.ErrorAs(t, err, &ce)
}

// TestRenderAXIPaginated_NonPositiveMaxLinesClampsToDefault mirrors PaginateAXI's
// clamp on the standard path: a non-positive cap behaves as AXIMaxLinesDefault,
// so the header line is never the only thing emitted.
func TestRenderAXIPaginated_NonPositiveMaxLinesClampsToDefault(t *testing.T) {
	for _, maxLines := range []int{0, -3} {
		doc := axiPaginatedDoc(sample(), maxLines)
		assert.Len(t, doc.Findings, 2, "maxLines=%d must clamp to the default cap", maxLines)
		assert.False(t, doc.Truncated)
		assert.Equal(t, 2, doc.Total)
	}
}
