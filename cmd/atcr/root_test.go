package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepoRoot_SymlinkToGitDirIsRejected(t *testing.T) {
	// repoRoot must NOT follow a .git symlink to an arbitrary directory —
	// os.Stat follows symlinks, which would let a .git symlink to another
	// directory bypass the repo-root check. Use os.Lstat instead.
	isolate(t)

	// Create a nested structure:
	//   outer/
	//     .git/         (real directory — valid repo root)
	//     inner/
	//       .git -> ../ (symlink to outer/.git)
	// chdir into inner/. repoRoot should skip the symlink, walk up to outer/,
	// and return outer/ — NOT inner/ (which would indicate the symlink was followed).
	outer := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outer, ".git"), 0o755))
	inner := filepath.Join(outer, "inner")
	require.NoError(t, os.MkdirAll(inner, 0o755))
	require.NoError(t, os.Symlink("..", filepath.Join(inner, ".git")))

	require.NoError(t, os.Chdir(inner))

	got, err := repoRoot()
	require.NoError(t, err)

	// After the fix (os.Lstat), repoRoot skips the symlink and returns outer.
	// Before the fix (os.Stat), repoRoot follows the symlink and returns inner.
	// Resolve symlinks in outer to match the path os.Getwd() returns (macOS
	// symlinks /var to /private/var).
	expectedOuter, err := filepath.EvalSymlinks(outer)
	require.NoError(t, err)
	require.Equal(t, expectedOuter, got, "repoRoot must skip .git symlink and walk up to the real repo root")
}

func TestRepoRoot_FallbackToCwdWhenNoMarker(t *testing.T) {
	// When no .git or .atcr marker exists, repoRoot must return cwd.
	isolate(t)

	got, err := repoRoot()
	require.NoError(t, err)

	cwd, err := os.Getwd()
	require.NoError(t, err)

	require.Equal(t, cwd, got, "repoRoot must return cwd when no marker is found")
}
