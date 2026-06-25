package payload

import (
	"os"
	"path/filepath"
	"strconv"
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

// The file variant must reject a relative, lexically-in-tree path that is a
// symlink resolving OUTSIDE the working tree — the lexical isSafeDiffPath guard
// cannot catch this, so a runtime symlink check closes the gap.
func TestBuildEntriesFromDiffFile_RejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	external := filepath.Join(outside, "secret.diff")
	require.NoError(t, os.WriteFile(external, []byte("--- a/x.go\n+++ b/x.go\n@@ -1,1 +1,1 @@\n-a\n+b\n"), 0o644))
	t.Chdir(root)
	require.NoError(t, os.Symlink(external, "link.diff"))

	_, err := BuildEntriesFromDiffFile("link.diff", DefaultMaxDiffBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the working tree")
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

// REGRESSION (independent review HIGH): a single file whose hunk body contains a
// removed line rendering as `--- X` and an added line rendering as `+++ Y`
// immediately before the next hunk's `@@` header must NOT be mis-split into a
// second (phantom) file entry. The hunk-line-counting parser consumes each hunk's
// declared budget so body lines can never spoof a file header.
func TestBuildEntriesFromDiff_HunkBodyDoesNotSpoofHeader(t *testing.T) {
	diff := "--- a/real.go\n" +
		"+++ b/real.go\n" +
		"@@ -1,1 +1,1 @@\n" +
		"--- X\n" + // removed line of source content "-- X"
		"+++ Y\n" + // added line of source content "++ Y"
		"@@ -10,1 +10,1 @@\n" +
		"-old\n" +
		"+new\n"
	entries, err := BuildEntriesFromDiff(diff)
	require.NoError(t, err)
	require.Len(t, entries, 1, "hunk-body lines rendering as ---/+++ must not spoof a file header")
	assert.Equal(t, "real.go", entries[0].Path)
	assert.Equal(t, diff, joinBodies(entries), "must still round-trip verbatim")
}

// A normal multi-hunk single-file diff stays one entry under the counting parser.
func TestBuildEntriesFromDiff_MultiHunkSingleFile(t *testing.T) {
	diff := "--- a/m.go\n+++ b/m.go\n" +
		"@@ -1,2 +1,2 @@\n ctx\n-a\n+b\n" +
		"@@ -10,2 +10,2 @@\n ctx2\n-c\n+d\n"
	entries, err := BuildEntriesFromDiff(diff)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "m.go", entries[0].Path)
	assert.Equal(t, diff, joinBodies(entries))
}

// A path extracted from untrusted diff content that escapes the working tree
// (absolute, or a `..` traversal) must be rejected at the ingestion boundary
// rather than returned as a FileEntry path a downstream consumer might resolve.
func TestBuildEntriesFromDiff_RejectsTraversalInContentPath(t *testing.T) {
	for _, p := range []string{"../../etc/passwd", "/etc/passwd"} {
		diff := "--- a/" + p + "\n+++ b/" + p + "\n@@ -1,1 +1,1 @@\n-a\n+b\n"
		_, err := BuildEntriesFromDiff(diff)
		require.Error(t, err, "path %q must be rejected", p)
		assert.Contains(t, err.Error(), "unsafe")
	}
}

// A loose diff whose input ends in `\n\n` (a final blank context line plus the
// terminating newline) produces multiple trailing empty lines after splitting;
// the tolerance must consume ALL of them so the diff round-trips rather than
// aborting with "unexpected content".
func TestBuildEntriesFromDiff_TrailingBlankLineRoundTrips(t *testing.T) {
	diff := "--- a/x.go\n+++ b/x.go\n@@ -1,1 +1,1 @@\n-a\n+b\n\n"
	entries, err := BuildEntriesFromDiff(diff)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "x.go", entries[0].Path)
	assert.Equal(t, diff, joinBodies(entries), "trailing blank line must round-trip verbatim")
}

// An untrusted loose diff whose first hunk header inflates its declared line
// count must be rejected, not silently merged with the following file's content.
// A real body line is always prefixed (-/+/space/\); an over-count overruns into
// the next hunk's bare `@@ ` header, which the parser detects and rejects rather
// than swallowing two files into one entry (the bytes would still round-trip,
// hiding the corruption from the round-trip contract test).
func TestBuildEntriesFromDiff_InflatedHunkCountRejected(t *testing.T) {
	diff := "--- a/first.go\n+++ b/first.go\n@@ -1,5 +1,1 @@\n-a\n+b\n" +
		"--- a/second.go\n+++ b/second.go\n@@ -1,1 +1,1 @@\n-c\n+d\n"
	_, err := BuildEntriesFromDiff(diff)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claims more")
}

// A loose diff carrying `\ No newline at end of file` after a hunk's counted
// body lines must attach the marker to the current hunk (consume it) rather than
// leaving it to be mis-read as content after the section — both on the final file
// and on an interior file of a multi-file diff.
func TestBuildEntriesFromDiff_NoNewlineMarkerRoundTrips(t *testing.T) {
	// Single file, marker trailing the last hunk line.
	single := "--- a/x.go\n+++ b/x.go\n@@ -1,1 +1,1 @@\n-a\n+b\n\\ No newline at end of file\n"
	entries, err := BuildEntriesFromDiff(single)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "x.go", entries[0].Path)
	assert.Equal(t, single, joinBodies(entries), "trailing no-newline marker must round-trip verbatim")

	// Multi-file: the no-newline marker terminates the FIRST file's hunk; the
	// second file's header must still be recognized as a new section.
	multi := "--- a/x.go\n+++ b/x.go\n@@ -1,1 +1,1 @@\n-a\n+b\n\\ No newline at end of file\n" +
		"--- a/y.go\n+++ b/y.go\n@@ -1,1 +1,1 @@\n-c\n+d\n"
	entries, err = BuildEntriesFromDiff(multi)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "x.go", entries[0].Path)
	assert.Equal(t, "y.go", entries[1].Path)
	assert.Equal(t, multi, joinBodies(entries), "interior no-newline marker must not break section splitting")
}

