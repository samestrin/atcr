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

// fixtureReconciledWithAmbiguous writes findings.json plus an ambiguous.json
// sidecar under a review dir, with a .atcr/latest pointer.
func fixtureReconciledWithAmbiguous(t *testing.T, id, findingsJSON, ambiguousJSON string) {
	t.Helper()
	dir := filepath.Join(".atcr", "reviews", id, "reconciled")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "findings.json"), []byte(findingsJSON), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ambiguous.json"), []byte(ambiguousJSON), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "latest"), []byte(id+"\n"), 0o644))
}

const splitFinding = `[{"severity":"CRITICAL","file":"a.go","line":1,"problem":"boom","fix":"f","category":"security","est_minutes":10,"evidence":"e","reviewers":["greta","host"],"confidence":"HIGH","disagreement":"LOW vs CRITICAL"}]`

func TestReportCmd_DisagreementsMode(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_d", splitFinding)

	out := filepath.Join(t.TempDir(), "radar.md")
	require.Equal(t, 0, execCmd(t, "report", "--disagreements", "--output", out, "2026-06-10_d"))
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	s := string(data)
	require.Contains(t, s, "Disagreement Radar")
	require.Contains(t, s, "LOW vs CRITICAL")
	require.Contains(t, s, "`a.go:1`")
}

func TestReportCmd_DisagreementsModeReadsAmbiguousSidecar(t *testing.T) {
	isolate(t)
	ambiguous := `[{"id":"amb-1","file":"g.go","line":7,"similarity":0.55,"findings":[` +
		`{"severity":"HIGH","file":"g.go","line":7,"problem":"overrun","reviewer":"greta"},` +
		`{"severity":"LOW","file":"g.go","line":8,"problem":"bounds","reviewer":"kai"}]}]`
	fixtureReconciledWithAmbiguous(t, "2026-06-10_g", oneFinding, ambiguous)

	code, out := execCmdCapture(t, "report", "--disagreements", "2026-06-10_g")
	require.Equal(t, 0, code)
	// The ambiguous sidecar must surface as a gray-zone cluster in the rendered
	// radar — not merely parse without error. Assert the cluster's kind, location,
	// representative problem, and both reviewers' side-by-side positions reach the
	// output, so a regression that silently dropped gray-zone rendering would fail.
	require.Contains(t, out, "gray_zone")
	require.Contains(t, out, "`g.go:7`")
	require.Contains(t, out, "overrun")
	require.Contains(t, out, "bounds")
	require.Contains(t, out, "kai")
}

func TestReportCmd_DisagreementsModeEmptyIsClean(t *testing.T) {
	isolate(t)
	// A multi-reviewer consensus finding with no disagreement → no tension.
	consensus := `[{"severity":"HIGH","file":"a.go","line":1,"problem":"p","reviewers":["greta","host"],"confidence":"HIGH"}]`
	fixtureReconciled(t, "2026-06-10_c", consensus)
	require.Equal(t, 0, execCmd(t, "report", "--disagreements", "2026-06-10_c"))
}

func TestReportCmd_DisagreementsModeMalformedAmbiguousIsUsageError(t *testing.T) {
	isolate(t)
	fixtureReconciledWithAmbiguous(t, "2026-06-10_bad", oneFinding, "{not json")
	require.Equal(t, 2, execCmd(t, "report", "--disagreements", "2026-06-10_bad"))
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

func TestReportCmd_OutputWriteFailureIsUsageError(t *testing.T) {
	// A local I/O failure is an infrastructure/usage error (exit 2), matching
	// how `atcr reconcile` classifies the same disk-write failure class.
	isolate(t)
	fixtureReconciled(t, "r", oneFinding)

	ro := filepath.Join(t.TempDir(), "ro")
	require.NoError(t, os.MkdirAll(ro, 0o555)) // read-only dir → WriteFile fails
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	require.Equal(t, 2, execCmd(t, "report", "--output", filepath.Join(ro, "report.md"), "r"))
}

func TestReportCmd_SystemDirOutputIsUsageError(t *testing.T) {
	// Epic 4.3: a --output path under a system directory is rejected by the
	// input-validation layer (exit 2) before any rendering — not left to the
	// filesystem to refuse on write.
	isolate(t)
	fixtureReconciled(t, "r", oneFinding)
	code, out := execCmdCapture(t, "report", "--output", "/etc/atcr-report.md", "r")
	require.Equal(t, 2, code)
	require.Contains(t, out, "must not reference system directories")
}

func TestReportCmd_EmptyFindingsFileIsUsageError(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "r", "") // 0-byte findings.json → malformed, not "no findings"
	require.Equal(t, 2, execCmd(t, "report", "r"))
}

func TestReportCmd_DisagreementsWithJSONFormat(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_dj", splitFinding)

	out := filepath.Join(t.TempDir(), "radar.json")
	require.Equal(t, 0, execCmd(t, "report", "--disagreements", "--format", "json", "--output", out, "2026-06-10_dj"))
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	s := string(data)
	require.Contains(t, s, `"schemaVersion"`, "JSON output must include the schema version")
	require.Contains(t, s, `"items"`, "JSON output must include the items array")
	require.Contains(t, s, `"LOW vs CRITICAL"`, "JSON output must include the disagreement annotation")
}
