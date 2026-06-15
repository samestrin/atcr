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
