package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderHomeViewAXI_SingleRow pins the home-view AXI payload shape: a
// single-row TOON tabular array (home[1|]{...}:) reusing the shared toonQuote/
// axiDelim encoder, one header line plus exactly one data row.
func TestRenderHomeViewAXI_SingleRow(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, RenderHomeViewAXI(&b, HomeViewAXI{
		ExecPath:     "~/go/bin/atcr",
		Description:  "Agent Team Code Review — a review panel, not a reviewer",
		ReviewID:     "2026-06-10_x",
		ReviewStatus: "completed",
	}))

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	require.Len(t, lines, 2, "single-row TOON payload: header + one data row")
	assert.Equal(t, "home[1|]{exec_path|description|review_id|review_status}:", lines[0],
		"header declares the fixed home-view column order")
	assert.Contains(t, lines[1], "~/go/bin/atcr")
	assert.Contains(t, lines[1], "2026-06-10_x")
	assert.Contains(t, lines[1], "completed")
	assert.Contains(t, lines[1], "Agent Team Code Review — a review panel, not a reviewer",
		"the description (spaces + em dash, no TOON specials) renders unquoted")
}

// TestRenderHomeViewAXI_NoReview covers the first-run state: an empty review_id
// must be quoted per TOON must-quote rules, with an explicit "none" status.
func TestRenderHomeViewAXI_NoReview(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, RenderHomeViewAXI(&b, HomeViewAXI{
		ExecPath:     "~/go/bin/atcr",
		Description:  "desc",
		ReviewID:     "",
		ReviewStatus: "none",
	}))

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[1], `""`, "empty review_id renders as a quoted empty TOON string")
	assert.Contains(t, lines[1], "none", "no-review status is explicit")
}

// TestRenderHomeViewAXI_NoEscapeSequences pins the same no-ANSI structural
// guarantee renderAXI/RenderReviewSummaryAXI carry: control/escape bytes in any
// field are stripped by toonQuote.
func TestRenderHomeViewAXI_NoEscapeSequences(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, RenderHomeViewAXI(&b, HomeViewAXI{
		ExecPath:     "~/go/bin/atcr",
		Description:  "desc\x1b[31mred\x1b[0m",
		ReviewID:     "id",
		ReviewStatus: "completed",
	}))
	assert.NotContains(t, b.String(), "\x1b", "control/ANSI bytes are stripped by toonQuote")
}
