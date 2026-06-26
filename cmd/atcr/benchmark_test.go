package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeValidSuite writes a minimal valid benchmark suite (manifest + one diff
// file) into a fresh temp dir and returns its path.
func writeValidSuite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "case-01.diff"),
		[]byte("--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new // planted defect\n"), 0o600))
	manifest := `{"suite":"mini","suite_version":"1.2.0","cases":[` +
		`{"id":"case-01","diff":"case-01.diff","expected_categories":["correctness"]}]}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"), []byte(manifest), 0o600))
	return dir
}

func TestBenchmarkVerify_ValidSuite(t *testing.T) {
	dir := writeValidSuite(t)
	code, out := execCmdCapture(t, "benchmark", "verify", "--suite-path", dir)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "mini")
	require.Contains(t, out, "1.2.0")
	require.Contains(t, out, "1 case")
	require.Regexp(t, "[0-9a-f]{64}", out, "verify must print the 64-hex reproducibility hash")
}

func TestBenchmarkVerify_InvalidSuiteFails(t *testing.T) {
	code, out := execCmdCapture(t, "benchmark", "verify", "--suite-path", t.TempDir())
	require.NotEqual(t, 0, code, "a directory without suite.json must fail verify: %s", out)
}

func TestBenchmarkVerify_RequiresSuitePath(t *testing.T) {
	code, _ := execCmdCapture(t, "benchmark", "verify")
	require.NotEqual(t, 0, code, "verify without --suite-path is a usage error")
}

func writeRunResult(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"reviewers":[{"model":"claude-sonnet-4-6","persona":"bruce","runs":2,` +
		`"findings_raised_avg":10.5,"corroboration_rate":0.6,` +
		`"cost_per_corroborated_finding_usd":0.006,"latency_p50_ms":8900}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestBenchmarkExport_ProducesSuiteTaggedJSON(t *testing.T) {
	in := writeRunResult(t)
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.Equal(t, 0, code, out)

	var sub struct {
		SubmissionSchema int    `json:"submission_schema"`
		Source           string `json:"source"`
		Suite            string `json:"suite"`
		SuiteVersion     string `json:"suite_version"`
		Reviewers        []struct {
			Persona string `json:"persona"`
		} `json:"reviewers"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &sub), "export stdout must be valid JSON: %s", out)
	require.Equal(t, 1, sub.SubmissionSchema)
	require.Equal(t, "benchmark-suite", sub.Source, "distinct from production export")
	require.Equal(t, "mini", sub.Suite)
	require.Equal(t, "1.2.0", sub.SuiteVersion)
	require.Len(t, sub.Reviewers, 1)
	require.Equal(t, "bruce", sub.Reviewers[0].Persona)
	// Distinct from production --export: the production envelope has no source/suite.
	require.NotContains(t, out, `"filters"`)
}

func TestBenchmarkExport_OutputFlagWritesFile(t *testing.T) {
	in := writeRunResult(t)
	dest := filepath.Join(t.TempDir(), "nested", "submission.json")
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in, "--output", dest)
	require.Equal(t, 0, code, out)
	require.NotContains(t, out, "submission_schema", "JSON goes to the file, not stdout")

	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Contains(t, string(data), "benchmark-suite")
}

func TestBenchmarkExport_UsesGeneratedAtForSubmittedAt(t *testing.T) {
	in := writeRunResult(t)
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.Equal(t, 0, code, out)

	var sub struct {
		SubmittedAt string `json:"submitted_at"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &sub))
	// The run-result has generated_at="2026-06-24T12:00:00Z" (see writeRunResult).
	// submitted_at must match it for reproducibility, not be time.Now().
	require.Equal(t, "2026-06-24T12:00:00Z", sub.SubmittedAt,
		"submitted_at must use run-result's generated_at for reproducibility")
}

func TestBenchmarkExport_RejectsWhitespaceOnlySuiteFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run-result.json")
	body := `{"suite":" ","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"reviewers":[{"model":"claude-sonnet-4-6","persona":"bruce","runs":2,` +
		`"findings_raised_avg":10.5,"corroboration_rate":0.6,` +
		`"cost_per_corroborated_finding_usd":0.006,"latency_p50_ms":8900}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "whitespace-only suite must fail: %s", out)
	require.Contains(t, out, "missing suite/suite_version")
}

func TestBenchmarkExport_RejectsEmptyReviewers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z","reviewers":[]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "empty reviewers must fail: %s", out)
	require.Contains(t, out, "no reviewers")
}

func TestBenchmarkExport_MissingInputFails(t *testing.T) {
	code, out := execCmdCapture(t, "benchmark", "export", "--in", filepath.Join(t.TempDir(), "nope.json"))
	require.NotEqual(t, 0, code, "a missing run-result file must fail export: %s", out)
}

func TestBenchmarkExport_RequiresInput(t *testing.T) {
	code, _ := execCmdCapture(t, "benchmark", "export")
	require.NotEqual(t, 0, code, "export without --in is a usage error")
}

// The --checkpoint flag routes through writeExportFile, which replaces a symlink
// at the target path rather than following it. That behavior must be documented
// in the flag help for parity with --out/--output.
func TestBenchmarkRunCmd_CheckpointHelpMentionsSymlink(t *testing.T) {
	cmd := newBenchmarkRunCmd()
	f := cmd.Flags().Lookup("checkpoint")
	require.NotNil(t, f, "benchmark run exposes a --checkpoint flag")
	require.Contains(t, f.Usage, "symlink", "checkpoint help must document symlink replace-not-follow behavior")
}
