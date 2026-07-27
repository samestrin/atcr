package payload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/cache"
	"github.com/samestrin/atcr/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// indexPath returns the canonical .atcr/index/file-hashes.json location under root,
// exercising the same helper the CLI wiring uses.
func indexPath(root string) string { return FileHashIndexPath(root) }

// AC 04-01 Happy Path 1: a first-ever baseline run writes a fresh index with, for
// each candidate, its slash-normalized path, its cache.HashText "sha256:<hex>"
// digest, and the completed run's id.
func TestFileHashIndex_FirstRunWritesFreshIndex(t *testing.T) {
	root := t.TempDir()
	path := indexPath(root)

	idx := Load(path, log.Discard()) // missing → empty, no error
	require.NotNil(t, idx)

	files := map[string]string{
		"a.go":          "package a\n",
		"b.go":          "package b\n",
		"internal/c.go": "package c\n",
	}
	for p, body := range files {
		idx.Record(p, cache.HashText(body), "run-abc123")
	}
	require.NoError(t, idx.Save(path))

	// Reload from disk and assert every entry round-tripped exactly.
	got := Load(path, log.Discard())
	require.NotNil(t, got)
	assert.ElementsMatch(t, []string{"a.go", "b.go", "internal/c.go"}, got.Paths())
	for p, body := range files {
		hash, runID, ok := got.Get(p)
		require.Truef(t, ok, "entry for %q must exist", p)
		assert.Equal(t, cache.HashText(body), hash)
		assert.True(t, strings.HasPrefix(hash, "sha256:"), "digest must be canonical sha256:<hex>")
		assert.Equal(t, "run-abc123", runID)
	}
}

// AC 04-01 Happy Path 2: a subsequent run updates only changed/new entries; the
// unchanged (skipped) entries retain their original hash AND their original run id
// — a file is not re-stamped merely because a sibling run touched other files.
func TestFileHashIndex_SubsequentRunUpdatesOnlyChangedEntries(t *testing.T) {
	root := t.TempDir()
	path := indexPath(root)

	// run-1 records A, B, C.
	idx := Load(path, log.Discard())
	idx.Record("A.go", cache.HashText("A v1\n"), "run-1")
	idx.Record("B.go", cache.HashText("B v1\n"), "run-1")
	idx.Record("C.go", cache.HashText("C v1\n"), "run-1")
	require.NoError(t, idx.Save(path))

	// run-2 reviews only B (changed) and newly-added D; A and C are skipped
	// (unchanged) and therefore left untouched.
	idx2 := Load(path, log.Discard())
	idx2.Record("B.go", cache.HashText("B v2\n"), "run-2")
	idx2.Record("D.go", cache.HashText("D v1\n"), "run-2")
	require.NoError(t, idx2.Save(path))

	got := Load(path, log.Discard())
	// B updated to its new hash + run-2.
	h, r, ok := got.Get("B.go")
	require.True(t, ok)
	assert.Equal(t, cache.HashText("B v2\n"), h)
	assert.Equal(t, "run-2", r)
	// D added under run-2.
	h, r, ok = got.Get("D.go")
	require.True(t, ok)
	assert.Equal(t, cache.HashText("D v1\n"), h)
	assert.Equal(t, "run-2", r)
	// A and C retain their original run-1 hash and run id.
	h, r, ok = got.Get("A.go")
	require.True(t, ok)
	assert.Equal(t, cache.HashText("A v1\n"), h)
	assert.Equal(t, "run-1", r, "unchanged sibling must NOT be re-stamped with run-2")
	h, r, ok = got.Get("C.go")
	require.True(t, ok)
	assert.Equal(t, cache.HashText("C v1\n"), h)
	assert.Equal(t, "run-1", r, "unchanged sibling must NOT be re-stamped with run-2")
}

// AC 04-01 Edge Case 1: a file no longer in the tracked set is self-trimmed from the
// written index, keeping it bounded to currently-tracked files.
func TestFileHashIndex_SelfTrimsStalePaths(t *testing.T) {
	root := t.TempDir()
	path := indexPath(root)

	idx := Load(path, log.Discard())
	idx.Record("keep.go", cache.HashText("keep\n"), "run-1")
	idx.Record("old/removed.go", cache.HashText("gone\n"), "run-1")
	require.NoError(t, idx.Save(path))

	// Next run: only keep.go is still tracked. Trim to the current tracked set
	// before writing.
	idx2 := Load(path, log.Discard())
	idx2.Trim(map[string]struct{}{"keep.go": {}})
	require.NoError(t, idx2.Save(path))

	got := Load(path, log.Discard())
	assert.Equal(t, []string{"keep.go"}, got.Paths())
	_, _, ok := got.Get("old/removed.go")
	assert.False(t, ok, "stale entry must be pruned on write")
}

