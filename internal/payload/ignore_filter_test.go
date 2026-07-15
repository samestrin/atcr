package payload

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/samestrin/atcr/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ignoreFixture builds a repo whose base..head range changes three tracked
// files — a normal source file, a .gitignore-matched file (vendor/), and an
// .atcrignore-matched file (go.sum) — plus committed .gitignore/.atcrignore
// that are unchanged across the range (so they never appear in the diff).
func ignoreFixture(t *testing.T, withIgnoreFiles bool) (dir, base, head string) {
	t.Helper()
	dir = initRepo(t)
	if withIgnoreFiles {
		write(t, dir, ".gitignore", "vendor/\n")
		write(t, dir, ".atcrignore", "go.sum\n")
	}
	write(t, dir, "main.go", goFileV1)
	write(t, dir, "vendor/lib.go", goFileV1)
	write(t, dir, "go.sum", "hash v1\n")
	if withIgnoreFiles {
		// Force-track vendor/lib.go despite .gitignore matching it — the exact
		// case the filter targets: a file committed to git but that should never
		// reach the reviewer (e.g. vendored deps committed before the ignore rule).
		gitCmd(t, dir, "add", "-f", "vendor/lib.go")
	}
	base = commitAll(t, dir, "v1")

	write(t, dir, "main.go", goFileV2)
	write(t, dir, "vendor/lib.go", goFileV2)
	write(t, dir, "go.sum", "hash v2\n")
	head = commitAll(t, dir, "v2")
	return dir, base, head
}

func entryPaths(entries []FileEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Path
	}
	return out
}

func TestBuildEntries_ExcludesIgnored(t *testing.T) {
	dir, base, head := ignoreFixture(t, true)

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)

	paths := entryPaths(entries)
	assert.Contains(t, paths, "main.go")
	assert.NotContains(t, paths, "vendor/lib.go", "vendor/ is .gitignore-matched")
	assert.NotContains(t, paths, "go.sum", "go.sum is .atcrignore-matched")

	// The flat payload must not error (splitter stays consistent) and must not
	// carry the ignored files' content.
	out, err := BuildDiff(context.Background(), dir, base, head)
	require.NoError(t, err)
	assert.NotContains(t, out, "vendor/lib.go")
	assert.NotContains(t, out, "go.sum")
}

func TestChangedFileCount_ExcludesIgnored(t *testing.T) {
	dir, base, head := ignoreFixture(t, true)

	n, err := ChangedFileCount(context.Background(), dir, base, head)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "only main.go survives ignore filtering")
}

// Baseline: with no ignore files, every changed file is present (proves the
// filter is a strict no-op when there is nothing to ignore).
func TestBuildEntries_NoIgnoreFiles_AllPresent(t *testing.T) {
	dir, base, head := ignoreFixture(t, false)

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)

	paths := entryPaths(entries)
	assert.ElementsMatch(t, []string{"main.go", "vendor/lib.go", "go.sum"}, paths)
}

// AC #3: skipped files are logged at slog debug via the gitRunner logger.
func TestBuildEntries_LogsSkippedAtDebug(t *testing.T) {
	dir, base, head := ignoreFixture(t, true)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := log.NewContext(context.Background(), logger)

	_, err := BuildEntries(ctx, ModeDiff, dir, base, head)
	require.NoError(t, err)

	logged := buf.String()
	assert.Contains(t, logged, "vendor/lib.go")
	assert.Contains(t, logged, "go.sum")
}

// The production fan-out path (RangeBuilder) excludes ignored files too.
func TestRangeBuilder_ExcludesIgnored(t *testing.T) {
	dir, base, head := ignoreFixture(t, true)

	rb := NewRangeBuilder(context.Background(), dir, base, head)
	entries, err := rb.BuildEntries(ModeDiff)
	require.NoError(t, err)

	paths := entryPaths(entries)
	assert.Equal(t, []string{"main.go"}, paths)
}

// Regression: an ignored filename containing git pathspec glob metacharacters
// must NOT over-exclude an unrelated changed file. Without `literal` magic,
// :(exclude)a[b].go also matches ab.go, silently dropping ab.go's diff body.
// The .atcrignore uses escaped brackets so the MATCHER excludes only the literal
// a[b].go — any collateral loss of ab.go can then come only from the pathspec.
func TestBuildEntries_GlobMetacharFilename(t *testing.T) {
	dir := initRepo(t)
	writeIgnore(t, dir, ".atcrignore", `a\[b\].go`+"\n")
	write(t, dir, "a[b].go", "v1\n")
	write(t, dir, "ab.go", "keep-v1\n")
	base := commitAll(t, dir, "v1")
	write(t, dir, "a[b].go", "v2\n")
	write(t, dir, "ab.go", "keep-v2\n")
	head := commitAll(t, dir, "v2")

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)

	paths := entryPaths(entries)
	assert.NotContains(t, paths, "a[b].go", "the metachar file is ignored")
	require.Contains(t, paths, "ab.go", "ab.go must not be collaterally excluded by the glob pathspec")

	// ab.go must carry its real change, not an empty body (the silent-loss symptom).
	out, err := BuildDiff(context.Background(), dir, base, head)
	require.NoError(t, err)
	assert.Contains(t, out, "keep-v2", "ab.go's changed content must survive")
}

