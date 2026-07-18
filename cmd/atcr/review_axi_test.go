package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// (execCmdSplit — separated stdout/stderr capture — is defined in
// scorecard_wiring_test.go and shared across the package.)

// assertNoANSIOrMarkdown fails if s carries an ANSI/OSC escape byte or the
// Markdown table/heading syntax the human report uses — the structural half of the
// --axi clean-stdout guarantee (AC 01-03 Story-Specific DoD).
func assertNoANSIOrMarkdown(t *testing.T, s string) {
	t.Helper()
	assert.NotContains(t, s, "\x1b[", "axi stdout must carry zero ANSI CSI escapes")
	assert.NotContains(t, s, "\x1b]", "axi stdout must carry zero ANSI OSC escapes")
	assert.NotContains(t, s, "|---", "axi stdout must carry no Markdown table rule")
	for _, ln := range strings.Split(s, "\n") {
		assert.False(t, strings.HasPrefix(ln, "# ") || strings.HasPrefix(ln, "## "),
			"axi stdout must carry no Markdown heading, got line %q", ln)
	}
}

// TestReviewCmd_AXIFlagRegistered verifies the --axi flag exists on the review
// command and defaults to false (AC 01-03).
func TestReviewCmd_AXIFlagRegistered(t *testing.T) {
	cmd := newReviewCmd()
	require.NotNil(t, cmd.Flags().Lookup("axi"), "review must define --axi")
	v, err := cmd.Flags().GetBool("axi")
	require.NoError(t, err)
	require.False(t, v, "--axi defaults to false")
}

// TestReviewCmd_AXISuppressesHumanSummary is the headline AC 01-03 test: a real
// review under --axi emits the token-dense run-summary payload on stdout and NONE
// of the pre-existing human progress/summary lines.
func TestReviewCmd_AXISuppressesHumanSummary(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, _ := execCmdSplit(t, "review", "--axi", "--base", "HEAD^")
	require.Equal(t, 0, code, "a completed review exits 0")

	// The token-dense summary payload is present.
	assert.Contains(t, stdout, "review_summary", "axi stdout must carry the run-summary payload header")
	assert.Contains(t, stdout, "agents_succeeded", "axi summary payload must declare the agent-count columns")

	// None of the human progress/summary line formats leak onto stdout.
	assert.NotContains(t, stdout, "Total elapsed:", "human summary line must be gated under --axi")
	assert.NotContains(t, stdout, "Agents:", "human agents line must be gated under --axi")
	assert.NotContains(t, stdout, "API calls:", "human API-calls line must be gated under --axi")
	assert.NotContains(t, stdout, "agents succeeded (", "human one-line outcome must be gated under --axi")
	assertNoANSIOrMarkdown(t, stdout)
}

// TestReviewCmd_NonAXIRegressionSummaryUnchanged pins that a review WITHOUT --axi
// still prints the human end-of-review summary byte-for-byte as before — the
// non-axi regression guard (AC 01-03 / AC 04-03).
func TestReviewCmd_NonAXIRegressionSummaryUnchanged(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, _ := execCmdSplit(t, "review", "--base", "HEAD^")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "Total elapsed:", "non-axi review must still print the human summary")
	assert.Contains(t, stdout, "Agents:", "non-axi review must still print the agents line")
	assert.NotContains(t, stdout, "review_summary", "non-axi review must not emit the axi payload")
}

// TestReviewCmd_NonAXIChainedLinesPresent is the non-axi companion to
// TestReviewCmd_AXIGatesChainedAndFindingsLines (and the review side of AC 04-03):
// a `review --verify --debate` run WITHOUT --axi must emit every human-oriented
// stdout line — including the chained "reconciled"/"verified"/"debated" lines and
// the writeReviewSummary "Findings:" line. Pairing the two tests proves the gated
// tests are non-tautological: these lines genuinely reach stdout by default and are
// suppressed only by the --axi gate, not because the single-persona mock never
// produces them.
func TestReviewCmd_NonAXIChainedLinesPresent(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, _ := execCmdSplit(t, "review", "--verify", "--debate", "--base", "HEAD^")
	require.Equal(t, 0, code, "a completed chained review exits 0")

	for _, human := range reviewHumanStdoutStrings {
		assert.Contains(t, stdout, human, "non-axi chained review must still print human line %q", human)
	}
	assert.NotContains(t, stdout, "review_summary", "a non-axi review must not emit the axi payload")
}

// TestReviewCmd_AXIFailOnGatesReconcileLine verifies the chained one-shot stage
// output ("reconciled N finding(s)") is also gated under --axi, not just the
// top-level summary (AC 01-03 Scenario 2). The mock returns a CRITICAL finding, so
// --fail-on high gates the run to exit 1 while stdout stays payload-only.
func TestReviewCmd_AXIFailOnGatesReconcileLine(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, _ := execCmdSplit(t, "review", "--axi", "--fail-on", "high", "--base", "HEAD^")
	require.Equal(t, 1, code, "a surviving CRITICAL finding gates to exit 1")
	assert.NotContains(t, stdout, "reconciled", "the reconcile-count line must be gated under --axi")
	assert.Contains(t, stdout, "review_summary", "the run-summary payload is still emitted under --axi")
	assertNoANSIOrMarkdown(t, stdout)
}

