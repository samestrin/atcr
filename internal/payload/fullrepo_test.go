package payload

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sortedPaths returns the Path set of a []FileEntry sorted for set comparison
// against `git ls-files` (enumeration order is intentionally unspecified — chunk
// determinism is partitionByBudget's job, not the walker's). It reuses the
// package-test entryPaths helper and sorts a copy.
func sortedPaths(entries []FileEntry) []string {
	out := entryPaths(entries)
	sort.Strings(out)
	return out
}

func lsFiles(t *testing.T, dir string) []string {
	t.Helper()
	raw := gitCmd(t, dir, "ls-files")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := strings.Split(strings.TrimSpace(raw), "\n")
	sort.Strings(out)
	return out
}

func findEntry(entries []FileEntry, path string) (FileEntry, bool) {
	for _, e := range entries {
		if e.Path == path {
			return e, true
		}
	}
	return FileEntry{}, false
}

// AC 01-02 Happy Path 1: all tracked, non-ignored files are enumerated as
// []FileEntry matching `git ls-files` exactly (repo-root-relative, slash-form).
func TestEnumerateRepoFiles_AllTrackedNonIgnored(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "b.go", "package b\n")
	write(t, dir, "internal/c.go", "package c\n")
	write(t, dir, "docs/readme.md", "# hi\n")
	write(t, dir, "Makefile", "all:\n")
	commitAll(t, dir, "init")

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false)
	require.NoError(t, err)
	assert.Equal(t, lsFiles(t, dir), sortedPaths(entries), "must match git ls-files exactly")

	// Body + Size are captured from the file content.
	a, ok := findEntry(entries, "a.go")
	require.True(t, ok)
	assert.Equal(t, "package a\n", a.Body)
	assert.Equal(t, int64(len("package a\n")), a.Size)
}

// AC 01-02 Happy Path 2: a tracked file matched by a repo-root .gitignore pattern
// is excluded from the result (tracked files remaining despite a later ignore rule
// is a realistic `git ls-files` scenario).
func TestEnumerateRepoFiles_GitignoreExcluded(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "keep.go", "package keep\n")
	write(t, dir, "vendor/lib.go", "package vendor\n")
	commitAll(t, dir, "track files") // vendor/lib.go is tracked BEFORE the rule
	write(t, dir, ".gitignore", "vendor/\n")
	commitAll(t, dir, "add ignore") // now tracked AND ignore-matched (the realistic case)

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false)
	require.NoError(t, err)
	_, present := findEntry(entries, "vendor/lib.go")
	assert.False(t, present, "vendor/lib.go must be ignore-filtered out")
	_, keep := findEntry(entries, "keep.go")
	assert.True(t, keep, "non-ignored files must remain")
}

// AC 01-02 Happy Path 3: a tracked file matched by .atcrignore is excluded.
func TestEnumerateRepoFiles_AtcrignoreExcluded(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "keep.go", "package keep\n")
	write(t, dir, "generated/schema.go", "package generated\n")
	commitAll(t, dir, "track files") // tracked BEFORE the rule
	write(t, dir, ".atcrignore", "generated/\n")
	commitAll(t, dir, "add ignore")

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false)
	require.NoError(t, err)
	_, present := findEntry(entries, "generated/schema.go")
	assert.False(t, present, "generated/schema.go must be .atcrignore-filtered out")
}

// AC 01-02 Edge Case 1: a repo with a commit but zero tracked files returns an
// empty (non-nil-with-error) slice so the caller surfaces "no reviewable content".
func TestEnumerateRepoFiles_ZeroTracked(t *testing.T) {
	dir := initRepo(t)
	gitCmd(t, dir, "commit", "-q", "--allow-empty", "-m", "empty")

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false)
	require.NoError(t, err, "zero tracked files is not an error")
	assert.Empty(t, entries)
}

// AC 01-02 Edge Case 2 / Error Scenario 1: a non-git-repo root makes
// BuildFileIndex return nil; the walker must return a clear error, not a panic.
func TestEnumerateRepoFiles_NonRepoErrors(t *testing.T) {
	dir := t.TempDir() // NOT a git repo
	var entries []FileEntry
	var err error
	require.NotPanics(t, func() {
		entries, err = enumerateRepoFiles(context.Background(), dir, log.Discard(), false)
	})
	require.Error(t, err)
	assert.Nil(t, entries)
	assert.Contains(t, err.Error(), "could not enumerate tracked files")
}

