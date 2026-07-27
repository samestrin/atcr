package payload

import (
	"bytes"
	"log/slog"
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

// warnLogger captures Warn-and-above into buf; the Debug-and-above capture used for
// the routine cases is debugLogger (fullrepo_test.go).
func warnLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})), &buf
}

// TD-009 (Sprint 35.0 hardening): an index file past the size ceiling degrades to
// a full scan with a Warn, instead of an unbounded os.ReadFile allocation ahead of
// any parse/validate step. A file AT the ceiling is still parsed.
func TestFileHashLoad_OverCapDegradesToFullScanWithWarn(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "idx.json")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("x", 256)), 0o644))

	restore := maxFileHashIndexBytes
	maxFileHashIndexBytes = 64
	defer func() { maxFileHashIndexBytes = restore }()

	logger, buf := warnLogger()
	idx := Load(path, logger)
	require.NotNil(t, idx)
	assert.Empty(t, idx.Paths(), "an over-cap index degrades to an empty index (full scan)")
	assert.Contains(t, buf.String(), "level=WARN")
	assert.Contains(t, buf.String(), "size ceiling", "the Warn names the over-cap cause")
}

// AC 04-03 Happy Path 1: a missing index file → empty index, no error, and NO
// Warn/Error line (the routine first-run state; Debug at most).
func TestFileHashLoad_MissingIsSilentEmpty(t *testing.T) {
	logger, buf := debugLogger() // captures Debug and above
	idx := Load(filepath.Join(t.TempDir(), "absent.json"), logger)
	require.NotNil(t, idx)
	assert.Empty(t, idx.Paths())
	assert.NotContains(t, buf.String(), "level=WARN", "a missing index is routine, never Warn")
	assert.NotContains(t, buf.String(), "level=ERROR")
}

// AC 04-03 Edge Case 1: an empty (0-byte) file → empty index, Debug log, not Warn.
func TestFileHashLoad_EmptyFileDebugNotWarn(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "idx.json")
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	logger, buf := debugLogger()
	idx := Load(path, logger)
	assert.Empty(t, idx.Paths())
	assert.NotContains(t, buf.String(), "level=WARN", "an empty index logs at Debug, not Warn")

	// And with a Warn-level logger the empty case is entirely silent.
	wl, wbuf := warnLogger()
	_ = Load(path, wl)
	assert.Empty(t, wbuf.String())
}

// AC 04-03 Edge Case 2: corrupt/truncated JSON → empty index, Warn naming the path +
// error, no propagated error; a later successful run rebuilds it.
func TestFileHashLoad_CorruptJSONWarnsAndRebuilds(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "idx.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"a.go": {"hash": "sha256:ab`), 0o644)) // truncated

	logger, buf := warnLogger()
	idx := Load(path, logger)
	assert.Empty(t, idx.Paths(), "corrupt index degrades to a full scan")
	assert.Contains(t, buf.String(), "level=WARN")
	assert.Contains(t, buf.String(), path, "the Warn line must name the offending path")

	// Rebuild: a completed run writes a fresh, valid index over the corrupt one.
	idx.Record("a.go", cache.HashText("a\n"), "run-2")
	require.NoError(t, idx.Save(path))
	reloaded := Load(path, log.Discard())
	h, r, ok := reloaded.Get("a.go")
	require.True(t, ok, "the corruption did not persist — next run resumes incremental skipping")
	assert.Equal(t, cache.HashText("a\n"), h)
	assert.Equal(t, "run-2", r)
}

// AC 04-03 Edge Case 3a: valid JSON of the wrong TYPE (an array, not the expected
// path-keyed object) → treated identically to corruption (Warn, empty, no panic).
func TestFileHashLoad_WrongShapeArrayTreatedAsCorrupt(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "idx.json")
	require.NoError(t, os.WriteFile(path, []byte(`["a.go","b.go"]`), 0o644))

	logger, buf := warnLogger()
	var idx *FileHashIndex
	require.NotPanics(t, func() { idx = Load(path, logger) })
	assert.Empty(t, idx.Paths())
	assert.Contains(t, buf.String(), "level=WARN")
}

// AC 04-03 Edge Case 3b: valid JSON object but entries missing the required `hash`
// field → treated identically to corruption (Warn, empty), not silently accepted as
// zero-valued entries.
func TestFileHashLoad_MissingRequiredFieldTreatedAsCorrupt(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "idx.json")
	// Well-formed object, but the entry has no "hash" (and no run id).
	require.NoError(t, os.WriteFile(path, []byte(`{"a.go": {"last_reviewed_run_id": "run-1"}}`), 0o644))

	logger, buf := warnLogger()
	idx := Load(path, logger)
	assert.Empty(t, idx.Paths(), "an entry missing its hash is a wrong-shape index → full scan")
	assert.Contains(t, buf.String(), "level=WARN", "wrong-shape must be observably distinct (Warn), not silent")
}

// AC 04-03 Edge Case 3 (hardening, 4.8.A): a bare JSON `null` and an entry with a
// truncated/non-hex hash are both wrong-shape → Warn + empty, not silent acceptance.
func TestFileHashLoad_NullAndMalformedHashTreatedAsCorrupt(t *testing.T) {
	cases := map[string]string{
		"bare null":      `null`,
		"empty digest":   `{"a.go": {"hash": "sha256:", "last_reviewed_run_id": "r"}}`,
		"truncated hex":  `{"a.go": {"hash": "sha256:abc", "last_reviewed_run_id": "r"}}`,
		"non-hex digest": `{"a.go": {"hash": "sha256:zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", "last_reviewed_run_id": "r"}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "idx.json")
			require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
			logger, buf := warnLogger()
			idx := Load(path, logger)
			assert.Empty(t, idx.Paths(), "wrong-shape index must degrade to a full scan")
			assert.Contains(t, buf.String(), "level=WARN")
		})
	}
}

// A well-formed index with a canonical cache.HashText digest loads cleanly (guards
// against isCanonicalDigest being over-strict and rejecting valid entries).
func TestFileHashLoad_CanonicalDigestAccepted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idx.json")
	src := newFileHashIndex()
	src.Record("a.go", cache.HashText("hello\n"), "run-1")
	require.NoError(t, src.Save(path))

	logger, buf := warnLogger()
	got := Load(path, logger)
	assert.Equal(t, []string{"a.go"}, got.Paths())
	assert.Empty(t, buf.String(), "a valid index must not Warn")
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
