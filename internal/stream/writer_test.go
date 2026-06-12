package stream

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteSource_HeaderAndColumns(t *testing.T) {
	var b strings.Builder
	err := WriteSource(&b, []Finding{{
		Severity: "HIGH", File: "a.go", Line: 10,
		Problem: "p", Fix: "f", Category: "security", EstMinutes: 20,
		Evidence: "ev", Reviewer: "bruce",
	}})
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, Version, lines[0])
	assert.Equal(t, "HIGH|a.go:10|p|f|security|20|ev|bruce", lines[1])
}

func TestWriteSource_EscapesPipes(t *testing.T) {
	var b strings.Builder
	err := WriteSource(&b, []Finding{{
		Severity: "LOW", File: "a.go", Line: 1,
		Problem: "use a || b here", Fix: "f", Category: "style", EstMinutes: 5,
		Evidence: "x | y", Reviewer: "otto",
	}})
	require.NoError(t, err)
	out := b.String()
	// Pipes inside fields become '/', so the column count stays 8.
	assert.Contains(t, out, "use a // b here")
	assert.Contains(t, out, "x / y")
	// Round-trips back to exactly 8 columns.
	res, err := ParseSource([]byte(out))
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Empty(t, res.Skipped)
}

func TestWriteSource_EscapesNewlines(t *testing.T) {
	var b strings.Builder
	err := WriteSource(&b, []Finding{{
		Severity: "HIGH", File: "a.go", Line: 1,
		Problem: "line1\nline2", Fix: "do\r\nstuff", Category: "correctness", EstMinutes: 5,
		Evidence: "ev", Reviewer: "bruce",
	}})
	require.NoError(t, err)
	// Exactly two physical lines: header + one finding (no split).
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	require.Len(t, lines, 2)
	res, err := ParseSource([]byte(b.String()))
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Empty(t, res.Skipped)
	assert.Equal(t, "bruce", res.Findings[0].Reviewer) // not lost to a split
}

func TestWriteReconciled_CommaInReviewerNotForged(t *testing.T) {
	var b strings.Builder
	err := WriteReconciled(&b, []Finding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Fix: "f",
		Category: "security", EstMinutes: 10, Evidence: "ev",
		Reviewers: []string{"a,b,c"}, Confidence: "MEDIUM",
	}})
	require.NoError(t, err)
	res, err := ParseReconciled([]byte(b.String()))
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	// The single reviewer is not split into three by the embedded comma.
	assert.Len(t, res.Findings[0].Reviewers, 1)
}

func TestParse_TrailingPipeNotDropped(t *testing.T) {
	// A valid 8-col finding written with a trailing pipe must still parse.
	data := "# atcr-findings/v1\nLOW|a.go:1|p|f|style|5|ev|otto|\n"
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Empty(t, res.Skipped)
	assert.Equal(t, "otto", res.Findings[0].Reviewer)
}

func TestWriteReconciled_NineColumns(t *testing.T) {
	var b strings.Builder
	err := WriteReconciled(&b, []Finding{{
		Severity: "CRITICAL", File: "auth.go", Line: 42,
		Problem: "p", Fix: "f", Category: "security", EstMinutes: 45,
		Evidence: "ev", Reviewers: []string{"greta", "kai"}, Confidence: "HIGH",
	}})
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, "CRITICAL|auth.go:42|p|f|security|45|ev|greta,kai|HIGH", lines[1])
}

func TestFinding_AsReconciled(t *testing.T) {
	src := Finding{
		Severity: "HIGH", File: "a.go", Line: 5, Problem: "p", Fix: "f",
		Category: "security", EstMinutes: 20, Evidence: "ev", Reviewer: "bruce",
	}
	rec := src.AsReconciled([]string{"bruce", "greta"}, "HIGH")
	assert.Empty(t, rec.Reviewer)
	assert.Equal(t, []string{"bruce", "greta"}, rec.Reviewers)
	assert.Equal(t, "HIGH", rec.Confidence)
	// Detail/location fields carry across unchanged.
	assert.Equal(t, "a.go", rec.File)
	assert.Equal(t, 5, rec.Line)
	assert.Equal(t, "p", rec.Problem)

	var b strings.Builder
	require.NoError(t, WriteReconciled(&b, []Finding{rec}))
	res, err := ParseReconciled([]byte(b.String()))
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Equal(t, []string{"bruce", "greta"}, res.Findings[0].Reviewers)
}

func TestRoundTrip_SourceThenReconciled(t *testing.T) {
	src := []Finding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p1", Fix: "f1", Category: "correctness", EstMinutes: 10, Evidence: "e1", Reviewer: "bruce"},
		{Severity: "LOW", File: "b.go", Line: 0, Problem: "p2", Fix: "f2", Category: "style", EstMinutes: 5, Evidence: "e2", Reviewer: "otto"},
	}
	var b strings.Builder
	require.NoError(t, WriteSource(&b, src))
	res, err := ParseSource([]byte(b.String()))
	require.NoError(t, err)
	require.Len(t, res.Findings, 2)
	assert.Equal(t, src[0].File, res.Findings[0].File)
	assert.Equal(t, src[0].Line, res.Findings[0].Line)
	assert.Equal(t, src[1].Reviewer, res.Findings[1].Reviewer)
}