// A nil keep set (e.g. from a git-degraded BuildFileIndex) means "unknown tracked
// set — keep everything", never "trim all" (4.5.A LOW). An empty-but-non-nil keep is
// a genuine "nothing tracked" signal and does trim everything.
func TestFileHashIndex_TrimNilKeepPreservesAll(t *testing.T) {
	idx := newFileHashIndex()
	idx.Record("a.go", cache.HashText("a\n"), "run-1")
	idx.Record("b.go", cache.HashText("b\n"), "run-1")

	idx.Trim(nil)
	assert.ElementsMatch(t, []string{"a.go", "b.go"}, idx.Paths(), "nil keep must not wipe the index")

	idx.Trim(map[string]struct{}{})
	assert.Empty(t, idx.Paths(), "an explicit empty keep set does trim everything")
}

// AC 04-01 Edge Case 2: an interrupted run (index never Saved) leaves the on-disk
// index unmodified — the index is written last, only on completion.
func TestFileHashIndex_InterruptedRunLeavesIndexUnmodified(t *testing.T) {
	root := t.TempDir()
	path := indexPath(root)

	idx := Load(path, log.Discard())
	idx.Record("a.go", cache.HashText("v1\n"), "run-1")
	require.NoError(t, idx.Save(path))
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	// A second run mutates its in-memory index but is "interrupted" before Save.
	idx2 := Load(path, log.Discard())
	idx2.Record("a.go", cache.HashText("v2\n"), "run-2")
	idx2.Record("b.go", cache.HashText("new\n"), "run-2")
	// ... process dies here; Save is never called.

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after, "on-disk index must be untouched until a completed run Saves it")
}

// AC 04-01 Error Scenario 1: an index directory that cannot be created surfaces a
// wrapped "writing file-hash index" error (the caller logs Warn and does not fail
// the review).
func TestFileHashIndex_SaveDirCreateFailureIsWrappedError(t *testing.T) {
	root := t.TempDir()
	// Make the parent of .atcr a regular file so MkdirAll for .atcr/index cannot
	// succeed.
	blocker := filepath.Join(root, ".atcr")
	require.NoError(t, os.WriteFile(blocker, []byte("not a dir"), 0o644))
	path := filepath.Join(blocker, "index", "file-hashes.json")

	idx := Load(path, log.Discard())
	idx.Record("a.go", cache.HashText("v1\n"), "run-1")
	err := idx.Save(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing file-hash index")
}

// AC 04-01 Error Scenario 2: a repeat write (last-writer-wins, atomic temp-rename)
// leaves a fully-formed file, never a torn/interleaved one.
func TestFileHashIndex_RepeatWriteLeavesWellFormedFile(t *testing.T) {
	root := t.TempDir()
	path := indexPath(root)

	first := Load(path, log.Discard())
	first.Record("a.go", cache.HashText("a\n"), "run-1")
	require.NoError(t, first.Save(path))

	second := Load(path, log.Discard())
	second.Record("a.go", cache.HashText("a2\n"), "run-2")
	second.Record("b.go", cache.HashText("b\n"), "run-2")
	require.NoError(t, second.Save(path))

	// The file re-parses cleanly (a valid, complete index) and reflects the last write.
	got := Load(path, log.Discard())
	assert.ElementsMatch(t, []string{"a.go", "b.go"}, got.Paths())
	h, r, ok := got.Get("a.go")
	require.True(t, ok)
	assert.Equal(t, cache.HashText("a2\n"), h)
	assert.Equal(t, "run-2", r)
}

// AC 04-01 Story-Specific DoD: the write goes through atomicfs.WriteJSON (indented,
// trailing newline) — a proxy assertion that the artifact is human-diffable JSON.
func TestFileHashIndex_OnDiskFormatIsIndentedJSON(t *testing.T) {
	root := t.TempDir()
	path := indexPath(root)
	idx := Load(path, log.Discard())
	idx.Record("a.go", cache.HashText("a\n"), "run-1")
	require.NoError(t, idx.Save(path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(string(data), "\n"), "atomicfs.WriteJSON appends a trailing newline")
	assert.Contains(t, string(data), "\n  ", "two-space indented JSON")
	assert.Contains(t, string(data), "last_reviewed_run_id")
}

// Nil-receiver safety (used by AC 04-02's skip filter when --fresh disables loading):
// a nil *FileHashIndex reports every path as unrecorded/changed and never panics.
func TestFileHashIndex_NilReceiverSafe(t *testing.T) {
	var idx *FileHashIndex
	assert.NotPanics(t, func() {
		assert.False(t, idx.Unchanged("a.go", "sha256:whatever"))
		_, _, ok := idx.Get("a.go")
		assert.False(t, ok)
		assert.Empty(t, idx.Paths())
	})
}
