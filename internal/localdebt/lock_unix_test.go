//go:build unix

package localdebt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithLockRefusesSymlinkedLockFile locks the O_NOFOLLOW open: a symlink at
// <dir>/.flock must fail the acquire rather than create or lock the file it
// points at, and the error carries only the base name.
func TestWithLockRefusesSymlinkedLockFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secretuser", "debt")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	target := filepath.Join(t.TempDir(), "elsewhere")
	require.NoError(t, os.Symlink(target, filepath.Join(dir, flockName)))

	ran := false
	err := withLock(dir, "waiter", func() error { ran = true; return nil })
	require.Error(t, err)
	assert.False(t, ran, "fn must not run when the lock file cannot be opened")
	assert.Contains(t, err.Error(), "acquire localdebt lock")
	assert.NotContains(t, err.Error(), "secretuser", "error must carry only the base name")
	_, statErr := os.Lstat(target)
	assert.True(t, os.IsNotExist(statErr), "the symlink target must not be created")
}

// TestWithLockLeavesNoLockFileBehind locks the release: the holder removes
// .flock before it unlocks, so a store a user tracks in git never gains an
// untracked lock file, and the directory holds only what fn wrote.
func TestWithLockLeavesNoLockFileBehind(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, withLock(dir, "append", func() error {
		_, err := os.Lstat(filepath.Join(dir, flockName))
		assert.NoError(t, err, ".flock must exist while the lock is held")
		return nil
	}))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "withLock must not leave .flock behind")
}

// TestWithLockReusesALeftoverLockFile covers a holder that died while holding:
// its .flock stays on disk, unlocked by the kernel. The next caller locks that
// same file without waiting and removes it on release.
func TestWithLockReusesALeftoverLockFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, flockName), nil, 0o600))
	ran := false
	require.NoError(t, withLock(dir, "append", func() error { ran = true; return nil }))
	assert.True(t, ran)
	_, err := os.Lstat(filepath.Join(dir, flockName))
	assert.True(t, os.IsNotExist(err), "the leftover .flock is removed on release")
}
