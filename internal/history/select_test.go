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

// mustAppendShard writes one record into the shard for ts under dir.
func mustAppendShard(t *testing.T, dir string, ts time.Time, id string) string {
	t.Helper()
	path := ShardPath(dir, ts)
	require.NoError(t, Append(path, []Record{{Timestamp: ts, ID: id, File: id + ".go", Package: id}}))
	return path
}

// AC4: a narrow --since window must not open shards whose month falls entirely
// outside it. The shard filename encodes the month, so selection happens before
// any file is read — that is what makes a 30d query cost proportionally to its
// window instead of scanning all of history.
//
// "Did not open" is proven two ways, because neither alone is sufficient.
// LoadShardsSince applies no record-level filtering, so a READABLE out-of-window
// shard's records appearing in the result is direct proof it was opened — an
// implementation that reads everything and filters afterwards cannot pass that.
// A second out-of-window shard is chmod 000: an implementation that opens it
// would have to swallow the read error to stay green, so the two together pin
// both the selection and the absence of an open attempt.
func TestLoadShardsSince_DoesNotOpenOutOfWindowShards(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read 000-permission files")
	}
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	mustAppendShard(t, dir, now, "aug")
	mustAppendShard(t, dir, time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC), "may-readable")

	unreadable := mustAppendShard(t, dir, time.Date(2025, 12, 10, 12, 0, 0, 0, time.UTC), "dec")
	require.NoError(t, os.Chmod(unreadable, 0o000))
	defer func() { _ = os.Chmod(unreadable, 0o600) }()

	recs, err := LoadShardsSince(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	require.Len(t, recs, 1, "only the in-window shard may be opened")
	assert.Equal(t, "aug", recs[0].ID)
}

// A window that reaches back into the previous month opens exactly the two
// shards it spans — no more, no fewer.
func TestLoadShardsSince_OpensEveryShardTheWindowTouches(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) // 30d reaches back to Jul 4
	mustAppendShard(t, dir, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), "aug")
	mustAppendShard(t, dir, time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC), "jul")
	mustAppendShard(t, dir, time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC), "jun")

	recs, err := LoadShardsSince(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	ids := idSet(recs)
	assert.Equal(t, map[string]bool{"aug": true, "jul": true}, ids, "June is wholly outside the window")
}

// The cutoff falling inside a month selects that whole shard: the shard is the
// unit of selection, and the record-level Filter still applies the exact window
// afterwards. Selection may over-select, never under-select.
func TestLoadShardsSince_SelectsTheCutoffMonthWhole(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) // 30d cutoff = Jul 21
	// A July record from BEFORE the cutoff still loads; Filter, not selection,
	// is what excludes it from the report.
	mustAppendShard(t, dir, time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC), "jul-early")

	recs, err := LoadShardsSince(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "jul-early", recs[0].ID)
	assert.Empty(t, Filter(recs, 30*24*time.Hour, "", now), "the record-level window still excludes it")
}

// A shard stamped in a future month (clock skew, or a record backfilled by a
// machine running ahead) is never excluded: the window has a lower bound only.
func TestLoadShardsSince_IncludesFutureShards(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	mustAppendShard(t, dir, time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), "future")

	recs, err := LoadShardsSince(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "future", recs[0].ID)
}

// A *.jsonl file whose stem is not a YYYY-MM month cannot be proven out of
// window, so it is read rather than silently dropped — the same tolerant-read
// posture Load takes with a malformed line.
func TestLoadShardsSince_IncludesUnparseableStems(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	old := time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, Append(filepath.Join(dir, "backup.jsonl"), []Record{{Timestamp: old, ID: "odd", File: "a.go"}}))

	recs, err := LoadShardsSince(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	require.Len(t, recs, 1, "an unparseable stem must be read, not assumed out of window")
	assert.Equal(t, "odd", recs[0].ID)
}

// A symlinked (or otherwise non-regular) shard is never followed on read:
// selection accepts the name, but opening it would read a file outside the
// shard dir — and a FIFO in its place would hang the read entirely. (Prune
// unlinks such entries as links; the read path simply skips them.)
func TestLoadShardsSince_SkipsSymlinkedShards(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "history")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	outside := filepath.Join(root, "outside.jsonl")
	require.NoError(t, Append(outside, []Record{{Timestamp: now, ID: "outside", File: "a.go"}}))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "2026-08.jsonl")))

	recs, err := LoadShardsSince(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	assert.Empty(t, recs, "a symlinked shard is never followed on read")
}

