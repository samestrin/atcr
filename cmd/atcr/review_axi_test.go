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
