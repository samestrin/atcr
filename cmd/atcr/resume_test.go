package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/require"
)

// liveReviewConfig writes a user registry whose agents are pointed at srvURL (a
// live mock provider) and a project config selecting them, so a real `atcr review`
// produces a genuinely complete review on disk. Each agent's persona equals its
// name so resolution falls through to the embedded default.
func liveReviewConfig(t *testing.T, srvURL string, agents ...string) {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	reg := "providers:\n  p:\n    api_key_env: ATCR_TEST_REVIEW_KEY\n    base_url: " + srvURL + "\nagents:\n"
	for _, a := range agents {
		reg += "  " + a + ":\n    provider: p\n    model: m-" + a + "\n    persona: " + a + "\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(reg), 0o644))
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"),
		[]byte("agents: ["+strings.Join(agents, ", ")+"]\n"), 0o644))
}

// liveMockProvider returns an httptest server speaking the OpenAI chat-completions
// shape, replying with one finding for any model.
func liveMockProvider(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		content := "CRITICAL|a.txt:1|Unchecked call|Guard it|security|15|evidence"
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// scaffoldResumeReview creates a minimal review directory (id under
// .atcr/reviews/) with a sources/ tree so resolveResumeDir's completeness check
// passes. Returns the review dir path.
func scaffoldResumeReview(t *testing.T, id string) string {
	t.Helper()
	dir := filepath.Join(fanout.ReviewsRoot("."), id)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources"), 0o755))
	return dir
}

func TestResolveResumeDir_LatestAndEmpty(t *testing.T) {
	isolate(t)
	dir := scaffoldResumeReview(t, "2026-06-18_demo")
	require.NoError(t, fanout.WriteLatest(".", "2026-06-18_demo"))

	// Both the literal "latest" and an empty anchor resolve the .atcr/latest pointer.
	for _, anchor := range []string{"latest", ""} {
		got, err := resolveResumeDir(anchor)
		require.NoError(t, err, "anchor %q", anchor)
		require.Equal(t, dir, got, "anchor %q", anchor)
	}
}

func TestResolveResumeDir_ByID(t *testing.T) {
	isolate(t)
	dir := scaffoldResumeReview(t, "2026-06-18_demo")
	got, err := resolveResumeDir("2026-06-18_demo")
	require.NoError(t, err)
	require.Equal(t, dir, got)
}

func TestResolveResumeDir_ExplicitPath(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources"), 0o755))
	got, err := resolveResumeDir(dir)
	require.NoError(t, err)
	require.Equal(t, dir, got)
}

func TestResolveResumeDir_MissingLatestErrors(t *testing.T) {
	isolate(t)
	_, err := resolveResumeDir("latest")
	require.Error(t, err)
}

func TestResolveResumeDir_UnknownIDErrors(t *testing.T) {
	isolate(t)
	_, err := resolveResumeDir("2026-06-18_nope")
	require.Error(t, err)
}

// gitRevParse resolves a ref to its SHA in the current repo (tests run inside an
// isolated, chdir'd repo).
func gitRevParse(t *testing.T, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", ref).Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

// writeResumeReviewFixture writes a review directory under .atcr/reviews/<id> with
// a manifest for the given range/roster and per-agent ok status.json for each
// completed agent, and repoints .atcr/latest. sources/ always exists so
// resolveResumeDir's completeness check passes.
func writeResumeReviewFixture(t *testing.T, id, base, head string, roster, completed []string) string {
	t.Helper()
	dir := filepath.Join(fanout.ReviewsRoot("."), id)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool"), 0o755))
	m := &payload.Manifest{
		Base: base, Head: head, Roster: roster,
		StartedAt: time.Now().UTC(), TimeoutSecs: 600, PayloadMode: "blocks",
		PerAgentPayload: map[string]string{}, Stages: []string{"review"},
	}
	require.NoError(t, fanout.WriteManifest(dir, m))
	for _, name := range completed {
		ad := filepath.Join(dir, "sources", "pool", "raw", "agent", name)
		require.NoError(t, os.MkdirAll(ad, 0o755))
		require.NoError(t, fanout.WriteStatus(filepath.Join(ad, "status.json"),
			&fanout.AgentStatus{Agent: name, Status: fanout.StatusOK}))
	}
	require.NoError(t, fanout.WriteLatest(".", id))
	return dir
}

// execResume runs the command tree and returns the exit code plus the combined
// stdout/stderr and error text, so a test can assert resume-specific diagnostics.
func execResume(t *testing.T, args ...string) (int, string) {
	t.Helper()
	root := newRootCmd()
	root.SetArgs(args)
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	err := root.ExecuteContext(context.Background())
	out := buf.String()
	if err != nil {
		out += err.Error()
	}
	return exitCode(err), out
}

func TestResume_IncompatibleWithIDIsExit2(t *testing.T) {
	isolate(t)
	code, out := execResume(t, "review", "--resume", "latest", "--id", "x")
	require.Equal(t, 2, code)
	require.Contains(t, out, "--resume cannot be combined")
}

func TestResume_UnknownAnchorIsExit2(t *testing.T) {
	isolate(t)
	code := execCmd(t, "review", "--resume", "2026-06-18_nope")
	require.Equal(t, 2, code)
}

func TestResume_RangeMismatchIsExit2(t *testing.T) {
	isolate(t)
	initGitRepoWithChange(t)
	writeReviewFixtureConfig(t) // roster = [bruce]
	// Manifest range deliberately does not match HEAD^..HEAD.
	writeResumeReviewFixture(t, "2026-06-18_demo", "deadbeefdeadbeef", "cafebabecafebabe", []string{"bruce"}, nil)

	code, out := execResume(t, "review", "--resume", "latest", "--base", "HEAD^")
	require.Equal(t, 2, code, "AC3: changed range aborts with exit 2")
	require.Contains(t, out, "working tree changed", "AC3: clear range-mismatch error")
}

func TestResume_RosterMismatchIsExit2(t *testing.T) {
	isolate(t)
	initGitRepoWithChange(t)
	writeReviewFixtureConfig(t) // roster = [bruce]
	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")
	// Range matches; roster differs ([bruce, robin] vs configured [bruce]).
	writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, nil)

	code, out := execResume(t, "review", "--resume", "latest", "--base", "HEAD^")
	require.Equal(t, 2, code, "roster drift aborts with exit 2")
	require.Contains(t, out, "roster changed")
}

func TestResume_AllCompleteReconcilesAndExitsZero(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TEST_REVIEW_KEY", "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	// A real review completes bruce and writes the full review tree + manifest.
	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^"))

	// Resume finds nothing pending: AC2 — announce + re-reconcile + exit 0.
	code, out := execResume(t, "review", "--resume", "latest", "--base", "HEAD^")
	require.Equal(t, 0, code, "AC2: all agents already completed -> clean exit")
	require.Contains(t, out, "All configured agents already completed")
	require.Contains(t, out, "reconciled")
}

func TestResume_RunsPendingAgentThenReconciles(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TEST_REVIEW_KEY", "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "robin")
	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")
	// bruce already completed; robin is pending (no status.json). Range + roster match.
	writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, []string{"bruce"})

	code, out := execResume(t, "review", "--resume", "latest", "--base", "HEAD^")
	require.Equal(t, 0, code, "resume runs the pending agent and reconciles -> exit 0")
	require.Contains(t, out, "1 completed, 1 pending")
	require.Contains(t, out, "reconciled")

	// AC4/AC6: robin now has results and the review derives to completed.
	dir := filepath.Join(fanout.ReviewsRoot("."), "2026-06-18_demo")
	st, err := fanout.ReadReviewStatus(dir, "2026-06-18_demo")
	require.NoError(t, err)
	require.Equal(t, fanout.RunCompleted, st.Status)
	require.False(t, st.Partial)
}

