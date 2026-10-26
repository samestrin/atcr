package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/sast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sastReview builds a repo holding the files the fixture SARIF names, parses
// the fixture (no real scanner runs), writes the SAST sidecar, and lays down
// an LLM source with the given findings.txt body (empty: the source ran and
// found nothing).
func sastReview(t *testing.T, llmBody string) (root, reviewDir string) {
	t.Helper()
	root = gitRepoWithFiles(t, "app/db.go", "app/handler v2.go")
	f, err := os.Open(filepath.Join("..", "sast", "testdata", "semgrep.sarif"))
	require.NoError(t, err)
	defer f.Close()
	findings, _, err := sast.ParseSARIF(f, root)
	require.NoError(t, err)
	require.Len(t, findings, 2)

	reviewDir = t.TempDir()
	require.NoError(t, sast.WriteReport(reviewDir, sast.Report{Scanner: "semgrep", Findings: findings}))
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt", llmBody)
	return root, reviewDir
}

func runSASTReconcile(t *testing.T, root, reviewDir string) Result {
	t.Helper()
	withFakeTier4(t, &fakeTier4{})
	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	return res
}

// TestRunReconcile_SASTOnlyFindingsReachReconciledReport is the T4 success
// criterion: with no LLM finding at all, the SAST findings are in the
// reconciled report and ReadReconciledFindings returns them HIGH-confidence at
// a severity the default Fixer floor (MEDIUM) accepts.
func TestRunReconcile_SASTOnlyFindingsReachReconciledReport(t *testing.T) {
	root, reviewDir := sastReview(t, "")
	res := runSASTReconcile(t, root, reviewDir)

	require.Len(t, res.Findings, 2)
	assert.Equal(t, 2, res.Summary.TotalFindings)
	assert.Equal(t, 2, res.Summary.PerSourceCounts[sastSourceName])
	assert.Contains(t, res.Summary.SourcesScanned, sastSourceName)

	got, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, got, 2)

	db := got[0]
	assert.Equal(t, "app/db.go", db.File)
	assert.Equal(t, 12, db.Line)
	assert.Equal(t, ConfHigh, db.Confidence)
	assert.True(t, AtOrAbove(db.Severity, SevMedium), "severity %q must meet the default min_severity_for_fix", db.Severity)
	assert.Equal(t, "security", db.Category)
	assert.Equal(t, []string{"sast-semgrep"}, db.Reviewers)
	assert.Contains(t, db.Problem, "String-formatted SQL query")
	assert.Contains(t, db.Problem, "go.lang.security.audit.sqli.string-formatted-query")
	assert.NotEmpty(t, db.ID, "a SAST record carries a stamped wire id like any other")
	assert.Empty(t, db.PathWarning)

	assert.Equal(t, "app/handler v2.go", got[1].File)
	assert.Equal(t, 40, got[1].Line)

	report, err := os.ReadFile(filepath.Join(reviewDir, reconciledSubdir, ReportMD))
	require.NoError(t, err)
	assert.Contains(t, string(report), "String-formatted SQL query")
}

// SAST findings are appended beside the LLM findings, never folded into them,
// and the combined set keeps the severity-first report order.
func TestRunReconcile_SASTFindingsMergeBesideLLMFindings(t *testing.T) {
	root, reviewDir := sastReview(t,
		"CRITICAL|app/db.go:3|`Open` leaks the handle|close it|bug|10|ev|greta\n"+
			"LOW|app/db.go:30|`Query` is slow|cache it|perf|5|ev|greta\n")
	res := runSASTReconcile(t, root, reviewDir)

	got, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, 4, res.Summary.TotalFindings)
	assert.Equal(t, SevCritical, got[0].Severity)
	assert.Equal(t, []string{"greta"}, got[0].Reviewers)
	assert.Equal(t, SevLow, got[3].Severity)
	for _, f := range got[1:3] {
		assert.Equal(t, []string{"sast-semgrep"}, f.Reviewers)
		assert.Equal(t, ConfHigh, f.Confidence)
	}
}

// A review that ran no scanner (the default: SAST is off) reconciles exactly
// as before.
func TestRunReconcile_NoSASTReportIsUnchanged(t *testing.T) {
	root := gitRepoWithFiles(t, "app/db.go")
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"LOW|app/db.go:1|`Query` is slow|cache it|perf|5|ev|greta\n")
	res := runSASTReconcile(t, root, reviewDir)

	require.Len(t, res.Findings, 1)
	assert.NotContains(t, res.Summary.SourcesScanned, sastSourceName)
	_, ok := res.Summary.PerSourceCounts[sastSourceName]
	assert.False(t, ok)
}

// A corrupt sidecar is a hard error, like a malformed adjudication.json: the
// run must not report "no SAST findings" when it could not tell.
func TestRunReconcile_MalformedSASTReportFails(t *testing.T) {
	root := gitRepoWithFiles(t, "app/db.go")
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt", "")
	require.NoError(t, os.WriteFile(filepath.Join(reviewDir, sast.ReportFile),
		[]byte(`{"scanner":"semgrep","findings":[{"file":"../x.go","line":1}]}`), 0o644))

	withFakeTier4(t, &fakeTier4{})
	_, err := RunReconcile(context.Background(), reviewDir, nil, Options{Root: root})
	require.Error(t, err)
	_, statErr := os.Stat(filepath.Join(reviewDir, reconciledSubdir, FindingsJSON))
	assert.True(t, os.IsNotExist(statErr), "nothing is emitted on a corrupt sidecar")
}

// An ignore rule suppresses a SAST finding the same as any other.
func TestRunReconcile_IgnoreRuleSuppressesSASTFinding(t *testing.T) {
	root, reviewDir := sastReview(t, "")
	writeIgnoreYAML(t, root, "rules:\n"+
		"  - file: app/db.go\n"+
		"    category: security\n"+
		"    match: SQL\n"+
		"    reason: parameterized upstream\n")
	res := runSASTReconcile(t, root, reviewDir)

	require.Len(t, res.Suppressed, 1)
	assert.Equal(t, "app/db.go", res.Suppressed[0].File)
	require.Len(t, res.Findings, 1)
	assert.Equal(t, "app/handler v2.go", res.Findings[0].File)
}