// WithoutIgnoreFilter (the --no-ignore opt-out) bypasses the filter: every
// changed file — including .gitignore/.atcrignore-matched ones — is reviewed.
func TestRangeBuilder_NoIgnoreOption_IncludesIgnored(t *testing.T) {
	dir, base, head := ignoreFixture(t, true)

	rb := NewRangeBuilder(context.Background(), dir, base, head, WithoutIgnoreFilter())
	entries, err := rb.BuildEntries(ModeDiff)
	require.NoError(t, err)

	paths := entryPaths(entries)
	assert.ElementsMatch(t, []string{"main.go", "vendor/lib.go", "go.sum"}, paths)
}

// TD internal/payload/grounding.go:37 — the standalone BuildChangedLines honors
// the --no-ignore opt-out via a RangeOption, so grounding and payload agree when
// the fan-out falls back to the standalone grounding path under --no-ignore.
func TestBuildChangedLines_NoIgnoreOption(t *testing.T) {
	dir, base, head := ignoreFixture(t, true)

	// Default: ignored files are absent from the grounding map.
	filtered, err := BuildChangedLines(context.Background(), dir, base, head)
	require.NoError(t, err)
	_, hasVendor := filtered["vendor/lib.go"]
	assert.False(t, hasVendor, "grounding filters ignored files by default")

	// With the opt-out: ignored files are present, matching an unfiltered payload,
	// so findings on them survive the grounding gate.
	unfiltered, err := BuildChangedLines(context.Background(), dir, base, head, WithoutIgnoreFilter())
	require.NoError(t, err)
	_, keepsVendor := unfiltered["vendor/lib.go"]
	assert.True(t, keepsVendor, "--no-ignore grounding must keep ignored files")
}

// TD internal/payload/diff.go:74 — buildDiffPathspec bounds the whole-range diff
// argv by min(|kept|,|ignored|): it excludes the ignored files by default, but
// switches to a positive kept-file list once the exclude set is large and the
// kept set is strictly smaller, so a huge ignored set cannot blow ARG_MAX.
func TestBuildDiffPathspec(t *testing.T) {
	old := excludePathspecThreshold
	excludePathspecThreshold = 3
	t.Cleanup(func() { excludePathspecThreshold = old })

	kept := []changedFile{{path: "a.go"}}
	small := []string{":(exclude,literal)i1", ":(exclude,literal)i2"}
	big := []string{":(exclude,literal)i1", ":(exclude,literal)i2", ":(exclude,literal)i3", ":(exclude,literal)i4"}

	// Nothing ignored → whole-range diff, no pathspec.
	assert.Nil(t, buildDiffPathspec(kept, nil))

	// Exclude set within threshold → root-anchored exclude form.
	assert.Equal(t, []string{"--", ":/", ":(exclude,literal)i1", ":(exclude,literal)i2"},
		buildDiffPathspec(kept, small))

	// Large exclude set, strictly-smaller kept set → positive kept form.
	assert.Equal(t, []string{"--", ":(top,literal)a.go"}, buildDiffPathspec(kept, big))

	// A kept rename contributes both paths (old first) so git renders the rename,
	// not a bare addition.
	ren := []changedFile{{path: "new.go", oldPath: "old.go", kind: kindRenamed}}
	assert.Equal(t, []string{"--", ":(top,literal)old.go", ":(top,literal)new.go"},
		buildDiffPathspec(ren, big))

	// Positive set not smaller than the exclude set → stay on the exclude form.
	manyKept := []changedFile{{path: "a.go"}, {path: "b.go"}, {path: "c.go"}, {path: "d.go"}, {path: "e.go"}}
	assert.Equal(t, append([]string{"--", ":/"}, big...), buildDiffPathspec(manyKept, big))
}

// End-to-end: with a lowered threshold the RangeBuilder takes the positive-kept
// pathspec branch and produces byte-identical results to the exclude branch —
// the ignored files are still absent and the kept file's content is intact.
func TestBuildEntries_LargeExcludeUsesPositivePathspec(t *testing.T) {
	old := excludePathspecThreshold
	excludePathspecThreshold = 1
	t.Cleanup(func() { excludePathspecThreshold = old })

	dir, base, head := ignoreFixture(t, true) // kept: main.go; ignored: vendor/lib.go, go.sum

	rb := NewRangeBuilder(context.Background(), dir, base, head)
	entries, err := rb.BuildEntries(ModeDiff)
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, entryPaths(entries),
		"positive-pathspec branch must select the same kept file the exclude branch would")

	// The positive branch was taken: argv lists the kept file, not the ignored set.
	assert.Equal(t, []string{"--", ":(top,literal)main.go"}, rb.g.state.diffPathspec)

	// The flat diff carries no ignored content.
	out, err := BuildDiff(context.Background(), dir, base, head)
	require.NoError(t, err)
	assert.NotContains(t, out, "vendor/lib.go")
	assert.NotContains(t, out, "go.sum")
}

