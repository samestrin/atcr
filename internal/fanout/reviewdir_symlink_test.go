package fanout

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// symlinkedReviewRoots returns repo roots whose .atcr (or .atcr/reviews) is a
// symlink to outside, the shape a cloned repository can ship (EST-8260), and
// the outside directory each one points at.
func symlinkedReviewRoots(t *testing.T) map[string][2]string {
	t.Helper()
	out := map[string][2]string{}

	root, outside := t.TempDir(), t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, ".atcr")))
	out[".atcr"] = [2]string{root, outside}

	root, outside = t.TempDir(), t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, ".atcr", "reviews")))
	out[".atcr/reviews"] = [2]string{root, outside}
	return out
}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing may be written through the symlink into %s", dir)
}

func TestScaffoldReviewDir_RefusesSymlinkedReviewsRoot(t *testing.T) {
	for name, rc := range symlinkedReviewRoots(t) {
		t.Run(name, func(t *testing.T) {
			_, err := ScaffoldReviewDir(rc[0], "r1")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "symlink")
			assertEmptyDir(t, rc[1])
		})
	}
}

func TestClaimReviewDir_RefusesSymlinkedReviewsRoot(t *testing.T) {
	for name, rc := range symlinkedReviewRoots(t) {
		t.Run(name, func(t *testing.T) {
			_, _, err := claimReviewDir(rc[0], "r1", "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "symlink")
			assertEmptyDir(t, rc[1])
		})
	}
}

func TestForceBackupReviewDir_RefusesSymlinkedReviewsRoot(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "reviews", "r1"), 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, ".atcr")))

	_, err := forceBackupReviewDir(context.Background(), root, "r1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink")
	assert.DirExists(t, filepath.Join(outside, "reviews", "r1"), "the outside review dir must not be moved")
}
