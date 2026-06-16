package scorecard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleRecord builds a minimal valid reviewer record for store tests.
func sampleRecord(runID, reviewer string) Record {
	return Record{
		SchemaVersion:  SchemaVersion,
		RecordType:     RecordTypeReviewer,
		RunID:          runID,
		Reviewer:       reviewer,
		Model:          "claude-sonnet-4-6",
		Role:           "reviewer",
		FindingsRaised: 3,
		TokensIn:       100,
		TokensOut:      50,
		LatencyMS:      1200,
	}
}

func TestStore_AppendAndRead(t *testing.T) {
	dir := t.TempDir()
	runID := "2026-06-14T10:00:00Z-abc123"

	require.NoError(t, Append(dir, sampleRecord(runID, "bruce")))
	require.NoError(t, Append(dir, sampleRecord(runID, "greta")))

	path := filepath.Join(dir, "2026-06.jsonl")
	recs, err := ReadRecords(path)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	assert.Equal(t, "bruce", recs[0].Reviewer)
	assert.Equal(t, "greta", recs[1].Reviewer)
	assert.Equal(t, SchemaVersion, recs[1].SchemaVersion)
}

func TestStore_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	runID := "2026-06-14T10:00:00Z-abc123"
	require.NoError(t, Append(dir, sampleRecord(runID, "bruce")))

	// JSONL file must be 0600 (user read/write only).
	fi, err := os.Stat(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "JSONL file must be 0600")

	// Directory created by Append (when nested) must be 0700.
	nested := filepath.Join(dir, "sub", "scorecard")
	require.NoError(t, Append(nested, sampleRecord(runID, "bruce")))
	di, err := os.Stat(nested)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), di.Mode().Perm(), "scorecard dir must be 0700")
}

func TestStore_AppendPreservesExistingLines(t *testing.T) {
	dir := t.TempDir()
	runID := "2026-06-14T10:00:00Z-abc123"
	require.NoError(t, Append(dir, sampleRecord(runID, "bruce")))

	path := filepath.Join(dir, "2026-06.jsonl")
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	require.NoError(t, Append(dir, sampleRecord(runID, "greta")))
	after, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.True(t, len(after) > len(before), "append must grow the file")
	assert.Equal(t, before, after[:len(before)], "existing bytes must be untouched")
}

func TestStore_MonthBoundaryNewFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Append(dir, sampleRecord("2026-06-30T23:59:00Z-jun", "bruce")))
	require.NoError(t, Append(dir, sampleRecord("2026-07-01T00:01:00Z-jul", "bruce")))

	assert.FileExists(t, filepath.Join(dir, "2026-06.jsonl"))
	assert.FileExists(t, filepath.Join(dir, "2026-07.jsonl"))
}

func TestStore_ReadRecords_SkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-06.jsonl")
	good, _ := json.Marshal(sampleRecord("2026-06-14T10:00:00Z-abc123", "bruce"))
	content := string(good) + "\n" + "{not valid json\n" + string(good) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	recs, err := ReadRecords(path)
	require.NoError(t, err)
	assert.Len(t, recs, 2, "malformed line skipped, valid lines retained")
}

// TestStore_ReadRecords_SkipsFutureSchemaVersion locks the schema-negotiation
// gate: a record from a future, forward-incompatible schema must be skipped with
// a warning, NOT unmarshaled into the v1 struct and silently aggregated as v1
// (which would corrupt leaderboard totals after a field rename/semantic change).
func TestStore_ReadRecords_SkipsFutureSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-06.jsonl")
	v1, err := json.Marshal(sampleRecord("2026-06-14T10:00:00Z-abc123", "bruce"))
	require.NoError(t, err)
	future := sampleRecord("2026-06-14T10:00:00Z-abc123", "greta")
	future.SchemaVersion = SchemaVersion + 1
	v2, err := json.Marshal(future)
	require.NoError(t, err)
	content := string(v1) + "\n" + string(v2) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	recs, err := ReadRecords(path)
	require.NoError(t, err)
	require.Len(t, recs, 1, "a record with schema_version > current must be skipped, not read as v1")
	assert.Equal(t, "bruce", recs[0].Reviewer)
	assert.Equal(t, SchemaVersion, recs[0].SchemaVersion)
}

// TestStore_ConcurrentAppend_SameMonthFile covers the sprint-design atomic-append
// risk: N goroutines appending to the same month file must produce intact,
// individually parseable lines with no interleaving and no lost writes.
func TestStore_ConcurrentAppend_SameMonthFile(t *testing.T) {
	dir := t.TempDir()
	runID := "2026-06-14T10:00:00Z-abc123"
	const n = 50

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			rec := sampleRecord(runID, "rev")
			rec.FindingsRaised = i // unique sentinel per record
			_ = Append(dir, rec)
		}(i)
	}
	wg.Wait()

	// Parse every raw line independently: each must be a complete, valid record
	// (no torn/interleaved lines) and the set of sentinels must be exactly 0..n-1
	// (no lost or duplicated writes).
	raw, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	lines := splitNonEmptyLines(raw)
	require.Len(t, lines, n, "every concurrent append must land as exactly one line (no lost/merged lines)")

	seen := make(map[int]bool, n)
	for _, line := range lines {
		var r Record
		require.NoError(t, json.Unmarshal(line, &r), "each line must be an intact, parseable record (no torn lines): %q", string(line))
		assert.False(t, seen[r.FindingsRaised], "sentinel %d appeared twice", r.FindingsRaised)
		seen[r.FindingsRaised] = true
	}
	for i := 0; i < n; i++ {
		assert.True(t, seen[i], "sentinel %d was lost", i)
	}
}

