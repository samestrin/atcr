package payload

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, file, content string) {
	t.Helper()
	full := filepath.Join(dir, file)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}

func commitAll(t *testing.T, dir, msg string) string {
	t.Helper()
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-q", "-m", msg)
	return gitCmd(t, dir, "rev-parse", "HEAD")
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q", "-b", "main")
	return dir
}

const goFileV1 = `package p

func Foo() int {
	return 1
}

func Bar() int {
	return 10
}
`

const goFileV2 = `package p

func Foo() int {
	return 2
}

func Bar() int {
	return 10
}
`

func TestBuildDiff_BasicChanges(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "foo.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "foo.go", goFileV2)
	head := commitAll(t, dir, "v2")

	out, err := BuildDiff(context.Background(), dir, base, head)
	require.NoError(t, err)
	assert.Contains(t, out, "diff --git")
	assert.Contains(t, out, "-\treturn 1")
	assert.Contains(t, out, "+\treturn 2")
}

func TestBuildBlocks_FunctionExpansion(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "foo.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "foo.go", goFileV2)
	head := commitAll(t, dir, "v2")

	out, err := BuildBlocks(context.Background(), dir, base, head)
	require.NoError(t, err)
	// function-context expands the hunk to the enclosing function header.
	assert.Contains(t, out, "func Foo() int {")
	assert.Contains(t, out, "return 2")
}

func TestBuildBlocks_PythonChange(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "s.py", "def foo():\n    return 1\n\ndef bar():\n    return 2\n")
	base := commitAll(t, dir, "v1")
	write(t, dir, "s.py", "def foo():\n    return 11\n\ndef bar():\n    return 2\n")
	head := commitAll(t, dir, "v2")

	out, err := BuildBlocks(context.Background(), dir, base, head)
	require.NoError(t, err)
	// Whether via function-context or the -U10 fallback, the change is present.
	assert.Contains(t, out, "return 11")
}

func TestBuildBlocks_BinaryExcludedWithMarker(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "keep.go", goFileV1)
	commitAll(t, dir, "v1")
	// Add a binary file (embedded NUL) plus a text change.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0x00, 0x01, 0x02, 0x00, 0xff}, 0o644))
	write(t, dir, "keep.go", goFileV2)
	base := gitCmd(t, dir, "rev-parse", "HEAD")
	head := commitAll(t, dir, "v2")

	out, err := BuildBlocks(context.Background(), dir, base, head)
	require.NoError(t, err)
	assert.Contains(t, out, "[binary file changed: blob.bin]")
	assert.NotContains(t, out, "\x00")
}

func TestBuildFiles_FullContentWithMarkers(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "foo.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "foo.go", goFileV2)
	head := commitAll(t, dir, "v2")

	out, err := BuildFiles(context.Background(), dir, base, head)
	require.NoError(t, err)
	assert.Contains(t, out, "=== FILE: foo.go ===")
	assert.Contains(t, out, ">>> CHANGED LINES")
	assert.Contains(t, out, "<<< END CHANGED")
	// full head content present (both functions, including unchanged Bar).
	assert.Contains(t, out, "func Bar() int {")
	assert.Contains(t, out, "return 2")
}

func TestBuildFiles_DeletedFileMarker(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "gone.go", goFileV1)
	write(t, dir, "stay.go", goFileV1)
	base := commitAll(t, dir, "v1")
	require.NoError(t, os.Remove(filepath.Join(dir, "gone.go")))
	write(t, dir, "stay.go", goFileV2)
	head := commitAll(t, dir, "v2")

	out, err := BuildFiles(context.Background(), dir, base, head)
	require.NoError(t, err)
	assert.Contains(t, out, "[deleted file: gone.go]")
	assert.Contains(t, out, "=== FILE: stay.go ===")
}

