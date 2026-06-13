package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

func setupGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	gitCmd(t, repo, "init", "-q")
	gitCommitEmpty(t, repo, "initial")
	return repo
}

func gitCommitEmpty(t *testing.T, repo, msg string) {
	t.Helper()
	gitCmd(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-q", "-m", msg)
}

func gitHead(t *testing.T, repo string) string {
	t.Helper()
	return gitCmd(t, repo, "rev-parse", "HEAD")
}

func TestSnapshotFor_FastPathLive(t *testing.T) {
	repo := setupGitRepo(t)
	head := gitHead(t, repo)
	m := NewSnapshotManager(repo)

	root, cleanup, err := m.SnapshotFor(head)
	require.NoError(t, err)
	defer cleanup()
	assert.Equal(t, repo, root, "clean + head==HEAD uses the live worktree")
}

func TestSnapshotFor_SlowPathDifferentHead(t *testing.T) {
	repo := setupGitRepo(t)
	old := gitHead(t, repo)
	gitCommitEmpty(t, repo, "second")
	m := NewSnapshotManager(repo)

	root, cleanup, err := m.SnapshotFor(old)
	require.NoError(t, err)
	assert.NotEqual(t, repo, root)
	_, statErr := os.Stat(root)
	require.NoError(t, statErr, "worktree exists before cleanup")

	cleanup()
	_, statErr = os.Stat(root)
	assert.True(t, os.IsNotExist(statErr), "worktree removed after cleanup")
}

func TestSnapshotFor_SlowPathDirty(t *testing.T) {
	repo := setupGitRepo(t)
	head := gitHead(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("x"), 0o644))
	m := NewSnapshotManager(repo)

	root, cleanup, err := m.SnapshotFor(head)
	require.NoError(t, err)
	defer cleanup()
	assert.NotEqual(t, repo, root, "dirty worktree forces the slow path")
}

func TestSnapshotFor_CleanupIdempotent(t *testing.T) {
	repo := setupGitRepo(t)
	old := gitHead(t, repo)
	gitCommitEmpty(t, repo, "second")
	m := NewSnapshotManager(repo)

	root, cleanup, err := m.SnapshotFor(old)
	require.NoError(t, err)
	cleanup()
	cleanup()
	cleanup()
	_, statErr := os.Stat(root)
	assert.True(t, os.IsNotExist(statErr))
}

func TestSnapshotFor_InvalidSHA(t *testing.T) {
	repo := setupGitRepo(t)
	m := NewSnapshotManager(repo)
	_, _, err := m.SnapshotFor("not-a-valid-sha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid head SHA")
}

func TestSnapshotFor_UnreachableSHA(t *testing.T) {
	repo := setupGitRepo(t)
	m := NewSnapshotManager(repo)
	_, _, err := m.SnapshotFor("fffffffffffffffffffffffffffffffffffffffff"[:40])
	require.Error(t, err)
}

// TestSnapshotFor_ConcurrentSlowPath exercises concurrent slow-path calls on
// the same SnapshotManager. The per-manager mutex (addMu) must serialize the
// worktree-add + prune block so concurrent goroutines cannot interleave a prune
// from one goroutine with an add from another.
// Run with -race to verify no Go-level data races.
func TestSnapshotFor_ConcurrentSlowPath(t *testing.T) {
	repo := setupGitRepo(t)
	old := gitHead(t, repo)
	gitCommitEmpty(t, repo, "second")
	m := NewSnapshotManager(repo)

	const workers = 4
	var wg sync.WaitGroup
	roots := make([]string, workers)
	cleanups := make([]func(), workers)
	errs := make([]error, workers)

	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			roots[i], cleanups[i], errs[i] = m.SnapshotFor(old)
		}(i)
	}
	wg.Wait()

	for i, c := range cleanups {
		if c != nil {
			c()
		}
		assert.NoError(t, errs[i], "goroutine %d SnapshotFor failed", i)
		if roots[i] != "" {
			assert.NotEqual(t, repo, roots[i], "goroutine %d must use a worktree, not live root", i)
		}
	}
}
