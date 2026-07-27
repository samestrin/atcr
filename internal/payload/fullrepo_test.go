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

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard())
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
	write(t, dir, ".gitignore", "vendor/\n")
	commitAll(t, dir, "init")

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard())
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
	write(t, dir, ".atcrignore", "generated/\n")
	commitAll(t, dir, "init")

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard())
	require.NoError(t, err)
	_, present := findEntry(entries, "generated/schema.go")
	assert.False(t, present, "generated/schema.go must be .atcrignore-filtered out")
}

// AC 01-02 Edge Case 1: a repo with a commit but zero tracked files returns an
// empty (non-nil-with-error) slice so the caller surfaces "no reviewable content".
func TestEnumerateRepoFiles_ZeroTracked(t *testing.T) {
	dir := initRepo(t)
	gitCmd(t, dir, "commit", "-q", "--allow-empty", "-m", "empty")

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard())
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
		entries, err = enumerateRepoFiles(context.Background(), dir, log.Discard())
	})
	require.Error(t, err)
	assert.Nil(t, entries)
	assert.Contains(t, err.Error(), "could not enumerate tracked files")
}

// AC 01-02 Edge Case 3: a tracked binary (non-UTF8) file is included with its raw
// byte size recorded; the walker must not crash or corrupt output.
func TestEnumerateRepoFiles_BinaryFile(t *testing.T) {
	dir := initRepo(t)
	blob := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0x00, 0x99}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blob.bin"), blob, 0o644))
	write(t, dir, "a.go", "package a\n")
	commitAll(t, dir, "init")

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard())
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

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard())
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

	entries, err := enumerateRepoFiles(context.Background(), dir, log.Discard())
	require.NoError(t, err)
	assert.Equal(t, lsFiles(t, dir), sortedPaths(entries), "result set must equal git ls-files (no untracked files)")
	_, present := findEntry(entries, "notes.txt")
	assert.False(t, present, "untracked notes.txt must be absent")
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

	_, err := enumerateRepoFiles(context.Background(), dir, log.Discard())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading tracked file")
	assert.Contains(t, err.Error(), "gone.go")
}
