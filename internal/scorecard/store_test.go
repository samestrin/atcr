package scorecard

import (
	"bytes"
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

// TestAppend_ErrorDoesNotLeakAbsoluteStorePath verifies a store-write failure is
// reported with the file base name only — the username-bearing absolute
// ~/.config/atcr path must never be embedded in an error that reaches the
// unredacted scorecard Diag sink.
func TestAppend_ErrorDoesNotLeakAbsoluteStorePath(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	// A store dir UNDER a regular file makes MkdirAll fail with a *PathError whose
	// Path is the absolute store path.
	storeDir := filepath.Join(blocker, "atcr", "scorecard")

	err := Append(storeDir, sampleRecord("2026-06-run", "rev"))
	require.Error(t, err)
	require.NotContains(t, err.Error(), tmp, "error must not embed an absolute (username-bearing) store path")
	require.Contains(t, err.Error(), "scorecard dir", "the operational context is preserved")
}

func TestStore_AppendAndRead(t *testing.T) {
	dir := t.TempDir()
	runID := "2026-06-14T10:00:00Z-abc123"

	require.NoError(t, Append(dir, sampleRecord(runID, "bruce")))
	require.NoError(t, Append(dir, sampleRecord(runID, "greta")))

	path := filepath.Join(dir, "2026-06.jsonl")
	recs, err := ReadRecords(path, ReadOpts{})
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

	recs, err := ReadRecords(path, ReadOpts{})
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

	recs, err := ReadRecords(path, ReadOpts{})
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

	recs, err := FindByRunID(dir, runID, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 2, "only the two records for the requested run_id")
	for _, r := range recs {
		assert.Equal(t, runID, r.RunID)
	}
}

func TestStore_FindByRunID_InvalidFormat(t *testing.T) {
	dir := t.TempDir()
	_, err := FindByRunID(dir, "not-a-valid-run-id", ReadOpts{})
	require.Error(t, err, "a run_id without a YYYY-MM prefix is a clear error, not empty")
}

func TestStore_FindByRunID_MissingFileNoMatch(t *testing.T) {
	dir := t.TempDir()
	recs, err := FindByRunID(dir, "2026-06-14T10:00:00Z-abc123", ReadOpts{})
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

	recs, err := FindByRunID(dir, runID, ReadOpts{})
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

	recs, err := FindByRunID(dir, runID, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 2, "records split across month files must all be returned")
}

func TestStore_FindByRunID_RejectsInvalidMonth(t *testing.T) {
	dir := t.TempDir()
	_, err := FindByRunID(dir, "2026-13-01T00:00:00Z-x", ReadOpts{})
	require.Error(t, err, "an impossible month (13) is a clear error, not an empty result")
}

func TestStore_ReadAll(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Append(dir, sampleRecord("2026-06-14T10:00:00Z-jun", "bruce")))
	require.NoError(t, Append(dir, sampleRecord("2026-07-01T00:01:00Z-jul", "greta")))
	// A non-JSONL file in the directory must be ignored.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me\n"), 0o600))

	recs, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 2, "records from every monthly JSONL file, txt ignored")
}

func TestStore_ReadAll_MissingDir(t *testing.T) {
	recs, err := ReadAll(filepath.Join(t.TempDir(), "does-not-exist"), ReadOpts{})
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

// TestStore_ReadRecords_SkipsOverLongLine locks the fix: a single line exceeding
// maxLineBytes must be logged and skipped, and reading must continue — the valid
// records both BEFORE and AFTER the oversized line are returned and no error is
// propagated. (bufio.Scanner's ErrTooLong is terminal and would drop everything
// after the big line; bufio.Reader can drain and resume.)
func TestStore_ReadRecords_SkipsOverLongLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-06.jsonl")
	good1, err := json.Marshal(sampleRecord("2026-06-14T10:00:00Z-a", "bruce"))
	require.NoError(t, err)
	good2, err := json.Marshal(sampleRecord("2026-06-14T10:00:00Z-b", "greta"))
	require.NoError(t, err)
	huge := bytes.Repeat([]byte("x"), maxLineBytes+1024) // one line over the cap

	var content []byte
	content = append(content, good1...)
	content = append(content, '\n')
	content = append(content, huge...)
	content = append(content, '\n')
	content = append(content, good2...)
	content = append(content, '\n')
	require.NoError(t, os.WriteFile(path, content, 0o600))

	recs, err := ReadRecords(path, ReadOpts{})
	require.NoError(t, err, "an over-long line must be skipped, not abort the read")
	require.Len(t, recs, 2, "valid records before AND after the over-long line are retained")
	assert.Equal(t, "bruce", recs[0].Reviewer)
	assert.Equal(t, "greta", recs[1].Reviewer)
}

