//go:build unix

package localdebt

import (
	"os"
	"os/exec"
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

// TestWithLockHolderHelper is not a test: it is the body of the subprocess that
// TestWithLockSurvivesKilledHolder starts. It takes the lock, tells the parent
// through a ready file, and holds until killed.
func TestWithLockHolderHelper(t *testing.T) {
	dir := os.Getenv("LOCALDEBT_LOCK_HELPER_DIR")
	if dir == "" {
		t.Skip("helper for TestWithLockSurvivesKilledHolder")
	}
	err := withLock(dir, "holder", func() error {
		if err := os.WriteFile(filepath.Join(dir, "ready"), nil, 0o600); err != nil {
			return err
		}
		time.Sleep(time.Minute)
		return nil
	})
	t.Fatalf("holder returned before being killed: %v", err)
}

// TestWithLockSurvivesKilledHolder kills a real process while it holds the lock
// (SIGKILL, so no deferred cleanup runs). The kernel drops the dead holder's
// flock, so the next caller must acquire at once, with no stale-wait.
func TestWithLockSurvivesKilledHolder(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestWithLockHolderHelper$")
	cmd.Env = append(os.Environ(), "LOCALDEBT_LOCK_HELPER_DIR="+dir)
	require.NoError(t, cmd.Start())
	exited := false
	defer func() {
		if !exited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()

	ready := filepath.Join(dir, "ready")
	require.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil },
		10*time.Second, 10*time.Millisecond, "holder never took the lock")

	// While the holder lives, a waiter times out.
	oldWait := lockWait
	lockWait = 50 * time.Millisecond
	err := withLock(dir, "waiter", func() error { return nil })
	lockWait = oldWait
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout acquiring localdebt lock")

	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()
	exited = true

	start := time.Now()
	ran := false
	require.NoError(t, withLock(dir, "next", func() error { ran = true; return nil }))
	assert.True(t, ran)
	assert.Less(t, time.Since(start), 5*time.Second, "a killed holder's lock must not need a stale wait")
}
