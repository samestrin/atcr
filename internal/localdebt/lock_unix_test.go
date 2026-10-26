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
