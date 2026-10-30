//go:build unix || windows

package localdebt

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests in this file drive only withLock, so they hold for both kernel
// locks: flock on unix (lock_unix.go) and LockFileEx on Windows
// (lock_windows.go).

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
// (SIGKILL on unix, TerminateProcess on Windows, so no deferred cleanup runs).
// The kernel drops the dead holder's lock, so the next caller must acquire at
// once, with no stale-wait.
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