// An absent shard dir is a valid empty history, not an error (AC6).
func TestLoadShardsSince_AbsentDirIsEmptyNotError(t *testing.T) {
	recs, err := LoadShardsSince(filepath.Join(t.TempDir(), "nope"), 30*24*time.Hour, time.Now())
	require.NoError(t, err)
	assert.Empty(t, recs)
}

// A non-positive window selects nothing rather than every shard: ParseSince
// rejects such values at the CLI boundary, so reaching here means a programming
// error, and "load everything" is the wrong failure mode for a bounded query.
func TestLoadShardsSince_NonPositiveWindowSelectsNothing(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	mustAppendShard(t, dir, now, "aug")

	recs, err := LoadShardsSince(dir, 0, now)
	require.NoError(t, err)
	assert.Empty(t, recs)
}

// AC2 + AC4 compose: the windowed read still unions the legacy flat ledger,
// which is not month-sharded and therefore always read.
func TestLoadAllSince_UnionsLegacyWithSelectedShards(t *testing.T) {
	root := t.TempDir()
	shardDir := filepath.Join(root, ".atcr", "history")
	legacyPath := filepath.Join(root, ".atcr", "findings-history.jsonl")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	mustAppendShard(t, shardDir, now, "aug")
	mustAppendShard(t, shardDir, time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), "jan")
	require.NoError(t, Append(legacyPath, []Record{{Timestamp: now.Add(-24 * time.Hour), ID: "legacy", File: "l.go"}}))

	recs, err := LoadAllSince(shardDir, legacyPath, 30*24*time.Hour, now)
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"legacy": true, "aug": true}, idSet(recs))
}

func idSet(recs []Record) map[string]bool {
	out := map[string]bool{}
	for _, r := range recs {
		out[r.ID] = true
	}
	return out
}

// HasAny distinguishes "nothing ever recorded" from "nothing in this window",
// the ambiguity windowed selection introduces.
func TestHasAny(t *testing.T) {
	t.Run("both absent", func(t *testing.T) {
		root := t.TempDir()
		assert.False(t, HasAny(filepath.Join(root, ".atcr", "history"), filepath.Join(root, ".atcr", "findings-history.jsonl")))
	})

	t.Run("out-of-window shard still counts", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, ".atcr", "history")
		mustAppendShard(t, dir, time.Date(2019, 3, 1, 12, 0, 0, 0, time.UTC), "ancient")
		assert.True(t, HasAny(dir, filepath.Join(root, ".atcr", "findings-history.jsonl")))
	})

	t.Run("legacy ledger alone counts", func(t *testing.T) {
		root := t.TempDir()
		legacy := filepath.Join(root, ".atcr", "findings-history.jsonl")
		require.NoError(t, Append(legacy, []Record{{Timestamp: time.Now(), ID: "l", File: "a.go"}}))
		assert.True(t, HasAny(filepath.Join(root, ".atcr", "history"), legacy))
	})

	t.Run("empty shard dir and empty legacy file do not count", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, ".atcr", "history")
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600))
		legacy := filepath.Join(root, ".atcr", "findings-history.jsonl")
		require.NoError(t, os.WriteFile(legacy, nil, 0o600))
		assert.False(t, HasAny(dir, legacy))
	})
}

// An unreadable legacy ledger is a hard error on the windowed path too, exactly
// as on LoadAll: the flat ledger is one file, so failing to read it silently
// would drop every pre-19.4 record without saying so.
func TestLoadAllSince_UnreadableLegacyIsError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read 000-permission files")
	}
	root := t.TempDir()
	legacy := LegacyLedgerPath(root)
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	require.NoError(t, Append(legacy, []Record{{Timestamp: now, ID: "l", File: "a.go"}}))
	require.NoError(t, os.Chmod(legacy, 0o000))
	defer func() { _ = os.Chmod(legacy, 0o600) }()

	_, err := LoadAllSince(filepath.Join(root, ".atcr", "history"), legacy, 30*24*time.Hour, now)
	require.Error(t, err)
}

// An unreadable shard DIRECTORY (as opposed to an individual shard) is a hard
// error: it means the query cannot see the store at all, which is different from
// one torn shard being skipped.
func TestLoadAllSince_UnreadableShardDirIsError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read 000-permission directories")
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".atcr", "history")
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	mustAppendShard(t, dir, now, "aug")
	require.NoError(t, os.Chmod(dir, 0o000))
	defer func() { _ = os.Chmod(dir, 0o700) }()

	_, err := LoadAllSince(dir, LegacyLedgerPath(root), 30*24*time.Hour, now)
	require.Error(t, err)
}
