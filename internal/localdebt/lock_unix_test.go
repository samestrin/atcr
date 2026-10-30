//go:build unix

package localdebt

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

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

// TestWithLockRetriesWhenTheLockFileIsReplaced covers the window the release's
// remove opens: a waiter opens .flock, the holder removes it and unlocks, and a
// new caller creates a fresh .flock and locks it — all before the waiter's
// flock. The waiter then locks the removed file without contention; it must see
// that the file is no longer the one at .flock and keep waiting, or two callers
// hold the lock at once.
func TestWithLockRetriesWhenTheLockFileIsReplaced(t *testing.T) {
	oldWait, oldHook := lockWait, afterLockOpen
	lockWait = 50 * time.Millisecond
	defer func() { lockWait, afterLockOpen = oldWait, oldHook }()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, flockName)
	var newHolder *os.File
	afterLockOpen = func() {
		afterLockOpen = func() {}
		require.NoError(t, os.Remove(lockPath))
		f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o600)
		require.NoError(t, err)
		require.NoError(t, syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
		newHolder = f
	}

	ran := false
	err := withLock(dir, "waiter", func() error { ran = true; return nil })
	require.NotNil(t, newHolder, "the hook must have replaced .flock")
	defer func() { _ = newHolder.Close() }()
	assert.False(t, ran, "fn must not run while the replacement .flock is held")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout acquiring localdebt lock")
}
