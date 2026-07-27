package payload

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/cache"
	"github.com/samestrin/atcr/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// indexOf builds an in-memory FileHashIndex recording every entry's content hash
// under runID, for the AC 04-02 skip-filter tests.
func indexOf(entries []FileEntry, runID string) *FileHashIndex {
	idx := newFileHashIndex()
	for _, e := range entries {
		idx.Record(e.Path, cache.HashText(e.Body), runID)
	}
	return idx
}

// AC 04-02 Scenario 1: all files unchanged → empty pre-chunking candidate list.
func TestApplyHashSkip_AllUnchangedYieldsEmpty(t *testing.T) {
	entries := []FileEntry{
		{Path: "a.go", Size: 3, Body: "aaa"},
		{Path: "b.go", Size: 3, Body: "bbb"},
		{Path: "c.go", Size: 3, Body: "ccc"},
	}
	idx := indexOf(entries, "run-1")
	got, _ := applyHashSkip(entries, idx, false)
	assert.Empty(t, got, "every unchanged file must be dropped before chunking")
}

// AC 04-02 Scenario 2: only changed files reach chunking — verified by path AND
// content, not count.
func TestApplyHashSkip_OnlyChangedSurvive(t *testing.T) {
	v1 := []FileEntry{
		{Path: "a.go", Body: "a1"},
		{Path: "b.go", Body: "b1"},
		{Path: "c.go", Body: "c1"},
		{Path: "d.go", Body: "d1"},
	}
	idx := indexOf(v1, "run-1")
	// b and d change; a and c stay byte-identical.
	v2 := []FileEntry{
		{Path: "a.go", Body: "a1"},
		{Path: "b.go", Body: "b2-CHANGED"},
		{Path: "c.go", Body: "c1"},
		{Path: "d.go", Body: "d2-CHANGED"},
	}
	got, _ := applyHashSkip(v2, idx, false)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"b.go", "d.go"}, sortedPaths(got))
	b, ok := findEntry(got, "b.go")
	require.True(t, ok)
	assert.Equal(t, "b2-CHANGED", b.Body, "the changed content, not merely a count, must reach chunking")
}

// AC 04-02 Edge Case 1: a new file with no index entry is always a candidate.
func TestApplyHashSkip_NewFileAlwaysIncluded(t *testing.T) {
	known := []FileEntry{{Path: "a.go", Body: "a1"}}
	idx := indexOf(known, "run-1")
	entries := []FileEntry{
		{Path: "a.go", Body: "a1"},          // unchanged → skipped
		{Path: "new.go", Body: "brand new"}, // no entry → included
	}
	got, _ := applyHashSkip(entries, idx, false)
	assert.Equal(t, []string{"new.go"}, sortedPaths(got))
}

// AC 04-02 Edge Case 3: a renamed file with byte-identical content has no entry
// under its new (path-keyed) name and is treated as unreviewed — included.
func TestApplyHashSkip_RenamedIdenticalContentIncluded(t *testing.T) {
	orig := []FileEntry{{Path: "old/name.go", Body: "same bytes"}}
	idx := indexOf(orig, "run-1")
	renamed := []FileEntry{{Path: "new/name.go", Body: "same bytes"}}
	got, _ := applyHashSkip(renamed, idx, false)
	assert.Equal(t, []string{"new/name.go"}, sortedPaths(got), "path-keyed index treats a rename as unreviewed")
}

// AC 04-02 Edge Case 4: the skip decision is content-based only — byte-identical
// content is skipped regardless of any size field drift.
func TestApplyHashSkip_ContentBasedOnly(t *testing.T) {
	idx := newFileHashIndex()
	idx.Record("a.go", cache.HashText("payload"), "run-1")
	// Same bytes, deliberately wrong Size field: still skipped (hash matches).
	entries := []FileEntry{{Path: "a.go", Size: 99999, Body: "payload"}}
	got, _ := applyHashSkip(entries, idx, false)
	assert.Empty(t, got, "skip is SHA-256 content-based, never size/mtime-based")
}

// AC 04-04 preview / AC 04-02 nil-safety: fresh=true or a nil index bypasses the
// skip entirely (every file included).
func TestApplyHashSkip_FreshOrNilIndexIncludesAll(t *testing.T) {
	entries := []FileEntry{{Path: "a.go", Body: "a1"}, {Path: "b.go", Body: "b1"}}
	idx := indexOf(entries, "run-1") // would skip both

	fresh, _ := applyHashSkip(entries, idx, true)
	assert.Equal(t, []string{"a.go", "b.go"}, sortedPaths(fresh), "--fresh bypasses the skip")

	nilIdx, _ := applyHashSkip(entries, nil, false)
	assert.Equal(t, []string{"a.go", "b.go"}, sortedPaths(nilIdx), "a nil index treats every file as unreviewed")
}

