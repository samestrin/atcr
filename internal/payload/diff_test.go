package payload

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// numstatNewPath must locate the {old => new} segment by finding the arrow
// first, then expanding to the surrounding braces — not by taking the first
// '{' in the field. A parent-directory name containing '{' must not shadow
// the actual rename delimiters.
func TestNumstatNewPath_BraceInParentDir(t *testing.T) {
	cases := []struct{ field, want string }{
		// Parent dir contains '{' — the bug case.
		{"a{x/{old.bin => new.bin}", "a{x/new.bin"},
		// Standard abbreviated rename.
		{"dir/{old.go => new.go}", "dir/new.go"},
		// Simple rename, no braces.
		{"old.go => new.go", "new.go"},
		// Unchanged path — no arrow.
		{"unchanged.go", "unchanged.go"},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			assert.Equal(t, tc.want, numstatNewPath(tc.field))
		})
	}
}

// splitDiffByFile must return an error when a chunk cannot be attributed to
// any known head path. Silent data loss (log + drop) is not acceptable for a
// CLI where log lines are invisible and the build still returns success.
func TestSplitDiffByFile_UnattributableChunkErrors(t *testing.T) {
	diff := "diff --git a/gone.go b/gone.go\n--- a/gone.go\n+++ b/gone.go\n@@ -1 +1 @@ whatever\n"
	// heads does not contain gone.go, so the chunk is unattributable.
	_, err := splitDiffByFile(diff, map[string]bool{"other.go": true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gone.go")
}

// chunkKey must return the correct head path for a large head set, validating
// that the O(1) fast path does not break longest-suffix correctness.
func TestChunkKey_LargeHeadSetCorrectness(t *testing.T) {
	heads := make(map[string]bool, 2000)
	for i := 0; i < 2000; i++ {
		heads[fmt.Sprintf("dir/%04d/f.go", i)] = true
	}
	chunk := "diff --git a/dir/0042/f.go b/dir/0042/f.go\n@@ -1 +1 @@ x"
	assert.Equal(t, "dir/0042/f.go", chunkKey(chunk, heads))
}

// BenchmarkChunkKey_FastPath measures chunkKey with the O(1) fast path active
// (common case: paths are unique and do not contain " b/").
func BenchmarkChunkKey_FastPath(b *testing.B) {
	heads := make(map[string]bool, 2000)
	for i := 0; i < 2000; i++ {
		heads[fmt.Sprintf("dir/%04d/f.go", i)] = true
	}
	chunk := "diff --git a/dir/0042/f.go b/dir/0042/f.go\n@@ -1 +1 @@ x"
	b.ResetTimer()
	for range b.N {
		_ = chunkKey(chunk, heads)
	}
}

// TestForRange_ResetAcrossRanges verifies that forRange enforces cache
// reconciliation structurally — a new cache field added to rangeState is
// automatically reset with the rest of the state when the range changes,
// with no per-accessor call to ensureRange required.
func TestForRange_ResetAcrossRanges(t *testing.T) {
	g := &gitRunner{ctx: context.Background(), dir: t.TempDir()}

	// Populate files under range "a..b".
	s := g.forRange("a", "b")
	s.files = []changedFile{{path: "x.go"}}

	// Switching range must reset the state (including files).
	s2 := g.forRange("c", "d")
	assert.Nil(t, s2.files, "files cache must be nil after range change")

	// Re-entering the original range returns fresh state (old data gone).
	s3 := g.forRange("a", "b")
	assert.Nil(t, s3.files, "files cache must be nil when re-entering a different range")
}

// A fatal git failure (here: not a repository) must propagate from isBinary
// rather than being silently reported as "not binary" (TD-010).
func TestIsBinary_FatalGitErrorPropagates(t *testing.T) {
	g := &gitRunner{ctx: context.Background(), dir: t.TempDir()}
	_, err := g.isBinary("HEAD~1", "HEAD", "a.go")
	require.Error(t, err)
}

// A fatal git failure must propagate from functionContextFile rather than
// being masked as the zero-hunk fallback (TD-010).
func TestFunctionContextFile_FatalGitErrorPropagates(t *testing.T) {
	g := &gitRunner{ctx: context.Background(), dir: t.TempDir()}
	_, _, err := g.functionContextFile("HEAD~1", "HEAD", "a.go")
	require.Error(t, err)
}

// An empty diff (path unchanged in base..head) stays the non-fatal fallback:
// ok=false with a nil error.
func TestFunctionContextFile_NoDiffIsFallbackNotError(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "changed.go", goFileV1)
	write(t, dir, "stable.go", "package p\n\nfunc S() int { return 1 }\n")
	base := commitAll(t, dir, "v1")
	write(t, dir, "changed.go", goFileV2)
	head := commitAll(t, dir, "v2")

	g := &gitRunner{ctx: context.Background(), dir: dir}
	out, ok, err := g.functionContextFile(base, head, "stable.go")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, out)
}

// An unchanged path is "not binary" with a nil error.
func TestIsBinary_NoDiffIsNotBinaryNotError(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "changed.go", goFileV1)
	write(t, dir, "stable.go", "package p\n\nfunc S() int { return 1 }\n")
	base := commitAll(t, dir, "v1")
	write(t, dir, "changed.go", goFileV2)
	head := commitAll(t, dir, "v2")

	g := &gitRunner{ctx: context.Background(), dir: dir}
	bin, err := g.isBinary(base, head, "stable.go")
	require.NoError(t, err)
	assert.False(t, bin)
}
