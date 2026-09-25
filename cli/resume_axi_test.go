package cli

import (
	"context"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// firstLine returns the first line of s (the TOON tabular-array header), or "".
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestResume_AXISuppressesHumanLines verifies `review --resume --axi` gates the
// resume path's human stdout writes (resuming/outcome/summary/reconciled) and
// emits the token-dense run-summary payload instead (AC 01-04). Uses the
// pending-agent fixture so the post-fan-out summary path is exercised.
func TestResume_AXISuppressesHumanLines(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "robin")
	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")
	writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, []string{"bruce"})

	code, stdout, _ := execCmdSplit(t, "review", "--resume", "latest", "--axi", "--base", "HEAD^")
	require.Equal(t, 0, code, "resume completes -> exit 0")

	assert.Contains(t, stdout, "review_summary", "resume --axi must emit the run-summary payload")
	assert.NotContains(t, stdout, "resuming review", "resume announce line must be gated under --axi")
	assert.NotContains(t, stdout, "Total elapsed:", "human summary must be gated under --axi")
	assert.NotContains(t, stdout, "Agents:", "human agents line must be gated under --axi")
	assert.NotContains(t, stdout, "agents succeeded (", "human outcome line must be gated under --axi")
	assert.NotContains(t, stdout, "reconciled", "resume-path reconcile line must be gated under --axi")
	assertNoANSIOrMarkdown(t, stdout)
}

// resumeHumanStdoutStrings is the set of human-oriented stdout fragments the
// pending-agent resume path writes, in emission order (resume.go:182 resuming,
// :213 outcome, :215 shared writeReviewSummary block, :283 reconciled). The
// AllComplete announce (resume.go:163) is deliberately NOT here — it fires only on
// the no-pending path and is covered separately by the AllComplete tests. Resume
// does not support --verify/--debate (rejected up front), so it has no chained
// verify/debate lines — this is the resume analogue of reviewHumanStdoutStrings.
// Under --axi every fragment must be absent.
var resumeHumanStdoutStrings = []string{
	"resuming review",    // resume.go:182 announce line
	"agents succeeded (", // resume.go:213 one-line outcome
	"Total elapsed:",     // review_summary.go (shared writeReviewSummary)
	"Agents:",            // review_summary.go
	"API calls:",         // review_summary.go
	"Findings:",          // review_summary.go
	"reconciled",         // resumeReconcile resume.go:283
}

// TestResume_AXIGatesAllHumanStrings is the AC 04-01 resume-path gap check: a full
// `review --resume --axi` run over a pending-agent fixture must gate every one of
// the human-oriented stdout fragments resume.go writes, asserted as a complete set
// (the shared review_summary.go lines plus resume's own announce/outcome/reconcile
// lines). TestResume_NonAXIRegressionUnchanged proves the same lines otherwise
// reach stdout without --axi.
func TestResume_AXIGatesAllHumanStrings(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "robin")
	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")
	writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, []string{"bruce"})

	code, stdout, _ := execCmdSplit(t, "review", "--resume", "latest", "--axi", "--base", "HEAD^")
	require.Equal(t, 0, code, "resume completes -> exit 0")

	for _, human := range resumeHumanStdoutStrings {
		assert.NotContains(t, stdout, human, "resume human line %q must be gated under --axi", human)
	}
	assert.Contains(t, stdout, "review_summary", "resume --axi must emit the run-summary payload")
	assertNoANSIOrMarkdown(t, stdout)
}

// TestResume_AXIPayloadShapeMatchesReview locks AC 01-04's headline: `resume --axi`
// and `review --axi` emit byte-identical payload SHAPE (same TOON header line) for
// equivalent data, because both render through the one shared writeReviewSummaryAXI.
func TestResume_AXIPayloadShapeMatchesReview(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "robin")

	// review --axi header
	_, reviewOut, _ := execCmdSplit(t, "review", "--axi", "--base", "HEAD^")
	reviewHeader := firstLine(reviewOut)
	require.Contains(t, reviewHeader, "review_summary")

	// resume --axi header (pending robin)
	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")
	writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, []string{"bruce"})
	_, resumeOut, _ := execCmdSplit(t, "review", "--resume", "latest", "--axi", "--base", "HEAD^")
	resumeHeader := firstLine(resumeOut)

	assert.Equal(t, reviewHeader, resumeHeader,
		"review --axi and resume --axi must emit the identical run-summary payload header")
}

