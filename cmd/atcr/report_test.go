package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// fixtureReconciled writes a reconciled/findings.json under ./.atcr/reviews/<id>
// and a .atcr/latest pointer.
func fixtureReconciled(t *testing.T, id, findingsJSON string) {
	t.Helper()
	dir := filepath.Join(".atcr", "reviews", id, "reconciled")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "findings.json"), []byte(findingsJSON), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "latest"), []byte(id+"\n"), 0o644))
}

const oneFinding = `[{"severity":"CRITICAL","file":"a.go","line":1,"problem":"boom","fix":"f","category":"security","est_minutes":10,"evidence":"e","reviewers":["greta","host"],"confidence":"HIGH"}]`

func TestReportCmd_DefaultFormatAndOutputFile(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_r", oneFinding)

	out := filepath.Join(t.TempDir(), "report.md")
	require.Equal(t, 0, execCmd(t, "report", "--output", out, "2026-06-10_r"))
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Contains(t, string(data), "# atcr Review Report", "default format is markdown")
	require.Contains(t, string(data), "`a.go:1`")
}

func TestReportCmd_InvalidFormatIsUsageError(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "r", oneFinding)
	require.Equal(t, 2, execCmd(t, "report", "--format", "xml", "r"))
}

func TestReportCmd_MissingReconciledDataIsUsageError(t *testing.T) {
	isolate(t)
	// A review dir exists but no reconciled/findings.json.
	require.NoError(t, os.MkdirAll(filepath.Join(".atcr", "reviews", "r", "sources"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "latest"), []byte("r\n"), 0o644))
	require.Equal(t, 2, execCmd(t, "report", "r"))
}

func TestReportCmd_DefaultsToLatest(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_latest", oneFinding)
	// No anchor arg → resolves .atcr/latest.
	require.Equal(t, 0, execCmd(t, "report", "--format", "checklist"))
}

func TestReportCmd_EmptyFindingsFileIsUsageError(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "r", "") // 0-byte findings.json → malformed, not "no findings"
	require.Equal(t, 2, execCmd(t, "report", "r"))
}