// AC 04-02 (INTEGRATION): two --all passes through BuildRepoEntries — write the
// index after pass 1, reload it, and confirm pass 2 skips unchanged files and
// surfaces only the one file whose content changed.
func TestBuildRepoEntries_SkipsUnchangedAcrossPasses(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "b.go", "package b\n")
	write(t, dir, "internal/c.go", "package c\n")
	commitAll(t, dir, "init")
	ctx := context.Background()
	// Store the index outside the scanned repo so the fixture's `git add -A` cannot
	// sweep it into the tracked set (in production .atcr/ is gitignored). The skip
	// logic under test is independent of where the index file lives.
	idxPath := filepath.Join(t.TempDir(), "file-hashes.json")

	// Pass 1: full scan (empty index), then record + save.
	pass1, err := BuildRepoEntries(ctx, dir, log.Discard(), false, "", Load(idxPath, log.Discard()), false)
	require.NoError(t, err)
	require.Len(t, pass1, 3)
	idx := Load(idxPath, log.Discard())
	for _, e := range pass1 {
		idx.Record(e.Path, cache.HashText(e.Body), "run-1")
	}
	require.NoError(t, idx.Save(idxPath))

	// Pass 2, nothing changed → zero candidates.
	pass2, err := BuildRepoEntries(ctx, dir, log.Discard(), false, "", Load(idxPath, log.Discard()), false)
	require.NoError(t, err)
	assert.Empty(t, pass2, "an unchanged repo yields no candidates on the second pass")

	// Change one file → exactly that file is a candidate on pass 3.
	write(t, dir, "b.go", "package b // edited\n")
	commitAll(t, dir, "edit b")
	pass3, err := BuildRepoEntries(ctx, dir, log.Discard(), false, "", Load(idxPath, log.Discard()), false)
	require.NoError(t, err)
	assert.Equal(t, []string{"b.go"}, sortedPaths(pass3))
}

// AC 04-02 Edge Case 2: ignore-filtering runs BEFORE the hash-skip. An ignored file
// that would also hash-match is excluded once by the ignore filter and never reaches
// the skip step — no double-processing, deterministic outcome.
func TestBuildRepoEntries_IgnoreBeforeHashSkip(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "keep.go", "package keep\n")
	write(t, dir, "gen/out.go", "package gen\n")
	commitAll(t, dir, "track") // both tracked before the ignore rule
	write(t, dir, ".gitignore", "gen/\n")
	commitAll(t, dir, "ignore gen")
	ctx := context.Background()

	// Record hashes for BOTH files (as if a prior run without the ignore rule saw them).
	idx := newFileHashIndex()
	idx.Record("keep.go", cache.HashText("package keep\n"), "run-1")
	idx.Record("gen/out.go", cache.HashText("package gen\n"), "run-1")

	got, err := BuildRepoEntries(ctx, dir, log.Discard(), false, "", idx, false)
	require.NoError(t, err)
	// keep.go is ignore-surviving AND hash-matched → dropped by the hash-skip.
	_, keepPresent := findEntry(got, "keep.go")
	assert.False(t, keepPresent, "hash-matched non-ignored file must be dropped by the skip")
	// gen/out.go is ignore-excluded → never evaluated by the skip (ordering: ignore first).
	_, genPresent := findEntry(got, "gen/out.go")
	assert.False(t, genPresent, "ignored file must be excluded by the ignore filter, not the hash-skip")
	// The tracked, non-ignored, un-recorded .gitignore itself remains a candidate,
	// proving the skip only drops recorded hash-matches (not everything).
	_, giPresent := findEntry(got, ".gitignore")
	assert.True(t, giPresent, ".gitignore is tracked, non-ignored, and unrecorded → still a candidate")
}

// debugLogger returns a slog.Logger capturing Debug-and-above output into buf, for
// the AC 03-03 graceful-degradation log-line assertions.
func debugLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

// sortedPaths returns the Path set of a []FileEntry sorted for set comparison
// against `git ls-files` (enumeration order is intentionally unspecified — chunk
// determinism is PartitionByBudget's job, not the walker's). It reuses the
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

	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
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

	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
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

	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
	require.NoError(t, err)
	_, present := findEntry(entries, "generated/schema.go")
	assert.False(t, present, "generated/schema.go must be .atcrignore-filtered out")
}

