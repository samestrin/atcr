package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/report"
	"github.com/stretchr/testify/assert"
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

func TestReportCmd_EmptyFindingsFileIsOperationalError(t *testing.T) {
	// A present-but-empty/malformed findings.json is a parse/IO error (exit 1),
	// not a usage error (exit 2). Exit 2 is reserved for genuinely absent data.
	isolate(t)
	fixtureReconciled(t, "r", "") // 0-byte findings.json → present but malformed
	require.Equal(t, 1, execCmd(t, "report", "r"))
}

func TestReportCmd_SymlinkOutputToSystemDirIsUsageError(t *testing.T) {
	// Epic 4.3 hardening (reviewers: otto, greta): a --output that is a symlink
	// into a system directory must be rejected. filepath.Abs alone validates the
	// link path (which passes the prefix check) while os.WriteFile follows the
	// link to the real system file — so symlinks must be resolved before
	// validation, and the resolved path must be the one written.
	isolate(t)
	fixtureReconciled(t, "r", oneFinding)

	link := filepath.Join(t.TempDir(), "sneaky.md")
	require.NoError(t, os.Symlink("/etc/hosts", link))

	code, out := execCmdCapture(t, "report", "--output", link, "r")
	require.Equal(t, 2, code)
	require.Contains(t, out, "must not reference system directories")
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

// TestReportCmd_HelpMentionsSarif asserts the --format flag usage AND the command
// Short description both enumerate sarif (AC 01-04 Edge Case 1 / Story-Specific).
// Both surface in `atcr report --help`, so a stale Short would omit sarif from the
// summary line. RED until task 3.2 updates both strings.
func TestReportCmd_HelpMentionsSarif(t *testing.T) {
	cmd := newReportCmd()
	assert.Contains(t, cmd.Flags().Lookup("format").Usage, "sarif",
		"--format help text must list sarif")
	assert.Contains(t, cmd.Short, "sarif",
		"report command Short description must list sarif")
}

// TestReportCmd_SarifMatchesRender asserts `atcr report --format=sarif` output is
// byte-identical to calling report.Render(..., FormatSarif) directly — the CLI
// layer adds no formatting divergence (AC 01-04 Scenario 2).
func TestReportCmd_SarifMatchesRender(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_s", oneFinding)

	code, cliOut := execCmdCapture(t, "report", "--format", "sarif", "2026-06-10_s")
	require.Equal(t, 0, code)

	findings, err := reconcile.ReadReconciledFindings(filepath.Join(".atcr", "reviews", "2026-06-10_s"))
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, report.Render(&buf, findings, report.FormatSarif))

	assert.Equal(t, buf.String(), cliOut, "CLI --format=sarif must equal a direct report.Render")
	assert.Contains(t, cliOut, `"version": "2.1.0"`)
}

// TestReportCmd_SarifToOutputFile asserts --format=sarif --output writes the same
// bytes to a file that would have gone to stdout (AC 01-04 Edge Case 3).
func TestReportCmd_SarifToOutputFile(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_so", oneFinding)

	out := filepath.Join(t.TempDir(), "results.sarif")
	require.Equal(t, 0, execCmd(t, "report", "--format", "sarif", "--output", out, "2026-06-10_so"))
	data, err := os.ReadFile(out)
	require.NoError(t, err)

	findings, err := reconcile.ReadReconciledFindings(filepath.Join(".atcr", "reviews", "2026-06-10_so"))
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, report.Render(&buf, findings, report.FormatSarif))
	assert.Equal(t, buf.String(), string(data))
}
