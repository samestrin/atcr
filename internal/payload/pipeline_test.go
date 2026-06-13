package payload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// A single range combining every change kind — modified, added, deleted,
// renamed (with edit), and binary — must split cleanly: each file's body is
// attributed to the right path with no cross-contamination, in every mode.
// This pins the splitter against the high-impact mis-attribution risk.
func TestBuildEntries_MixedChangeKindsSplitCleanly(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "mod.go", goFileV1)
	write(t, dir, "gone.go", "package p\n\nfunc Gone() string { return \"gone\" }\n")
	write(t, dir, "old.go", goFileV1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pic.bin"), []byte{0x00, 0x01, 0x00}, 0o644))
	base := commitAll(t, dir, "v1")
	write(t, dir, "mod.go", goFileV2)                            // modified
	require.NoError(t, os.Remove(filepath.Join(dir, "gone.go"))) // deleted
	gitCmd(t, dir, "mv", "old.go", "new.go")                     // renamed...
	write(t, dir, "new.go", goFileV2)                            // ...with edit
	// Distinct content so -M does not pair it with the deleted file as a rename.
	write(t, dir, "added.go", "package p\n\nfunc Added() string { return \"added\" }\n")             // added
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pic.bin"), []byte{0xff, 0x00, 0xff}, 0o644)) // binary change
	head := commitAll(t, dir, "v2")
	ctx := context.Background()

	// Diff mode: the renamed file keeps its pairing (no spurious full-file add),
	// the binary file is a marker not raw bytes, and the deleted file is a real
	// deletion diff — each in its own entry.
	diffEntries, err := BuildEntries(ctx, ModeDiff, dir, base, head)
	require.NoError(t, err)
	byPath := map[string]string{}
	for _, e := range diffEntries {
		byPath[e.Path] = e.Body
	}
	assert.Contains(t, byPath["new.go"], "rename from old.go")
	assert.NotContains(t, byPath["new.go"], "+func Bar() int {", "rename pairing lost")
	assert.Equal(t, "[binary file changed: pic.bin]\n", byPath["pic.bin"])
	assert.Contains(t, byPath["mod.go"], "+\treturn 2")
	assert.Contains(t, byPath["gone.go"], "-package p")
	// No body may leak another file's path into its diff header.
	assert.NotContains(t, byPath["mod.go"], "new.go")
	assert.NotContains(t, byPath["added.go"], "mod.go")

	// Files mode: renamed header, deleted/binary markers, full content for the
	// modified and added files — all correctly keyed.
	fileEntries, err := BuildEntries(ctx, ModeFiles, dir, base, head)
	require.NoError(t, err)
	fByPath := map[string]string{}
	for _, e := range fileEntries {
		fByPath[e.Path] = e.Body
	}
	assert.Contains(t, fByPath["new.go"], "(renamed from old.go)")
	assert.Equal(t, "[deleted file: gone.go]\n", fByPath["gone.go"])
	assert.Equal(t, "[binary file changed: pic.bin]\n", fByPath["pic.bin"])
	assert.Contains(t, fByPath["added.go"], "=== FILE: added.go ===")
}

// Added content that mimics a unified-diff file header (`++ b/x` becomes the
// `+++ b/x` added line) must NOT mis-key the chunk: the splitter keys on the
// `diff --git` header against the known changed-file list, never on hunk-body
// lines. Before the fix, the spoofed `+++ b/` line stole the chunk's key and
// the real file's body silently became empty.
func TestBuildEntries_DiffHeaderSpoofInContentDoesNotMisattribute(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "real.txt", "alpha\nbeta\n")
	base := commitAll(t, dir, "v1")
	// The added line, rendered in the diff, is `+++ b/phantom.txt`.
	write(t, dir, "real.txt", "alpha\nbeta\n++ b/phantom.txt\n")
	head := commitAll(t, dir, "v2")
	ctx := context.Background()

	for _, mode := range []PayloadMode{ModeDiff, ModeBlocks} {
		entries, err := BuildEntries(ctx, mode, dir, base, head)
		require.NoErrorf(t, err, "mode %s", mode)
		require.Lenf(t, entries, 1, "mode %s: exactly one changed file", mode)
		assert.Equalf(t, "real.txt", entries[0].Path, "mode %s", mode)
		assert.NotEmptyf(t, entries[0].Body,
			"mode %s: real file body must not be stolen by the spoofed +++ b/ line", mode)
		assert.Containsf(t, entries[0].Body, "++ b/phantom.txt", "mode %s", mode)
	}
}

// A path whose name contains the substring " b/" must key correctly — the old
// `diff --git` fallback truncated it at the last " b/".
func TestBuildEntries_PathContainingSpaceBSlashKeysCorrectly(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a b/c.txt", "one\n")
	base := commitAll(t, dir, "v1")
	write(t, dir, "a b/c.txt", "two\n")
	head := commitAll(t, dir, "v2")

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "a b/c.txt", entries[0].Path)
	assert.Contains(t, entries[0].Body, "+two")
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