// TestReviewCmd_AXIInterruptCleanStdout verifies the interrupt path honors --axi:
// reportInterrupt writes only to stderr, so an interrupted review --axi leaves
// stdout byte-empty rather than falling back to human text (AC 01-03 Edge Case 2).
func TestReviewCmd_AXIInterruptCleanStdout(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A provider that cancels the root context mid-poll simulates a SIGINT during
	// the fan-out.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	liveReviewConfig(t, srv.URL, "bruce")

	var out, errb bytes.Buffer
	root := newRootCmd()
	root.SetArgs([]string{"review", "--axi", "--base", "HEAD^"})
	root.SetOut(&out)
	root.SetErr(&errb)
	err := root.ExecuteContext(ctx)

	require.Equal(t, 1, exitCode(err), "interrupted review exits 1")
	assert.Empty(t, out.String(), "an interrupted review --axi must leave stdout byte-clean (notice goes to stderr)")
}

// TestReviewCmd_AXIAutoFixIsUsageError verifies --axi combined with --auto-fix is a
// usage error (exit 2): --auto-fix drives an interactive write-back/PR flow whose
// output is not a consumable findings payload, so the combination is rejected
// rather than silently emitting unguarded human output (AC 01-03 Edge Case 3,
// AC 02-02 Error Scenario 2).
func TestReviewCmd_AXIAutoFixIsUsageError(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "review", "--axi", "--auto-fix")
	require.Equal(t, 2, code, "--axi + --auto-fix is a usage error (exit 2)")
	require.Contains(t, out, "--axi and --auto-fix are mutually exclusive")
}

// TestReviewCmd_AXIAutoFixResumeIsUsageError verifies the mutual-exclusion holds on
// the --resume variant too: the guard must fire before --resume short-circuits, so
// `review --resume --axi --auto-fix` is exit 2 rather than silently dropping
// --auto-fix (AC 02-02 Error Scenario 2; found by the 2.14.A adversarial review).
func TestReviewCmd_AXIAutoFixResumeIsUsageError(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "review", "--resume", "latest", "--axi", "--auto-fix")
	require.Equal(t, 2, code, "--axi + --auto-fix must be a usage error on the resume path too")
	require.Contains(t, out, "--axi and --auto-fix are mutually exclusive")
}

// reviewHumanStdoutStrings is the exhaustive set of human-oriented stdout
// fragments AC 04-01 enumerates for the fresh-review path (the one-line outcome,
// the four writeReviewSummary lines, and the three chained one-shot stage lines).
// Under --axi every one of these must be absent from stdout; without --axi every
// one must be present (TestReviewCmd_NonAXIChainedLinesPresent). Kept as one slice
// so the gate and regression tests assert against an identical, complete list and
// cannot silently drift apart.
var reviewHumanStdoutStrings = []string{
	"agents succeeded (", // review.go:466 one-line outcome
	"Total elapsed:",     // review_summary.go
	"Agents:",            // review_summary.go
	"API calls:",         // review_summary.go
	"Findings:",          // review_summary.go
	"reconciled",         // review.go:590 one-shot reconcile line
	"verified ",          // review.go:614 chained --verify line
	"debated ",           // review.go:634 chained --debate line
}

// TestReviewCmd_AXIGatesChainedAndFindingsLines is the AC 04-01 Scenario 3 gap
// check: a full `review --axi --verify --debate` run must gate EVERY human-oriented
// stdout write in the fresh path — not only the top-level summary already covered
// by TestReviewCmd_AXISuppressesHumanSummary, but also the chained one-shot stage
// lines ("Findings:", "reconciled", "verified", "debated"). The equivalent non-axi
// run (TestReviewCmd_NonAXIChainedLinesPresent) proves these lines otherwise reach
// stdout, so their absence here is caused by the gating, not by the lines never
// being produced.
func TestReviewCmd_AXIGatesChainedAndFindingsLines(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, _ := execCmdSplit(t, "review", "--axi", "--verify", "--debate", "--base", "HEAD^")
	require.Equal(t, 0, code, "a completed chained review exits 0")

	for _, human := range reviewHumanStdoutStrings {
		assert.NotContains(t, stdout, human, "chained human line %q must be gated under --axi", human)
	}
	assert.Contains(t, stdout, "review_summary", "the run-summary payload is still emitted under --axi")
	assertNoANSIOrMarkdown(t, stdout)
}

// TestReviewCmd_AXIRunSummaryUnaffectedByMaxLines is the review side of AC 03-04
// Edge Case 2. The review --axi run-summary is a single-row payload, out of
// pagination scope (Phase 2 gate: pagination wraps the findings path / renderAXI /
// report --axi, NOT the single-row run summary; sprint-plan.md Phase 2 gate). A
// tiny ATCR_AXI_MAX_LINES must therefore NOT truncate the run-summary — proving
// review.go neither reimplements nor misapplies the findings cap (there is one
// shared truncation implementation, in internal/report/pagination.go, and review
// does not carry a second one). Findings truncation is exercised on the report
// --axi path (report_test.go).
func TestReviewCmd_AXIRunSummaryUnaffectedByMaxLines(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	t.Setenv("ATCR_AXI_MAX_LINES", "1")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, _ := execCmdSplit(t, "review", "--axi", "--base", "HEAD^")
	require.Equal(t, 0, code)
	// The full run-summary (header + data row) survives despite cap=1: it is not a
	// paginated findings list.
	assert.Contains(t, stdout, "review_summary", "run-summary header present under cap=1")
	assert.Contains(t, stdout, "agents_succeeded", "run-summary row present under cap=1 (not capped away)")
}
