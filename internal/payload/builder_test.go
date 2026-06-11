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
		if mode != ModeDiff { // BuildDiff is the verbatim whole-range diff
			var joined string
			for _, e := range entries {
				joined += e.Body
			}
			assert.Equalf(t, flat, joined, "mode %s entries should join to the flat payload", mode)
		}
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
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	// a.txt has no diff in base..head, so function-context yields zero hunks
	// and fileBody degrades to the plain context fallback. That degradation
	// must leave an operator-visible record, never happen silently.
	g := &gitRunner{ctx: context.Background(), dir: dir}
	_, err := g.fileBody(ModeBlocks, base, head, changedFile{path: "a.txt"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "function context unavailable")
	assert.Contains(t, buf.String(), "a.txt")
}
