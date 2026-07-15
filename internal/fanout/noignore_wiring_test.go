package fanout

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitRun runs a git command in dir and returns trimmed stdout, failing the test
// on error. It mirrors the closure in initRepo so the ignore-aware fixtures below
// are self-contained.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func writeFileAt(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// ignoreRepo builds a repo whose base..head range changes one normal file
// (auth.go) and one .gitignore-matched, force-tracked file (vendor/lib.go). The
// filter drops vendor/lib.go by default; --no-ignore keeps it.
func ignoreRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	writeFileAt(t, dir, ".gitignore", "vendor/\n")
	writeFileAt(t, dir, "auth.go", "package main\n\nfunc a() {}\n")
	writeFileAt(t, dir, "vendor/lib.go", "package vendor\n\nfunc v() {}\n")
	gitRun(t, dir, "add", "-f", "vendor/lib.go")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	base = gitRun(t, dir, "rev-parse", "HEAD")
	writeFileAt(t, dir, "auth.go", "package main\n\nfunc a() { b() }\n\nfunc b() {}\n")
	writeFileAt(t, dir, "vendor/lib.go", "package vendor\n\nfunc v() { w() }\n\nfunc w() {}\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")
	head = gitRun(t, dir, "rev-parse", "HEAD")
	return dir, base, head
}

// allIgnoredRepo builds a range where EVERY changed file is ignore-filtered, so
// PrepareReview's payload is empty for a reason other than "no changed files".
func allIgnoredRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	writeFileAt(t, dir, ".gitignore", "vendor/\n")
	writeFileAt(t, dir, "vendor/lib.go", "package vendor\n\nfunc v() {}\n")
	gitRun(t, dir, "add", "-f", "vendor/lib.go")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	base = gitRun(t, dir, "rev-parse", "HEAD")
	writeFileAt(t, dir, "vendor/lib.go", "package vendor\n\nfunc v() { w() }\n\nfunc w() {}\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")
	head = gitRun(t, dir, "rev-parse", "HEAD")
	return dir, base, head
}

func readBlocksPayload(t *testing.T, reviewDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(reviewDir, "payload", "blocks.txt"))
	require.NoError(t, err)
	return string(data)
}

// TD internal/payload/diff.go:223 — when every changed file is ignore-filtered,
// PrepareReview must hint at --no-ignore rather than emit the misleading
// "no changed files (only merge or empty commits?)" hypothesis.
func TestPrepareReview_AllIgnored_HintsNoIgnore(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := allIgnoredRepo(t)
	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)

	_, err := PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoReviewableContent)
	assert.Contains(t, err.Error(), "--no-ignore",
		"an all-ignored range must hint at --no-ignore")
	assert.NotContains(t, err.Error(), "only merge or empty commits",
		"must not fall back to the misleading empty-range hypothesis")
}

// TD internal/fanout/review.go:244 — PrepareReview honors NoIgnore end to end:
// the ignored file is present in the built payload when NoIgnore=true and absent
// when false.
func TestPrepareReview_NoIgnoreWiring(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)

	t.Run("filtered by default", func(t *testing.T) {
		repo, base, head := ignoreRepo(t)
		req := reviewReq(repo, repo, base, head)
		prep, err := PrepareReview(context.Background(), cfg, req)
		require.NoError(t, err)
		payloadText := readBlocksPayload(t, prep.Dir)
		assert.Contains(t, payloadText, "auth.go")
		assert.NotContains(t, payloadText, "vendor/lib.go", "ignored file must be filtered by default")
	})

	t.Run("kept with NoIgnore", func(t *testing.T) {
		repo, base, head := ignoreRepo(t)
		req := reviewReq(repo, repo, base, head)
		req.NoIgnore = true
		prep, err := PrepareReview(context.Background(), cfg, req)
		require.NoError(t, err)
		payloadText := readBlocksPayload(t, prep.Dir)
		assert.Contains(t, payloadText, "auth.go")
		assert.Contains(t, payloadText, "vendor/lib.go", "--no-ignore must keep the ignored file")
	})
}

// TD cmd/atcr/resume.go:104 — NoIgnore is persisted in the manifest and recovered
// by PrepareResume from on-disk state, mirroring how scope is locked to the
// original run. A resume request that does NOT set NoIgnore must still see the
// ignored file, because the value comes from the manifest, not the resume request.
func TestPrepareResume_RecoversNoIgnoreFromManifest(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := ignoreRepo(t)
	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)

	// Fresh run WITH --no-ignore persists the flag to the manifest.
	freshReq := reviewReq(repo, repo, base, head)
	freshReq.NoIgnore = true
	prep, err := PrepareReview(context.Background(), cfg, freshReq)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(prep.Dir, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(data, &m))
	require.True(t, m.NoIgnore, "manifest must persist NoIgnore so resume can recover it")

	// Resume with a request that does NOT set NoIgnore: the value must be recovered
	// from the manifest. Grounding (built from the recovered NoIgnore) keeps the
	// ignored file's changed lines only when NoIgnore was correctly recovered.
	resumeReq := reviewReq(repo, repo, base, head)
	resumeReq.NoIgnore = false
	resumePrep, _, err := PrepareResume(context.Background(), cfg, prep.Dir, resumeReq)
	require.NoError(t, err)
	require.Empty(t, resumePrep.GroundingDisabledReason, "grounding should be active for this range")

	_, ok := resumePrep.Changed["vendor/lib.go"]
	assert.True(t, ok, "resume must recover NoIgnore=true from the manifest and keep the ignored file")
}
