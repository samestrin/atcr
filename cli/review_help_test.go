package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reviewHelpBlock is the help[] block a successful fresh `atcr review --axi`
// with no gate ends with (Epic 42.0 T2): the registry's review next
// steps with <id> left as a literal placeholder, in goaxi.WriteHelp's inline
// TOON form. Nothing reconciled the review, so report and verify (which exit 2
// without reconciled data) give way to reconcile (EST-8139).
const reviewHelpBlock = "help[1]: atcr reconcile <id>\n"

// reviewNextLines is the human-mode rendering of the same next steps, on stderr.
const reviewNextLines = "Next: atcr reconcile <id>\n"

// reconciledReviewHelpBlock is the help[] block of a review that reconciled
// in-process, and of a completed --resume (which always reconciles): report and
// verify now have the data they read.
const reconciledReviewHelpBlock = "help[2]: atcr report <id>,atcr verify <id>\n"

// TestInterruptMessage_ByteIdentical pins the whole interrupt notice to the
// text it had before the registry took over its hint line (Epic 42.0 T2).
func TestInterruptMessage_ByteIdentical(t *testing.T) {
	prep := &fanout.PreparedReview{ID: "rev-7", Dir: "/fallback"}
	result := &fanout.ReviewResult{ID: "rev-7", Dir: "/real/dir", Summary: fanout.Summary{Total: 5, Succeeded: 2}}
	want := "\n⚠️ Review interrupted. 2/5 agents completed; partial results saved to /real/dir.\n" +
		"   Run 'atcr status rev-7' to inspect, or 'atcr review --resume rev-7' to finish the remaining agents.\n"
	assert.Equal(t, want, interruptMessage(result, prep))
}

// TestInterruptMessage_HintComesFromRegistry proves the hint is rendered from the
// review:interrupted registry entry, not a second hardcoded copy: changing the
// entry changes the notice.
func TestInterruptMessage_HintComesFromRegistry(t *testing.T) {
	saved := suggestionRegistry[suggestReviewInterrupted]
	t.Cleanup(func() { suggestionRegistry[suggestReviewInterrupted] = saved })
	suggestionRegistry[suggestReviewInterrupted] = []nextStep{
		{Command: "show", Args: []string{idPlaceholder}},
		{Command: "review", Args: []string{"--continue", idPlaceholder}},
	}
	msg := interruptMessage(nil, &fanout.PreparedReview{ID: "rev-7", Dir: "/d"})
	assert.Contains(t, msg, "Run 'atcr show rev-7' to inspect, or 'atcr review --continue rev-7' to finish the remaining agents.")
}

// TestReviewCmd_AXIEndsWithHelpBlock: a fresh `review --axi` ends with the
// help[] block, and everything before it is the unchanged run-summary payload.
func TestReviewCmd_AXIEndsWithHelpBlock(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, stderr := execCmdSplit(t, "review", "--axi", "--base", "HEAD^")
	require.Equal(t, 0, code)
	require.True(t, strings.HasSuffix(stdout, reviewHelpBlock), "axi stdout must end with the help[] block, got:\n%s", stdout)
	body := strings.TrimSuffix(stdout, reviewHelpBlock)
	assert.True(t, strings.HasPrefix(body, "review_summary[1]{"), "the run-summary payload still leads stdout")
	assert.NotContains(t, body, "help[", "exactly one help[] block")
	assert.NotContains(t, stderr, "Next:", "--axi carries next steps in help[], not human Next: lines")
}

// TestResume_AXIEndsWithHelpBlock: the --resume flag path of review ends with
// a help[] block too. A completed resume always reconciles (epic 4.1.1), so it
// offers report and verify, which then run (EST-8140; it offered reconcile
// before the resume recorded its review dir).
func TestResume_AXIEndsWithHelpBlock(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "robin")
	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")
	writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, []string{"bruce"})

	code, stdout, _ := execCmdSplit(t, "review", "--resume", "2026-06-18_demo", "--axi", "--base", "HEAD^")
	require.Equal(t, 0, code)
	require.True(t, strings.HasSuffix(stdout, reconciledReviewHelpBlock), "resume --axi stdout must end with the help[] block, got:\n%s", stdout)
	assert.True(t, strings.HasPrefix(stdout, "review_summary[1]{"), "the run-summary payload still leads stdout")

	code, _, stderr := execCmdSplit(t, "report", "2026-06-18_demo", "--format", "axi")
	assert.Equal(t, 0, code, stderr)
}

// TestReviewCmd_HumanNextLinesOnStderr: without --axi the next steps are human
// Next: lines on stderr, so the human stdout is exactly what it was.
func TestReviewCmd_HumanNextLinesOnStderr(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, stderr := execCmdSplit(t, "review", "--base", "HEAD^")
	require.Equal(t, 0, code)
	assert.True(t, strings.HasSuffix(stderr, reviewNextLines), "stderr must end with the Next: lines, got:\n%s", stderr)
	assert.NotContains(t, stdout, "Next:", "human stdout is unchanged")
	assert.NotContains(t, stdout, "help[", "human stdout is unchanged")
}

// TestResume_HumanNextLinesOnStderr: the human --resume path gets the same
// steps as Next: lines.
func TestResume_HumanNextLinesOnStderr(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "robin")
	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")
	writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, []string{"bruce"})

	code, stdout, stderr := execCmdSplit(t, "review", "--resume", "latest", "--base", "HEAD^")
	require.Equal(t, 0, code)
	assert.True(t, strings.HasSuffix(stderr, "Next: atcr report <id>\nNext: atcr verify <id>\n"), "stderr must end with the Next: lines, got:\n%s", stderr)
	assert.NotContains(t, stdout, "Next:", "human stdout is unchanged")
}

