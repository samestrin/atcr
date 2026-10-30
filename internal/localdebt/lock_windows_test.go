//go:build windows

package localdebt

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithLockRefusesSymlinkedLockFile is the Windows twin of the unix test: a
// symlink at <dir>/.flock must fail the acquire rather than create or lock the
// file it points at, and the error carries only the base name. Creating a
// symlink needs Developer Mode or an elevated token, so the test skips without
// one.
func TestWithLockRefusesSymlinkedLockFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secretuser", "debt")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Symlink(target, filepath.Join(dir, flockName)); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	ran := false
	err := withLock(dir, "waiter", func() error { ran = true; return nil })
	require.Error(t, err)
	assert.False(t, ran, "fn must not run when the lock file is a symlink")
	assert.Contains(t, err.Error(), "acquire localdebt lock")
	assert.NotContains(t, err.Error(), "secretuser", "error must carry only the base name")
	_, statErr := os.Lstat(target)
	assert.True(t, os.IsNotExist(statErr), "the symlink target must not be created")
}

// TestWithLockRetriesWhenTheLockFileIsReplaced is the Windows twin of the unix
// successor-lock-delete test: a waiter opens .flock, the holder removes it and
// unlocks, and a new caller creates a fresh .flock and locks it — all before
// the waiter's LockFileEx. The waiter then locks the removed file without
// contention; it must see that the file is no longer the one at .flock and keep
// waiting, or two callers hold the lock at once.
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
		f, err := openLockFile(lockPath)
		require.NoError(t, err)
		locked, err := lockFileExclusive(f)
		require.NoError(t, err)
		require.True(t, locked, "the fresh .flock must lock uncontended")
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

// TestLockFileExclusiveRefusesASecondHandle locks the primitive itself: a
// second handle on a .flock another handle holds must report "held", not an
// error and not success, and must lock once the first handle closes.
func TestLockFileExclusiveRefusesASecondHandle(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), flockName)
	first, err := openLockFile(lockPath)
	require.NoError(t, err)
	locked, err := lockFileExclusive(first)
	require.NoError(t, err)
	require.True(t, locked)

	second, err := openLockFile(lockPath)
	require.NoError(t, err)
	defer func() { _ = second.Close() }()
	locked, err = lockFileExclusive(second)
	require.NoError(t, err)
	assert.False(t, locked, "a second handle must not take a held lock")

	require.NoError(t, first.Close())
	locked, err = lockFileExclusive(second)
	require.NoError(t, err)
	assert.True(t, locked, "the lock must be free once its holder's handle closes")
}