// AC 01-02 Edge Case 1: a repo with a commit but zero tracked files returns an
// empty (non-nil-with-error) slice so the caller surfaces "no reviewable content".
func TestEnumerateRepoFiles_ZeroTracked(t *testing.T) {
	dir := initRepo(t)
	gitCmd(t, dir, "commit", "-q", "--allow-empty", "-m", "empty")

	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
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
		entries, _, err = enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
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
	filtered, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
	require.NoError(t, err)
	_, present := findEntry(filtered, "vendor/lib.go")
	require.False(t, present, "baseline default must filter the ignored file")

	all, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), true, "")
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

	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
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

	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
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

	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
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

	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
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

	_, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading tracked file")
	assert.Contains(t, err.Error(), "gone.go")
}

// TD-002 (Sprint 35.0 hardening): an over-cap tracked file is skipped and
// Warn-flagged instead of slurped whole into memory (OOM vector) — the walk
// survives and still returns the under-cap files.
func TestEnumerateRepoFiles_SkipsOverCapFile(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "small.go", "package small\n")
	write(t, dir, "big.bin", strings.Repeat("x", 64))
	commitAll(t, dir, "seed")

	restore := maxTrackedFileReadBytes
	maxTrackedFileReadBytes = 16
	defer func() { maxTrackedFileReadBytes = restore }()

	logger, buf := debugLogger()
	entries, _, err := enumerateRepoFiles(context.Background(), dir, logger, false, "")
	require.NoError(t, err, "an over-cap file must be skipped, not abort the scan")
	assert.Equal(t, []string{"small.go"}, sortedPaths(entries), "over-cap file omitted, under-cap kept")
	assert.Contains(t, buf.String(), "over-cap", "the skip is Warn-flagged, never silent")
	assert.Contains(t, buf.String(), "big.bin", "the flag names the skipped path")
}

// TD-002: readTrackedFile rejects an over-cap file with the sentinel so the
// walker can distinguish "skip me" from a genuine read failure (which aborts).
// A file AT the cap is still accepted (boundary).
func TestReadTrackedFile_OverCapSentinel(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "big.bin", strings.Repeat("x", 64))
	write(t, dir, "atcap.bin", strings.Repeat("y", 16))
	commitAll(t, dir, "seed")

	restore := maxTrackedFileReadBytes
	maxTrackedFileReadBytes = 16
	defer func() { maxTrackedFileReadBytes = restore }()

	_, err := readTrackedFile(dir, "big.bin")
	require.ErrorIs(t, err, errTrackedFileTooLarge, "over-cap read must return the sentinel")

	e, err := readTrackedFile(dir, "atcap.bin")
	require.NoError(t, err, "a file at exactly the cap is accepted")
	assert.Equal(t, strings.Repeat("y", 16), e.Body)
}

// TD-005 (Sprint 35.0 hardening): a baseline scan whose tracked files total
// more than the in-memory assembly cap fails loudly during enumeration — before
// any payload work — instead of slurping an unbounded repository into memory.
// The whole-repo counterpart of DefaultMaxDiffBytes: a per-file over-cap skip
// does NOT count toward the total (it never entered memory), and many small
// files trip the total cap even though each is individually under the per-file
// ceiling.
func TestEnumerateRepoFiles_TotalBytesCapFailsLoud(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.bin", strings.Repeat("a", 32))
	write(t, dir, "b.bin", strings.Repeat("b", 32))
	write(t, dir, "c.bin", strings.Repeat("c", 32))
	commitAll(t, dir, "seed")

	restore := DefaultMaxRepoBytes
	DefaultMaxRepoBytes = 64 // three 32-byte files total 96 > 64
	defer func() { DefaultMaxRepoBytes = restore }()

	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
	require.Error(t, err, "a repo past the total in-memory cap must fail loudly, not assemble")
	assert.Nil(t, entries, "no partial payload work on a capped scan")
	assert.Contains(t, err.Error(), "64", "the error names the cap")
}

