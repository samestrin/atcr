package history

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

// Dedup keeps the FIRST occurrence, preserving input order for everything else —
// the chronological legacy-then-shards ordering LoadAll depends on.
func TestDedupeOccurrences_PreservesOrderKeepingFirst(t *testing.T) {
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	recs := []Record{
		{Timestamp: ts, ID: "x", Severity: "HIGH"},
		{Timestamp: ts, ID: "y", Severity: "LOW"},
		{Timestamp: ts, ID: "x", Severity: "MEDIUM"},
	}
	got := dedupeOccurrences(recs)
	assert.Equal(t, []string{"x", "y"}, []string{got[0].ID, got[1].ID})
	assert.Equal(t, "HIGH", got[0].Severity, "the first copy wins, not the last")
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
