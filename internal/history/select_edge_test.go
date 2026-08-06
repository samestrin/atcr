package history

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// The cutoff landing exactly on a month boundary must not drop the month that
// starts there: the comparison is against the month's exclusive END, so a shard
// is kept while any instant of its month is at or after the cutoff.
func TestShardMonthIntersects_CutoffOnBoundary(t *testing.T) {
	julyStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	assert.True(t, shardMonthIntersects("2026-07.jsonl", julyStart), "July starts exactly at the cutoff")
	assert.False(t, shardMonthIntersects("2026-06.jsonl", julyStart), "June ends exactly at the cutoff, so holds nothing at/after it")
	assert.True(t, shardMonthIntersects("2026-08.jsonl", julyStart))
}

// A cutoff one nanosecond before a month rolls over keeps the ending month: it
// still holds that final nanosecond.
func TestShardMonthIntersects_CutoffJustBeforeRollover(t *testing.T) {
	lastInstantOfJune := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC).Add(-time.Nanosecond)
	assert.True(t, shardMonthIntersects("2026-06.jsonl", lastInstantOfJune))
}

// The cutoff is compared as an instant, so a cutoff expressed in a non-UTC zone
// selects the same shards as the identical instant in UTC. Shard names are UTC
// months (ShardPath), and a naive same-wall-clock comparison would shift the
// boundary by the offset.
func TestShardMonthIntersects_CutoffZoneIndependent(t *testing.T) {
	utc := time.Date(2026, 7, 1, 0, 30, 0, 0, time.UTC)
	west := utc.In(time.FixedZone("west", -8*60*60)) // same instant, 2026-06-30 16:30 -08:00

	for _, name := range []string{"2026-05.jsonl", "2026-06.jsonl", "2026-07.jsonl", "2026-08.jsonl"} {
		assert.Equal(t, shardMonthIntersects(name, utc), shardMonthIntersects(name, west), name)
	}
}

// Year boundaries are ordinary month arithmetic, not a special case.
func TestShardMonthIntersects_AcrossYearBoundary(t *testing.T) {
	cutoff := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	assert.True(t, shardMonthIntersects("2026-01.jsonl", cutoff))
	assert.False(t, shardMonthIntersects("2025-12.jsonl", cutoff))
	assert.False(t, shardMonthIntersects("2025-01.jsonl", cutoff))
}

// Stems that merely look month-like are treated as unparseable and therefore
// kept: selection may over-select, never under-select.
func TestShardMonthIntersects_MalformedStemsAreKept(t *testing.T) {
	cutoff := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range []string{
		"2026-13.jsonl",     // month 13 does not exist
		"2026-7.jsonl",      // unpadded month
		"backup.jsonl",      // not a date at all
		"2026-07-15.jsonl",  // a day-granular stem
		".jsonl",            // empty stem
		"2026-07.old.jsonl", // suffixed stem
	} {
		assert.True(t, shardMonthIntersects(name, cutoff), name)
	}
}
