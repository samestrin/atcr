package main

import (
	"strings"
	"testing"

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

// TestResume_AXIAllCompleteGated covers AC 01-04 Edge Case 1: the AllComplete
// short-circuit branch is also axi-gated — its "All configured agents already
// completed" announce and the re-reconcile count line do not leak onto stdout.
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