// TD-005: a per-file over-cap skip (maxTrackedFileReadBytes) is NOT counted
// toward the total in-memory cap — it never entered memory — so a repo with
// one huge skipped file and a small readable remainder still scans.
func TestEnumerateRepoFiles_PerFileSkipDoesNotCountTowardTotal(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "small.go", "package small\n")
	write(t, dir, "big.bin", strings.Repeat("x", 256))
	commitAll(t, dir, "seed")

	restoreFile := maxTrackedFileReadBytes
	maxTrackedFileReadBytes = 32
	defer func() { maxTrackedFileReadBytes = restoreFile }()
	restoreTotal := DefaultMaxRepoBytes
	DefaultMaxRepoBytes = 64 // small.go alone (~14 bytes) fits; big.bin must not count
	defer func() { DefaultMaxRepoBytes = restoreTotal }()

	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
	require.NoError(t, err, "a skipped per-file-over-cap file must not trip the total cap")
	assert.Equal(t, []string{"small.go"}, sortedPaths(entries))
}

// TD-003 (Sprint 35.0 hardening): a cancelled context interrupts the per-file
// read loop instead of enumerating the whole repository first. The custom ctx
// reports Err()=Canceled but never closes Done(), so the git ls-files inside
// BuildFileIndex still succeeds and the loop's own ctx check is what fires.
type cancelErrContext struct{ context.Context }

func (cancelErrContext) Err() error                  { return context.Canceled }
func (cancelErrContext) Done() <-chan struct{}       { return nil }
func (cancelErrContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func TestEnumerateRepoFiles_ContextCancelInterruptsReadLoop(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "b.go", "package b\n")
	commitAll(t, dir, "seed")

	ctx := cancelErrContext{context.Background()}
	_, _, err := enumerateRepoFiles(ctx, dir, log.Discard(), false, "")
	require.ErrorIs(t, err, context.Canceled, "a cancelled ctx must interrupt the read loop")
}

// TD-007 (Sprint 35.0 hardening): a --dir scope matching zero tracked files while
// the repo HAS tracked files elsewhere yields a scope-specific diagnostic, not the
// generic "no reviewable content" an entirely empty repository produces.
func TestEnumerateRepoFiles_ScopeZeroMatchDiagnostic(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	commitAll(t, dir, "seed")

	_, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `--dir "does-not-exist" matched no tracked files`)

	// Whole-repo scopes keep the old contract: empty set, no error, so the
	// caller's generic no-reviewable-content guard fires.
	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, ".")
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
}

// --- AC 02-02: path-segment scope filter (--dir) -----------------------------

// TestFilterByScope_WholeRepoWhenEmptyOrDot covers AC 02-01 Edge Case 2 / AC 02-02:
// an empty scope (--all) and "." (--dir . degenerate) both mean the whole repo —
// the filter returns the input unchanged.
func TestFilterByScope_WholeRepoWhenEmptyOrDot(t *testing.T) {
	paths := []string{"a.go", "internal/x.go", "internal/fanout/y.go"}
	assert.Equal(t, paths, filterByScope(paths, ""))
	assert.Equal(t, paths, filterByScope(paths, "."))
}

// TestFilterByScope_NestedOnly covers AC 02-02 Happy Path 1: only files nested
// under the scope survive.
func TestFilterByScope_NestedOnly(t *testing.T) {
	paths := []string{
		"internal/fanout/review.go",
		"internal/fanout/review_test.go",
		"internal/reconcile/reconcile.go",
		"main.go",
	}
	got := filterByScope(paths, "internal/fanout")
	assert.ElementsMatch(t, []string{"internal/fanout/review.go", "internal/fanout/review_test.go"}, got)
}

// TestFilterByScope_SiblingPrefixCollision covers AC 02-02 Edge Case 1: a
// full-path-segment match, NOT a raw strings.HasPrefix — internal/fan must never
// pull in internal/fanout.
func TestFilterByScope_SiblingPrefixCollision(t *testing.T) {
	paths := []string{"internal/fan/a.go", "internal/fanout/b.go"}
	got := filterByScope(paths, "internal/fan")
	assert.Equal(t, []string{"internal/fan/a.go"}, got)
}

// TestFilterByScope_ZeroMatch covers AC 02-02 Edge Cases 2-3: a scope with no
// matching tracked files yields an empty set (the caller's empty-payload guard
// then fires).
func TestFilterByScope_ZeroMatch(t *testing.T) {
	paths := []string{"main.go", "internal/x.go"}
	assert.Empty(t, filterByScope(paths, "cli"))
}

