package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC 06-04: `atcr debt add` / `atcr report` operate IDENTICALLY on baseline
// (--all/--dir) review output as on diff output. These drive a real baseline review
// end-to-end (review --all -> reconcile) and then run the downstream commands against
// its zero-valued-Range review dir, proving no provenance-specific branch is needed.

// baselineReviewWithReconciled runs a real `atcr review --all` + `atcr reconcile` in
// an isolated temp repo and returns nothing — the review is the .atcr/latest anchor
// the downstream commands resolve. Files are sized to force a multi-chunk baseline
// fan-out so the reconciled output is genuinely baseline-derived.
func baselineReviewWithReconciled(t *testing.T) {
	t.Helper()
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	// Two oversized files force per-file baseline chunks (multi-chunk fan-out).
	big := make([]byte, 80_000)
	for i := range big {
		big[i] = 'x'
	}
	require.NoError(t, os.WriteFile("big1.txt", big, 0o644))
	require.NoError(t, os.WriteFile("big2.txt", big, 0o644))
	gitRun(t, "add", "-A")
	gitRun(t, "commit", "-qm", "add big files")
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")
	require.Equal(t, 0, execCmd(t, "review", "--all"))
	require.Equal(t, 0, execCmd(t, "reconcile"))
}

// AC 06-04 Scenario 1: `atcr report` renders a baseline review identically to a diff
// review — md and json both exit 0 via the same rendering path.
func TestBaselineDownstream_ReportRendersMdAndJson(t *testing.T) {
	baselineReviewWithReconciled(t)
	require.Equal(t, 0, execCmd(t, "report", "--format", "md"), "report --format md over a baseline review exits 0")
	require.Equal(t, 0, execCmd(t, "report", "--format", "json"), "report --format json over a baseline review exits 0")
}

// AC 06-04 Edge Case 1: `atcr report` over a baseline review's zero-valued Range
// renders no blank/malformed base..head line — report reads only findings.json.
func TestBaselineDownstream_ReportNoMalformedRange(t *testing.T) {
	baselineReviewWithReconciled(t)
	code, out := execCmdCapture(t, "report", "--format", "md")
	require.Equal(t, 0, code)
	require.NotEmpty(t, out)
	assert.NotContains(t, out, "base..head", "no base..head placeholder for a range-less baseline review")
	assert.NotContains(t, out, "..head", "no malformed empty-base range reference")
	assert.NotContains(t, out, "base..", "no malformed empty-head range reference")
}

// AC 06-04 Scenario 2: `atcr debt add` files a baseline-sourced finding into the
// technical-debt store and exits 0, identically to a diff-sourced finding. debt add
// consumes flag-driven fields directly (no review-dir/provenance read), so it is
// exercised standalone with a non-TTY stdin (flag mode, no wizard).
func TestBaselineDownstream_DebtAddFilesBaselineFinding(t *testing.T) {
	readme, items := emptyTDRepo(t) // pre-seeds the README AppendItem reads

	cmd := newDebtCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(&bytes.Buffer{}) // non-TTY → flag mode, never the wizard
	cmd.SetArgs([]string{"add",
		"--readme", readme, "--items", items,
		"--severity", "HIGH", "--file", "a.txt:1",
		"--problem", "Unchecked call from baseline scan", "--fix", "Guard it",
		"--category", "security", "--est", "15", "--date", "2026-07-26"})
	require.NoError(t, cmd.Execute(), "debt add of a baseline-sourced finding succeeds")

	data, err := os.ReadFile(readme)
	require.NoError(t, err)
	body := string(data)
	assert.Contains(t, body, "a.txt:1", "the baseline finding's location is filed")
	assert.Contains(t, body, "Unchecked call from baseline scan", "the baseline finding's problem is filed")
}

// AC 06-04 Error Scenario 1: `atcr report` against a baseline review with no
// reconciled output yet returns the existing "run reconcile first" usage error (exit
// 2), unchanged for baseline provenance.
func TestBaselineDownstream_ReportMissingReconcileErrors(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")
	require.Equal(t, 0, execCmd(t, "review", "--all")) // no reconcile step
	code, out := execCmdCapture(t, "report", "--format", "md")
	assert.Equal(t, 2, code, "report before reconcile exits 2 for baseline, same as diff")
	assert.True(t, strings.Contains(out, "reconcile") || strings.Contains(out, "reconciled"),
		"the error guides the user to run reconcile first")
}

// AC 06-04 Error Scenario 2: `atcr debt add` with a missing required flag against a
// baseline finding returns the existing missing-flags usage error (exit 2) on a
// non-interactive (non-TTY) shell — unchanged for baseline provenance.
func TestBaselineDownstream_DebtAddMissingFlagErrors(t *testing.T) {
	readme := filepath.Join(t.TempDir(), "TECH_DEBT.md")
	items := filepath.Join(t.TempDir(), "items")

	cmd := newDebtCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(&bytes.Buffer{}) // non-TTY → no wizard, so missing flags are a usage error
	// Omit --fix.
	cmd.SetArgs([]string{"add",
		"--readme", readme, "--items", items,
		"--severity", "HIGH", "--file", "a.txt:1",
		"--problem", "p", "--category", "security"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err), "missing --fix exits 2 for baseline provenance, same as diff")
	assert.Contains(t, err.Error(), "fix", "the error names the missing flag")
}