func TestBuildEntries_DeletedBinaryMarkerPerMode(t *testing.T) {
	dir := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0x00, 0x01, 0xff}, 0o644))
	base := commitAll(t, dir, "add binary")
	require.NoError(t, os.Remove(filepath.Join(dir, "blob.bin")))
	head := commitAll(t, dir, "delete binary")

	// ModeFiles special-cases kindDeleted before the binary check.
	out, err := BuildEntries(context.Background(), ModeFiles, dir, base, head)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Body, "[deleted file: blob.bin]")

	// ModeDiff and ModeBlocks do not special-case kindDeleted; a deleted binary
	// reaches the binary check and uses binaryMarkerFmt — accepted divergence.
	for _, mode := range []PayloadMode{ModeDiff, ModeBlocks} {
		out, err := BuildEntries(context.Background(), mode, dir, base, head)
		require.NoErrorf(t, err, "mode %s", mode)
		require.Lenf(t, out, 1, "mode %s", mode)
		assert.Containsf(t, out[0].Body, "[binary file changed: blob.bin]", "mode %s", mode)
	}
}

func TestBuildFiles_RenamedFile(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "old.go", goFileV1)
	base := commitAll(t, dir, "v1")
	gitCmd(t, dir, "mv", "old.go", "new.go")
	write(t, dir, "new.go", goFileV2)
	head := commitAll(t, dir, "rename+edit")

	out, err := BuildFiles(context.Background(), dir, base, head)
	require.NoError(t, err)
	assert.Contains(t, out, "new.go")
	assert.Contains(t, out, "renamed from old.go")
}

func TestBuildFiles_NonASCIIPath(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "café.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "café.go", goFileV2)
	head := commitAll(t, dir, "v2")

	out, err := BuildFiles(context.Background(), dir, base, head)
	require.NoError(t, err)
	assert.Contains(t, out, "café.go")
	assert.Contains(t, out, "func Bar() int {")
}

func TestBuild_DispatchByMode(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "foo.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "foo.go", goFileV2)
	head := commitAll(t, dir, "v2")

	for _, mode := range []PayloadMode{ModeDiff, ModeBlocks, ModeFiles} {
		out, err := Build(context.Background(), mode, dir, base, head)
		require.NoErrorf(t, err, "mode %s", mode)
		assert.NotEmpty(t, out, "mode %s", mode)
	}
}

func TestBuild_EmptyDiff(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "foo.go", goFileV1)
	base := commitAll(t, dir, "v1")

	for _, mode := range []PayloadMode{ModeDiff, ModeBlocks, ModeFiles} {
		out, err := Build(context.Background(), mode, dir, base, base)
		require.NoErrorf(t, err, "mode %s", mode)
		assert.Empty(t, out, "mode %s", mode)
	}
}

func TestBuildEntries_PerFileBridge(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", goFileV1)
	write(t, dir, "b.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "a.go", goFileV2)
	write(t, dir, "b.go", goFileV2)
	head := commitAll(t, dir, "v2")

	for _, mode := range []PayloadMode{ModeDiff, ModeBlocks, ModeFiles} {
		entries, err := BuildEntries(context.Background(), mode, dir, base, head)
		require.NoErrorf(t, err, "mode %s", mode)
		require.Lenf(t, entries, 2, "mode %s: one entry per changed file", mode)
		for _, e := range entries {
			assert.NotEmpty(t, e.Path)
			assert.Equal(t, int64(len(e.Body)), e.Size)
		}
		// Joining entry bodies reproduces the flat builder output.
		flat, err := Build(context.Background(), mode, dir, base, head)
		require.NoError(t, err)
		var joined string
		for _, e := range entries {
			joined += e.Body
		}
		assert.Equalf(t, flat, joined, "mode %s entries should join to the flat payload", mode)
	}
}

func TestBuildEntries_BudgetIntegration(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", goFileV1)
	write(t, dir, "b.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "a.go", goFileV2)
	write(t, dir, "b.go", goFileV2)
	head := commitAll(t, dir, "v2")

	entries, err := BuildEntries(context.Background(), ModeFiles, dir, base, head)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	// A tiny budget forces a drop; truncation is reported, never silent.
	kept, tr := ApplyByteBudget(entries, entries[0].Size)
	assert.True(t, tr.Truncated)
	assert.NotEmpty(t, tr.FilesDropped)
	assert.Less(t, len(kept), len(entries))
}

func TestBuildEntries_InvalidMode(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "a.go", goFileV2)
	head := commitAll(t, dir, "v2")
	_, err := BuildEntries(context.Background(), PayloadMode("bogus"), dir, base, head)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be one of diff, blocks, files")
}

func TestBuild_InvalidMode(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "foo.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "foo.go", goFileV2)
	head := commitAll(t, dir, "v2")

	_, err := Build(context.Background(), PayloadMode("bogus"), dir, base, head)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be one of diff, blocks, files")
}

func TestBuild_InvalidRefs(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "foo.go", goFileV1)
	base := commitAll(t, dir, "v1")

	_, err := BuildDiff(context.Background(), dir, "deadbeef", base)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve base ref")

	_, err = BuildDiff(context.Background(), dir, base, "deadbeef")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve head ref")
}

