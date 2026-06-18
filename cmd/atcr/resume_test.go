package main

import (
	"bytes"
	"context"
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
