package history

import (
	"io/fs"
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

// A concurrent prune that already unlinked a doomed shard turns this pass's
// os.Remove into fs.ErrNotExist — the end state is already achieved, so the
// pass must skip it and continue rather than aborting with the remaining
// doomed shards left unpruned (the race the reviewer reproduced 40/40 with
// two concurrent PruneShards over one dir).
func TestPruneShards_RemoveNotExistRaceIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	mustAppendShard(t, dir, time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC), "a")
	mustAppendShard(t, dir, time.Date(2020, 2, 1, 12, 0, 0, 0, time.UTC), "b")

	realRemove := osRemove
	calls := 0
	osRemove = func(name string) error {
		calls++
		if calls == 1 {
			return fs.ErrNotExist // a concurrent prune won the race for 2020-01
		}
		return realRemove(name)
	}
	t.Cleanup(func() { osRemove = realRemove })

	res, err := PruneShards(dir, 30*24*time.Hour, now)
	require.NoError(t, err, "an already-achieved removal is not a failure")
	assert.Equal(t, []string{"2020-02.jsonl"}, res.Removed,
		"only removals this pass actually performed are reported")
	assert.NoFileExists(t, filepath.Join(dir, "2020-02.jsonl"))
}

// A symlinked shard DIR is refused, not followed: PruneShards is the only
// destructive operation in the package, so it must never operate on a
// directory the caller did not literally name. (Compare
// TestPruneShards_RemovesSymlinkNotTarget: a symlink INSIDE the dir is
// unlinked as a link — also never followed.)
func TestPruneShards_RefusesSymlinkedShardDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	root := t.TempDir()
	real := filepath.Join(root, "real-history")
	require.NoError(t, os.MkdirAll(real, 0o700))
	doomed := mustAppendShard(t, real, time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC), "old")
	link := filepath.Join(root, "history")
	require.NoError(t, os.Symlink(real, link))

	_, err := PruneShards(link, 30*24*time.Hour, time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
	require.Error(t, err, "a symlinked shard dir must be refused, not followed")
	assert.FileExists(t, doomed, "nothing in the symlink target may be removed")
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

// "Nothing inside a surviving shard is ever rewritten: a shard is removed whole
// or left entirely alone" (PruneShards doc, Epic 35.14 Design Decision 2) is
// pinned at record granularity: a surviving shard holding MULTIPLE records must
// be byte-identical after the prune. Every other prune test's surviving shard
// holds a single record, so a mutant that folds each survivor down to its first
// record would otherwise stay green.
func TestPruneShards_SurvivingShardIsByteIdentical(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	keep := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	keepPath := ShardPath(dir, keep)
	require.NoError(t, Append(keepPath, []Record{
		{Timestamp: keep, ID: "k1", Severity: "HIGH", File: "a.go", Package: "p"},
		{Timestamp: keep.Add(time.Minute), ID: "k2", Severity: "LOW", File: "b.go", Package: "q"},
		{Timestamp: keep.Add(2 * time.Minute), ID: "k3", Severity: "CRITICAL", File: "c.go", Package: "r"},
	}))
	mustAppendShard(t, dir, time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC), "old")

	before, err := os.ReadFile(keepPath)
	require.NoError(t, err)

	res, err := PruneShards(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	assert.Equal(t, []string{"2020-01.jsonl"}, res.Removed)
	assert.Equal(t, 1, res.Kept)

	after, err := os.ReadFile(keepPath)
	require.NoError(t, err)
	assert.Equal(t, before, after,
		"a surviving shard must be left entirely alone — never rewritten at record granularity")
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
