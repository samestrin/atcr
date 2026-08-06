package history

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A shard that cannot be unlinked fails the call rather than reporting a
// success it did not achieve. Here the very first unlink is blocked, so nothing
// was removed and Removed is empty — the result reports what actually happened.
// (The partial case, where an earlier unlink succeeded and a later one failed,
// cannot be provoked portably: unlink permission is a property of the directory,
// so it is all-or-nothing within one pass. PruneResult is built incrementally so
// that case would report the earlier removals, and the CLI prints Removed before
// surfacing the error — see TestHistoryCmd_PruneNoticeGoesToStderrNotStdout.)
func TestPruneShards_UnremovableShardIsAnErrorNotASilentSuccess(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs POSIX directory permissions and a non-root user")
	}
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	mustAppendShard(t, dir, time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC), "a")
	mustAppendShard(t, dir, time.Date(2020, 2, 1, 12, 0, 0, 0, time.UTC), "b")

	// A read-only directory blocks unlink of its entries.
	require.NoError(t, os.Chmod(dir, 0o500))
	defer func() { _ = os.Chmod(dir, 0o700) }()

	res, err := PruneShards(dir, 30*24*time.Hour, now)
	require.Error(t, err)
	assert.Empty(t, res.Removed, "nothing could be removed, so nothing is reported as removed")
	assert.Equal(t, 2, res.Kept,
		"the doomed-but-unremoved shards are still on disk — Kept must count every retained candidate file, including on the failure path")
}

// Symlinked shard names are removed as links, not followed: os.Remove unlinks
// the entry in the shard dir and never touches the target.
func TestPruneShards_RemovesSymlinkNotTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "history")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	target := filepath.Join(root, "important.jsonl")
	require.NoError(t, os.WriteFile(target, []byte("{}\n"), 0o600))
	link := filepath.Join(dir, "2020-01.jsonl")
	require.NoError(t, os.Symlink(target, link))

	res, err := PruneShards(dir, 30*24*time.Hour, time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Equal(t, []string{"2020-01.jsonl"}, res.Removed)
	assert.NoFileExists(t, link)
	assert.FileExists(t, target, "the symlink target outside the shard dir must survive")
}

// A stem with path separators or traversal segments cannot reach outside the
// shard dir: os.ReadDir yields base names only, and a name that is not a plain
// YYYY-MM month is never a deletion candidate in the first place.
func TestPruneShards_CannotEscapeShardDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "history")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	outside := filepath.Join(root, "2020-01.jsonl")
	require.NoError(t, os.WriteFile(outside, []byte("{}\n"), 0o600))

	res, err := PruneShards(dir, 30*24*time.Hour, time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Empty(t, res.Removed)
	assert.FileExists(t, outside, "a same-named shard outside the dir is untouched")
}

// Pruning an empty shard directory is a no-op, not an error, and never removes
// the directory itself — a later review run appends into it as usual.
func TestPruneShards_KeepsTheDirectoryItself(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	mustAppendShard(t, dir, time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC), "old")

	_, err := PruneShards(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	assert.DirExists(t, dir)

	// The dir is still writable for the next run.
	mustAppendShard(t, dir, now, "new")
	recs, err := LoadShardsSince(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}