// A git binary/mode-only section carries no `--- `/`+++ ` lines, so the head
// path must be parsed from the `diff --git a/<old> b/<new>` header instead.
func TestBuildEntriesFromDiff_GitBinarySection(t *testing.T) {
	diff := "diff --git a/logo.png b/logo.png\n" +
		"index abc1234..def5678 100644\n" +
		"Binary files a/logo.png and b/logo.png differ\n" +
		"diff --git a/main.go b/main.go\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-a\n" +
		"+b\n"
	entries, err := BuildEntriesFromDiff(diff)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "logo.png", entries[0].Path, "binary section path comes from the diff --git header")
	assert.Equal(t, "main.go", entries[1].Path)
	assert.Equal(t, diff, joinBodies(entries), "binary + text git diff must round-trip verbatim")
}

// A header-only (binary) git section whose path legitimately contains the literal
// " b/" substring must parse via the symmetric `a/<P> b/<P>` midpoint, not the
// last " b/" token (which would truncate the path to "dir.png").
func TestBuildEntriesFromDiff_GitBinarySectionSpacedPath(t *testing.T) {
	diff := "diff --git a/my b/dir.png b/my b/dir.png\n" +
		"index abc1234..def5678 100644\n" +
		"Binary files a/my b/dir.png and b/my b/dir.png differ\n"
	entries, err := BuildEntriesFromDiff(diff)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "my b/dir.png", entries[0].Path, "spaced binary path must parse via the symmetric midpoint")
	assert.Equal(t, diff, joinBodies(entries))
}

// A `git diff --no-prefix` binary/mode-only section carries no a/ b/ markers and
// no `+++` line; the head path must still be recovered from the symmetric
// `diff --git <P> <P>` header rather than erroring with "cannot determine path".
func TestBuildEntriesFromDiff_GitNoPrefixBinarySection(t *testing.T) {
	diff := "diff --git logo.png logo.png\n" +
		"index abc1234..def5678 100644\n" +
		"Binary files logo.png and logo.png differ\n"
	entries, err := BuildEntriesFromDiff(diff)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "logo.png", entries[0].Path, "no-prefix binary path comes from the symmetric diff --git header")
	assert.Equal(t, diff, joinBodies(entries))
}

// CRLF line endings (Windows-authored diffs) must still split correctly and
// round-trip verbatim, with the trailing CR stripped from the parsed path.
func TestBuildEntriesFromDiff_CRLFLineEndings(t *testing.T) {
	diff := "--- a/x.go\r\n+++ b/x.go\r\n@@ -1,1 +1,1 @@\r\n-a\r\n+b\r\n"
	entries, err := BuildEntriesFromDiff(diff)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "x.go", entries[0].Path, "trailing CR must be stripped from the path")
	assert.Equal(t, diff, joinBodies(entries), "CRLF diff must round-trip verbatim")
}

// readCapped pins the LimitReader "+1" / ">maxBytes" recheck pair that defends
// the diff-file read against a source larger than the cap (e.g. a file grown
// between Stat and read). Dropping the +1 would let LimitReader cap reads at
// exactly maxBytes, making the recheck dead code and silently accepting oversized
// input — this test fails if that regression is introduced.
func TestReadCapped_RejectsSourceLargerThanCap(t *testing.T) {
	const capBytes = 16
	_, err := readCapped(strings.NewReader(strings.Repeat("x", capBytes+5)), capBytes)
	require.Error(t, err, "a source larger than the cap must be rejected by the post-read recheck")
	assert.Contains(t, err.Error(), "exceeds")

	// A source exactly at the cap is accepted (boundary).
	data, err := readCapped(strings.NewReader(strings.Repeat("x", capBytes)), capBytes)
	require.NoError(t, err)
	assert.Len(t, data, capBytes)

	// maxBytes <= 0 disables the cap entirely.
	data, err = readCapped(strings.NewReader(strings.Repeat("x", capBytes+5)), 0)
	require.NoError(t, err)
	assert.Len(t, data, capBytes+5)
}

// A missing diff file surfaces a clear open error rather than a vacuous result.
func TestBuildEntriesFromDiffFile_MissingFileErrors(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	_, err := BuildEntriesFromDiffFile("does-not-exist.diff", DefaultMaxDiffBytes)
	require.Error(t, err)
}

// BenchmarkBuildEntriesFromDiff_LargeMultiFile exercises the ingestion hot path
// on a large multi-file loose diff so -benchmem surfaces the per-line index and
// per-section path-scan allocations (pre-sized splitLinesWithOffsets + early-break
// diffSectionPath).
func BenchmarkBuildEntriesFromDiff_LargeMultiFile(b *testing.B) {
	var sb strings.Builder
	for f := 0; f < 200; f++ {
		name := "pkg/file" + strconv.Itoa(f) + ".go"
		sb.WriteString("--- a/" + name + "\n+++ b/" + name + "\n@@ -1,40 +1,40 @@\n")
		for ln := 0; ln < 40; ln++ {
			sb.WriteString("-old line " + strconv.Itoa(ln) + "\n+new line " + strconv.Itoa(ln) + "\n")
		}
	}
	diff := sb.String()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := BuildEntriesFromDiff(diff); err != nil {
			b.Fatal(err)
		}
	}
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
