package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// manyFindingsJSON marshals n minimal reconciled findings so a report --axi run
// renders more than a chosen line cap.
func manyFindingsJSON(t *testing.T, n int) string {
	t.Helper()
	fs := make([]reconcile.JSONFinding, n)
	for i := range fs {
		fs[i] = reconcile.JSONFinding{Severity: "LOW", File: "a.go", Line: i + 1,
			Problem: "p", Category: "style", Confidence: "LOW"}
	}
	data, err := json.Marshal(fs)
	require.NoError(t, err)
	return string(data)
}

// TestReportCmd_AXITruncatesAtEnvCap is AC 03-04 Scenario 2 / AC 03-01: report
// --axi honors ATCR_AXI_MAX_LINES, capping the payload content while the header N
// preserves the true total and the truncated flag is emitted.
func TestReportCmd_AXITruncatesAtEnvCap(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "r", manyFindingsJSON(t, 20))
	t.Setenv("ATCR_AXI_MAX_LINES", "5")
	code, out := execCmdCapture(t, "report", "--format", "axi", "r")
	require.Equal(t, 0, code)
	assert.Contains(t, out, "truncated: true", "over-cap report --axi flags truncation")
	assert.Contains(t, out, "findings[20|]{", "header declares the true total (20), not the capped row count")
	content := strings.TrimSuffix(out, "truncated: true\n")
	assert.Equal(t, 5, strings.Count(content, "\n"), "content capped to exactly the env cap (5 physical lines)")
}

// TestReportCmd_AXIUnderCapNotTruncated is AC 03-01 Scenario 1 at the command
// level: an under-cap report --axi emits truncated: false with the full row set.
func TestReportCmd_AXIUnderCapNotTruncated(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "r", manyFindingsJSON(t, 20))
	code, out := execCmdCapture(t, "report", "--format", "axi", "r")
	require.Equal(t, 0, code)
	assert.Contains(t, out, "truncated: false", "under the default cap, report --axi is not truncated")
	assert.Contains(t, out, "findings[20|]{", "header declares the true total")
}

// TestReportCmd_AXIUsesSharedPaginationWrapper is AC 03-04 Error Scenario 1 /
// Edge Case 2: report --axi's output must be byte-identical to the shared
// report.RenderAXIPaginated for the same findings + cap, proving it routes
// through the single shared step rather than a private truncation copy.
func TestReportCmd_AXIUsesSharedPaginationWrapper(t *testing.T) {
	isolate(t)
	js := manyFindingsJSON(t, 12)
	fixtureReconciled(t, "r", js)
	t.Setenv("ATCR_AXI_MAX_LINES", "6")
	_, cmdOut := execCmdCapture(t, "report", "--format", "axi", "r")

	var fs []reconcile.JSONFinding
	require.NoError(t, json.Unmarshal([]byte(js), &fs))
	var want bytes.Buffer
	require.NoError(t, report.RenderAXIPaginated(&want, fs, 6))
	assert.Equal(t, want.String(), cmdOut, "report --axi must match the shared wrapper byte-for-byte (no private truncation logic)")
}

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

// TestReportCmd_DisagreementsWithSarifIsUsageError pins AC 01-04 Error Scenario 1:
// --disagreements is incompatible with --format=sarif (the radar view supports
// only md/json), so the combination is a usage error (exit 2) whose message names
// sarif — mirroring the existing checklist/json incompatibility.
func TestReportCmd_DisagreementsWithSarifIsUsageError(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "2026-06-10_ds", splitFinding)
	code, out := execCmdCapture(t, "report", "--disagreements", "--format", "sarif", "2026-06-10_ds")
	require.Equal(t, 2, code)
	require.Contains(t, out, "sarif")
}

// TestResolveOutputPath_ResolvesSymlinkedParentForNewLeaf locks the fix for the
// new-file symlink-parent bypass: when --output names a not-yet-existing file
// inside a symlinked directory, filepath.EvalSymlinks fails on the whole path
// (the leaf is absent), and the old code fell open to the un-resolved link path —
// leaving the parent symlink to be followed only later by os.WriteFile, after
// validation had vetted the wrong path. resolveOutputPath must resolve the parent
// symlink so validation and the write both act on the real parent directory.
func TestResolveOutputPath_ResolvesSymlinkedParentForNewLeaf(t *testing.T) {
	realDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(realDir, link))

	// Leaf does not exist yet, so the whole-path EvalSymlinks inside
	// resolveOutputPath fails and the parent-directory resolution must kick in.
	target := filepath.Join(link, "report.json")

	got, err := resolveOutputPath(target)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(realDir, "report.json"), got,
		"resolveOutputPath must resolve a symlinked parent even when the leaf is a new file")
}

// TestResolveOutputPath_FailsOpenWhenParentAbsent keeps the fail-open contract for
// a genuinely absent parent: neither the leaf nor its directory exist on disk, so
// there is nothing to resolve and the absolute (un-resolved) form is returned
// rather than erroring the write.
func TestResolveOutputPath_FailsOpenWhenParentAbsent(t *testing.T) {
	target := filepath.Join(t.TempDir(), "nope", "deeper", "report.json")

	got, err := resolveOutputPath(target)
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(got), "absent parent falls open to the absolute path, got %q", got)
	require.Equal(t, target, got)
}
