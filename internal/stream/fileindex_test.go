package stream

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/metrics"
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

	idx := BuildFileIndex(context.Background(), root)
	require.NotNil(t, idx)

	assert.True(t, idx.Has("internal/auth/validate.go"))
	assert.False(t, idx.Has("internal/auth/validator.go"))
	assert.ElementsMatch(t, []string{"internal/auth/validate.go"}, idx.ByBasename("validate.go"))
}

// TestBuildFileIndex_NonGitDir: a directory that is not a git repo yields a nil
// index — callers degrade to existence-only (no suggestion).
func TestBuildFileIndex_NonGitDir(t *testing.T) {
	idx := BuildFileIndex(context.Background(), t.TempDir())
	assert.Nil(t, idx)
}

// TestBuildFileIndex_EmptyRoot: an empty root yields a nil index (no base
// directory configured).
func TestBuildFileIndex_EmptyRoot(t *testing.T) {
	assert.Nil(t, BuildFileIndex(context.Background(), ""))
}

// TestBuildFileIndex_CountsGitFailure: when the git invocation fails the index
// still degrades to nil, but the failure is surfaced via an observability counter
// so a silently disabled path-matcher in CI is distinguishable from a healthy
// run. Previously every git failure was collapsed into a bare `return nil` with
// the error discarded entirely. Verified by removing git from PATH (bogus PATH).
func TestBuildFileIndex_CountsGitFailure(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Setenv("PATH", "") // git unavailable: ls-files fails

	idx := BuildFileIndex(context.Background(), t.TempDir())

	assert.Nil(t, idx, "git failure must still degrade to a nil index")
	assert.Equal(t, int64(1), metrics.Counter("atcr_path_index_unavailable_total").Value(),
		"a git failure that disables the index must increment the observability counter")
}

// TestBuildFileIndex_RespectsCancelledContext: a cancelled context aborts the
// `git ls-files` child so a shut-down reconcile degrades to a nil index rather
// than blocking on or completing the subprocess.
func TestBuildFileIndex_RespectsCancelledContext(t *testing.T) {
	root := gitRepo(t, "internal/auth/validate.go")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	idx := BuildFileIndex(ctx, root)
	assert.Nil(t, idx, "a cancelled context must abort git ls-files and degrade to a nil index")
}

// TestFileIndex_DirBasenames: the directory index lists the basenames tracked
// under a given directory, used by Tier 2.
func TestFileIndex_DirBasenames(t *testing.T) {
	root := gitRepo(t, "internal/auth/validate.go", "internal/auth/login.go", "cmd/atcr/main.go")
	idx := BuildFileIndex(context.Background(), root)
	require.NotNil(t, idx)

	assert.ElementsMatch(t, []string{"validate.go", "login.go"}, idx.DirBasenames("internal/auth"))
	assert.Empty(t, idx.DirBasenames("internal/nope"))
}

// TestFileIndex_FoldedLookup: the case-folded index resolves a path that differs
// only by case to the real tracked path(s), used by Tier 3.
func TestFileIndex_FoldedLookup(t *testing.T) {
	root := gitRepo(t, "internal/auth/parser.go")
	idx := BuildFileIndex(context.Background(), root)
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

// TestFileIndex_Paths: the Paths accessor returns the full tracked-path set in
// the same slash-normalized, repo-root-relative form the other methods expect,
// so baseline (--all/--dir) enumeration (Sprint 35.0) can iterate the tracked
// files without reaching around the unexported map.
func TestFileIndex_Paths(t *testing.T) {
	idx := indexFromPaths([]string{"internal/auth/validate.go", "cmd/atcr/main.go", "README.md"})
	require.NotNil(t, idx)

	assert.ElementsMatch(t,
		[]string{"internal/auth/validate.go", "cmd/atcr/main.go", "README.md"},
		idx.Paths(),
		"Paths must return exactly the tracked set")
}

// TestFileIndex_Paths_Normalized: a backslash-bearing path is stored slash-
// normalized by indexFromPaths, so Paths returns the forward-slash form that
// ignoreMatcher.match and FileEntry.Path consume — never the raw backslashes.
func TestFileIndex_Paths_Normalized(t *testing.T) {
	idx := indexFromPaths([]string{"a\\b\\c.go"})
	require.NotNil(t, idx)

	assert.Equal(t, []string{"a/b/c.go"}, idx.Paths(), "Paths must return slash-normalized keys")
}

// TestFileIndex_Paths_NilSafe: a nil *FileIndex returns an empty, non-panicking
// slice, matching the nil-receiver convention of Has/ByBasename/DirBasenames.
func TestFileIndex_Paths_NilSafe(t *testing.T) {
	var idx *FileIndex
	assert.NotPanics(t, func() {
		assert.Empty(t, idx.Paths(), "nil FileIndex must yield an empty path set, not a panic")
	})
}

// TestFileIndex_Paths_Empty: a populated-but-empty index (no tracked files, e.g.
// a repo with nothing committed) returns an empty slice, not nil-vs-populated
// ambiguity — the caller's "no reviewable content" guard reads len()==0.
func TestFileIndex_Paths_Empty(t *testing.T) {
	idx := indexFromPaths(nil)
	require.NotNil(t, idx)
	assert.Empty(t, idx.Paths())
}

// TestFileIndex_BackslashCitedPath: on non-Windows builds filepath.ToSlash is a
// no-op, so a reviewer-cited path with backslashes must be explicitly normalized
// before lookup.
func TestFileIndex_BackslashCitedPath(t *testing.T) {
	idx := indexFromPaths([]string{"a/validate.go"})
	require.NotNil(t, idx)

	assert.True(t, idx.Has("a\\validate.go"), "Has should normalize backslashes")
	assert.Equal(t, "a/validate.go", idx.MissingSuggestion("b\\validate.go"), "MissingSuggestion should normalize backslashes")
}