// Parity with diff-mode's --no-ignore: noIgnore=true bypasses the ignore filter,
// so a .gitignore-matched tracked file IS included (else the manifest's recorded
// NoIgnore would be a provenance lie while files were silently filtered).
func TestEnumerateRepoFiles_NoIgnoreBypassesFilter(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "keep.go", "package keep\n")
	write(t, dir, "vendor/lib.go", "package vendor\n")
	commitAll(t, dir, "track files") // tracked BEFORE the rule, so git ls-files still lists it
	write(t, dir, ".gitignore", "vendor/\n")
	commitAll(t, dir, "add ignore")

	// Default (noIgnore=false) filters vendor/lib.go; noIgnore=true keeps it.
	filtered, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false)
	require.NoError(t, err)
	_, present := findEntry(filtered, "vendor/lib.go")
	require.False(t, present, "baseline default must filter the ignored file")

	all, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), true)
	require.NoError(t, err)
	_, present = findEntry(all, "vendor/lib.go")
	assert.True(t, present, "--no-ignore must include the .gitignore-matched file")
}

// AC 01-02 Edge Case 3: a tracked binary (non-UTF8) file is included with its raw
// byte size recorded; the walker must not crash or corrupt output.
func TestEnumerateRepoFiles_BinaryFile(t *testing.T) {
	dir := initRepo(t)
	blob := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0x00, 0x99}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blob.bin"), blob, 0o644))
	write(t, dir, "a.go", "package a\n")
	commitAll(t, dir, "init")

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false)
	require.NoError(t, err)
	e, ok := findEntry(entries, "blob.bin")
	require.True(t, ok, "binary file must be included")
	assert.Equal(t, int64(len(blob)), e.Size, "raw byte size recorded")
}

// AC 01-02 Edge Case 4: a tracked symlink captures its LITERAL target string as
// Body (git-object semantics) and is never resolved/followed, so a link pointing
// outside root cannot cause a read escape. Must not panic on the non-regular entry.
func TestEnumerateRepoFiles_SymlinkLiteralTarget(t *testing.T) {
	dir := initRepo(t)
	target := "../outside/secret.txt"
	if err := os.Symlink(target, filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}
	write(t, dir, "a.go", "package a\n")
	commitAll(t, dir, "init")

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false)
	require.NoError(t, err)
	e, ok := findEntry(entries, "link.txt")
	require.True(t, ok, "tracked symlink must be included")
	assert.Equal(t, target, e.Body, "Body is the literal link target, never the resolved file")
	assert.Equal(t, int64(len(target)), e.Size)
}

// AC 01-02 Edge Case 5: untracked-but-not-ignored working-tree files are absent —
// the candidate set comes from `git ls-files` only, verified against ls-files output.
func TestEnumerateRepoFiles_UntrackedExcluded(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	commitAll(t, dir, "init")
	// A scratch file that is neither added nor ignored.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("scratch\n"), 0o644))

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false)
	require.NoError(t, err)
	assert.Equal(t, lsFiles(t, dir), sortedPaths(entries), "result set must equal git ls-files (no untracked files)")
	_, present := findEntry(entries, "notes.txt")
	assert.False(t, present, "untracked notes.txt must be absent")
}

// AC 01-02 Security Considerations: a read must stay rooted at root. If an
// intermediate working-tree directory (tracked as a real dir at commit time) is
// later replaced by a symlink pointing OUTSIDE root, reading a file "under" it must
// be refused, not followed — otherwise a full-repo scan could exfiltrate arbitrary
// files. Mirrors the rejectDiffSymlinkEscape defense-in-depth pattern.
func TestEnumerateRepoFiles_RejectsIntermediateSymlinkEscape(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "internal/foo.go", "package internal\n")
	commitAll(t, dir, "init")

	// Replace the tracked `internal` directory with a symlink to an outside dir
	// that also contains foo.go with sensitive content. `git ls-files` still reports
	// internal/foo.go (index unchanged), but the working-tree read now escapes root.
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "foo.go"), []byte("SECRET-OUTSIDE\n"), 0o644))
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "internal")))
	if err := os.Symlink(outside, filepath.Join(dir, "internal")); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false)
	require.Error(t, err, "a read that escapes root via a symlink must be refused")
	assert.Contains(t, err.Error(), "outside the repository root")
	// The sensitive outside content must never appear in the (aborted) result.
	for _, e := range entries {
		assert.NotContains(t, e.Body, "SECRET-OUTSIDE")
	}
}

// AC 01-02 Error Scenario 2: a tracked file that fails to read mid-walk (removed
// from the working tree after enumeration) surfaces a wrapped error naming the path.
func TestEnumerateRepoFiles_ReadFailureMidWalk(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "gone.go", "package gone\n")
	write(t, dir, "a.go", "package a\n")
	commitAll(t, dir, "init")
	// Remove from the working tree but leave it in the index, so `git ls-files`
	// still reports it while the read fails.
	require.NoError(t, os.Remove(filepath.Join(dir, "gone.go")))

	_, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading tracked file")
	assert.Contains(t, err.Error(), "gone.go")
}

// --- AC 01-03: byte-budget chunk partitioning -------------------------------

// allChunkPaths flattens a [][]FileEntry into a sorted path slice and the count of
// each path, for zero-omission / no-duplication verification.
func allChunkPaths(chunks [][]FileEntry) ([]string, map[string]int) {
	counts := map[string]int{}
	var all []string
	for _, c := range chunks {
		for _, e := range c {
			counts[e.Path]++
			all = append(all, e.Path)
		}
	}
	sort.Strings(all)
	return all, counts
}

