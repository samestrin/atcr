package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initBaselineRepo commits a small multi-file tracked set (including a.txt, which
// the liveMockProvider's finding references so it survives path validation), for
// the range-less `atcr review --all` path.
func initBaselineRepo(t *testing.T) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	require.NoError(t, os.WriteFile("a.txt", []byte("one\n"), 0o644))
	require.NoError(t, os.WriteFile("b.go", []byte("package b\n"), 0o644))
	require.NoError(t, os.MkdirAll("internal", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("internal", "c.go"), []byte("package c\n"), 0o644))
	run("add", "-A")
	run("commit", "-q", "-m", "init")
}

func latestReviewDir(t *testing.T) string {
	t.Helper()
	latest, err := fanout.ReadLatest(".")
	require.NoError(t, err)
	return filepath.Join(fanout.ReviewsRoot("."), latest)
}

// AC 01-05 Happy Path 1 (Phase 2 single-payload scope, TD-004): `atcr review --all`
// runs to completion and writes a full review tree covering every tracked file,
// with the manifest recording no git range (proving gitrange.Resolve was skipped).
func TestReviewAll_EndToEndWholeRepo(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code := execCmd(t, "review", "--all")
	require.Equal(t, 0, code, "a completed baseline review exits 0")

	dir := latestReviewDir(t)
	for _, sub := range []string{"payload", "sources", "reconciled"} {
		assert.DirExists(t, filepath.Join(dir, sub))
	}
	assert.FileExists(t, filepath.Join(dir, "manifest.json"))

	// Whole-repo coverage: the single files-mode payload contains every tracked
	// file's content (no git ls-files omissions on the single-payload path).
	body, err := os.ReadFile(filepath.Join(dir, "payload", "files.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "package b", "b.go must be in the baseline payload")
	assert.Contains(t, string(body), "package c", "internal/c.go must be in the baseline payload")
	assert.Contains(t, string(body), "one", "a.txt must be in the baseline payload")

	// Manifest records no base/head — gitrange.Resolve was never invoked.
	mdata, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(mdata, &m))
	assert.Equal(t, "files", m.PayloadMode)
	assert.Empty(t, m.Base, "baseline scan has no base")
	assert.Empty(t, m.Head, "baseline scan has no head")

	// The reviewer produced a source under the standard pool layout.
	assert.FileExists(t, filepath.Join(dir, "sources", "pool", "raw", "agent", "bruce", "findings.txt"))
}

// AC 01-05 Happy Path 2: `--all --fail-on high` gates non-zero when a surviving
// high-or-above finding exists — the reconcile/gate chain needs no --all branching.
func TestReviewAll_FailOnGate(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t) // returns a CRITICAL finding on a.txt:1
	liveReviewConfig(t, srv.URL, "bruce")

	code := execCmd(t, "review", "--all", "--fail-on", "high")
	assert.Equal(t, 1, code, "a surviving CRITICAL must fail the high gate on the --all path")
}

// AC 01-05 Edge Case 3: an interrupted `--all` review is resumable via the existing
// resume machinery. A baseline manifest records an empty git range, so resume must
// skip range re-resolution/validation (else ErrRangeChanged, exit 2) and rebuild the
// payload from the repo walker so the pending agent reviews the same whole-repo scan
// the completed agents saw (Sprint 35.0 task 2.14.A HIGH fix).
func TestReviewAll_BaselineReviewIsResumable(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "kai")

	require.Equal(t, 0, execCmd(t, "review", "--all"))
	dir := latestReviewDir(t)

	// Simulate an interrupted run: drop one agent's completed source so resume has a
	// pending agent to finish.
	kaiDir := filepath.Join(dir, "sources", "pool", "raw", "agent", "kai")
	require.DirExists(t, kaiDir)
	require.NoError(t, os.RemoveAll(kaiDir))

	code := execCmd(t, "review", "--resume", "latest")
	require.Equal(t, 0, code, "a baseline review must be resumable (no ErrRangeChanged)")
	assert.FileExists(t, filepath.Join(kaiDir, "findings.txt"), "the pending baseline agent must be re-reviewed via the repo payload")
}

// AC 01-05 Happy Path 3: `atcr reconcile <id>` and `atcr report <id>` work
// unmodified against an --all-produced review directory (provenance-agnostic).
func TestReviewAll_ReconcileAndReportWorkOnBaselineOutput(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--all"))
	latest, err := fanout.ReadLatest(".")
	require.NoError(t, err)

	assert.Equal(t, 0, execCmd(t, "reconcile", latest), "reconcile works on baseline output")
	assert.Equal(t, 0, execCmd(t, "report", latest), "report works on baseline output")
}
