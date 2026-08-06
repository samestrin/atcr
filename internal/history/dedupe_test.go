package history

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Records whose "ts" was missing or unparseable decode to the zero time. Two
// such records with different ids are still distinct occurrences and must both
// survive; two with the same id are the same occurrence.
func TestDedupeOccurrences_ZeroTimestamps(t *testing.T) {
	recs := []Record{
		{ID: "a", File: "a.go"},
		{ID: "b", File: "b.go"},
		{ID: "a", File: "a.go"},
	}
	got := dedupeOccurrences(recs)
	assert.Len(t, got, 2)
	assert.Equal(t, "a", got[0].ID)
	assert.Equal(t, "b", got[1].ID)
}

// The instant is keyed as (seconds, nanoseconds), NOT UnixNano (shard.go):
// UnixNano is undefined outside 1678-2262, and the zero time (what a missing or
// malformed "ts" decodes to) wraps to the same int64 as a real in-range date —
// 1754-08-30T22:43:41.128654848Z here. A UnixNano-keyed collapse would merge
// these two records into one occurrence; the (sec, nsec) key must keep both.
func TestDedupeOccurrences_ZeroTimeDoesNotAliasWrappedUnixNano(t *testing.T) {
	wrapped := time.Date(1754, 8, 30, 22, 43, 41, 128654848, time.UTC)
	var zero time.Time
	require.Equal(t, zero.UnixNano(), wrapped.UnixNano(),
		"precondition: these instants collide under UnixNano keying")
	recs := []Record{
		{Timestamp: zero, ID: "a", File: "a.go"},
		{Timestamp: wrapped, ID: "a", File: "a.go"},
	}
	assert.Len(t, dedupeOccurrences(recs), 2,
		"instants that merely share a wrapped UnixNano are distinct occurrences")
}

// A surviving record keeps the FIRST copy's position, preserving input order —
// the chronological legacy-then-shards ordering LoadAll depends on. (Its
// severity is separately promoted to the highest seen; see
// TestDedupeOccurrences_KeepsHighestSeverityOnDuplicate.)
func TestDedupeOccurrences_PreservesOrder(t *testing.T) {
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	recs := []Record{
		{Timestamp: ts, ID: "x", Severity: "HIGH"},
		{Timestamp: ts, ID: "y", Severity: "LOW"},
		{Timestamp: ts, ID: "x", Severity: "MEDIUM"},
	}
	got := dedupeOccurrences(recs)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"x", "y"}, []string{got[0].ID, got[1].ID},
		"the duplicate collapses into the first copy's slot, not the last")
}

// Sub-second precision is part of the key: two runs a millisecond apart are two
// occurrences.
func TestDedupeOccurrences_SubSecondPrecision(t *testing.T) {
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	recs := []Record{
		{Timestamp: base, ID: "x"},
		{Timestamp: base.Add(time.Millisecond), ID: "x"},
	}
	assert.Len(t, dedupeOccurrences(recs), 2)
}

// A resumed review re-records the whole pool stamped with the ORIGINAL run's
// start time, so the same (ts, id) legitimately appears twice with a severity
// the fuller run re-settled. The collapse must keep the highest severity — the
// same rule RecordReview applies within a run — rather than letting file read
// order pick the reported value.
func TestDedupeOccurrences_KeepsHighestSeverityOnDuplicate(t *testing.T) {
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	lowFirst := dedupeOccurrences([]Record{
		{Timestamp: ts, ID: "x", Severity: "LOW"},
		{Timestamp: ts, ID: "x", Severity: "HIGH"},
	})
	require.Len(t, lowFirst, 1)
	assert.Equal(t, "HIGH", lowFirst[0].Severity)

	highFirst := dedupeOccurrences([]Record{
		{Timestamp: ts, ID: "x", Severity: "HIGH"},
		{Timestamp: ts, ID: "x", Severity: "LOW"},
	})
	require.Len(t, highFirst, 1)
	assert.Equal(t, "HIGH", highFirst[0].Severity, "read order must not decide the severity")
}

// An id-less record — a malformed line that still parsed as JSON — has no
// identity to match on. Keying it as (ts, "") would collapse every id-less
// record from one run into a single row, which the plain concatenation this
// replaced never did.
func TestDedupeOccurrences_NeverCollapsesEmptyIDs(t *testing.T) {
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	recs := []Record{
		{Timestamp: ts, File: "a.go"},
		{Timestamp: ts, File: "b.go"},
		{Timestamp: ts, File: "c.go"},
	}
	got := dedupeOccurrences(recs)
	require.Len(t, got, 3, "id-less records are distinct occurrences, not duplicates")
	assert.Equal(t, []string{"a.go", "b.go", "c.go"}, []string{got[0].File, got[1].File, got[2].File})
}

// The other fields of a surviving record are untouched by the severity merge:
// only Severity is promoted.
func TestDedupeOccurrences_MergeTouchesOnlySeverity(t *testing.T) {
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	got := dedupeOccurrences([]Record{
		{Timestamp: ts, ID: "x", Severity: "LOW", File: "first.go", Package: "first", Category: "A"},
		{Timestamp: ts, ID: "x", Severity: "CRITICAL", File: "second.go", Package: "second", Category: "B"},
	})
	require.Len(t, got, 1)
	assert.Equal(t, "CRITICAL", got[0].Severity)
	assert.Equal(t, "first.go", got[0].File)
	assert.Equal(t, "first", got[0].Package)
	assert.Equal(t, "A", got[0].Category)
}
