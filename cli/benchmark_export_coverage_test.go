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

// shortCoverageRows carries `outcomes` and `fallback_cases` deliberately. The export
// tests assert those two keys do NOT reach the submission envelope, and an assertion
// that a field is absent proves nothing when the fixture never supplied it — it would
// pass just as happily against a BuildSubmission that published the untrimmed
// run-result row. Each tally sums to its row's covered-case count, which the
// outcomes-mismatch validation requires.
const shortCoverageRows = `{"model":"m-primary","persona":"brad","case_ids":["case-01","case-02"],` +
	`"outcomes":{"findings":2},"fallback_cases":1},` +
	`{"model":"m-backup","persona":"brad","case_ids":["case-03"],` +
	`"outcomes":{"clean":1}}`

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
// As of submission_schema 2 (epic 35.16.6.2) the shortfall IS carried into the
// submission envelope, so the opt-out is no longer an operator-only override that
// leaves the board blind: a consumer can compare each reviewer_coverage row's
// case_ids against suite_case_ids and see the short row for itself.
func TestBenchmarkExport_AllowPartialCoverageWarnsAndPublishes(t *testing.T) {
	in := writeCoverageRunResult(t, shortCoverageRows, 2, 1)
	code, stdout, stderr := execCmdSplit(t, "benchmark", "export", "--in", in, "--allow-partial-coverage")

	require.Equal(t, 0, code, "the explicit opt-out permits publication: %s%s", stdout, stderr)
	assert.Contains(t, stdout, "benchmark-suite", "the submission is produced")
	assert.Contains(t, stderr, "partial coverage", "the operator is told what they opted into")
	assert.Contains(t, stderr, "2/3", "and the shortfall is quantified, not merely named")
	assert.Contains(t, stderr, "1/3")
	// The warning must state the consequence truthfully. The old text promised the
	// shortfall stayed out of the submission; that promise is now false, so asserting
	// its ABSENCE is what keeps the warning honest as the behavior changed underneath it.
	assert.NotContains(t, stderr, "not carried into the submission",
		"the pre-schema-2 reassurance is now false and must not survive in the warning")
	assert.Contains(t, stderr, "carried into the submission",
		"the operator is told the shortfall is now consumer-visible")

	// The envelope carries the shortfall: the denominator plus each row's covered set.
	assert.Contains(t, stdout, "suite_case_ids", "the suite denominator reaches the board")
	assert.Contains(t, stdout, "reviewer_coverage", "each row's covered-case set reaches the board")
	assert.Contains(t, stdout, "case_ids")
	// Trimmed projection: the run-result's diagnostics stay out of the public envelope.
	assert.NotContains(t, stdout, "outcomes",
		"the per-case outcome tally is run-result-only, not a public field")
	assert.NotContains(t, stdout, "fallback_cases",
		"the fallback count is run-result-only, not a public field")
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

// The reviewer array gets the same distinct-raw-identity discriminator as the
// coverage array: two DIFFERENT raw reviewer identities that scrub to one published
// identity are version skew or hand-assembly — not a flat "malformed" — and the
// message must name the collision so the operator is not sent hunting for tampering.
func TestBenchmarkExport_ReviewerScrubCollisionNamesTheScrub(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02","case-03"],` +
		`"reviewer_coverage":[{"model":"m","persona":"brad","case_ids":["case-01","case-02","case-03"]}],` +
		`"reviewers":[{"model":"m","persona":"brad","runs":3,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10},` +
		`{"model":"m ~x","persona":"brad","runs":3,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "the rejection itself must stand: %s", out)

	assert.Contains(t, out, "scrub to the same published identity",
		"two DIFFERENT raw reviewer identities colliding under the scrub must get the coverage branch's wording, one array over")
	assert.NotContains(t, out, "this file is malformed",
		"the flat malformed verdict names the wrong cause for a scrub collision")
}

// The skew message must not OVERCLAIM. It may say version skew is possible; it may
// not say it is the likely cause, because the branch cannot tell the two causes
// apart.
//
// The discriminator is `prev.Model != c.Model || prev.Persona != c.Persona` —
// "these are two different raw strings" — and that fact is equally consistent with
// an older producer and with a hand-assembled file. The obvious sharper test does
// NOT exist: scrubField (scorecard/export.go) breaks its loop the instant one pass
// is stable, so "stable under one pass but not under the fixed point" is the empty
// set; and a value a single pass still rewrites is not evidence of hand-assembly
// either, because a pre-fixed-point producer really did emit such values
// (benchmark_run.go records "bedrock@us-east-1/claude" scrubbing once to "/claude"
// and twice to ""). Telling the two apart needs image-membership under scrubOnce,
// which a 7-regex sequential pipeline does not give cheaply — so the message names
// both causes instead of ranking them.
//
// The rejection itself is unaffected: two rows cannot share one published identity
// on the board whatever wrote them.
func TestBenchmarkExport_ScrubCollisionMessageDoesNotOverclaimSkew(t *testing.T) {
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

	assert.NotContains(t, out, "most likely",
		"the branch cannot rank the two causes, so the message must not claim one is more likely")
	assert.Contains(t, out, "either",
		"the message must present version skew and hand-assembly as the two possibilities it cannot distinguish")
	assert.Contains(t, out, "hand-assembled",
		"hand-assembly must stay named as a live possibility, not be argued away")
}

// The scrub-collision message must SHOW the two identities it says are distinct.
// Both halves used to render through stripTerminalControlRunes, which deletes every
// unicode.IsControl rune — including \r, \n and \t, exactly the runes the scrub
// collapses via strings.Fields. So the pair that most naturally reaches this branch
// rendered IDENTICALLY: the operator was shown two copies of one name, told they
// differ, and told to rename one — an instruction that cannot be followed from what
// is displayed. anchorSuiteDenominator already rejects this rendering for the same
// reason and uses %q instead.
func TestBenchmarkExport_ScrubCollisionMessageShowsTheDifference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	// "m" vs "m\r": distinct raw, collapse to the same published identity under the
	// scrub, and differ ONLY in a rune stripTerminalControlRunes erases.
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02","case-03"],` +
		`"reviewer_coverage":[{"model":"m","persona":"brad","case_ids":["case-01","case-02","case-03"]},` +
		`{"model":"m\r","persona":"brad","case_ids":["case-01","case-02","case-03"]}],` +
		`"reviewers":[{"model":"m","persona":"brad","runs":3,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "the scrub collision must still be rejected: %s", out)
	require.Contains(t, out, "distinct raw identities",
		"precondition: this pair must reach the scrub-collision branch, not the malformed-duplicate one")

	assert.Contains(t, out, `"m\r"`,
		"the control rune that makes the two identities distinct must be VISIBLE (%q), not deleted — otherwise the message shows two identical names")
	assert.Contains(t, out, `same published identity "m"/"brad"`,
		"the message asserts a collided published identity, so it must show which one")
}

// The distinct-raw-identity discriminator is `prev.Model != c.Model || prev.Persona
// != c.Persona`, and only the MODEL half was pinned: every existing collision case
// varies the model, so mutating the condition down to `prev.Model != c.Model` left
// the whole cli suite green while a persona-only collision silently fell through to
// the malformed-duplicate wording. The persona arm is live — two rows sharing a raw
// model and differing only in raw persona do collapse onto one published identity —
// so it needs its own case or it can be deleted without a failure.
func TestBenchmarkExport_ScrubCollisionOnPersonaAloneIsVersionSkewNotMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	// Identical raw model, distinct raw personas that scrub to the same value —
	// the mirror image of the model-only cases above.
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02","case-03"],` +
		`"reviewer_coverage":[{"model":"m","persona":"brad","case_ids":["case-01","case-02","case-03"]},` +
		`{"model":"m","persona":"brad ~x","case_ids":["case-01","case-02","case-03"]}],` +
		`"reviewers":[{"model":"m","persona":"brad","runs":3,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "a persona-only scrub collision must still be rejected: %s", out)

	assert.Contains(t, out, "distinct raw identities",
		"a persona-only collision reaches the same branch as a model-only one; without this case the persona half of the discriminator can be deleted with the suite still green")
	assert.Contains(t, out, `"brad ~x"`,
		"the raw persona that differs must be named, exactly as the differing raw model is")
	assert.NotContains(t, out, "this file is malformed",
		"two DIFFERENT raw personas colliding under the scrub is version skew, not tampering")
}

// The --allow-partial-coverage help text carries the same promise the runtime
// warning does, and it went stale for the same reason: submission_schema 2 made
// "the submission does not carry it" false. Unlike the warning, no test read this
// string, so nothing would have caught the rot.
//
// Asserted through the real command tree rather than against the literal, so the
// text is checked as a user actually encounters it.
func TestBenchmarkExport_AllowPartialCoverageHelpIsTruthful(t *testing.T) {
	_, stdout, stderr := execCmdSplit(t, "benchmark", "export", "--help")
	help := stdout + stderr

	require.Contains(t, help, "--allow-partial-coverage", "precondition: the flag is documented in help")

	assert.NotContains(t, help, "the submission does not carry it",
		"submission_schema 2 carries the shortfall; the old promise is now false")
	assert.NotContains(t, help, "consumers cannot distinguish these rows from fully-covered ones",
		"a consumer CAN now distinguish them, by comparing case_ids against suite_case_ids")

	assert.Contains(t, help, "suite_case_ids",
		"help must name the key a consumer reads the shortfall from")
	assert.Contains(t, help, "not comparable",
		"the real reason the gate still fails closed must survive the rewrite")
}

// The export command emits the very envelope that GAINED two keys at
// submission_schema 2, so a strict board decoder pinned to 1 fails closed on its
// output — a risk docs/scorecard.md explicitly calls unresolved. The leaderboard
// --export help carries the version notice (TestLeaderboardExportHelpNamesTheSchemaVersion
// pins it) but a benchmark submitter reads THIS command's help, not that one.
func TestBenchmarkExportHelpNamesTheSchemaVersion(t *testing.T) {
	_, stdout, stderr := execCmdSplit(t, "benchmark", "export", "--help")
	help := stdout + stderr

	require.Contains(t, help, "submission_schema 2",
		"a benchmark submitter must learn the envelope this command emits stamps submission_schema 2")
	require.Contains(t, help, "pinned to 1",
		"and that a board pinned to the old version needs updating")
}

// The coverage gate validates RAW case ids, but BuildSubmission publishes SCRUBBED
// ones. Where those two disagree the published document means something the gate
// never checked — the same seam the reviewer-identity check at benchmark.go closed,
// one field over.
//
// Two distinct raw ids that scrub to one value publish a denominator with a repeated
// entry, and under the documented SET comparison a short row then reads as fully
// covered. That defeats the whole point of carrying coverage, on exactly the
// --allow-partial-coverage path whose warning promises the shortfall is visible.
func TestBenchmarkExport_RejectsSuiteCaseIDsThatCollideOnceScrubbed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01 a@b.com","case-01 c@d.com"],` +
		`"reviewer_coverage":[{"model":"m","persona":"p","case_ids":["case-01 a@b.com"]}],` +
		`"reviewers":[{"model":"m","persona":"p","runs":1,"findings_raised_avg":1.0,` +
		`"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path, "--allow-partial-coverage")
	require.NotEqual(t, 0, code, "a denominator that collapses once scrubbed must not publish: %s", out)
	assert.Contains(t, out, "case-01 a@b.com", "the error names a colliding id in its PRE-scrub form")
	assert.Contains(t, out, "case-01 c@d.com", "and the id it collides with, or the operator cannot act")
	assert.NotContains(t, out, `"suite_case_ids"`, "nothing is published on the rejection path")
}

// A reviewer row with NO coverage row is not "short" — it is the one shape the
// producer cannot emit (buildRunResult appends reviewers and coverage from the same
// loop), so it is hand-assembly, and publishing it puts a reviewers[] entry on the
// board with no reviewer_coverage row for the documented join to visit. The gate
// rejects it as malformed, on the opt-out path too.
func TestBenchmarkExport_ReviewerWithoutCoverageRowIsMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-02","case-03"],` +
		`"reviewer_coverage":[{"model":"m-primary","persona":"brad","case_ids":["case-01","case-02","case-03"]}],` +
		`"reviewers":[{"model":"m-primary","persona":"brad","runs":3,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10},` +
		`{"model":"m-backup","persona":"brad","runs":3,` +
		`"findings_raised_avg":1.0,"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path, "--allow-partial-coverage")
	require.NotEqual(t, 0, code,
		"a reviewer with no coverage row must not publish, even with the opt-out: %s", out)
	assert.Contains(t, out, "m-backup", "the error names the reviewer lacking a coverage row")
	assert.NotContains(t, out, `"reviewers"`, "nothing is published on the rejection path")
}

// A run-result carrying reviewer_coverage but NO suite_case_ids is structurally
// malformed (the producer writes the two together), and checkCoverage's shape check
// says exactly that. The scrub gate must not pre-empt it with a privacy diagnostic
// about an individual covered id — the sharper, actionable defect is the missing
// denominator, so the coverage loop skips a file that has none.
func TestBenchmarkExport_CoverageWithoutDenominatorGetsStructuralError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"reviewer_coverage":[{"model":"m","persona":"p","case_ids":["sk-io-pr-42"]}],` +
		`"reviewers":[{"model":"m","persona":"p","runs":1,"findings_raised_avg":1.0,` +
		`"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "coverage without a denominator must not export: %s", out)
	assert.Contains(t, out, "records reviewer coverage but no suite_case_ids",
		"the structural rejection is the sharper diagnostic for this shape")
	assert.NotContains(t, out, "empty once scrubbed",
		"a privacy-scrub diagnostic about one id would misdirect the operator")
}

// A case id consumed entirely by the scrubber publishes as "" — the identical defect
// the reviewer-identity check rejects because "an identity that scrubs away publishes
// as \"\" on the leaderboard". Case ids are no different, and the shape is producible:
// the bundled importer builds ids as <owner>-<repo>-pr-<n>.
func TestBenchmarkExport_RejectsCaseIDThatScrubsAway(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["sk-io-pr-42","case-02"],` +
		`"reviewer_coverage":[{"model":"m","persona":"p","case_ids":["sk-io-pr-42","case-02"]}],` +
		`"reviewers":[{"model":"m","persona":"p","runs":2,"findings_raised_avg":1.0,` +
		`"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "an id that scrubs to empty must not publish as \"\": %s", out)
	assert.Contains(t, out, "sk-io-pr-42", "the error names the PRE-scrub id; the scrubbed one is empty by construction")
}

// The guard must not cost a well-formed suite anything.
func TestBenchmarkExport_CleanCaseIDsStillExport(t *testing.T) {
	in := writeCoverageRunResult(t, fullCoverageRows, 3, 3)
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.Equal(t, 0, code, "ordinary case ids are untouched by the scrub guard: %s", out)
	assert.Contains(t, out, `"case-01"`)
}

// A verbatim-repeated suite id must keep reaching checkCoverage's sharper
// "more than once" diagnostic, not the scrub-collision one. Both conditions are true
// of such a file, so the ordering between the two rules is a real choice: the raw
// duplicate is the plainer defect and the actionable one (delete a line), whereas
// blaming the privacy scrub would send the operator hunting the wrong thing.
func TestBenchmarkExport_VerbatimDuplicateSuiteIDPrefersTheRawDiagnostic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"suite_case_ids":["case-01","case-01"],` +
		`"reviewer_coverage":[{"model":"m","persona":"p","case_ids":["case-01"]}],` +
		`"reviewers":[{"model":"m","persona":"p","runs":1,"findings_raised_avg":1.0,` +
		`"corroboration_rate":0.5,"latency_p50_ms":10}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	code, out := execCmdCapture(t, "benchmark", "export", "--in", path)
	require.NotEqual(t, 0, code, "a repeated suite id is still rejected: %s", out)
	assert.Contains(t, out, "more than once", "the raw-duplicate rule owns this file")
	assert.NotContains(t, out, "once scrubbed for publication",
		"the scrub-collision rule must not claim a collision the raw file already had")
}