// TestFilterByScope_DefensiveNormalization is the 3.5.A LOW regression: a scope
// carrying a trailing slash or backslash separators (not the AC-guaranteed clean
// slash form) is normalized before matching rather than silently yielding an empty
// set. Guards the defense-in-depth normalization added in task 3.6.
func TestFilterByScope_DefensiveNormalization(t *testing.T) {
	paths := []string{"internal/fanout/a.go", "internal/reconcile/b.go"}
	want := []string{"internal/fanout/a.go"}
	assert.Equal(t, want, filterByScope(paths, "internal/fanout/"), "trailing slash must be trimmed")
	assert.Equal(t, want, filterByScope(paths, `internal\fanout`), "backslashes must normalize to forward slashes")
	// "./" degenerate forms still mean the whole repo after normalization.
	assert.Equal(t, paths, filterByScope(paths, "./"))
}

// TestEnumerateRepoFiles_ScopedNestedOnly covers AC 02-02 Happy Path 1 end-to-end
// against a real temp git repo: --dir internal/fanout enumerates only the two
// in-scope files, excluding internal/reconcile and the repo-root main.go.
func TestEnumerateRepoFiles_ScopedNestedOnly(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "internal/fanout/review.go", "package fanout\n")
	write(t, dir, "internal/fanout/engine.go", "package fanout\n")
	write(t, dir, "internal/reconcile/reconcile.go", "package reconcile\n")
	write(t, dir, "main.go", "package main\n")
	commitAll(t, dir, "seed")
	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "internal/fanout")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"internal/fanout/review.go", "internal/fanout/engine.go"}, sortedPaths(entries))
}

// TestEnumerateRepoFiles_ScopeIgnoreParity covers AC 02-02 Happy Path 2: ignore
// rules apply identically inside the scope — a .gitignore-matched (force-added)
// file under the scope is still excluded, via the same ignoreMatcher as --all.
func TestEnumerateRepoFiles_ScopeIgnoreParity(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "internal/fanout/generated.pb.go\n")
	write(t, dir, "internal/fanout/review.go", "package fanout\n")
	write(t, dir, "internal/fanout/generated.pb.go", "package fanout\n")
	gitCmd(t, dir, "add", "-f", "internal/fanout/generated.pb.go")
	commitAll(t, dir, "seed")
	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "internal/fanout")
	require.NoError(t, err)
	assert.Equal(t, []string{"internal/fanout/review.go"}, sortedPaths(entries))
}

// TestEnumerateRepoFiles_ScopeSiblingPrefix covers AC 02-02 Edge Case 1 end-to-end:
// --dir internal/fan must not pull in internal/fanout.
func TestEnumerateRepoFiles_ScopeSiblingPrefix(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "internal/fan/a.go", "package fan\n")
	write(t, dir, "internal/fanout/b.go", "package fanout\n")
	commitAll(t, dir, "seed")
	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "internal/fan")
	require.NoError(t, err)
	assert.Equal(t, []string{"internal/fan/a.go"}, sortedPaths(entries))
}

// TestEnumerateRepoFiles_ScopeUntrackedExcluded covers AC 02-02 Edge Case 3: an
// untracked file inside the scope is never selected — the filter only ever draws
// from git ls-files' tracked set, not the filesystem.
func TestEnumerateRepoFiles_ScopeUntrackedExcluded(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "internal/fanout/tracked.go", "package fanout\n")
	commitAll(t, dir, "seed")
	write(t, dir, "internal/fanout/untracked.go", "package fanout\n") // never committed
	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "internal/fanout")
	require.NoError(t, err)
	assert.Equal(t, []string{"internal/fanout/tracked.go"}, sortedPaths(entries))
}

// TestEnumerateRepoFiles_ScopeEmptyIsWholeRepo covers the --all parity path: an
// empty scope enumerates every tracked file, identical to git ls-files.
func TestEnumerateRepoFiles_ScopeEmptyIsWholeRepo(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "internal/b.go", "package b\n")
	commitAll(t, dir, "seed")
	entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
	require.NoError(t, err)
	assert.ElementsMatch(t, lsFiles(t, dir), sortedPaths(entries))
}

// --- AC 03-01: .gitignore parity in baseline and directory scans -------------

// TestBaseline_GitignoreParity_AllAndDir covers AC 03-01 Happy Paths 1-3: a
// .gitignore-matched (force-added, tracked) file is excluded from both --all and a
// --dir scan targeting its subtree, matched repo-root-relative via the same
// ignoreMatcher diff-mode uses; non-matched files pass through. Asserts EXACT set
// equality between expected-kept and actual-kept paths.
func TestBaseline_GitignoreParity_AllAndDir(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "vendor/\n")
	write(t, dir, "main.go", "package main\n")
	write(t, dir, "vendor/lib.go", "package vendor\n")
	gitCmd(t, dir, "add", "-f", "vendor/lib.go") // force-add the ignored file so it is tracked
	commitAll(t, dir, "seed")

	// --all: vendor/lib.go excluded, main.go and .gitignore kept.
	all, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
	require.NoError(t, err)
	assert.Equal(t, []string{".gitignore", "main.go"}, sortedPaths(all), "--all must exclude the .gitignore-matched file")

	// --dir vendor: the only in-scope file is .gitignore-matched, so the scoped set is empty.
	scoped, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "vendor")
	require.NoError(t, err)
	assert.Empty(t, sortedPaths(scoped), "--dir vendor must exclude the .gitignore-matched file identically to --all")
}