// TestStore_ReadAll_OverLongLineDoesNotAbortAllMonths locks the leaderboard-level
// guarantee: one oversized line in one month file must not abort aggregation
// across every month — healthy months and the valid records after the skipped
// line in the damaged month are all returned.
func TestStore_ReadAll_OverLongLineDoesNotAbortAllMonths(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Append(dir, sampleRecord("2026-06-14T10:00:00Z-a", "bruce")))

	good, err := json.Marshal(sampleRecord("2026-07-02T10:00:00Z-b", "greta"))
	require.NoError(t, err)
	huge := bytes.Repeat([]byte("x"), maxLineBytes+1024)
	var jul []byte
	jul = append(jul, huge...)
	jul = append(jul, '\n')
	jul = append(jul, good...)
	jul = append(jul, '\n')
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-07.jsonl"), jul, 0o600))

	recs, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err, "one oversized line in one month must not abort aggregation across all months")
	require.Len(t, recs, 2, "healthy June record + valid July record after the skipped over-long line")
}

// TestStore_ReadRecords_DiagnosticsRouteToInjectedWriter locks Epic 3.4 AC1/AC2
// for the read path: a read diagnostic (here the over-long-line warning) must be
// written to the ReadOpts.Writer the caller injects, not the process-global
// os.Stderr — so it can be captured and asserted by text.
func TestStore_ReadRecords_DiagnosticsRouteToInjectedWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-06.jsonl")
	good, err := json.Marshal(sampleRecord("2026-06-14T10:00:00Z-a", "bruce"))
	require.NoError(t, err)
	huge := bytes.Repeat([]byte("x"), maxLineBytes+1024) // one line over the cap
	var content []byte
	content = append(content, huge...)
	content = append(content, '\n')
	content = append(content, good...)
	content = append(content, '\n')
	require.NoError(t, os.WriteFile(path, content, 0o600))

	var buf bytes.Buffer
	recs, err := ReadRecords(path, ReadOpts{Writer: &buf})
	require.NoError(t, err)
	require.Len(t, recs, 1, "valid record after the over-long line is still read")
	assert.Contains(t, buf.String(), "skipping over-long line",
		"read diagnostic must route to the injected ReadOpts.Writer, not os.Stderr")
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

// TestStore_ReadRecords_MalformedDiagnosticRoutesToInjectedWriter locks Epic 3.4
// for the malformed-record warning the epic explicitly names: it must reach the
// injected ReadOpts.Writer, not os.Stderr.
func TestStore_ReadRecords_MalformedDiagnosticRoutesToInjectedWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-06.jsonl")
	good, err := json.Marshal(sampleRecord("2026-06-14T10:00:00Z-a", "bruce"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(string(good)+"\n{not valid json\n"), 0o600))

	var buf bytes.Buffer
	recs, err := ReadRecords(path, ReadOpts{Writer: &buf})
	require.NoError(t, err)
	require.Len(t, recs, 1, "valid record retained, malformed skipped")
	assert.Contains(t, buf.String(), MsgMalformedSkip,
		"malformed-record diagnostic must route to ReadOpts.Writer")
}

// TestStore_ReadRecords_NilWriterDefaultsToStderr locks Epic 3.4 AC5: a zero
// ReadOpts (nil Writer) preserves prior behavior — the read still succeeds (the
// diagnostic falls back to os.Stderr) and does not panic.
func TestStore_ReadRecords_NilWriterDefaultsToStderr(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-06.jsonl")
	good, err := json.Marshal(sampleRecord("2026-06-14T10:00:00Z-a", "bruce"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(string(good)+"\n{bad\n"), 0o600))

	recs, err := ReadRecords(path, ReadOpts{}) // nil Writer → os.Stderr, must not panic
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}

// TestStore_DiagWriter_TypedNilFallsBackToStderr guards the best-effort
// diagnostics contract against a typed-nil io.Writer — a non-nil interface
// wrapping a nil pointer (e.g. a (*bytes.Buffer)(nil) handed in as io.Writer).
// `w == nil` is false for such a value, so without an explicit typed-nil guard
// diagWriter returns the nil pointer and the first Fprintf panics, crashing the
// caller's reconcile (violating "scorecard emission never fails the caller's
// reconcile").
func TestStore_DiagWriter_TypedNilFallsBackToStderr(t *testing.T) {
	var typedNil *bytes.Buffer // nil pointer, but a non-nil io.Writer interface
	if got := diagWriter(typedNil); got != os.Stderr {
		t.Fatalf("typed-nil writer must fall back to os.Stderr, got %T", got)
	}
}
