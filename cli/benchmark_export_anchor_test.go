package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeThreeCaseSuite writes a suite whose manifest names case-01..case-03 — the
// same case ids writeCoverageRunResult declares — so a run-result built by that
// helper can be anchored against a real manifest rather than against itself.
func writeThreeCaseSuite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cases := ""
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("case-%02d", i)
		require.NoError(t, os.WriteFile(filepath.Join(dir, id+".diff"),
			[]byte("--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new // planted defect\n"), 0o600))
		if i > 1 {
			cases += ","
		}
		cases += fmt.Sprintf(`{"id":%q,"diff":%q,"expected_categories":["correctness"]}`, id, id+".diff")
	}
	manifest := fmt.Sprintf(`{"suite":"mini","suite_version":"1.2.0","cases":[%s]}`, cases)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"), []byte(manifest), 0o600))
	return dir
}

// writeTruncatedDenominatorRunResult writes an INTERNALLY CONSISTENT run-result
// that declares a two-case suite: both reviewer rows cover both declared cases and
// their `runs` agree. Every check keyed on the file alone passes it as full
// coverage — which is exactly the shape --suite-path exists to catch, because the
// real suite has three cases.
func writeTruncatedDenominatorRunResult(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02"],` +
		`"reviewer_coverage":[{"model":"m-primary","persona":"brad","case_ids":["case-01","case-02"]},` +
		`{"model":"m-backup","persona":"brad","case_ids":["case-01","case-02"]}],` +
		`"reviewers":[{"model":"m-primary","persona":"brad","runs":2,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10},` +
		`{"model":"m-backup","persona":"brad","runs":2,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// Without an external anchor the coverage gate's denominator is supplied by the
// same file it validates, so truncating suite_case_ids AND every row's case_ids to
// the same subset publishes a partial run as fully covered. --suite-path anchors
// the denominator to the suite manifest, where that shape is visible.
func TestBenchmarkExport_SuitePathRejectsTruncatedDenominator(t *testing.T) {
	in := writeTruncatedDenominatorRunResult(t)
	suite := writeThreeCaseSuite(t)

	code, out := execCmdCapture(t, "benchmark", "export", "--in", in, "--suite-path", suite)

	require.NotEqual(t, 0, code,
		"a run-result declaring a shrunken suite must not export against the real manifest: %s", out)
	assert.Contains(t, out, "case-03", "the error names the case the run-result dropped from its denominator")
	assert.Contains(t, out, "suite manifest", "and says the manifest is the authority it disagreed with")
}

// The anchor must not fire on the healthy path: a run-result whose denominator IS
// the manifest's case list exports exactly as it does without the flag.
func TestBenchmarkExport_SuitePathAcceptsMatchingDenominator(t *testing.T) {
	in := writeCoverageRunResult(t, fullCoverageRows, 3, 3)
	suite := writeThreeCaseSuite(t)

	code, out := execCmdCapture(t, "benchmark", "export", "--in", in, "--suite-path", suite)

	require.Equal(t, 0, code, "an anchored, fully-covered run-result exports: %s", out)
	assert.Contains(t, out, "benchmark-suite")
}

// runBenchmarkExport validates scrubbed case ids BEFORE anchoring so a poisoned id
// gets its own diagnostic — the anchor's "not in the manifest" text must never be
// the last word on a file whose published form differs from the checked one. With
// the calls swapped, this fixture fails with the anchor's mismatch text instead.
func TestBenchmarkExport_SuitePathScrubErrorPrecedesAnchor(t *testing.T) {
	suite := writeThreeCaseSuite(t)
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02","case-03","sk-io-pr-42"],` +
		`"reviewer_coverage":[{"model":"m-primary","persona":"brad","case_ids":["case-01","case-02","case-03"]},` +
		`{"model":"m-backup","persona":"brad","case_ids":["case-01","case-02","case-03"]}],` +
		`"reviewers":[{"model":"m-primary","persona":"brad","runs":3,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10},` +
		`{"model":"m-backup","persona":"brad","runs":3,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path, "--suite-path", suite)

	require.NotEqual(t, 0, code, "a scrub-poisoned id must not export: %s", out)
	assert.Contains(t, out, "publication scrub rewrites",
		"the scrub gate's own diagnostic must fire")
	assert.NotContains(t, out, "suite manifest at",
		"the anchor's mismatch text must not be the last word on a scrub-poisoned file")
}

// Anchoring to the WRONG suite is an operator error, not a coverage shortfall. The
// suite identity is checked before the case list so the diagnostic names the real
// mistake instead of reporting every case as missing.
func TestBenchmarkExport_SuitePathRejectsMismatchedSuiteIdentity(t *testing.T) {
	in := writeCoverageRunResult(t, fullCoverageRows, 3, 3)
	suite := writeThreeCaseSuite(t)
	manifest := `{"suite":"other","suite_version":"9.9.9","cases":[` +
		`{"id":"case-01","diff":"case-01.diff","expected_categories":["correctness"]}]}`
	require.NoError(t, os.WriteFile(filepath.Join(suite, "suite.json"), []byte(manifest), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", in, "--suite-path", suite)

	require.NotEqual(t, 0, code, "a run-result must not be anchored to a different suite: %s", out)
	assert.Contains(t, out, "other", "the manifest's identity is named")
	assert.Contains(t, out, "mini", "alongside the run-result's own")
}

// An unmeasured run-result (no denominator at all) WARNS and publishes when no
// anchor is offered — but passing --suite-path is a request to verify against the
// manifest, and there is nothing to verify. Failing here keeps the flag from
// reading as a check that silently did nothing.
func TestBenchmarkExport_SuitePathRejectsUnmeasuredRunResult(t *testing.T) {
	in := writeRunResult(t) // no suite_case_ids, no reviewer_coverage
	suite := writeThreeCaseSuite(t)

	code, out := execCmdCapture(t, "benchmark", "export", "--in", in, "--suite-path", suite)

	require.NotEqual(t, 0, code,
		"--suite-path must not silently pass a file that records no coverage: %s", out)
	assert.Contains(t, out, "no suite_case_ids")
}
