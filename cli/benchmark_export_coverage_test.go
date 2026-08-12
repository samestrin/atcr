package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeCoverageRunResult writes a run-result whose single suite has three cases and
// whose reviewer rows cover the case-id lists given, returning the file path.
func writeCoverageRunResult(t *testing.T, rows string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02","case-03"],` +
		`"reviewer_coverage":[` + rows + `],` +
		`"reviewers":[{"model":"m-primary","persona":"brad","runs":2,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10},` +
		`{"model":"m-backup","persona":"brad","runs":1,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

const fullCoverageRows = `{"model":"m-primary","persona":"brad","case_ids":["case-01","case-02","case-03"]},` +
	`{"model":"m-backup","persona":"brad","case_ids":["case-01","case-02","case-03"]}`

const shortCoverageRows = `{"model":"m-primary","persona":"brad","case_ids":["case-01","case-02"]},` +
	`{"model":"m-backup","persona":"brad","case_ids":["case-03"]}`

// A row measured over half the suite must not be published beside a full one. Case
// difficulty varies enormously across the suite, so two rows built from different
// subsets are not comparable — and splitting rows by realized model makes partial
// coverage the NORMAL outcome of a quota-limited run, not an exotic one.
func TestBenchmarkExport_RejectsPartialCoverage(t *testing.T) {
	in := writeCoverageRunResult(t, shortCoverageRows)
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)

	require.NotEqual(t, 0, code, "a partial-coverage run-result must not export by default: %s", out)
	assert.Contains(t, out, "m-primary", "the error must name the short row")
	assert.Contains(t, out, "m-backup", "every short row is named, not just the first")
	assert.Contains(t, out, "2/3", "the error must state the shortfall, not merely assert one")
	assert.Contains(t, out, "1/3")
	assert.Contains(t, out, "case-03", "the error must name a missing case so it is actionable")
}

// Full coverage exports normally — the gate must not fire on the healthy path.
func TestBenchmarkExport_AcceptsFullCoverage(t *testing.T) {
	in := writeCoverageRunResult(t, fullCoverageRows)
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.Equal(t, 0, code, "a fully-covered run-result exports: %s", out)
	assert.Contains(t, out, "benchmark-suite")
}

// Coverage is compared as a SET, not a count: a row with the right NUMBER of cases
// but the wrong ones is still short. A length check would pass this and publish a row
// measured over a case the suite does not contain.
func TestBenchmarkExport_RejectsRightCountWrongCases(t *testing.T) {
	in := writeCoverageRunResult(t,
		`{"model":"m-primary","persona":"brad","case_ids":["case-01","case-02","case-99"]},`+
			`{"model":"m-backup","persona":"brad","case_ids":["case-01","case-02","case-03"]}`)
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.NotEqual(t, 0, code, "three ids that are not THE three ids is still partial coverage: %s", out)
	assert.Contains(t, out, "case-03", "the missing suite case is named")
}

// The opt-out publishes, and the shortfall travels INTO the submission rather than
// being dropped at the boundary. If the escape hatch silently produced a submission
// indistinguishable from a full one, it would reintroduce exactly the
// misrepresentation this epic exists to remove.
func TestBenchmarkExport_AllowPartialCoveragePublishesShortfall(t *testing.T) {
	in := writeCoverageRunResult(t, shortCoverageRows)
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in, "--allow-partial-coverage")

	require.Equal(t, 0, code, "the explicit opt-out permits publication: %s", out)
	assert.Contains(t, out, "reviewer_coverage", "the submission carries the coverage it was published under")
	assert.Contains(t, out, "suite_case_ids", "and the denominator that makes it interpretable")
	assert.Contains(t, out, "case-03")
}

// A run-result carrying NO coverage at all — any file produced before coverage
// existed — is UNMEASURED, not short. It exports, with a warning that coverage could
// not be verified. Failing closed here would reject every pre-existing run-result
// over a field they had no way to write.
func TestBenchmarkExport_UnmeasuredCoverageWarnsButExports(t *testing.T) {
	in := writeRunResult(t) // no suite_case_ids, no reviewer_coverage
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)

	require.Equal(t, 0, code, "an unmeasured run-result still exports: %s", out)
	assert.Contains(t, out, "coverage", "but the operator is told it could not be verified")
	assert.Contains(t, out, "benchmark-suite", "and the submission is still produced")
}

// Coverage rows present but the suite case-id list missing is equally unverifiable:
// there is no denominator to be short of. Treated as unmeasured, not as a violation.
func TestBenchmarkExport_CoverageWithoutSuiteCaseIDsIsUnmeasured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"reviewer_coverage":[{"model":"m","persona":"p","case_ids":["case-01"]}],` +
		`"reviewers":[{"model":"m","persona":"p","runs":1,"findings_raised_avg":1.0,` +
		`"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.Equal(t, 0, code, "no denominator -> unmeasured, not rejected: %s", out)
}

// A reviewer row with NO matching coverage row cannot be verified either, and must
// not pass silently — an unverifiable row is precisely what a hand-supplied
// run-result would use to slip past the gate.
func TestBenchmarkExport_RejectsReviewerRowWithoutCoverage(t *testing.T) {
	in := writeCoverageRunResult(t,
		`{"model":"m-primary","persona":"brad","case_ids":["case-01","case-02","case-03"]}`)
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.NotEqual(t, 0, code, "a reviewer row with no coverage row must not publish: %s", out)
	assert.Contains(t, out, "m-backup", "the unverifiable row is named")
}