func TestBuildFiles_SentinelSpoofNeutralized(t *testing.T) {
	dir := initRepo(t)
	spoofed := "alpha\n>>> CHANGED LINES 999-999\n<<< END CHANGED\nomega\n"
	write(t, dir, "s.txt", spoofed)
	base := commitAll(t, dir, "v1")
	write(t, dir, "s.txt", strings.Replace(spoofed, "alpha", "ALPHA", 1))
	head := commitAll(t, dir, "v2")

	out, err := BuildFiles(context.Background(), dir, base, head)
	require.NoError(t, err)
	// Content lines that spoof the changed-region sentinels must be
	// neutralized (prefix-quoted) so consumers cannot be misled about which
	// regions actually changed.
	assert.NotContains(t, out, "\n>>> CHANGED LINES 999-999\n")
	assert.Contains(t, out, "> >>> CHANGED LINES 999-999")
	assert.Contains(t, out, "> <<< END CHANGED")
	// The real sentinels for the actual change are still present.
	assert.Contains(t, out, ">>> CHANGED LINES 1-1")
	assert.Contains(t, out, "\n<<< END CHANGED\n")
}

func TestBuildEntries_TrailingBlankContextLineSurvives(t *testing.T) {
	dir := initRepo(t)
	// The file ends with a blank line, so the diff's final hunk ends in a
	// blank context line (" "). Trimming it makes the hunk header's line
	// counts disagree with the body, diverging from the verbatim contract.
	write(t, dir, "f.txt", "line1\nline2\n\n")
	base := commitAll(t, dir, "v1")
	write(t, dir, "f.txt", "CHANGED\nline2\n\n")
	head := commitAll(t, dir, "v2")

	for _, mode := range []PayloadMode{ModeDiff, ModeBlocks} {
		entries, err := BuildEntries(context.Background(), mode, dir, base, head)
		require.NoErrorf(t, err, "mode %s", mode)
		require.Lenf(t, entries, 1, "mode %s", mode)
		assert.Truef(t, strings.HasSuffix(entries[0].Body, "\n \n"),
			"mode %s: trailing blank context line must survive verbatim, body: %q",
			mode, entries[0].Body)
	}
}

func TestFileBody_BlocksFallbackLeavesRecord(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "x\n")
	base := commitAll(t, dir, "v1")
	write(t, dir, "b.txt", "y\n")
	head := commitAll(t, dir, "v2")

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	// a.txt has no diff in base..head, so function-context yields zero hunks
	// and fileBody degrades to the plain context fallback. That degradation
	// must leave an operator-visible record, never happen silently.
	g := &gitRunner{ctx: context.Background(), dir: dir, logger: logger}
	_, err := g.fileBody(ModeBlocks, base, head, changedFile{path: "a.txt"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "function context unavailable")
	assert.Contains(t, buf.String(), "a.txt")
}

