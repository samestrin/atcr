package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC1: a shard is named by the run's calendar year-month.
func TestShardPath_MonthlyNaming(t *testing.T) {
	dir := filepath.Join("x", "history")
	ts := time.Date(2026, 7, 5, 19, 59, 0, 0, time.UTC)
	assert.Equal(t, filepath.Join(dir, "2026-07.jsonl"), ShardPath(dir, ts))
}

// The shard month is taken in UTC so shard names are deterministic regardless of
// the caller's local zone: a ts just past midnight in a +02:00 zone still belongs
// to the prior UTC month and must not split into an off-by-one shard.
func TestShardPath_UsesUTCMonth(t *testing.T) {
	dir := filepath.Join("x", "history")
	loc := time.FixedZone("east", 2*60*60)
	// 2026-08-01 00:30 +02:00 == 2026-07-31 22:30 UTC → July shard.
	ts := time.Date(2026, 8, 1, 0, 30, 0, 0, loc)
	assert.Equal(t, filepath.Join(dir, "2026-07.jsonl"), ShardPath(dir, ts))
}

// AC3: once the month rolls over, writes land in a new shard file and the prior
// month's shard is never reopened — so old shards stop producing new git blobs.
func TestShardPath_RolloverWritesSeparateFiles(t *testing.T) {
	dir := t.TempDir()
	july := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	aug := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)

	require.NoError(t, Append(ShardPath(dir, july), []Record{{Timestamp: july, ID: "a", File: "x.go"}}))
	require.NoError(t, Append(ShardPath(dir, aug), []Record{{Timestamp: aug, ID: "b", File: "y.go"}}))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.ElementsMatch(t, []string{"2026-07.jsonl", "2026-08.jsonl"}, names)

	// The July shard holds only the July record — the August write never reopened it.
	julyRecs, err := Load(ShardPath(dir, july))
	require.NoError(t, err)
	require.Len(t, julyRecs, 1)
	assert.Equal(t, "a", julyRecs[0].ID)
}

// AC2: a query loads across every monthly shard without the caller naming one.
func TestLoadShards_MergesAllMonthlyFiles(t *testing.T) {
	dir := t.TempDir()
	july := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	aug := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, Append(ShardPath(dir, july), []Record{
		{Timestamp: july, ID: "j1", File: "a.go", Package: "a", Severity: "HIGH"},
	}))
	require.NoError(t, Append(ShardPath(dir, aug), []Record{
		{Timestamp: aug, ID: "a1", File: "b.go", Package: "b", Severity: "LOW"},
		{Timestamp: aug, ID: "a2", File: "c.go", Package: "c", Severity: "MEDIUM"},
	}))

	recs, err := LoadShards(dir)
	require.NoError(t, err)
	require.Len(t, recs, 3)
	ids := map[string]bool{}
	for _, r := range recs {
		ids[r.ID] = true
	}
	assert.Equal(t, map[string]bool{"j1": true, "a1": true, "a2": true}, ids)
}

// An absent shard dir is a valid empty history, not an error (mirrors Load).
func TestLoadShards_AbsentDirIsEmptyNotError(t *testing.T) {
	recs, err := LoadShards(filepath.Join(t.TempDir(), "does-not-exist"))
	require.NoError(t, err)
	assert.Empty(t, recs)
}

// Only *.jsonl files are shards; unrelated files in the dir are ignored.
func TestLoadShards_IgnoresNonJSONLFiles(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, Append(ShardPath(dir, ts), []Record{{Timestamp: ts, ID: "x", File: "a.go"}}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("not a shard"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("garbage"), 0o644))

	recs, err := LoadShards(dir)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "x", recs[0].ID)
}

// Legacy migration: LoadAll returns monthly shards merged with the pre-19.4 flat
// ledger, so history that accrued before sharding stays queryable.
func TestLoadAll_MergesShardsAndLegacy(t *testing.T) {
	root := t.TempDir()
	shardDir := filepath.Join(root, ".planning", "history")
	legacyPath := filepath.Join(root, ".atcr", "findings-history.jsonl")

	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, Append(ShardPath(shardDir, ts), []Record{{Timestamp: ts, ID: "shard1", File: "a.go"}}))
	require.NoError(t, Append(legacyPath, []Record{{Timestamp: ts, ID: "legacy1", File: "b.go"}}))

	recs, err := LoadAll(shardDir, legacyPath)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	ids := map[string]bool{}
	for _, r := range recs {
		ids[r.ID] = true
	}
	assert.Equal(t, map[string]bool{"shard1": true, "legacy1": true}, ids)
	// Chronological ordering: the legacy ledger (pre-19.4) precedes monthly shards.
	assert.Equal(t, "legacy1", recs[0].ID)
	assert.Equal(t, "shard1", recs[1].ID)
}

// Absent shard dir and absent legacy file is a valid empty history.
func TestLoadAll_AbsentBothIsEmpty(t *testing.T) {
	root := t.TempDir()
	recs, err := LoadAll(filepath.Join(root, ".planning", "history"), filepath.Join(root, ".atcr", "findings-history.jsonl"))
	require.NoError(t, err)
	assert.Empty(t, recs)
}

// The legacy ledger is read in place and read-only — LoadAll must never rewrite
// it (Epic 19.4: "no complex write-migrations will be performed").
func TestLoadAll_DoesNotMutateLegacyFile(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, ".atcr", "findings-history.jsonl")
	ts := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, Append(legacyPath, []Record{{Timestamp: ts, ID: "legacy1", File: "b.go"}}))
	before, err := os.ReadFile(legacyPath)
	require.NoError(t, err)

	_, err = LoadAll(filepath.Join(root, ".planning", "history"), legacyPath)
	require.NoError(t, err)

	after, err := os.ReadFile(legacyPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "legacy ledger must be read-only")
}
