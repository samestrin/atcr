package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 83.0 T5 end to end: a review records each reviewer's persona role in the
// review dir, and both markdown reports (the reconciled report.md and `atcr
// report`) label the reviewer `name (role)`. host has no persona of its own (it
// resolves to the shared _base template) and stays bare. The machine formats
// stay bare-name.
func TestReview_MarkdownReportsLabelReviewersWithTheirRole(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithWideChange(t)
	srv := perAgentMockProvider(t, map[string]string{
		"m-greta": "MEDIUM|wide.go:10|possible nil deref on this path|Guard it|correctness|10|ev",
		"m-host":  "MEDIUM|wide.go:10|possible nil deref on this path|Guard it|correctness|10|ev",
	})
	liveReviewConfig(t, srv.URL, "greta", "host")

	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^", "--fail-on", "CRITICAL"))

	id, err := os.ReadFile(filepath.Join(".atcr", "latest"))
	require.NoError(t, err)
	reviewDir := filepath.Join(".atcr", "reviews", strings.TrimSpace(string(id)))
	assert.Equal(t, map[string]string{"greta": "algorithmic correctness reviewer"},
		reconcile.ReadReviewerRoles(reviewDir), "the review records the roster's roles; host has none")

	md, err := os.ReadFile(filepath.Join(reviewDir, "reconciled", reconcile.ReportMD))
	require.NoError(t, err)
	assert.Contains(t, string(md), "reviewers: greta (algorithmic correctness reviewer), host\n")

	findings, err := os.ReadFile(filepath.Join(reviewDir, "reconciled", reconcile.FindingsJSON))
	require.NoError(t, err)
	assert.NotContains(t, string(findings), "algorithmic correctness reviewer", "findings.json stays bare-name")

	code, out, _ := execCmdSplit(t, "report")
	require.Equal(t, 0, code)
	assert.Contains(t, out, "reviewers: greta (algorithmic correctness reviewer), host\n")

	for _, format := range []string{"json", "sarif", "axi", "pipe", "checklist"} {
		code, out, _ := execCmdSplit(t, "report", "--format", format)
		require.Equal(t, 0, code, format)
		assert.NotContains(t, out, "algorithmic correctness reviewer", "--format %s stays bare-name", format)
	}
}

// `atcr report` on a review dir with no roles sidecar (a review from before
// T5, or one whose personas name no role) prints bare names, as before.
func TestReportCmd_NoRolesSidecarKeepsBareNames(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_r", oneFinding)
	code, out, _ := execCmdSplit(t, "report", "2026-06-10_r")
	require.Equal(t, 0, code)
	assert.Contains(t, out, "reviewers: greta, host\n")
}

// `atcr report` reads the sidecar from the review dir it renders.
func TestReportCmd_ReadsRolesSidecar(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_r", oneFinding)
	require.NoError(t, reconcile.WriteReviewerRoles(filepath.Join(".atcr", "reviews", "2026-06-10_r"),
		map[string]string{"greta": "algorithmic correctness reviewer"}))
	code, out, _ := execCmdSplit(t, "report", "2026-06-10_r")
	require.Equal(t, 0, code)
	assert.Contains(t, out, "reviewers: greta (algorithmic correctness reviewer), host\n")
}
