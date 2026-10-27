package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- Epic 49.2 T1: --auto-fix-base overrides the fix pull request's base ----

// autoFixDiffProvider is a live mock provider whose single reviewer replies with
// one HIGH finding on a.txt:2 (the line initGitRepoWithChange adds) carrying a
// real unified diff as its fix, so a one-shot `review --auto-fix` reaches the PR.
func autoFixDiffProvider(t *testing.T) *httptest.Server {
	t.Helper()
	finding := []map[string]any{{
		"severity":    "HIGH",
		"file_line":   "a.txt:2",
		"problem":     "second line holds the wrong word",
		"fix":         "--- a/a.txt\n+++ b/a.txt\n@@ -1,2 +1,2 @@\n one\n-two\n+three\n",
		"category":    "correctness",
		"est_minutes": 5,
		"evidence":    "two",
	}}
	raw, err := json.Marshal(finding)
	require.NoError(t, err)
	content := "```json\n" + string(raw) + "\n```"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{"choices": []map[string]any{
			{"message": map[string]string{"role": "assistant", "content": content}},
		}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runReviewAutoFix drives a real one-shot `atcr review --auto-fix` with
// the GitHub client and HEAD resolution stubbed (setup, when non-nil, runs
// after the fixture repo exists), and returns the exit code, the
// combined output, and the fake client that recorded the pull request.
func runReviewAutoFix(t *testing.T, setup func(), extra ...string) (int, string, *fakeGitHub) {
	t.Helper()
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	if setup != nil {
		setup()
	}
	liveReviewConfig(t, autoFixDiffProvider(t).URL, "solo")
	appendProjectConfig(t, "consensus: off\nauto_fix:\n  validate_command: [\"true\"]\n")

	oldResolve := resolveHeadSHAFn
	resolveHeadSHAFn = func(context.Context, string) (string, error) { return "deadbeef", nil }
	t.Cleanup(func() { resolveHeadSHAFn = oldResolve })
	gh := &fakeGitHub{}
	oldClient := newAutoFixGitHubFn
	newAutoFixGitHubFn = func(string, string) autoFixGitHub { return gh }
	t.Cleanup(func() { newAutoFixGitHubFn = oldClient })

	args := append([]string{"review", "--fail-on", "LOW",
		"--auto-fix", "--no-sandbox", "--repo", "o/r", "--token", "tok"}, extra...)
	code, out := execCmdCapture(t, args...)
	return code, out, gh
}

// TestReviewCmd_AutoFixBaseOverrideTargetsNamedBranch: with --auto-fix-base the
// fix pull request is opened against the named branch, not the default branch.
func TestReviewCmd_AutoFixBaseOverrideTargetsNamedBranch(t *testing.T) {
	code, out, gh := runReviewAutoFix(t, nil, "--base", "HEAD^", "--auto-fix-base", "feature/linear-123")
	require.Equal(t, 0, code, out)
	require.Equal(t, 1, gh.createPR, "the run must open one pull request: %s", out)
	require.Equal(t, "feature/linear-123", gh.lastPRReq.Base,
		"--auto-fix-base must be the pull request's base branch")
}

// TestReviewCmd_AutoFixWithoutBaseOverrideTargetsDefaultBranch: without the
// override the pull request still targets the resolved default branch.
func TestReviewCmd_AutoFixWithoutBaseOverrideTargetsDefaultBranch(t *testing.T) {
	// Auto range detection: HEAD is a feature branch one commit ahead of a local
	// `main` (no remote), so gitrange resolves the default branch to "main".
	setup := func() {
		wd, err := os.Getwd()
		require.NoError(t, err)
		runFixtureGit(t, wd, "checkout", "-q", "-b", "feature")
		runFixtureGit(t, wd, "branch", "-f", "main", "HEAD^")
		_ = exec.Command("git", "branch", "-D", "master").Run()
	}
	code, out, gh := runReviewAutoFix(t, setup)
	require.Equal(t, 0, code, out)
	require.Equal(t, 1, gh.createPR, "the run must open one pull request: %s", out)
	require.Equal(t, "main", gh.lastPRReq.Base,
		"without --auto-fix-base the base stays the repository default branch")
}

// TestReviewCmd_AutoFixBaseRequiresAutoFix: the override alone does nothing, so
// it is a usage error (exit 2) before any review work, like --single-model.
func TestReviewCmd_AutoFixBaseRequiresAutoFix(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "review", "--auto-fix-base", "release/1.x")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "--auto-fix-base requires --auto-fix")
}

// TestAutoFixBaseOverride_RejectsMalformedNames: an empty, option-like, or
// whitespace-bearing branch name is refused before any review work.
func TestAutoFixBaseOverride_RejectsMalformedNames(t *testing.T) {
	for _, v := range []string{"", "   ", "-x", "two words", "bad\tname", "a..b", "a~1", "a:b", "x.lock"} {
		t.Run(v, func(t *testing.T) {
			c := newReviewCmd()
			require.NoError(t, c.Flags().Parse([]string{"--auto-fix", "--auto-fix-base", v}))
			_, err := autoFixBaseOverride(c)
			require.Error(t, err)
			require.Contains(t, err.Error(), "--auto-fix-base")
		})
	}
}

// TestAutoFixBaseOverride_AcceptsAndTrims: a valid name is returned trimmed; an
// unset flag returns "" so the caller falls back to the default branch.
func TestAutoFixBaseOverride_AcceptsAndTrims(t *testing.T) {
	c := newReviewCmd()
	require.NoError(t, c.Flags().Parse([]string{"--auto-fix", "--auto-fix-base", " release/1.x "}))
	got, err := autoFixBaseOverride(c)
	require.NoError(t, err)
	require.Equal(t, "release/1.x", got)

	unset := newReviewCmd()
	require.NoError(t, unset.Flags().Parse([]string{"--auto-fix"}))
	got, err = autoFixBaseOverride(unset)
	require.NoError(t, err)
	require.Empty(t, got)

	require.Equal(t, "release/1.x", autoFixBaseBranch("release/1.x", "main"))
	require.Equal(t, "main", autoFixBaseBranch("", "main"))
}

// TestReviewCmd_ResumeRejectsAutoFixBase: --resume branches off before the fresh
// path reads --auto-fix-base, so it is refused there rather than silently dropped.
func TestReviewCmd_ResumeRejectsAutoFixBase(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "review", "--resume", "latest", "--auto-fix-base", "release/1.x")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "--resume does not support --auto-fix-base")
}