// TestBaseline_GitignoreRootAnchored_UnderDir covers AC 03-01 Edge Case 1: a
// root-anchored pattern (/build/) matches the repo-root-relative path (build/x.go)
// under a --dir build scan — proving the matcher is constructed at the repo root
// and matched repo-root-relative, never relative to the --dir subtree. Binary
// match/no-match: the in-scope root-anchored match excludes; an unrelated file stays.
func TestBaseline_GitignoreRootAnchored_UnderDir(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "/build/\n")
	write(t, dir, "build/output.go", "package build\n")
	write(t, dir, "build/keep_test.go", "package build\n")
	gitCmd(t, dir, "add", "-f", "build/output.go", "build/keep_test.go")
	commitAll(t, dir, "seed")

	scoped, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "build")
	require.NoError(t, err)
	assert.Empty(t, sortedPaths(scoped), "a root-anchored /build/ pattern must exclude build/* under --dir build (matched repo-root-relative)")
}

// TestBaseline_EmptyGitignore_NoOp covers AC 03-01 Edge Case 3: an empty .gitignore
// excludes nothing (identical to diff-based reviews with an empty .gitignore).
func TestBaseline_EmptyGitignore_NoOp(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "")
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "internal/b.go", "package b\n")
	commitAll(t, dir, "seed")

	all, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
	require.NoError(t, err)
	assert.Equal(t, lsFiles(t, dir), sortedPaths(all), "an empty .gitignore excludes nothing")
}

// --- AC 03-02: .atcrignore parity and additive-only negation -----------------

// TestBaseline_AtcrignoreParity_AllAndDir covers AC 03-02 Happy Path 1-2: an
// .atcrignore-matched tracked file is excluded from --all and from a --dir scan.
func TestBaseline_AtcrignoreParity_AllAndDir(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".atcrignore", "go.sum\ntools/gen.go\n")
	write(t, dir, "main.go", "package main\n")
	write(t, dir, "go.sum", "checksums\n")
	write(t, dir, "tools/gen.go", "package tools\n")
	write(t, dir, "tools/keep.go", "package tools\n")
	commitAll(t, dir, "seed")

	all, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
	require.NoError(t, err)
	assert.Equal(t, []string{".atcrignore", "main.go", "tools/keep.go"}, sortedPaths(all), "--all excludes .atcrignore matches")

	scoped, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "tools")
	require.NoError(t, err)
	assert.Equal(t, []string{"tools/keep.go"}, sortedPaths(scoped), "--dir tools excludes tools/gen.go identically")
}

// TestBaseline_CombinedGitAtcrignore_Union covers AC 03-02 Happy Path 3: the
// combined .gitignore + .atcrignore exclusion set is a union (OR); a file matched
// by neither remains.
func TestBaseline_CombinedGitAtcrignore_Union(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "vendor/\n")
	write(t, dir, ".atcrignore", "go.sum\n")
	write(t, dir, "main.go", "package main\n")
	write(t, dir, "vendor/lib.go", "package vendor\n")
	write(t, dir, "go.sum", "checksums\n")
	gitCmd(t, dir, "add", "-f", "vendor/lib.go")
	commitAll(t, dir, "seed")

	all, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
	require.NoError(t, err)
	assert.Equal(t, []string{".atcrignore", ".gitignore", "main.go"}, sortedPaths(all),
		"both vendor/lib.go (.gitignore) and go.sum (.atcrignore) excluded; main.go kept")
}

