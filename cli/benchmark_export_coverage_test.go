package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeCoverageRunResult writes a run-result whose suite has three cases and whose
// two reviewer rows carry the given coverage rows, returning the file path.
//
// runsPrimary/runsBackup are explicit rather than fixed because `runs` and the
// covered case set are written together by the real producer and must agree; a
// helper that hardcoded one of them would produce fixtures the gate rightly rejects
// as malformed, masking the property each test is actually about.
func writeCoverageRunResult(t *testing.T, rows string, runsPrimary, runsBackup int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := fmt.Sprintf(`{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",`+
		`"suite_case_ids":["case-01","case-02","case-03"],`+
		`"reviewer_coverage":[%s],`+
		`"reviewers":[{"model":"m-primary","persona":"brad","runs":%d,`+
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10},`+
		`{"model":"m-backup","persona":"brad","runs":%d,`+
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`,
		rows, runsPrimary, runsBackup)
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
	in := writeCoverageRunResult(t, shortCoverageRows, 2, 1)
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
	in := writeCoverageRunResult(t, fullCoverageRows, 3, 3)
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
			`{"model":"m-backup","persona":"brad","case_ids":["case-01","case-02","case-03"]}`, 3, 3)
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.NotEqual(t, 0, code, "three ids that are not THE three ids is still partial coverage: %s", out)
	assert.Contains(t, out, "case-99", "the foreign case id is named")
	assert.Contains(t, out, "not one of the 3 cases", "and identified as outside the suite")
}

// A coverage row that repeats a case id is rejected. Without this, ["case-01",
// "case-02","case-03","case-01"] satisfies a 3-case suite with nothing missing while
// reporting runs=4 — a row that scored one case twice, published with a denominator
// larger than the suite itself.
func TestBenchmarkExport_RejectsDuplicateCaseIDsWithinARow(t *testing.T) {
	in := writeCoverageRunResult(t,
		`{"model":"m-primary","persona":"brad","case_ids":["case-01","case-02","case-03","case-01"]},`+
			`{"model":"m-backup","persona":"brad","case_ids":["case-01","case-02","case-03"]}`, 4, 3)
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.NotEqual(t, 0, code, "a repeated case id must not buy extra coverage: %s", out)
	assert.Contains(t, out, "more than once in its coverage")
}

