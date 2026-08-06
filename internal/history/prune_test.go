package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC7: retention is enforced at FILE granularity — whole shards past the horizon
// are removed, and nothing inside a surviving shard is touched. This is the
// opposite of the debt store's record-level fold: folding a trend ledger by id
// would destroy the history the ledger exists to report (Design Decision 2).
func TestPruneShards_RemovesWholeShardsPastHorizon(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	keep := mustAppendShard(t, dir, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), "aug")
	edge := mustAppendShard(t, dir, time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC), "jun")
	drop := mustAppendShard(t, dir, time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), "jan")
	older := mustAppendShard(t, dir, time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC), "old")

	// 90d horizon → cutoff 2026-05-22, so June survives whole and Jan/2024 go.
	res, err := PruneShards(dir, 90*24*time.Hour, now)
	require.NoError(t, err)

	assert.FileExists(t, keep)
	assert.FileExists(t, edge)
	assert.NoFileExists(t, drop)
	assert.NoFileExists(t, older)
	// assert.Equal, not ElementsMatch: the PruneResult doc promises Removed is
	// sorted ("so the report is deterministic"), and only an ordered assertion
	// pins that contract.
	assert.Equal(t, []string{"2024-03.jsonl", "2026-01.jsonl"}, res.Removed, "Removed must be sorted for a deterministic report")
	assert.Equal(t, 2, res.Kept)
}

// A shard whose month merely CONTAINS the cutoff is kept whole: it still holds
// in-horizon records, and pruning is file-granular, so removing it would delete
// data inside the retention window. Selection and pruning share the same
// month-intersection rule, so the two can never disagree about a boundary month.
func TestPruneShards_KeepsTheCutoffMonthWhole(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	// 30d horizon → cutoff 2026-07-21, inside July.
	july := mustAppendShard(t, dir, time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC), "jul")

	res, err := PruneShards(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	assert.FileExists(t, july, "the month holding the cutoff still has in-horizon days")
	assert.Empty(t, res.Removed)
}

// The legacy flat ledger is never pruned. It is one file spanning every pre-19.4
// month, so deleting it would be record-granularity pruning by proxy — exactly
// what AC7 excludes — and it is documented read-only. The adversarial case is
// the ledger INSIDE the scanned shard dir (a user moved or copied it there):
// only the unparseable-stem guard protects it, and a PruneShards that deleted
// every .jsonl it enumerated would destroy it.
func TestPruneShards_NeverTouchesLegacyLedger(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".atcr", "history")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	ancient := time.Date(2019, 1, 1, 12, 0, 0, 0, time.UTC)
	inside := filepath.Join(dir, "findings-history.jsonl")
	require.NoError(t, Append(inside, []Record{{Timestamp: ancient, ID: "l", File: "a.go"}}))
	mustAppendShard(t, dir, ancient, "ancient")

	res, err := PruneShards(dir, 30*24*time.Hour, time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.FileExists(t, inside, "a ledger file inside the shard dir survives on its unparseable stem")
	assert.NotContains(t, res.Removed, "findings-history.jsonl")
	assert.Equal(t, []string{"2019-01.jsonl"}, res.Removed, "only the genuine past-month shard is pruned")

	// Secondary: the ledger at its real location (a different directory) is
	// never even enumerated.
	legacy := LegacyLedgerPath(root)
	require.NoError(t, Append(legacy, []Record{{Timestamp: ancient, ID: "l2", File: "b.go"}}))
	_, err = PruneShards(dir, 30*24*time.Hour, time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.FileExists(t, legacy, "the legacy ledger is not a shard and is never pruned")
}

// Non-shard files in the directory are left alone: pruning only ever deletes
// files it can positively identify as an out-of-horizon monthly shard.
func TestPruneShards_LeavesNonShardFilesAlone(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	mustAppendShard(t, dir, time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC), "old")
	notes := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(notes, []byte("keep me"), 0o600))
	sub := filepath.Join(dir, "2020-01.jsonl.d")
	require.NoError(t, os.MkdirAll(sub, 0o700))

	res, err := PruneShards(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	assert.FileExists(t, notes)
	assert.DirExists(t, sub)
	assert.Equal(t, []string{"2020-01.jsonl"}, res.Removed)
}

