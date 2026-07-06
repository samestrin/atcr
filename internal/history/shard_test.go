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
