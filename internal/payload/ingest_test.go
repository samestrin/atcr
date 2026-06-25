package payload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// joinBodies concatenates entry bodies, mirroring joinEntries — the round-trip
// target the ingestion primitive must reproduce byte-for-byte.
func joinBodies(entries []FileEntry) string {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Body)
	}
	return b.String()
}

// A loose unified diff (no `diff --git` header, the suite-fixture format) must
// split into one entry per file, with bodies that round-trip to the input
// byte-for-byte and head paths parsed from the `+++ b/<path>` line.
func TestBuildEntriesFromDiff_LooseRoundTrip(t *testing.T) {
	diff := "--- a/alpha.go\n" +
		"+++ b/alpha.go\n" +
		"@@ -1,3 +1,3 @@\n" +
		" func a() int {\n" +
		"-\treturn 0\n" +
		"+\treturn 1\n" +
		" }\n" +
		"--- a/sub/beta.go\n" +
		"+++ b/sub/beta.go\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-old\n" +
		"+new\n"

	entries, err := BuildEntriesFromDiff(diff)
	require.NoError(t, err)
	require.Len(t, entries, 2, "one entry per file section")

	assert.Equal(t, "alpha.go", entries[0].Path)
	assert.Equal(t, "sub/beta.go", entries[1].Path)
	assert.Equal(t, diff, joinBodies(entries), "joined bodies must reproduce the input diff verbatim")
	for _, e := range entries {
		assert.Equal(t, int64(len(e.Body)), e.Size, "Size must equal len(Body)")
	}
}

// A full `git diff` patch (with `diff --git`/`index` headers) must split on the
// `diff --git ` boundaries, round-trip verbatim, and parse the head path from
// the `+++ b/<path>` line.
func TestBuildEntriesFromDiff_GitFormatRoundTrip(t *testing.T) {
	diff := "diff --git a/alpha.go b/alpha.go\n" +
		"index e69de29..4b825dc 100644\n" +
		"--- a/alpha.go\n" +
		"+++ b/alpha.go\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-x\n" +
		"+y\n" +
		"diff --git a/sub/beta.go b/sub/beta.go\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/sub/beta.go\n" +
		"+++ b/sub/beta.go\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-p\n" +
		"+q\n"

	entries, err := BuildEntriesFromDiff(diff)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "alpha.go", entries[0].Path)
	assert.Equal(t, "sub/beta.go", entries[1].Path)
	assert.Equal(t, diff, joinBodies(entries), "git-format diff must round-trip verbatim")
}

// A deleted file renders `+++ /dev/null`; the head path must fall back to the
// old (`--- a/<path>`) side rather than recording "/dev/null".
func TestBuildEntriesFromDiff_DeletedFileUsesOldPath(t *testing.T) {
	diff := "--- a/gone.go\n" +
		"+++ /dev/null\n" +
		"@@ -1,1 +0,0 @@\n" +
		"-was here\n"
	entries, err := BuildEntriesFromDiff(diff)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "gone.go", entries[0].Path)
}

// An empty (or whitespace-only) diff yields zero entries with no error — the
// no-content case the fanout layer maps to ErrNoReviewableContent.
func TestBuildEntriesFromDiff_EmptyIsZeroEntries(t *testing.T) {
	for _, in := range []string{"", "   \n  \n"} {
		entries, err := BuildEntriesFromDiff(in)
		require.NoError(t, err)
		assert.Empty(t, entries)
	}
}

// Non-diff garbage (no recognizable file headers) is an explicit error, not a
// silently empty payload.
func TestBuildEntriesFromDiff_GarbageErrors(t *testing.T) {
	_, err := BuildEntriesFromDiff("this is not a diff\njust some text\n")
	require.Error(t, err)
}

// Content before the first file section would be lost on round-trip, so it is
// rejected rather than silently misattributed.
func TestBuildEntriesFromDiff_LeadingContentErrors(t *testing.T) {
	diff := "garbage preamble\n--- a/x.go\n+++ b/x.go\n@@ -1,1 +1,1 @@\n-a\n+b\n"
	_, err := BuildEntriesFromDiff(diff)
	require.Error(t, err)
}

// The file variant reads a relative diff path, honoring the size cap.
func TestBuildEntriesFromDiffFile_ReadsRelativePath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	diff := "--- a/x.go\n+++ b/x.go\n@@ -1,1 +1,1 @@\n-a\n+b\n"
	require.NoError(t, os.WriteFile("patch.diff", []byte(diff), 0o644))

	entries, err := BuildEntriesFromDiffFile("patch.diff", DefaultMaxDiffBytes)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "x.go", entries[0].Path)
	assert.Equal(t, diff, joinBodies(entries))
}

// The file variant rejects absolute paths and `..` traversal, mirroring the
// suite manifest's isSafeRelPath guard.
func TestBuildEntriesFromDiffFile_RejectsUnsafePaths(t *testing.T) {
	for _, p := range []string{"/etc/passwd", "../escape.diff", "../../x.diff"} {
		_, err := BuildEntriesFromDiffFile(p, 0)
		require.Error(t, err, "path %q must be rejected", p)
		assert.Contains(t, err.Error(), "unsafe")
	}
}

// The file variant enforces the byte cap before parsing, so a hostile multi-GB
// diff cannot exhaust memory.
func TestBuildEntriesFromDiffFile_SizeCapRejectsOversized(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	big := "--- a/x.go\n+++ b/x.go\n@@ -1,1 +1,1 @@\n-a\n+" + strings.Repeat("z", 4096) + "\n"
	require.NoError(t, os.WriteFile("big.diff", []byte(big), 0o644))

	_, err := BuildEntriesFromDiffFile("big.diff", 64)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

// Parity anchor (AC4): the suite fixture case-01.diff, fed through the ingestion
// path, must round-trip verbatim and parse the head path — the same []FileEntry
// shape a git-sourced ModeDiff payload would carry for the same file.
func TestBuildEntriesFromDiff_FixtureParity(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "benchmark", "testdata", "suite-valid", "case-01.diff"))
	require.NoError(t, err)
	diff := string(data)

	entries, err := BuildEntriesFromDiff(diff)
	require.NoError(t, err)
	require.Len(t, entries, 1, "case-01.diff is a single-file diff")
	assert.Equal(t, "pay.go", entries[0].Path)
	assert.Equal(t, diff, joinBodies(entries), "ingested entry must reproduce the fixture bytes verbatim")
}