// A DIRECTORY named exactly like a past-month shard (2020-01.jsonl/) is not a
// deletion candidate: the IsDir guard rejects it before the month test, so it
// survives with its contents and is not reported in Removed. (The
// 2020-01.jsonl.d directory above never reaches the guard — the suffix check
// rejects it first — so it cannot pin this.)
func TestPruneShards_NeverDeletesShardNamedDirectory(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	sub := filepath.Join(dir, "2020-01.jsonl")
	require.NoError(t, os.MkdirAll(sub, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "inner.txt"), []byte("keep"), 0o600))

	res, err := PruneShards(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	assert.DirExists(t, sub, "a directory is never a prune candidate, even named like a shard")
	assert.FileExists(t, filepath.Join(sub, "inner.txt"))
	assert.Empty(t, res.Removed)
}

// A *.jsonl file whose stem is not a YYYY-MM month cannot be proven past the
// horizon, so it is never deleted. Deleting on an unparseable name would make an
// unrelated file in the directory destroyable by a prune.
func TestPruneShards_NeverDeletesUnparseableStems(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	odd := filepath.Join(dir, "backup.jsonl")
	require.NoError(t, Append(odd, []Record{{Timestamp: time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), ID: "x", File: "a.go"}}))

	res, err := PruneShards(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	assert.FileExists(t, odd)
	assert.Empty(t, res.Removed)
	assert.Equal(t, 1, res.Kept)
}

// A non-positive horizon deletes NOTHING. "Retain for zero time" must not be
// spelled the same way as "delete everything": an unset or miscomputed horizon
// reaching this function is a caller bug, and the safe failure mode for a
// destructive operation is to do nothing.
func TestPruneShards_NonPositiveHorizonDeletesNothing(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	old := mustAppendShard(t, dir, time.Date(2019, 1, 1, 12, 0, 0, 0, time.UTC), "old")

	for _, h := range []time.Duration{0, -time.Hour} {
		res, err := PruneShards(dir, h, now)
		require.Error(t, err, "a non-positive horizon is a caller error, not a silent wipe")
		assert.Empty(t, res.Removed)
		assert.FileExists(t, old)
	}
}

// An absent shard dir is nothing to prune, not an error (mirrors the read path).
func TestPruneShards_AbsentDirIsNoop(t *testing.T) {
	res, err := PruneShards(filepath.Join(t.TempDir(), "nope"), 30*24*time.Hour, time.Now())
	require.NoError(t, err)
	assert.Empty(t, res.Removed)
	assert.Zero(t, res.Kept)
}

// Pruning is idempotent: a second pass over an already-pruned directory removes
// nothing more.
func TestPruneShards_Idempotent(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	mustAppendShard(t, dir, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), "aug")
	mustAppendShard(t, dir, time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC), "old")

	first, err := PruneShards(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	require.Len(t, first.Removed, 1)

	second, err := PruneShards(dir, 30*24*time.Hour, now)
	require.NoError(t, err)
	assert.Empty(t, second.Removed)
	assert.Equal(t, 1, second.Kept)
}

// What survives a prune is exactly what a query over the same window would have
// read — retention and selection agree by construction, both keying on the
// month's exclusive end.
func TestPruneShards_AgreesWithShardSelection(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	window := 90 * 24 * time.Hour
	for _, m := range []time.Time{
		time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC),
	} {
		mustAppendShard(t, dir, m, m.Format("2006-01"))
	}

	before, err := LoadShardsSince(dir, window, now)
	require.NoError(t, err)
	_, err = PruneShards(dir, window, now)
	require.NoError(t, err)
	after, err := LoadShardsSince(dir, window, now)
	require.NoError(t, err)

	assert.Equal(t, idSet(before), idSet(after), "pruning must not change what an in-window query returns")
}
