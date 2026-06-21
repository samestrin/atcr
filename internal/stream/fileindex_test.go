package stream

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitRepo initializes a throwaway git repo at a temp dir, writes the given
// relpaths (with package-stub content), commits them, and returns the root. The
// candidate index is built from `git ls-files`, so files must be tracked.
func gitRepo(t *testing.T, relpaths ...string) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	run("config", "user.email", "t@t.t")
	run("config", "user.name", "t")
	for _, rel := range relpaths {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte("package x\n"), 0o644))
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return root
}

// TestBuildFileIndex_TracksFiles: a built index reports tracked relpaths and
// indexes them by basename.
func TestBuildFileIndex_TracksFiles(t *testing.T) {
	root := gitRepo(t, "internal/auth/validate.go", "cmd/atcr/main.go")

	idx := BuildFileIndex(root)
	require.NotNil(t, idx)

	assert.True(t, idx.Has("internal/auth/validate.go"))
	assert.False(t, idx.Has("internal/auth/validator.go"))
	assert.ElementsMatch(t, []string{"internal/auth/validate.go"}, idx.ByBasename("validate.go"))
}

// TestBuildFileIndex_NonGitDir: a directory that is not a git repo yields a nil
// index — callers degrade to existence-only (no suggestion).
func TestBuildFileIndex_NonGitDir(t *testing.T) {
	idx := BuildFileIndex(t.TempDir())
	assert.Nil(t, idx)
}

// TestBuildFileIndex_EmptyRoot: an empty root yields a nil index (no base
// directory configured).
func TestBuildFileIndex_EmptyRoot(t *testing.T) {
	assert.Nil(t, BuildFileIndex(""))
}

// TestFileIndex_DirBasenames: the directory index lists the basenames tracked
// under a given directory, used by Tier 2.
func TestFileIndex_DirBasenames(t *testing.T) {
	root := gitRepo(t, "internal/auth/validate.go", "internal/auth/login.go", "cmd/atcr/main.go")
	idx := BuildFileIndex(root)
	require.NotNil(t, idx)

	assert.ElementsMatch(t, []string{"validate.go", "login.go"}, idx.DirBasenames("internal/auth"))
	assert.Empty(t, idx.DirBasenames("internal/nope"))
}

// TestFileIndex_FoldedLookup: the case-folded index resolves a path that differs
// only by case to the real tracked path(s), used by Tier 3.
func TestFileIndex_FoldedLookup(t *testing.T) {
	root := gitRepo(t, "internal/auth/parser.go")
	idx := BuildFileIndex(root)
	require.NotNil(t, idx)

	assert.ElementsMatch(t, []string{"internal/auth/parser.go"}, idx.ByFold("internal/auth/Parser.go"))
	assert.ElementsMatch(t, []string{"internal/auth/parser.go"}, idx.ByFold("INTERNAL/AUTH/PARSER.GO"))
	assert.Empty(t, idx.ByFold("internal/auth/other.go"))
}

// TestIndexFromPaths_PreservesWhitespace: git ls-files -z emits paths verbatim,
// including any leading or trailing spaces in filenames, so the index must not
// trim whitespace.
func TestIndexFromPaths_PreservesWhitespace(t *testing.T) {
	idx := indexFromPaths([]string{" path with spaces.go ", "normal.go"})
	require.NotNil(t, idx)

	assert.True(t, idx.Has(" path with spaces.go "), "tracked path with spaces should be preserved")
	assert.True(t, idx.Has("normal.go"))
	assert.False(t, idx.Has("path with spaces.go"), "trimmed variant should not be tracked")
}