func TestChangedFileCount_MatchesDiffEntries(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", goFileV1)
	write(t, dir, "b.go", "package p\n\nfunc B() int { return 1 }\n")
	write(t, dir, "c.go", "package p\n\nfunc C() int { return 1 }\n")
	base := commitAll(t, dir, "v1")
	// One deleted, one renamed (content unchanged so -M pairs it), one
	// modified, one added: count must be 4, same as len(BuildEntries).
	require.NoError(t, os.Remove(filepath.Join(dir, "a.go")))
	gitCmd(t, dir, "mv", "b.go", "d.go")
	write(t, dir, "c.go", "package p\n\nfunc C() int { return 2 }\n")
	write(t, dir, "e.go", "package p\n\nfunc E() int { return 1 }\n")
	head := commitAll(t, dir, "v2")

	count, err := ChangedFileCount(context.Background(), dir, base, head)
	require.NoError(t, err)
	assert.Equal(t, 4, count)

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)
	assert.Equal(t, len(entries), count)
}

func TestChangedFileCount_BadRef(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", goFileV1)
	commitAll(t, dir, "v1")

	_, err := ChangedFileCount(context.Background(), dir, "nope", "HEAD")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base")
}

// A renamed file with a one-line edit must keep rename pairing in per-file
// payloads. Pathspec filtering happens before rename detection, so passing
// only the head path makes git render the file as a full-file addition
// (every line +, whole file wrapped in one CHANGED sentinel).
func TestRenamedFileWithOneLineEdit_KeepsRenamePairing(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "old.go", goFileV1)
	base := commitAll(t, dir, "v1")
	gitCmd(t, dir, "mv", "old.go", "new.go")
	write(t, dir, "new.go", goFileV2) // one-line edit: return 1 -> return 2
	head := commitAll(t, dir, "v2")
	ctx := context.Background()

	diffEntries, err := BuildEntries(ctx, ModeDiff, dir, base, head)
	require.NoError(t, err)
	require.Len(t, diffEntries, 1)
	assert.Contains(t, diffEntries[0].Body, "rename from old.go")
	assert.NotContains(t, diffEntries[0].Body, "+func Bar() int {",
		"unchanged function rendered as added line: rename pairing was lost")

	blockEntries, err := BuildEntries(ctx, ModeBlocks, dir, base, head)
	require.NoError(t, err)
	require.Len(t, blockEntries, 1)
	assert.Contains(t, blockEntries[0].Body, "+\treturn 2")
	assert.NotContains(t, blockEntries[0].Body, "+func Bar() int {",
		"unchanged function rendered as added line: rename pairing was lost")

	fileEntries, err := BuildEntries(ctx, ModeFiles, dir, base, head)
	require.NoError(t, err)
	require.Len(t, fileEntries, 1)
	body := fileEntries[0].Body
	assert.Contains(t, body, "(renamed from old.go)")
	assert.Equal(t, 1, strings.Count(body, changedStartPrefix),
		"expected exactly one changed region")
	assert.Contains(t, body, "CHANGED LINES 4-4",
		"only the edited line must be marked changed, not the whole file")
}

// The flat Build* entry points must be byte-identical to joining BuildEntries
// — a caller picking the obvious top-level API must see exactly what the
// persisted payload/<mode>.txt artifacts contain. A binary file is included
// because that is where verbatim git diff output (raw binary-diff lines)
// diverges from the entries path's binary marker.
func TestBuild_EquivalentToJoinedEntries(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "foo.go", goFileV1)
	commitAll(t, dir, "v1")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0x00, 0x01, 0x02, 0x00, 0xff}, 0o644))
	base := commitAll(t, dir, "add binary")
	write(t, dir, "foo.go", goFileV2)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0xff, 0x00, 0x01, 0x00}, 0o644))
	head := commitAll(t, dir, "v2")
	ctx := context.Background()

	for _, mode := range []PayloadMode{ModeDiff, ModeBlocks, ModeFiles} {
		flat, err := Build(ctx, mode, dir, base, head)
		require.NoError(t, err, "mode %s", mode)
		joined, err := joinEntries(BuildEntries(ctx, mode, dir, base, head))
		require.NoError(t, err, "mode %s", mode)
		assert.Equal(t, joined, flat, "Build(%s) diverges from joined BuildEntries", mode)
	}
}