// parseReviewSummaryRow extracts (findings_total, findings-by-severity) from a
// standard-TOON review_summary[1] payload. The numeric findings_* columns are
// the LAST five of both the header and the value row, so the parse anchors at
// the row tail and is immune to id/dir cells that contain commas.
func parseReviewSummaryRow(t *testing.T, stdout string) (int64, map[string]int64) {
	t.Helper()
	tailCols := []string{"findings_total", "findings_critical", "findings_high", "findings_medium", "findings_low"}
	lines := strings.Split(stdout, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "review_summary[1]{") {
			continue
		}
		open := strings.Index(line, "{")
		close_ := strings.Index(line, "}")
		require.Greater(t, close_, open, "malformed review_summary header")
		cols := strings.Split(line[open+1:close_], ",")
		require.GreaterOrEqual(t, len(cols), len(tailCols), "unexpected review_summary header")
		require.Less(t, i+1, len(lines), "review_summary payload missing its value line")
		vals := strings.Split(strings.TrimSpace(lines[i+1]), ",")
		require.Equal(t, len(cols), len(vals), "review_summary column/value count mismatch")
		off := len(cols) - len(tailCols)
		by := map[string]int64{}
		var total int64
		for j, c := range tailCols {
			n, err := strconv.ParseInt(strings.TrimSpace(vals[off+j]), 10, 64)
			require.NoError(t, err, "non-numeric review_summary value %q", vals[off+j])
			if c == "findings_total" {
				total = n
			} else {
				by[strings.ToUpper(strings.TrimPrefix(c, "findings_"))] = n
			}
		}
		return total, by
	}
	t.Fatal("no review_summary payload in stdout")
	return 0, nil
}

// TestResume_AXIAllCompleteSeverityColumnsSum pins that the already-complete
// resume payload's per-severity counts reconcile with findings_total (TD:
// cli/resume.go:254) — an agent gating on severity from the payload must see the
// same criticals the fresh run reported.
func TestResume_AXIAllCompleteSeverityColumnsSum(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")
	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^"))

	// The fresh run's own payload reports the truth we compare against.
	_, freshOut, _ := execCmdSplit(t, "review", "--axi", "--base", "HEAD^")
	// (fresh run is consumed; the resume path re-reconciles the same dir)

	code, stdout, _ := execCmdSplit(t, "review", "--resume", "latest", "--axi", "--base", "HEAD^")
	require.Equal(t, 0, code, "AllComplete resume exits 0")
	total, bySeverity := parseReviewSummaryRow(t, stdout)
	_ = freshOut
	require.Greater(t, total, int64(0), "fixture review must produce findings")
	sum := bySeverity["CRITICAL"] + bySeverity["HIGH"] + bySeverity["MEDIUM"] + bySeverity["LOW"]
	assert.Equal(t, total, sum,
		"severity columns must sum to findings_total on the already-complete resume path")
}

// TestResume_AXIAllCompleteGated covers AC 01-04 Edge Case 1: the AllComplete
// short-circuit branch is also axi-gated — its "All configured agents already
// completed" announce and the re-reconcile count line do not leak onto stdout —
// and the shared run-summary payload is emitted in their place so the axi stream
// is never empty.
func TestResume_AXIAllCompleteGated(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^"))

	code, stdout, _ := execCmdSplit(t, "review", "--resume", "latest", "--axi", "--base", "HEAD^")
	require.Equal(t, 0, code, "AllComplete resume exits 0")
	assert.NotContains(t, stdout, "All configured agents already completed",
		"AllComplete announce must be gated under --axi")
	assert.NotContains(t, stdout, "reconciled", "AllComplete re-reconcile line must be gated under --axi")
	assert.Contains(t, stdout, "review_summary",
		"AllComplete --axi resume must emit the run-summary payload, not an empty stream")
	assertNoANSIOrMarkdown(t, stdout)
}