// TestBaseline_AtcrignoreNegation_Inert covers AC 03-02 Edge Cases 1-2: an
// .atcrignore "!" negation line never re-includes a file — neither one excluded by
// .gitignore (EC1) nor one excluded by .atcrignore itself (EC2). loadAtcrignore
// strips negation lines (additive-only contract).
func TestBaseline_AtcrignoreNegation_Inert(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "vendor/\n")
	// !vendor/lib.go tries to re-include a .gitignore exclusion (EC1); secrets.env
	// + !secrets.env is a self-contradictory .atcrignore negation (EC2).
	write(t, dir, ".atcrignore", "!vendor/lib.go\nsecrets.env\n!secrets.env\n")
	write(t, dir, "main.go", "package main\n")
	write(t, dir, "vendor/lib.go", "package vendor\n")
	write(t, dir, "secrets.env", "TOKEN=x\n")
	gitCmd(t, dir, "add", "-f", "vendor/lib.go", "secrets.env")
	commitAll(t, dir, "seed")

	all, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
	require.NoError(t, err)
	kept := sortedPaths(all)
	assert.NotContains(t, kept, "vendor/lib.go", "an .atcrignore !negation cannot re-include a .gitignore exclusion")
	assert.NotContains(t, kept, "secrets.env", "an .atcrignore !negation is inert against its own exclusion too")
}

// TestBaseline_AtcrignoreEscapedLiteral covers AC 03-02 Edge Case 3: a
// backslash-escaped literal `\!important.txt` is kept as a real pattern (not a
// negation) and matches the literal-`!`-prefixed filename.
func TestBaseline_AtcrignoreEscapedLiteral(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".atcrignore", "\\!important.txt\n")
	write(t, dir, "main.go", "package main\n")
	write(t, dir, "!important.txt", "secret\n")
	gitCmd(t, dir, "add", "-f", "!important.txt")
	commitAll(t, dir, "seed")

	all, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
	require.NoError(t, err)
	assert.NotContains(t, sortedPaths(all), "!important.txt", "escaped literal !-pattern must still exclude the file")
}

// TestBaseline_EmptyAtcrignore_NoOp covers AC 03-02 Edge Case 4: an empty
// .atcrignore excludes nothing.
func TestBaseline_EmptyAtcrignore_NoOp(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".atcrignore", "")
	write(t, dir, "a.go", "package a\n")
	commitAll(t, dir, "seed")

	all, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
	require.NoError(t, err)
	assert.Equal(t, lsFiles(t, dir), sortedPaths(all), "an empty .atcrignore excludes nothing")
}

// TestBaseline_GitignoreNegation_CannotReincludeAtcrignore covers AC 03-02 Edge
// Case 5: because the two sources are separate matchers OR'd together, a
// .gitignore "!" negation can never un-exclude an .atcrignore-only match.
func TestBaseline_GitignoreNegation_CannotReincludeAtcrignore(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".atcrignore", "vendor/keep.go\n")
	write(t, dir, ".gitignore", "vendor/\n!vendor/keep.go\n") // .gitignore's own negation re-includes it for git, but...
	write(t, dir, "main.go", "package main\n")
	write(t, dir, "vendor/keep.go", "package vendor\n")
	gitCmd(t, dir, "add", "-f", "vendor/keep.go")
	commitAll(t, dir, "seed")

	all, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), false, "")
	require.NoError(t, err)
	assert.NotContains(t, sortedPaths(all), "vendor/keep.go",
		"a .gitignore negation cannot re-include an .atcrignore-only exclusion (separate OR'd matchers)")
}

// --- AC 03-03: graceful degradation on missing/unreadable ignore files -------

// TestBaseline_Degradation_BothAbsent covers AC 03-03 Happy Path 1: with no
// .gitignore and no .atcrignore the scan succeeds, every tracked file is kept, and
// NO Debug "ignore filtering skips it" line is emitted (a missing file is a silent
// no-op — the observable distinction from the unreadable cases).
func TestBaseline_Degradation_BothAbsent(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "internal/b.go", "package b\n")
	commitAll(t, dir, "seed")

	logger, buf := debugLogger()
	all, _, err := enumerateRepoFiles(context.Background(), dir, logger, false, "")
	require.NoError(t, err)
	assert.Equal(t, lsFiles(t, dir), sortedPaths(all), "no ignore files → unfiltered scan")
	assert.NotContains(t, buf.String(), "ignore filtering skips it", "absent ignore files are a silent no-op")
}

// TestBaseline_Degradation_OnePresentOneAbsent covers AC 03-03 Happy Path 2: a
// present .gitignore filters normally while the absent .atcrignore contributes
// nothing and causes no error.
func TestBaseline_Degradation_OnePresentOneAbsent(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "vendor/\n")
	write(t, dir, "keep.go", "package keep\n")
	write(t, dir, "vendor/lib.go", "package vendor\n")
	gitCmd(t, dir, "add", "-f", "vendor/lib.go")
	commitAll(t, dir, "seed")

	logger, _ := debugLogger()
	all, _, err := enumerateRepoFiles(context.Background(), dir, logger, false, "")
	require.NoError(t, err)
	kept := sortedPaths(all)
	assert.NotContains(t, kept, "vendor/lib.go", "present .gitignore filters normally")
	assert.Contains(t, kept, "keep.go")
}

