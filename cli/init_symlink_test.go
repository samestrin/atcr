package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/atomicfs"
)

// TestInit_RefusesSymlinkedAtcrDir is the EST-8260 probe: a cloned repository
// can ship .atcr as a symlink, and `init --force` overwrote the .gitignore in
// the directory it pointed at. init now refuses a symlinked .atcr or
// .atcr/personas and writes nothing.
func TestInit_RefusesSymlinkedAtcrDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	for _, rel := range []string{".atcr", filepath.Join(".atcr", "personas")} {
		for _, force := range []bool{false, true} {
			dir, outside := t.TempDir(), t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(outside, ".gitignore"), []byte("OUTSIDE\n"), 0o644))
			require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755))
			require.NoError(t, os.Symlink(outside, filepath.Join(dir, rel)))

			err := runInit(dir, force, &bytes.Buffer{}, &bytes.Buffer{})
			require.Error(t, err, "%s force=%v", rel, force)
			assert.Contains(t, err.Error(), "symlink")
			assert.Equal(t, "OUTSIDE\n", readFile(t, filepath.Join(outside, ".gitignore")))
			entries, rerr := os.ReadDir(outside)
			require.NoError(t, rerr)
			assert.Len(t, entries, 1, "%s force=%v: nothing may be written through the symlink", rel, force)
		}
	}
}

func TestRefreshPersonas_RefusesSymlinkedAtcrDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	dir, outside := t.TempDir(), t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(outside, "personas"), 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, ".atcr")))
	err := runRefreshPersonas(dir, false, &bytes.Buffer{}, atomicfs.WriteFileAtomic)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink")
	entries, rerr := os.ReadDir(filepath.Join(outside, "personas"))
	require.NoError(t, rerr)
	assert.Empty(t, entries)
}