func mkEntries(spec map[string]int64) []FileEntry {
	out := make([]FileEntry, 0, len(spec))
	for p, sz := range spec {
		out = append(out, FileEntry{Path: p, Size: sz, Body: strings.Repeat("x", int(max64(sz, 0)))})
	}
	return out
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// AC 01-03 Happy Path 1: a set whose total size is below one chunk's budget
// returns exactly one chunk with every entry.
func TestPartitionByBudget_SmallFitsOneChunk(t *testing.T) {
	entries := mkEntries(map[string]int64{"a.go": 30, "b.go": 30, "c.go": 20})
	chunks, err := partitionByBudget(entries, 100)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	assert.Len(t, chunks[0], 3)
}

// AC 01-03 Happy Path 2: a set ~3x over budget splits into 3+ chunks and the union
// of chunk paths equals the input set exactly (each path exactly once).
func TestPartitionByBudget_LargeSplitsZeroOmissions(t *testing.T) {
	spec := map[string]int64{}
	want := []string{}
	for i := 0; i < 9; i++ {
		p := "f" + string(rune('0'+i)) + ".go"
		spec[p] = 40
		want = append(want, p)
	}
	sort.Strings(want)
	entries := mkEntries(spec)

	chunks, err := partitionByBudget(entries, 100)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(chunks), 3, "should split into 3+ chunks")

	all, counts := allChunkPaths(chunks)
	assert.Equal(t, want, all, "union of chunks must equal the full input set")
	for p, n := range counts {
		assert.Equal(t, 1, n, "path %q must appear in exactly one chunk", p)
	}
}

// AC 01-03 Happy Path 3: a single file larger than the budget gets its own chunk,
// never split, never dropped.
func TestPartitionByBudget_OversizedFileOwnChunk(t *testing.T) {
	entries := mkEntries(map[string]int64{"huge.go": 250, "a.go": 30, "b.go": 30})
	chunks, err := partitionByBudget(entries, 100)
	require.NoError(t, err)

	all, counts := allChunkPaths(chunks)
	assert.Equal(t, []string{"a.go", "b.go", "huge.go"}, all, "no file dropped")
	assert.Equal(t, 1, counts["huge.go"])
	// huge.go must be alone in its chunk.
	for _, c := range chunks {
		for _, e := range c {
			if e.Path == "huge.go" {
				assert.Len(t, c, 1, "oversized file must be alone in its chunk")
			}
		}
	}
}

// AC 01-03 Edge Case 1: empty input returns zero chunks (not one empty chunk).
func TestPartitionByBudget_EmptyReturnsZeroChunks(t *testing.T) {
	chunks, err := partitionByBudget(nil, 100)
	require.NoError(t, err)
	assert.Empty(t, chunks)
}

// AC 01-03 Edge Case 2: identical input produces identical chunk membership and
// ordering across repeated runs (no map-iteration-order leakage).
func TestPartitionByBudget_Deterministic(t *testing.T) {
	entries := mkEntries(map[string]int64{
		"a.go": 40, "b.go": 40, "c.go": 40, "d.go": 40, "e.go": 40, "f.go": 40,
	})
	first, err := partitionByBudget(entries, 100)
	require.NoError(t, err)
	second, err := partitionByBudget(entries, 100)
	require.NoError(t, err)
	require.Equal(t, len(first), len(second))
	for i := range first {
		assert.Equal(t, entryPaths(first[i]), entryPaths(second[i]), "chunk %d must be identical across runs", i)
	}
}

// AC 01-03 Edge Case 3 / Error Scenario 1: a non-positive budget fails fast at
// entry, before any packing — never loops or emits one-chunk-per-file.
func TestPartitionByBudget_ZeroBudgetFailsFast(t *testing.T) {
	entries := mkEntries(map[string]int64{"a.go": 10, "b.go": 10})
	chunks, err := partitionByBudget(entries, 0)
	require.Error(t, err)
	assert.Nil(t, chunks)
	assert.Contains(t, err.Error(), "no effective byte budget")

	_, errNeg := partitionByBudget(entries, -5)
	require.Error(t, errNeg, "negative budget must also fail fast")
}

// AC 01-03 Security / Input Validation: a negative/corrupt FileEntry.Size is
// clamped to zero for budget accounting and the file is still included.
func TestPartitionByBudget_ClampsNegativeSize(t *testing.T) {
	entries := []FileEntry{
		{Path: "neg.go", Size: -100, Body: ""},
		{Path: "a.go", Size: 30, Body: strings.Repeat("x", 30)},
	}
	chunks, err := partitionByBudget(entries, 100)
	require.NoError(t, err)
	all, _ := allChunkPaths(chunks)
	assert.Equal(t, []string{"a.go", "neg.go"}, all, "clamped-size file still included")
}