// TestBaseline_Degradation_UnreadableGitignore covers AC 03-03 Edge Case 1: an
// unreadable/unparseable .gitignore (a directory in place of the file) disables ONLY
// that source at Debug level and never aborts the scan.
func TestBaseline_Degradation_UnreadableGitignore(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	commitAll(t, dir, "seed")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".gitignore"), 0o755)) // a directory, not a file → unreadable

	logger, buf := debugLogger()
	all, _, err := enumerateRepoFiles(context.Background(), dir, logger, false, "")
	require.NoError(t, err, "an unreadable .gitignore must not abort the scan (exit 0 parity)")
	assert.Contains(t, sortedPaths(all), "a.go", "scan proceeds unfiltered by the broken source")
	assert.Contains(t, buf.String(), "unreadable .gitignore", "the disabled source is logged at Debug")
}

// TestBaseline_Degradation_UnreadableAtcrignore covers AC 03-03 Edge Case 2: an
// unreadable .atcrignore (a directory in place of the file → non-IsNotExist read
// error) disables only that source at Debug level and never aborts the scan.
func TestBaseline_Degradation_UnreadableAtcrignore(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	commitAll(t, dir, "seed")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".atcrignore"), 0o755))

	logger, buf := debugLogger()
	all, _, err := enumerateRepoFiles(context.Background(), dir, logger, false, "")
	require.NoError(t, err, "an unreadable .atcrignore must not abort the scan")
	assert.Contains(t, sortedPaths(all), "a.go")
	assert.Contains(t, buf.String(), "unreadable .atcrignore", "the disabled source is logged at Debug")
}

// TestBaseline_Degradation_BothUnreadable covers AC 03-03 Edge Case 3 / Error
// Scenario 1: with both ignore sources unreadable the scan proceeds fully unfiltered
// and returns no error (exit 0) — fullrepo.go adds no error path of its own.
func TestBaseline_Degradation_BothUnreadable(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "vendor/lib.go", "package vendor\n")
	commitAll(t, dir, "seed")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".gitignore"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".atcrignore"), 0o755))

	logger, _ := debugLogger()
	all, _, err := enumerateRepoFiles(context.Background(), dir, logger, false, "")
	require.NoError(t, err, "both sources unreadable must degrade to a full unfiltered scan, not an error")
	assert.ElementsMatch(t, lsFiles(t, dir), sortedPaths(all), "fully unfiltered when both sources fail to load")
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
	chunks, err := PartitionByBudget(entries, 100)
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

	chunks, err := PartitionByBudget(entries, 100)
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
	chunks, err := PartitionByBudget(entries, 100)
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
	chunks, err := PartitionByBudget(nil, 100)
	require.NoError(t, err)
	assert.Empty(t, chunks)
}

// AC 01-03 Edge Case 2: identical input produces identical chunk membership and
// ordering across repeated runs (no map-iteration-order leakage).
func TestPartitionByBudget_Deterministic(t *testing.T) {
	entries := mkEntries(map[string]int64{
		"a.go": 40, "b.go": 40, "c.go": 40, "d.go": 40, "e.go": 40, "f.go": 40,
	})
	first, err := PartitionByBudget(entries, 100)
	require.NoError(t, err)
	second, err := PartitionByBudget(entries, 100)
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
	chunks, err := PartitionByBudget(entries, 0)
	require.Error(t, err)
	assert.Nil(t, chunks)
	assert.Contains(t, err.Error(), "no effective byte budget")

	_, errNeg := PartitionByBudget(entries, -5)
	require.Error(t, errNeg, "negative budget must also fail fast")
}

// AC 01-03 Security / Input Validation: a negative/corrupt FileEntry.Size is
// clamped to zero for budget accounting and the file is still included.
func TestPartitionByBudget_ClampsNegativeSize(t *testing.T) {
	entries := []FileEntry{
		{Path: "neg.go", Size: -100, Body: ""},
		{Path: "a.go", Size: 30, Body: strings.Repeat("x", 30)},
	}
	chunks, err := PartitionByBudget(entries, 100)
	require.NoError(t, err)
	all, _ := allChunkPaths(chunks)
	assert.Equal(t, []string{"a.go", "neg.go"}, all, "clamped-size file still included")
}
