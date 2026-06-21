package reconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitRepoWithFiles initializes a throwaway git repo, commits the given tracked
// relpaths, and returns the root. The candidate index reads `git ls-files`, so
// suggestions only work against tracked files.
func gitRepoWithFiles(t *testing.T, relpaths ...string) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	run("config", "user.email", "t@t.t")
	run("config", "user.name", "t")
	for _, rel := range relpaths {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte("package x\n"), 0o644))
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return root
}

// TestRunReconcile_SuggestsHallucinatedPathEndToEnd is the Epic 5.4 AC8 end-to-
// end acceptance test: a review citing a typo'd path (validator.go, where the
// real tracked file is validate.go in the same directory) flows through the full
// reconcile pipeline against a real git repo, and the correction surfaces as a
// PathSuggestion in the merged result, in findings.json (path_suggestion), and
// as a "(did you mean …)" clause in report.md — while the original hallucinated
// path is preserved (suggest-only, AC7).
func TestRunReconcile_SuggestsHallucinatedPathEndToEnd(t *testing.T) {
	root := gitRepoWithFiles(t, "internal/auth/validate.go")

	reviewDir := t.TempDir()
	sources := filepath.Join(reviewDir, "sources")
	writeFindings(t, sources, "greta/findings.txt",
		"HIGH|internal/auth/validator.go:12|hallucinated path finding|fix|security|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)

	hall := res.Findings[0]
	assert.Equal(t, "internal/auth/validator.go", hall.File, "original cited path preserved (AC7)")
	assert.False(t, hall.PathValid)
	assert.Equal(t, stream.PathNotFoundWarning, hall.PathWarning)
	assert.Equal(t, "internal/auth/validate.go", hall.PathSuggestion)

	// findings.json carries the suggestion (report command's input).
	js, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, js, 1)
	assert.Equal(t, "internal/auth/validate.go", js[0].PathSuggestion)
	assert.Equal(t, "internal/auth/validator.go", js[0].File)

	// report.md shows the "(did you mean …)" correction.
	reportMD, err := os.ReadFile(filepath.Join(reviewDir, "reconciled", ReportMD))
	require.NoError(t, err)
	md := string(reportMD)
	assert.Contains(t, md, "⚠️ File not found: internal/auth/validator.go")
	assert.Contains(t, md, "did you mean")
	assert.Contains(t, md, "internal/auth/validate.go")
}
