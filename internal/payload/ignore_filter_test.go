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