// TestReviewCmd_GateFailureSuggestsReport (EST-8140, replacing the Epic 42.0
// rule that a non-zero review suggests nothing): a review that tripped its
// --fail-on gate reconciled its findings, so its help[] points at the report
// of what tripped it, and that report runs.
func TestReviewCmd_GateFailureSuggestsReport(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, stderr := execCmdSplit(t, "review", "--axi", "--fail-on", "high", "--base", "HEAD^")
	require.Equal(t, 1, code)
	assert.True(t, strings.HasSuffix(stdout, reconciledReviewHelpBlock), "got:\n%s", stdout)
	assert.NotContains(t, stderr, "Next:")

	code, _, stderr = execCmdSplit(t, "report", "--format", "axi")
	assert.Equal(t, 0, code, stderr)
}

// TestReviewCmd_PreReviewFailureNoHelp: a run that failed before it produced a
// review (here a missing API key) has no review to suggest anything for.
func TestReviewCmd_PreReviewFailureNoHelp(t *testing.T) {
	isolate(t)
	initGitRepoWithChange(t)
	writeReviewFixtureConfig(t)

	code, stdout, stderr := execCmdSplit(t, "review", "--axi", "--base", "HEAD^")
	require.Equal(t, 2, code)
	assert.NotContains(t, stdout, "help[")
	assert.NotContains(t, stderr, "Next:")
}

// TestReviewCmd_LegacyPipeNoHelp: the deprecated pipe payload stays
// byte-identical; a TOON help[] block appended to it would break pipe parsers.
func TestReviewCmd_LegacyPipeNoHelp(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, stderr := execCmdSplit(t, "review", "--axi", "--legacy-pipe", "--base", "HEAD^")
	require.Equal(t, 0, code)
	assert.NotContains(t, stdout, "help[")
	assert.NotContains(t, stderr, "Next:")
}

// TestReviewCmd_DryRunNoHelp: --dry-run runs no review, so there is no review
// to report or verify.
func TestReviewCmd_DryRunNoHelp(t *testing.T) {
	isolate(t)
	code, stdout, stderr := execCmdSplit(t, "review", "--dry-run")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.NotContains(t, stdout, "help[")
	assert.NotContains(t, stderr, "Next:")
}

// TestInterruptMessage_ShortRegistryEntryDoesNotPanic: shrinking the
// review:interrupted entry must not panic the interrupt path.
func TestInterruptMessage_ShortRegistryEntryDoesNotPanic(t *testing.T) {
	saved := suggestionRegistry[suggestReviewInterrupted]
	t.Cleanup(func() { suggestionRegistry[suggestReviewInterrupted] = saved })
	suggestionRegistry[suggestReviewInterrupted] = []nextStep{{Command: "show", Args: []string{idPlaceholder}}}
	var msg string
	require.NotPanics(t, func() {
		msg = interruptMessage(nil, &fanout.PreparedReview{ID: "rev-7", Dir: "/d"})
	})
	assert.Contains(t, msg, "atcr status rev-7")
	assert.Contains(t, msg, "atcr review --resume rev-7")
}

// lowFindingProvider is liveMockProvider with a LOW finding, so a HIGH gate
// reconciles in-process and passes.
func lowFindingProvider(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		content := "LOW|a.txt:1|Nit|Tidy it|style|5|evidence"
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestReviewCmd_GateReconciledSuggestsReport (EST-8139): a review that
// reconciled in-process under a passing --fail-on gate suggests report and
// verify, which can read its reconciled findings, and no reconcile.
func TestReviewCmd_GateReconciledSuggestsReport(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := lowFindingProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, stderr := execCmdSplit(t, "review", "--axi", "--fail-on", "high", "--base", "HEAD^")
	require.Equal(t, 0, code, stderr)
	require.True(t, strings.HasSuffix(stdout, reconciledReviewHelpBlock), "got:\n%s", stdout)

	// The suggested report runs: the review has the data it reads.
	code, _, stderr = execCmdSplit(t, "report", "--format", "axi")
	assert.Equal(t, 0, code, stderr)
}

// TestReviewCmd_ConfigGateReconciledSuggestsReport (EST-8139): a fail_on in
// the project config (init and quickstart write HIGH) reconciles in-process
// just as the flag does, so the suggestion follows what ran, not the flag.
func TestReviewCmd_ConfigGateReconciledSuggestsReport(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := lowFindingProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"), []byte("agents: [bruce]\nfail_on: HIGH\n"), 0o644))

	code, _, stderr := execCmdSplit(t, "review", "--base", "HEAD^")
	require.Equal(t, 0, code, stderr)
	assert.True(t, strings.HasSuffix(stderr, "Next: atcr report <id>\nNext: atcr verify <id>\n"), "got:\n%s", stderr)
}

// TestReviewCmd_UnreconciledSuggestionRuns (EST-8139): the reconcile a plain
// review suggests runs, where the report it used to suggest exited 2.
func TestReviewCmd_UnreconciledSuggestionRuns(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, stderr := execCmdSplit(t, "review", "--axi", "--base", "HEAD^")
	require.Equal(t, 0, code, stderr)
	require.True(t, strings.HasSuffix(stdout, reviewHelpBlock), "got:\n%s", stdout)

	code, _, _ = execCmdSplit(t, "report", "--format", "axi")
	require.Equal(t, 2, code, "report needs reconciled data")
	code, _, stderr = execCmdSplit(t, "reconcile")
	require.Equal(t, 0, code, stderr)
	code, _, stderr = execCmdSplit(t, "report", "--format", "axi")
	assert.Equal(t, 0, code, stderr)
}
