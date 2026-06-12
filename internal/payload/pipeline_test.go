package payload

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitProcessCount builds a payload for a range touching nFiles changed files
// and reports how many git subprocesses the build spawned.
func gitProcessCount(t *testing.T, mode PayloadMode, nFiles int) int {
	t.Helper()
	dir := initRepo(t)
	for i := 0; i < nFiles; i++ {
		write(t, dir, fmt.Sprintf("f%d.go", i), goFileV1)
	}
	base := commitAll(t, dir, "v1")
	for i := 0; i < nFiles; i++ {
		write(t, dir, fmt.Sprintf("f%d.go", i), goFileV2)
	}
	head := commitAll(t, dir, "v2")

	g := &gitRunner{ctx: context.Background(), dir: dir}
	entries, err := g.buildEntries(mode, base, head)
	require.NoErrorf(t, err, "mode %s, %d files", mode, nFiles)
	require.Lenf(t, entries, nFiles, "mode %s: one entry per changed file", mode)
	return g.execCount
}

// The git-process count for blocks and diff modes must be constant — fully
// independent of the number of changed files. Before batching, each file
// triggered its own numstat + diff fan-out, so the count grew with N.
func TestBuildEntries_ConstantGitProcessCount(t *testing.T) {
	for _, mode := range []PayloadMode{ModeDiff, ModeBlocks} {
		small := gitProcessCount(t, mode, 2)
		large := gitProcessCount(t, mode, 8)
		assert.Equalf(t, small, large,
			"mode %s: git-process count must not grow with changed-file count (2 files: %d, 8 files: %d)",
			mode, small, large)
	}
}

// Files mode is constant EXCEPT for the per-file `git show` of head content,
// which the epic explicitly excludes from the constant-process requirement.
// Adding 6 files must therefore add exactly 6 git processes (the 6 extra
// shows), proving everything else — classification and the changed-range diff —
// is batched.
func TestBuildEntries_FilesModeOnlyShowScalesWithN(t *testing.T) {
	small := gitProcessCount(t, ModeFiles, 2)
	large := gitProcessCount(t, ModeFiles, 8)
	assert.Equalf(t, 6, large-small,
		"files mode should grow by exactly one git show per added file (2 files: %d, 8 files: %d)",
		small, large)
}