// The empty-coverage error must not report the RAW suite-id count: with a
// repeated suite_case_ids entry it would claim a larger suite than the distinct
// denominator every other diagnostic uses. The duplicate-id rejection fires
// first, so this file is told the truth about which shape of malformed it is.
func TestBenchmarkExport_DuplicateSuiteIDsWithStrippedCoverageReportsDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-01"],` +
		`"reviewers":[{"model":"m","persona":"p","runs":1,"findings_raised_avg":1.0,` +
		`"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "a doubly-malformed file still fails: %s", out)
	assert.Contains(t, out, "more than once", "the duplicate-id shape is named, not a miscounted suite size")
	assert.NotContains(t, out, "2-case suite")
}

// The Outcomes tally is written together with case_ids by the producer
// (applyReviewerOutcome increments one outcome per folded case), so its values
// must sum to len(case_ids). A hand-assembled run-result can otherwise present
// outcomes {"clean":17} on a row that covered two cases — the same tamper family
// the runs/coverage pair check closed, one field over. A present-but-wrong tally
// is malformed; an absent one stays legal (omitempty — pre-field files).
func TestBenchmarkExport_RejectsOutcomesTallyMismatch(t *testing.T) {
	in := writeCoverageRunResult(t,
		`{"model":"m-primary","persona":"brad","case_ids":["case-01","case-02","case-03"],`+
			`"outcomes":{"clean":17}},`+
			`{"model":"m-backup","persona":"brad","case_ids":["case-01","case-02","case-03"]}`, 3, 3)
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.NotEqual(t, 0, code, "a tally exceeding the covered set is a malformed file: %s", out)
	assert.Contains(t, out, "outcomes")
	assert.Contains(t, out, "malformed")
}

// Duplicate (model, persona) identities are rejected in reviewer_coverage; the
// reviewer array gets the same rule. Two identical reviewer rows both join the
// single coverage row and both publish, putting two different metric sets on the
// board under one identity — the rejection rationale ("a reviewer identity has
// exactly one covered case set") applies verbatim to the reviewer array.
func TestBenchmarkExport_RejectsDuplicateReviewerIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02","case-03"],` +
		`"reviewer_coverage":[{"model":"m","persona":"p","case_ids":["case-01","case-02","case-03"]}],` +
		`"reviewers":[{"model":"m","persona":"p","runs":3,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10},` +
		`{"model":"m","persona":"p","runs":3,` +
		`"findings_raised_avg":9.0,"corroboration_rate":0.9,"latency_p50_ms":20}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code,
		"two reviewer rows of one identity must be rejected, not both published: %s", out)
	assert.Contains(t, out, "more than once")
}

// The gate must key identity on the SCRUBBED (model, persona), because publication
// re-scrubs every reviewer via scorecard.ScrubPublicRecord and the board keys on
// the scrubbed value. Two raw identities that differ only by a scrub-stripped token
// are distinct to a raw-keyed duplicate check but identical on the public board —
// they pass here as two rows and publish as two rows of one identity.
func TestBenchmarkExport_RejectsScrubCollidingCoverageIdentities(t *testing.T) {
	// The reviewers array carries the same two raw identities, so the join
	// direction checks pass and only the scrub-collision duplicate check can fire.
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02","case-03"],` +
		`"reviewer_coverage":[{"model":"m","persona":"brad","case_ids":["case-01","case-02","case-03"]},` +
		`{"model":"m ~x","persona":"brad","case_ids":["case-01","case-02","case-03"]}],` +
		`"reviewers":[{"model":"m","persona":"brad","runs":3,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10},` +
		`{"model":"m ~x","persona":"brad","runs":3,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code,
		"two raw identities scrubbing to one public identity must be rejected as a duplicate: %s", out)
	assert.Contains(t, out, "scrub to the same published identity",
		"the rejection must name the collision it found; the wording is pinned in full by "+
			"TestBenchmarkExport_ScrubCollisionMessageNamesTheScrub")
}

// A scrub collision is REJECTED (above), but "this file is malformed" names the
// wrong cause for it. scrubField now iterates to a fixed point, so strictly more
// distinct raw identities collapse to one published value than when the producer's
// own collision check (benchmark_run.go) passed the file — a run-result written by
// an EARLIER atcr can therefore fail here having been perfectly well-formed when
// written. That is version skew, and the operator needs to be told to regenerate the
// file rather than to go hunting for tampering that did not happen.
func TestBenchmarkExport_ScrubCollisionMessageNamesTheScrub(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02","case-03"],` +
		`"reviewer_coverage":[{"model":"m","persona":"brad","case_ids":["case-01","case-02","case-03"]},` +
		`{"model":"m ~x","persona":"brad","case_ids":["case-01","case-02","case-03"]}],` +
		`"reviewers":[{"model":"m","persona":"brad","runs":3,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10},` +
		`{"model":"m ~x","persona":"brad","runs":3,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "the rejection itself must stand: %s", out)

	assert.Contains(t, out, "scrub", "the message must name the privacy scrub as the mechanism that collapsed the two identities")
	assert.Contains(t, out, "m ~x", "both colliding raw identities must be named so the operator can see they differ")
	assert.NotContains(t, out, "this file is malformed",
		"two DIFFERENT raw identities colliding under the scrub is version skew, not tampering — asserting malformed sends the operator after the wrong cause")
}

// A genuine duplicate — the SAME raw identity twice — keeps the malformed wording,
// so the skew message above cannot become a blanket excuse for a hand-assembled file.
func TestBenchmarkExport_IdenticalDuplicateIdentityIsStillMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02","case-03"],` +
		`"reviewer_coverage":[{"model":"m","persona":"brad","case_ids":["case-01","case-02","case-03"]},` +
		`{"model":"m","persona":"brad","case_ids":["case-01"]}],` +
		`"reviewers":[{"model":"m","persona":"brad","runs":3,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "a repeated identity must still be rejected: %s", out)
	assert.Contains(t, out, "this file is malformed",
		"one identity listed twice is hand-assembly, and must keep saying so")
}

// The reverse direction of the reviewer/coverage join is checked too: a coverage
// row with NO matching reviewer row is silently discarded today, so a row citing
// cases the suite never saw exports at exit 0 with no warning. A file whose two
// arrays disagree on which reviewers exist is malformed by the same argument as
// the forward direction.
func TestBenchmarkExport_RejectsOrphanCoverageRow(t *testing.T) {
	in := writeCoverageRunResult(t,
		`{"model":"m-primary","persona":"brad","case_ids":["case-01","case-02","case-03"]},`+
			`{"model":"m-backup","persona":"brad","case_ids":["case-01","case-02","case-03"]},`+
			`{"model":"m-ghost","persona":"brad","case_ids":["case-99"]}`, 3, 3)
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.NotEqual(t, 0, code, "an unjoined coverage row must not be silently discarded: %s", out)
	assert.Contains(t, out, "m-ghost", "the orphan coverage row is named")
	assert.Contains(t, out, "no matching reviewer row")
}

// The suite case-id list is the gate's denominator and comes from the same file
// it validates, so it must be a SET BY CONSTRUCTION: a repeated id would shrink
// the required set while inflating the reported suite size (["case-01"] x3 reads
// as "full coverage of a 3-case suite"). The producer writes the manifest's case
// list, which cannot repeat — a repeat means the file was hand-assembled.
func TestBenchmarkExport_RejectsDuplicateSuiteCaseIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-01","case-01"],` +
		`"reviewer_coverage":[{"model":"m","persona":"p","case_ids":["case-01"]}],` +
		`"reviewers":[{"model":"m","persona":"p","runs":1,"findings_raised_avg":1.0,` +
		`"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code,
		"a repeated suite_case_ids entry must be malformed, not full coverage of an inflated suite: %s", out)
	assert.Contains(t, out, "more than once")
	assert.Contains(t, out, "case-01")
}

// suite_case_ids present but the coverage array stripped is MALFORMED, not
// unmeasured. Treating it as unmeasured would make deleting the whole array a
// cheaper bypass than any of the tampering shapes this gate defends against — the
// gate would be disarmed by removing more data rather than less.
func TestBenchmarkExport_StrippedCoverageArrayIsRejectedNotWarned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02","case-03"],` +
		`"reviewers":[{"model":"m","persona":"p","runs":1,"findings_raised_avg":1.0,` +
		`"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "a stripped coverage array must fail, not downgrade to a warning: %s", out)
	assert.Contains(t, out, "records no reviewer coverage")
}

// The opt-out publishes, and names the shortfall to the operator on stderr.
//
// The shortfall is deliberately NOT carried into the submission envelope: adding a
// key there is a submission_schema decision, and that constant is shared with the
// production leaderboard export. So the opt-out is an operator-visible override, not
// a consumer-visible annotation — see the TD row filed alongside this change.
func TestBenchmarkExport_AllowPartialCoverageWarnsAndPublishes(t *testing.T) {
	in := writeCoverageRunResult(t, shortCoverageRows, 2, 1)
	code, stdout, stderr := execCmdSplit(t, "benchmark", "export", "--in", in, "--allow-partial-coverage")

	require.Equal(t, 0, code, "the explicit opt-out permits publication: %s%s", stdout, stderr)
	assert.Contains(t, stdout, "benchmark-suite", "the submission is produced")
	assert.Contains(t, stderr, "partial coverage", "the operator is told what they opted into")
	assert.Contains(t, stderr, "2/3", "and the shortfall is quantified, not merely named")
	assert.Contains(t, stderr, "1/3")
	// The warning must state the consequence truthfully: the shortfall lives in the
	// run-result ONLY. benchmark.Submission carries no coverage field, so once
	// published, a consumer cannot tell these rows from fully-covered ones.
	assert.Contains(t, stderr, "not carried into the submission",
		"the one reassurance attached to bypassing a data-integrity gate must be true")
	assert.NotContains(t, stderr, "submission records each row's covered cases")

	// The envelope stays at the frozen schema with no new keys.
	assert.NotContains(t, stdout, "reviewer_coverage",
		"widening the public envelope is a submission_schema decision, not part of this change")
	assert.NotContains(t, stdout, "suite_case_ids")
}

// A run-result carrying NO coverage at all — any file produced before coverage
// existed — is UNMEASURED, not short. It exports, with a warning that coverage could
// not be verified. Failing closed here would reject every pre-existing run-result
// over a field they had no way to write.
func TestBenchmarkExport_UnmeasuredCoverageWarnsButExports(t *testing.T) {
	in := writeRunResult(t) // no suite_case_ids, no reviewer_coverage
	code, stdout, stderr := execCmdSplit(t, "benchmark", "export", "--in", in)

	require.Equal(t, 0, code, "an unmeasured run-result still exports: %s%s", stdout, stderr)
	assert.Contains(t, stderr, "no case coverage", "the operator is told it could not be verified")
	assert.Contains(t, stdout, "benchmark-suite", "and the submission is still produced")
	assert.NotContains(t, stdout, "warning",
		"the warning goes to stderr — it must never corrupt the JSON a caller pipes off stdout")
}

// Coverage rows present but the suite case-id list missing is MALFORMED, not
// unmeasured: the producer writes suite_case_ids and reviewer_coverage together,
// so a post-epic file missing exactly the denominator had it stripped — the
// cheapest way past this gate. Only a file with NEITHER field (genuinely
// pre-epic) takes the warn-only unmeasured path.
func TestBenchmarkExport_CoverageWithoutSuiteCaseIDsIsMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"reviewer_coverage":[{"model":"m","persona":"p","case_ids":["case-01"]}],` +
		`"reviewers":[{"model":"m","persona":"p","runs":1,"findings_raised_avg":1.0,` +
		`"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code,
		"a stripped denominator on a demonstrably post-epic file must fail, not warn and publish: %s", out)
	assert.Contains(t, out, "malformed")
}

// A duplicate coverage identity is rejected rather than resolved. Indexing coverage
// by identity is last-write-wins, so a file carrying a SHORT row and a FULL row under
// the same (model, persona) would have the full one mask the short one — the cheapest
// possible way to walk a partial run past this gate. The producer emits one row per
// identity, so a duplicate can only come from hand-assembly.
func TestBenchmarkExport_RejectsDuplicateCoverageIdentity(t *testing.T) {
	in := writeCoverageRunResult(t,
		`{"model":"m-primary","persona":"brad","case_ids":["case-01"]},`+
			`{"model":"m-primary","persona":"brad","case_ids":["case-01","case-02","case-03"]},`+
			`{"model":"m-backup","persona":"brad","case_ids":["case-01","case-02","case-03"]}`, 3, 3)
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.NotEqual(t, 0, code, "a full row must not be able to mask a short row of the same identity: %s", out)
	assert.Contains(t, out, "more than once")
	assert.Contains(t, out, "m-primary")
}

// `runs` and the covered case set are written together by the producer, so they are
// equal by construction. A mismatch means the file was edited — and accepting it
// would let a row publish a number measured over two cases while presenting a
// full-suite coverage list as its provenance.
func TestBenchmarkExport_RejectsRunsCoverageMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02","case-03"],` +
		`"reviewer_coverage":[{"model":"m","persona":"p","case_ids":["case-01","case-02","case-03"]}],` +
		`"reviewers":[{"model":"m","persona":"p","runs":2,"findings_raised_avg":1.0,` +
		`"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "runs=2 with a 3-case coverage list is a malformed file: %s", out)
	assert.Contains(t, out, "runs=2")
	assert.Contains(t, out, "3 covered case")
}

// A reviewer row with NO matching coverage row cannot be verified either, and must
// not pass silently — an unverifiable row is precisely what a hand-supplied
// run-result would use to slip past the gate.
func TestBenchmarkExport_RejectsReviewerRowWithoutCoverage(t *testing.T) {
	in := writeCoverageRunResult(t,
		`{"model":"m-primary","persona":"brad","case_ids":["case-01","case-02","case-03"]}`, 3, 1)
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.NotEqual(t, 0, code, "a reviewer row with no coverage row must not publish: %s", out)
	assert.Contains(t, out, "m-backup", "the unverifiable row is named")
}