func TestResume_FailOnFlagIsExit2(t *testing.T) {
	isolate(t)
	code, out := execResume(t, "review", "--resume", "latest", "--fail-on", "high")
	require.Equal(t, 2, code, "the one-shot gate is not supported with --resume")
	require.Contains(t, out, "does not support --fail-on")
}

func TestResume_VerifyFlagIsExit2(t *testing.T) {
	isolate(t)
	code, out := execResume(t, "review", "--resume", "latest", "--verify")
	require.Equal(t, 2, code)
	require.Contains(t, out, "does not support --verify")
}

func TestResume_AllCompleteClearsStaleInterrupted(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TEST_REVIEW_KEY", "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")
	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^"))

	// Simulate the narrow window: every agent is ok on disk, but the manifest is
	// still marked interrupted (signal landed after the last agent wrote ok).
	id, err := fanout.ReadLatest(".")
	require.NoError(t, err)
	dir := filepath.Join(fanout.ReviewsRoot("."), id)
	m, err := fanout.ReadManifest(dir)
	require.NoError(t, err)
	m.Interrupted = true
	require.NoError(t, fanout.WriteManifest(dir, m))
	st, err := fanout.ReadReviewStatus(dir, id)
	require.NoError(t, err)
	require.Equal(t, fanout.RunInterrupted, st.Status, "precondition: stale interrupted state")

	code, _ := execResume(t, "review", "--resume", "latest", "--base", "HEAD^")
	require.Equal(t, 0, code)
	stAfter, err := fanout.ReadReviewStatus(dir, id)
	require.NoError(t, err)
	require.Equal(t, fanout.RunCompleted, stAfter.Status, "AC6: resume clears the stale interrupted marker")
}

func TestResume_VerifyOnlyFlagsAreExit2(t *testing.T) {
	for _, tc := range []struct {
		flag string
		args []string
	}{
		{"fresh", []string{"review", "--resume", "latest", "--fresh"}},
		{"thorough", []string{"review", "--resume", "latest", "--thorough"}},
		{"min-severity", []string{"review", "--resume", "latest", "--min-severity", "HIGH"}},
	} {
		tc := tc
		t.Run(tc.flag, func(t *testing.T) {
			isolate(t)
			code, out := execResume(t, tc.args...)
			require.Equal(t, 2, code, "--%s should be rejected with exit 2 when using --resume", tc.flag)
			require.Contains(t, out, "does not support --"+tc.flag)
		})
	}
}
