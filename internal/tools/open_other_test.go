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

// TestOpenReadOnly_OtherPlatform_DirectoryRejected verifies that a directory
// path is explicitly rejected, not opened as a readable *os.File. On non-unix
// platforms without O_NOFOLLOW, OpenFile(O_RDONLY) succeeds on directories,
// so openReadOnly must check IsRegular() after Lstat and refuse non-regular
// files before opening.
func TestOpenReadOnly_OtherPlatform_DirectoryRejected(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "subdir")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	f, err := openReadOnly(dir)
	if f != nil {
		_ = f.Close()
	}
	require.Error(t, err, "openReadOnly must reject directory paths on non-unix")
}
