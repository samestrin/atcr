package audit

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeCell_EscapesBackslashBeforePipe(t *testing.T) {
	// A literal backslash followed by a pipe must escape both, otherwise the
	// pipe would still open a spurious column after the backslash was written raw.
	assert.Equal(t, `\\\|`, sanitizeCell(`\|`))
	assert.Equal(t, `a\\b`, sanitizeCell(`a\b`))
}

func TestRenderReport_RendersPerRunTableWithTotals(t *testing.T) {
	gen := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	recs := []Record{
		// Deliberately out of chronological order to prove the render sorts ascending.
		{Timestamp: time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC), PR: 1234, Base: "basesha0003aaaaaaaa", Head: "headsha0004bbbbbbbb", Findings: map[string]int{"HIGH": 1}},
		{Timestamp: time.Date(2026, 7, 4, 9, 30, 0, 0, time.UTC), PR: 1234, Base: "basesha0001cccccccc", Head: "headsha0002dddddddd", Findings: map[string]int{"HIGH": 1, "LOW": 2}},
	}
	out := RenderReport(recs, 1234, gen)

	assert.Contains(t, out, "# Audit Report")
	assert.Contains(t, out, "1234")                 // PR number in the heading
	assert.Contains(t, out, "2026-07-05T00:00:00Z") // generated timestamp
	for _, sev := range []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"} {
		assert.Contains(t, out, sev) // all canonical severity columns present
	}
	assert.Contains(t, out, "**Total**") // grand-total row

	// SHAs are truncated to 12 chars for a readable one-page report.
	assert.Contains(t, out, "basesha0001c")
	assert.NotContains(t, out, "basesha0001cccccccc")

	// Runs render in ascending timestamp order (earlier run's row precedes later).
	early := strings.Index(out, "2026-07-04T09:30:00Z")
	late := strings.Index(out, "2026-07-05T10:00:00Z")
	require.GreaterOrEqual(t, early, 0)
	require.GreaterOrEqual(t, late, 0)
	assert.Less(t, early, late)
}

func TestRenderReport_EscapesMarkdownInjectionInCells(t *testing.T) {
	gen := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	// A crafted base value with a pipe + newline must not break the table shape.
	recs := []Record{
		{Timestamp: gen, PR: 7, Base: "aaa|bbb\nccc", Head: "hhh", Findings: nil},
	}
	out := RenderReport(recs, 7, gen)
	assert.NotContains(t, out, "aaa|bbb") // raw pipe would open a spurious column
	assert.Contains(t, out, `aaa\|bbb`)   // escaped instead
	assert.NotContains(t, out, "\naaa")   // no embedded newline splitting the row
}