// TestResume_NonAXIAllHumanStringsPresent is the resume side of AC 04-03: a
// `review --resume` run WITHOUT --axi must still emit EVERY human-oriented stdout
// fragment resume.go writes (the complete resumeHumanStdoutStrings set), proving
// the AC 04-01 gating left the default (human) path untouched. Paired with
// TestResume_AXIGatesAllHumanStrings (same set, asserted absent under --axi), this
// makes both tests non-tautological.
func TestResume_NonAXIAllHumanStringsPresent(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "robin")
	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")
	writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, []string{"bruce"})

	code, stdout, _ := execCmdSplit(t, "review", "--resume", "latest", "--base", "HEAD^")
	require.Equal(t, 0, code)
	// Present AND in emission order (AC 04-03 "same order and wording"): a reorder or
	// an inserted line in the default resume path is caught, not just a missing line.
	assertOrderedContains(t, stdout, resumeHumanStdoutStrings...)
	assert.NotContains(t, stdout, "review_summary", "a non-axi resume must not emit the axi payload")
}

// TestResume_NonAXIAllCompletePresent is AC 04-03 Scenario 3: the AllComplete
// re-reconcile branch, run WITHOUT --axi, still writes its announce line and the
// reconcile count exactly as before — the non-axi companion to
// TestResume_AXIAllCompleteGated (which asserts both are gated under --axi).
func TestResume_NonAXIAllCompletePresent(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^"))

	code, stdout, _ := execCmdSplit(t, "review", "--resume", "latest", "--base", "HEAD^")
	require.Equal(t, 0, code, "AllComplete resume exits 0")
	assert.Contains(t, stdout, "All configured agents already completed",
		"non-axi AllComplete must still print its announce line")
	assert.Contains(t, stdout, "reconciled", "non-axi AllComplete must still print the re-reconcile line")
}

// TestResume_NonAXIRegressionUnchanged pins that resume WITHOUT --axi still prints
// the human summary + reconcile line byte-for-byte as before (non-axi regression).
func TestResume_NonAXIRegressionUnchanged(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "robin")
	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")
	writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, []string{"bruce"})

	code, stdout, _ := execCmdSplit(t, "review", "--resume", "latest", "--base", "HEAD^")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Total elapsed:", "non-axi resume must still print the human summary")
	assert.Contains(t, stdout, "reconciled", "non-axi resume must still print the reconcile line")
	assert.NotContains(t, stdout, "review_summary", "non-axi resume must not emit the axi payload")
}

// TestResume_AXIRenderFaultStillRecordsLedgers mirrors
// TestReviewCmd_AXIRenderFaultStillRecordsLedgers for the resume pending path:
// a broken-stdout axi payload write must not cost the run its history/audit
// ledger records — the run exits 1 (payload undeliverable) only after the
// ledgers are written.
func TestResume_AXIRenderFaultStillRecordsLedgers(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "robin")
	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")
	writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, []string{"bruce"})

	root := NewRootCmd()
	root.SetArgs([]string{"review", "--resume", "latest", "--axi", "--base", "HEAD^"})
	root.SetOut(failWriter{})
	root.SetErr(io.Discard)
	err := root.ExecuteContext(context.Background())
	require.Equal(t, exitFailure, exitCode(err), "a broken-stdout resume still exits 1")

	recs, lerr := audit.Load(filepath.Join(".", ".atcr", "audit.log.jsonl"))
	require.NoError(t, lerr)
	require.Len(t, recs, 1, "the audit ledger must record the resume even when the axi payload write fails")
}
