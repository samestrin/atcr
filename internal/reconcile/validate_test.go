package reconcile

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Path validation operates on the JSONFinding records (the extracted library
// Merged no longer carries path-validation fields — Epic 8.0 Phase 2
// Clarification Q1). validateFindingPaths stamps PathValid/PathWarning/
// PathSuggestion onto those records, RunReconcile caches them on the Result, and
// the report/findings.json layers read them from there.

// TestValidateFindingPaths_SkipsWhenRootEmpty: with no base dir configured,
// validation is a no-op so existing reconcile tests (synthetic paths) are never
// falsely flagged.
func TestValidateFindingPaths_SkipsWhenRootEmpty(t *testing.T) {
	findings := []JSONFinding{{File: "does/not/exist.go"}}
	validateFindingPaths(context.Background(), findings, "")

	assert.False(t, findings[0].PathValid)
	assert.Empty(t, findings[0].PathWarning)
}

// TestValidateFindingPaths_StampsWhenRootSet: a configured root validates each
// finding record against the filesystem.
func TestValidateFindingPaths_StampsWhenRootSet(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "exists.go"), []byte("package x\n"), 0o644))

	findings := []JSONFinding{
		{File: "exists.go"},
		{File: "missing.go"},
	}
	validateFindingPaths(context.Background(), findings, root)

	assert.True(t, findings[0].PathValid)
	assert.Empty(t, findings[0].PathWarning)
	assert.False(t, findings[1].PathValid)
	assert.Equal(t, stream.PathNotFoundWarning, findings[1].PathWarning)
}

// TestValidateFindingPaths_FlowsIntoJSONRecord: a stamped warning rides on the
// findings.json record the report command reads.
func TestValidateFindingPaths_FlowsIntoJSONRecord(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "real.go"), []byte("package x\n"), 0o644))

	js := []JSONFinding{
		{Severity: "HIGH", File: "missing.go", Line: 7},
		{Severity: "LOW", File: "real.go", Line: 1},
	}
	validateFindingPaths(context.Background(), js, root)

	require.Len(t, js, 2)
	assert.False(t, js[0].PathValid)
	assert.Equal(t, stream.PathNotFoundWarning, js[0].PathWarning)
	assert.True(t, js[1].PathValid)
	assert.Empty(t, js[1].PathWarning)
}

// resultWithPaths builds a Result whose cached path-stamped JSONFinding records
// carry the given path fields, mirroring what RunReconcile produces after
// validateFindingPaths runs.
func resultWithPaths(recs ...JSONFinding) Result {
	merged := make([]Merged, len(recs))
	for i, r := range recs {
		merged[i] = Merged{Finding: Finding{
			Severity: r.Severity, File: r.File, Line: r.Line, Problem: r.Problem,
			Confidence: r.Confidence,
		}}
	}
	return Result{Findings: merged, jsonFindings: recs}
}

// TestRenderMarkdown_ShowsPathWarning: report.md surfaces a per-finding warning
// for a hallucinated path (AC3), preserving the finding (AC4).
func TestRenderMarkdown_ShowsPathWarning(t *testing.T) {
	r := resultWithPaths(JSONFinding{
		Severity: "HIGH", File: "internal/auth/validator.go", Line: 12,
		Problem: "token never expires", Confidence: "MEDIUM",
		PathValid: false, PathWarning: stream.PathNotFoundWarning,
	})

	var b bytes.Buffer
	require.NoError(t, RenderMarkdown(&b, r))
	out := b.String()
	assert.Contains(t, out, "⚠️ File not found: internal/auth/validator.go")
	// The finding itself is preserved, not discarded.
	assert.Contains(t, out, "token never expires")
}

// TestRenderMarkdown_NoWarningWhenValid: a valid path adds no warning line, so
// report.md for clean findings is unchanged.
func TestRenderMarkdown_NoWarningWhenValid(t *testing.T) {
	r := resultWithPaths(JSONFinding{
		Severity: "HIGH", File: "a.go", Line: 1,
		Problem: "x", Confidence: "MEDIUM", PathValid: true,
	})

	var b bytes.Buffer
	require.NoError(t, RenderMarkdown(&b, r))
	assert.NotContains(t, b.String(), "File not found")
}

// TestRunReconcile_FlagsHallucinatedPathEndToEnd is the AC6 acceptance test: a
// real review with a hallucinated path (validator.go, a typo for validate.go)
// flows through the full pipeline and the warning surfaces in findings.json and
// report.md, while the real-path finding stays clean and both findings are
// preserved. The path-validated records are read from res.JSONFindings() (the
// cached, path-stamped records — Epic 8.0 Q1), not res.Findings.
func TestRunReconcile_FlagsHallucinatedPathEndToEnd(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal/auth"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal/auth/validate.go"), []byte("package auth\n"), 0o644))

	reviewDir := t.TempDir()
	sources := filepath.Join(reviewDir, "sources")
	writeFindings(t, sources, "greta/findings.txt",
		"HIGH|internal/auth/validate.go:10|real finding|fix|security|10|ev|greta\n"+
			"HIGH|internal/auth/validator.go:12|hallucinated path finding|fix|security|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 2)

	byFile := map[string]JSONFinding{}
	for _, m := range res.JSONFindings() {
		byFile[m.File] = m
	}
	valid := byFile["internal/auth/validate.go"]
	hall := byFile["internal/auth/validator.go"]
	assert.True(t, valid.PathValid)
	assert.Empty(t, valid.PathWarning)
	assert.False(t, hall.PathValid)
	assert.Equal(t, stream.PathNotFoundWarning, hall.PathWarning)

	// findings.json (the report command's input) carries the warning.
	js, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	var sawHallucinated bool
	for _, f := range js {
		switch f.File {
		case "internal/auth/validator.go":
			sawHallucinated = true
			assert.Equal(t, stream.PathNotFoundWarning, f.PathWarning)
		case "internal/auth/validate.go":
			assert.Empty(t, f.PathWarning)
		}
	}
	assert.True(t, sawHallucinated, "hallucinated finding present in findings.json")

	// report.md shows the warning and preserves the finding (AC3, AC4).
	reportMD, err := os.ReadFile(filepath.Join(reviewDir, "reconciled", ReportMD))
	require.NoError(t, err)
	assert.Contains(t, string(reportMD), "⚠️ File not found: internal/auth/validator.go")
	assert.Contains(t, string(reportMD), "hallucinated path finding")
}