// An all-ignored range grounds to an empty map without ever passing a large
// exclude argv: diffChunks short-circuits when no files are kept.
func TestBuildChangedLines_AllIgnored_Empty(t *testing.T) {
	old := excludePathspecThreshold
	excludePathspecThreshold = 1
	t.Cleanup(func() { excludePathspecThreshold = old })

	dir, base, head := allIgnoredFixture(t)
	cl, err := BuildChangedLines(context.Background(), dir, base, head)
	require.NoError(t, err)
	assert.Empty(t, cl, "all-ignored range grounds to an empty map")
}

// Regression: a copied file whose head path is ignored must not cause applyIgnore
// to exclude the copy source. The source may be an independently modified file
// with its own diff chunk; excluding it would leave that chunk orphaned and
// produce an empty body.
func TestApplyIgnore_CopyDoesNotExcludeSource(t *testing.T) {
	dir := initRepo(t)
	writeIgnore(t, dir, ".atcrignore", "new.go\n")
	write(t, dir, "old.go", goFileV1)
	write(t, dir, "new.go", goFileV1)
	commitAll(t, dir, "v1")
	write(t, dir, "old.go", goFileV2)
	commitAll(t, dir, "v2")

	g := &gitRunner{ctx: context.Background(), dir: dir}
	files := []changedFile{
		// Simulate a copy status: new.go is a copy of old.go and is ignored.
		{path: "new.go", oldPath: "old.go", kind: kindCopied},
		{path: "old.go", kind: kindModified},
	}
	kept, exclude := g.applyIgnore(files)

	// The copy target is filtered out; the source survives as an independent change.
	require.Len(t, kept, 1)
	assert.Equal(t, "old.go", kept[0].path)
	for _, e := range exclude {
		assert.NotContains(t, e, "old.go", "copy source must not be excluded")
	}
}

// Ignored-file exclusions should be visible at the default (info) log level so
// operators notice when a misconfigured .atcrignore silently drops changes.
func TestBuildEntries_LogsSkippedSummaryAtInfo(t *testing.T) {
	dir, base, head := ignoreFixture(t, true)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil)) // default level = Info
	ctx := log.NewContext(context.Background(), logger)

	_, err := BuildEntries(ctx, ModeDiff, dir, base, head)
	require.NoError(t, err)

	logged := buf.String()
	assert.Contains(t, logged, "skipped", "summary should mention skipped files")
	assert.Contains(t, logged, "ignore", "summary should mention ignore rules")
}

// allIgnoredFixture builds a range where EVERY changed file is ignore-filtered
// (a lockfile-only / vendored-only PR), so the kept set is empty even though the
// range has changed files.
func allIgnoredFixture(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = initRepo(t)
	writeIgnore(t, dir, ".atcrignore", "vendor/\n")
	write(t, dir, "vendor/a.go", goFileV1)
	write(t, dir, "vendor/b.go", goFileV1)
	gitCmd(t, dir, "add", "-f", "vendor/a.go", "vendor/b.go")
	base = commitAll(t, dir, "v1")
	write(t, dir, "vendor/a.go", goFileV2)
	write(t, dir, "vendor/b.go", goFileV2)
	head = commitAll(t, dir, "v2")
	return dir, base, head
}

// When every changed file is ignore-filtered, the RangeBuilder exposes an
// all-ignored signal (the ignore-stage analogue of Truncation.AllDropped) so the
// review layer can emit a --no-ignore hint instead of a misleading
// "no changed files" error. TD: internal/payload/diff.go:174 / diff.go:223.
func TestRangeBuilder_AllIgnored_Signal(t *testing.T) {
	dir, base, head := allIgnoredFixture(t)

	rb := NewRangeBuilder(context.Background(), dir, base, head)
	entries, err := rb.BuildEntries(ModeDiff)
	require.NoError(t, err)
	require.Empty(t, entries, "every changed file is ignore-filtered")

	all, n := rb.AllIgnored()
	assert.True(t, all, "all changed files were ignore-filtered")
	assert.Equal(t, 2, n, "two changed files were excluded")
}

// Baseline: a range where some files survive the filter must NOT report
// all-ignored.
func TestRangeBuilder_AllIgnored_FalseWhenSomeKept(t *testing.T) {
	dir, base, head := ignoreFixture(t, true)

	rb := NewRangeBuilder(context.Background(), dir, base, head)
	_, err := rb.BuildEntries(ModeDiff)
	require.NoError(t, err)

	all, _ := rb.AllIgnored()
	assert.False(t, all, "main.go survives filtering, so not all-ignored")
}

// Baseline: a genuinely empty range (no changed files) is not all-ignored —
// nothing was filtered, so the --no-ignore hint must not fire.
func TestRangeBuilder_AllIgnored_FalseWhenNoChanges(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "main.go", goFileV1)
	base := commitAll(t, dir, "v1")

	rb := NewRangeBuilder(context.Background(), dir, base, base)
	_, err := rb.BuildEntries(ModeDiff)
	require.NoError(t, err)

	all, n := rb.AllIgnored()
	assert.False(t, all, "an empty range is not all-ignored")
	assert.Zero(t, n)
}
