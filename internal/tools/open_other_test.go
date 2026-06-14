//go:build !unix

package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenReadOnly_OtherPlatform_SymlinkDetected verifies the post-open inode
// check rejects a path that is a symlink (TOCTOU guard for non-O_NOFOLLOW builds).
func TestOpenReadOnly_OtherPlatform_SymlinkDetected(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real.txt")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))
	link := filepath.Join(root, "link.txt")
	require.NoError(t, os.Symlink(target, link))

	f, err := openReadOnly(link)
	if f != nil {
		_ = f.Close()
	}
	require.Error(t, err, "openReadOnly must detect symlink via SameFile check on non-unix")
}

// TestOpenReadOnly_OtherPlatform_RegularFile verifies that a plain regular
// file (pre/post inodes agree) opens successfully.
func TestOpenReadOnly_OtherPlatform_RegularFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real.txt")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))

	f, err := openReadOnly(target)
	require.NoError(t, err)
	require.NotNil(t, f)
	_ = f.Close()
}
