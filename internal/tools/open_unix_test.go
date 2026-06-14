//go:build unix

package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenReadOnly_RefusesSymlink confirms O_NOFOLLOW blocks opening a symlink
// directly (the final-component TOCTOU guard).
func TestOpenReadOnly_RefusesSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real.txt")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))
	link := filepath.Join(root, "link.txt")
	require.NoError(t, os.Symlink(target, link))

	f, err := openReadOnly(link)
	if f != nil {
		_ = f.Close()
	}
	require.Error(t, err, "O_NOFOLLOW must refuse to open a symlink")
}