func TestStore_FindByRunID(t *testing.T) {
	dir := t.TempDir()
	runID := "2026-06-14T10:00:00Z-abc123"
	require.NoError(t, Append(dir, sampleRecord(runID, "bruce")))
	require.NoError(t, Append(dir, sampleRecord(runID, "greta")))
	// A different run in the same month file must NOT match.
	require.NoError(t, Append(dir, sampleRecord("2026-06-13T08:00:00Z-xyz789", "diana")))

	recs, err := FindByRunID(dir, runID)
	require.NoError(t, err)
	require.Len(t, recs, 2, "only the two records for the requested run_id")
	for _, r := range recs {
		assert.Equal(t, runID, r.RunID)
	}
}

func TestStore_FindByRunID_InvalidFormat(t *testing.T) {
	dir := t.TempDir()
	_, err := FindByRunID(dir, "not-a-valid-run-id")
	require.Error(t, err, "a run_id without a YYYY-MM prefix is a clear error, not empty")
}

func TestStore_FindByRunID_MissingFileNoMatch(t *testing.T) {
	dir := t.TempDir()
	recs, err := FindByRunID(dir, "2026-06-14T10:00:00Z-abc123")
	require.NoError(t, err, "a missing month file is 'no records', not an error")
	assert.Empty(t, recs)
}

// TestStore_FindByRunID_AdjacentMonthFallback covers AC 02-01 EC1: a run whose
// timestamp month is 2026-06 but whose record landed in 2026-07.jsonl (clock
// skew / late write) is still found by scanning adjacent month files.
func TestStore_FindByRunID_AdjacentMonthFallback(t *testing.T) {
	dir := t.TempDir()
	runID := "2026-06-30T23:59:59Z-edge"
	rec := sampleRecord(runID, "bruce")
	line, err := json.Marshal(rec)
	require.NoError(t, err)
	// Write the June run_id's record directly into the July file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-07.jsonl"), append(line, '\n'), 0o600))

	recs, err := FindByRunID(dir, runID)
	require.NoError(t, err)
	require.Len(t, recs, 1, "record found in adjacent month via fallback scan")
	assert.Equal(t, runID, recs[0].RunID)
}

// TestStore_FindByRunID_UnionAcrossMonths locks the adversarial-review fix: when
// one run's records are split across a month boundary (June primary + a late
// July write), FindByRunID must return BOTH, not just the first file's records.
func TestStore_FindByRunID_UnionAcrossMonths(t *testing.T) {
	dir := t.TempDir()
	runID := "2026-06-30T23:59:59Z-edge"
	require.NoError(t, Append(dir, sampleRecord(runID, "bruce"))) // → 2026-06.jsonl
	line, err := json.Marshal(sampleRecord(runID, "greta"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-07.jsonl"), append(line, '\n'), 0o600))

	recs, err := FindByRunID(dir, runID)
	require.NoError(t, err)
	require.Len(t, recs, 2, "records split across month files must all be returned")
}

func TestStore_FindByRunID_RejectsInvalidMonth(t *testing.T) {
	dir := t.TempDir()
	_, err := FindByRunID(dir, "2026-13-01T00:00:00Z-x")
	require.Error(t, err, "an impossible month (13) is a clear error, not an empty result")
}

func TestStore_ReadAll(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Append(dir, sampleRecord("2026-06-14T10:00:00Z-jun", "bruce")))
	require.NoError(t, Append(dir, sampleRecord("2026-07-01T00:01:00Z-jul", "greta")))
	// A non-JSONL file in the directory must be ignored.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me\n"), 0o600))

	recs, err := ReadAll(dir)
	require.NoError(t, err)
	require.Len(t, recs, 2, "records from every monthly JSONL file, txt ignored")
}

func TestStore_ReadAll_MissingDir(t *testing.T) {
	recs, err := ReadAll(filepath.Join(t.TempDir(), "does-not-exist"))
	require.NoError(t, err, "a missing store directory is empty, not an error")
	assert.Empty(t, recs)
}

// TestStore_monthsToScan_InvalidDayNoBoundaryScan locks the fix: the day that
// drives boundary scanning must come from a real parsed timestamp, not fixed
// offset slicing. An impossible calendar day (Feb 30) must not be read off as a
// boundary day and trigger a spurious adjacent-month scan.
func TestStore_monthsToScan_InvalidDayNoBoundaryScan(t *testing.T) {
	got := monthsToScan("2026-02-30T10:00:00Z-x", "2026-02")
	require.Len(t, got, 1, "an impossible calendar day must not trigger adjacent-month scanning")
	assert.Equal(t, "2026-02", got[0])
}

// TestStore_monthsToScan_ValidBoundaryStillScans is the regression guard: real
// boundary days (1st, 28th-31st) still pull in the neighbouring month, and a
// mid-month day stays a single-file read.
func TestStore_monthsToScan_ValidBoundaryStillScans(t *testing.T) {
	require.Len(t, monthsToScan("2026-06-30T23:59:59Z-x", "2026-06"), 2, "last-day run scans next month")
	require.Len(t, monthsToScan("2026-06-01T00:00:00Z-x", "2026-06"), 2, "first-day run scans prev month")
	require.Len(t, monthsToScan("2026-06-15T12:00:00Z-x", "2026-06"), 1, "mid-month run stays single-file")
}

// splitNonEmptyLines splits raw JSONL bytes into trimmed non-empty lines so a
// torn line (missing newline / merged with another) is observable as a parse
// failure or a wrong line count, not silently absorbed.
func splitNonEmptyLines(raw []byte) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == '\n' {
			line := raw[start:i]
			if len(line) > 0 {
				out = append(out, line)
			}
			start = i + 1
		}
	}
	return out
}
