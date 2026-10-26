package cli

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reviewHelpBlock is the help[] block a successful `atcr review --axi` (fresh
// or --resume) ends with (Epic 42.0 T2): the registry's review next steps with
// <id> left as a literal placeholder, in goaxi.WriteHelp's inline TOON form.
const reviewHelpBlock = "help[2]: atcr report <id>,atcr verify <id>\n"

// reviewNextLines is the human-mode rendering of the same next steps, on stderr.
const reviewNextLines = "Next: atcr report <id>\nNext: atcr verify <id>\n"

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
// the same help[] block as a fresh review.
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
	require.True(t, strings.HasSuffix(stdout, reviewHelpBlock), "resume --axi stdout must end with the help[] block, got:\n%s", stdout)
	assert.True(t, strings.HasPrefix(stdout, "review_summary[1]{"), "the run-summary payload still leads stdout")
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
// Next: lines.
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
	assert.True(t, strings.HasSuffix(stderr, reviewNextLines), "stderr must end with the Next: lines, got:\n%s", stderr)
	assert.NotContains(t, stdout, "Next:", "human stdout is unchanged")
}

// TestReviewCmd_FailedRunNoHelp: a review that exits non-zero (here a --fail-on
// gate) did not finish successfully, so it suggests nothing.
func TestReviewCmd_FailedRunNoHelp(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, stdout, stderr := execCmdSplit(t, "review", "--axi", "--fail-on", "high", "--base", "HEAD^")
	require.Equal(t, 1, code)
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
