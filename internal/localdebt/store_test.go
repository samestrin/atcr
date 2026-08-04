package localdebt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleRecord builds a minimal valid current-schema record for store tests
// (SchemaVersion is taken from the constant, so it tracks the bump), populating every
// required field with a plausible value plus the optional justification/source
// block so round-trip equivalence exercises the optional fields too.
func sampleRecord(runID string) Record {
	rec := Record{
		SchemaVersion: SchemaVersion,
		RunID:         runID,
		Timestamp:     "2026-06-14T10:00:00Z",
		Severity:      "HIGH",
		File:          "internal/scorecard/store.go",
		Line:          89,
		Problem:       "(Append) Concurrent writers may tear JSONL lines",
		Fix:           "Issue exactly one os.Write per record under O_APPEND",
		Category:      "correctness",
		EstMinutes:    30,
		Evidence:      "Scorecard comment notes POSIX atomic-append guarantee",
		Reviewers:     []string{"bruce", "host"},
		Confidence:    "HIGH",
		Justification: "One record marshaled and written in a single Write call.",
		SourceReport: &SourceReport{
			Path:    "sources/bruce/review.md",
			Line:    42,
			Section: "Concurrency concerns",
		},
	}
	rec.StampID()
	return rec
}

// TestStore_AppendAndRead locks AC 01-01 Scenario 1: an appended record reads back
// byte-for-byte equivalent, including optional fields.
func TestStore_AppendAndRead(t *testing.T) {
	dir := t.TempDir()
	runID := "2026-06-14T10:00:00Z-abc123"
	rec := sampleRecord(runID)

	require.NoError(t, Append(dir, rec))

	path := filepath.Join(dir, "2026-06.jsonl")
	recs, err := ReadRecords(path, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, rec, recs[0], "record must round-trip byte-for-byte, including optional fields")
}

// TestStore_AppendTwice locks AC 01-01 Scenario 2: two separate appends produce two
// independently parseable lines in order.
func TestStore_AppendTwice(t *testing.T) {
	dir := t.TempDir()
	runID := "2026-06-14T10:00:00Z-abc123"
	a := sampleRecord(runID)
	b := sampleRecord(runID)
	b.Problem = "a different problem"
	b.StampID()

	require.NoError(t, Append(dir, a))
	require.NoError(t, Append(dir, b))

	recs, err := ReadRecords(filepath.Join(dir, "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 2)
	assert.Equal(t, a.Problem, recs[0].Problem)
	assert.Equal(t, b.Problem, recs[1].Problem)
}

// TestStore_AppendPreservesExistingLines confirms append never rewrites prior bytes.
func TestStore_AppendPreservesExistingLines(t *testing.T) {
	dir := t.TempDir()
	runID := "2026-06-14T10:00:00Z-abc123"
	require.NoError(t, Append(dir, sampleRecord(runID)))

	path := filepath.Join(dir, "2026-06.jsonl")
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	next := sampleRecord(runID)
	next.Problem = "second"
	next.StampID()
	require.NoError(t, Append(dir, next))
	after, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.Greater(t, len(after), len(before))
	assert.Equal(t, before, after[:len(before)], "existing bytes must be untouched")
}

// TestStore_FilePermissions locks AC 01-01 Security: dir 0700, file 0600.
func TestStore_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "sub", "debt")
	require.NoError(t, Append(nested, sampleRecord("2026-06-14T10:00:00Z-abc123")))

	fi, err := os.Stat(filepath.Join(nested, "2026-06.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "JSONL file must be 0600")

	di, err := os.Stat(nested)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), di.Mode().Perm(), "debt dir must be 0700")
}

// TestStore_MonthBoundaryNewFile locks AC 01-01 Edge Case 3: run_ids spanning a
// month boundary produce two shard files.
func TestStore_MonthBoundaryNewFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Append(dir, sampleRecord("2026-06-30T23:59:00Z-jun")))
	require.NoError(t, Append(dir, sampleRecord("2026-07-01T00:01:00Z-jul")))

	assert.FileExists(t, filepath.Join(dir, "2026-06.jsonl"))
	assert.FileExists(t, filepath.Join(dir, "2026-07.jsonl"))
}

// --- Plan 35.13 T1: ManualRunID + schema v3 round trips -------------------

// TestManualRunID_HasResolvableMonthPrefix locks AC2 (shard half): a manual add's
// synthetic run_id carries the YYYY-MM prefix monthFromRunID requires, so a
// manually-filed item lands in the correct month shard rather than failing the
// append outright. The second case pins the .UTC() normalization: a local instant
// just past midnight on the 1st belongs to the PREVIOUS month once normalized.
func TestManualRunID_HasResolvableMonthPrefix(t *testing.T) {
	fixed := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	month, err := monthFromRunID(ManualRunID(fixed))
	require.NoError(t, err, "a ManualRunID must always resolve to a month shard")
	assert.Equal(t, "2026-06", month)

	// 2026-07-01T00:30:00+02:00 is 2026-06-30T22:30:00Z — the June shard.
	crossing := time.Date(2026, 7, 1, 0, 30, 0, 0, time.FixedZone("UTC+2", 2*60*60))
	month, err = monthFromRunID(ManualRunID(crossing))
	require.NoError(t, err)
	assert.Equal(t, "2026-06", month,
		"ManualRunID normalizes to UTC, so a local-time month boundary does not misfile the shard")
}

// TestManualRunID_SuffixDistinguishesManual locks the provenance-legibility half:
// a manual entry's run_id is visibly distinct from a reconcile run_id
// (<RFC3339>-<review-dir base>) in the raw JSONL, not only in the origin field.
func TestManualRunID_SuffixDistinguishesManual(t *testing.T) {
	id := ManualRunID(time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC))
	assert.Equal(t, "2026-06-14T10:00:00Z-manual", id)
	assert.True(t, strings.HasSuffix(id, "-manual"),
		"the -manual suffix marks provenance in the raw JSONL")
}

// TestStore_AppendReadAll_V3RoundTrip locks AC2/AC4 end to end: a v3 record built
// from ManualRunID survives an Append -> ReadAll cycle with all three new fields
// intact, in the month shard ManualRunID implies.
func TestStore_AppendReadAll_V3RoundTrip(t *testing.T) {
	dir := t.TempDir()
	fixed := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)

	rec := sampleRecord(ManualRunID(fixed))
	rec.Origin = OriginManual
	rec.Occurrences = 3
	rec.FirstSeen = "2026-01-02T03:04:05Z"

	require.NoError(t, Append(dir, rec))
	assert.FileExists(t, filepath.Join(dir, "2026-06.jsonl"),
		"a ManualRunID lands in the month shard its UTC prefix implies")

	recs, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, rec, recs[0], "every v3 field must survive the JSONL round trip")
	assert.Equal(t, OriginManual, recs[0].EffectiveOrigin())

	// Pin the wire format, not just the decoded struct: a struct-level compare
	// cannot catch a tag rename, and comparing recs[0].SchemaVersion to the
	// constant is tautological because sampleRecord seeds it from that constant.
	raw, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), fmt.Sprintf(`"schema_version":%d`, SchemaVersion),
		"the record is stamped with the current schema version on disk")
	assert.Contains(t, string(raw), `"origin":"manual"`)
	assert.Contains(t, string(raw), `"occurrences":3`)
	assert.Contains(t, string(raw), `"first_seen":"2026-01-02T03:04:05Z"`)
}

// TestStore_ReadAll_MixedSchemaVersions proves the v2->v3 bump widened
// comprehension without loosening the forward-incompatible gate: v1, v2, and v3
// lines all decode, in file order, while the next-version line is skipped and
// warned about. The future line is derived from SchemaVersion+1 so it stays one
// version ahead across bumps instead of quietly becoming a supported version.
func TestStore_ReadAll_MixedSchemaVersions(t *testing.T) {
	dir := t.TempDir()
	lines := strings.Join([]string{
		`{"schema_version":1,"id":"v1","run_id":"2026-06-01T00:00:00Z-a","ts":"2026-06-01T00:00:00Z","severity":"HIGH","file":"a.go","line":1,"problem":"p1"}`,
		`{"schema_version":2,"id":"v2","run_id":"2026-06-02T00:00:00Z-b","ts":"2026-06-02T00:00:00Z","severity":"HIGH","file":"b.go","line":2,"problem":"p2","model":"claude-sonnet-4-6"}`,
		`{"schema_version":3,"id":"v3","run_id":"2026-06-03T00:00:00Z-c","ts":"2026-06-03T00:00:00Z","severity":"HIGH","file":"c.go","line":3,"problem":"p3","origin":"manual","occurrences":2,"first_seen":"2026-05-01T00:00:00Z"}`,
		fmt.Sprintf(`{"schema_version":%d,"id":"vNext","run_id":"2026-06-04T00:00:00Z-d","ts":"2026-06-04T00:00:00Z","severity":"HIGH","file":"d.go","line":4,"problem":"p4"}`, SchemaVersion+1),
	}, "\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-06.jsonl"), []byte(lines+"\n"), 0o600))

	var diag bytes.Buffer
	recs, err := ReadAll(dir, ReadOpts{Writer: &diag})
	require.NoError(t, err)
	require.Len(t, recs, 3, "v1, v2 and v3 decode; the next-version line is skipped")
	assert.Equal(t, []string{"v1", "v2", "v3"}, []string{recs[0].ID, recs[1].ID, recs[2].ID},
		"records return in file order")

	assert.Equal(t, OriginReview, recs[0].EffectiveOrigin(), "a v1 record defaults to review")
	assert.Equal(t, OriginReview, recs[1].EffectiveOrigin(), "a v2 record defaults to review")
	assert.Equal(t, OriginManual, recs[2].Origin)
	assert.Equal(t, 2, recs[2].Occurrences)
	assert.Equal(t, "2026-05-01T00:00:00Z", recs[2].FirstSeen)

	assert.Contains(t, diag.String(), fmt.Sprintf("unsupported schema_version %d", SchemaVersion+1),
		"the forward-incompatible line is still skipped with a warning")
}

// TestStore_Append_InvalidRunID locks AC 01-01 Error Scenario 1.
func TestStore_Append_InvalidRunID(t *testing.T) {
	dir := t.TempDir()
	rec := sampleRecord("2026-06-14T10:00:00Z-abc123")
	rec.RunID = "not-a-run-id"

	err := Append(dir, rec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `cannot derive month from run_id "not-a-run-id"`)
}

// TestStore_Append_ErrorDoesNotLeakAbsolutePath locks AC 01-01 Error Scenario 2:
// a MkdirAll failure is reported with the base name only, not the absolute path.
func TestStore_Append_ErrorDoesNotLeakAbsolutePath(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	storeDir := filepath.Join(blocker, "debt")

	err := Append(storeDir, sampleRecord("2026-06-14T10:00:00Z-abc123"))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), tmp, "error must not embed an absolute (username-bearing) path")
	assert.Contains(t, err.Error(), "localdebt dir", "operational context preserved")
}

// TestStore_ReadAll aggregates every month shard and ignores non-.jsonl files.
func TestStore_ReadAll(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Append(dir, sampleRecord("2026-06-14T10:00:00Z-jun")))
	require.NoError(t, Append(dir, sampleRecord("2026-07-01T00:01:00Z-jul")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore\n"), 0o600))

	recs, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 2, "records from every month shard, non-JSONL ignored")
}

// TestStore_ReadAll_MissingDir locks AC 01-01 Edge Case 2: missing dir → (nil,nil).
func TestStore_ReadAll_MissingDir(t *testing.T) {
	recs, err := ReadAll(filepath.Join(t.TempDir(), "does-not-exist"), ReadOpts{})
	require.NoError(t, err, "a missing store directory is empty, not an error")
	assert.Nil(t, recs)
}

// TestStore_ReadAll_ShardErrorDoesNotLeakAbsolutePath locks the redaction posture on
// the per-shard read-error path: a non-ENOENT shard open failure (EACCES) surfaced
// through ReadAll must be reduced to its base name, matching the ReadDir branch
// (store.go basePathErr) and the write path (Append). A genuinely missing shard is
// still distinguishable via os.IsNotExist because ReadAll's own ENOENT check runs on
// the raw error before any wrapping.
func TestStore_ReadAll_ShardErrorDoesNotLeakAbsolutePath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a permission-denied open cannot be provoked as root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-06.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o600))
	require.NoError(t, os.Chmod(path, 0o000)) // unreadable → os.Open fails with EACCES
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err := ReadAll(dir, ReadOpts{})
	require.Error(t, err)
	assert.False(t, os.IsNotExist(err), "EACCES is a real failure, not a missing-file signal")
	assert.NotContains(t, err.Error(), dir,
		"a non-ENOENT shard error must not embed the absolute (username-bearing) store path")
}

// TestStore_ReadRecords_SkipsMalformedLines locks AC 01-03 Edge Case 1.
func TestStore_ReadRecords_SkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-06.jsonl")
	good, _ := json.Marshal(sampleRecord("2026-06-14T10:00:00Z-abc123"))
	content := string(good) + "\n{not valid json\n" + string(good) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	var buf bytes.Buffer
	recs, err := ReadRecords(path, ReadOpts{Writer: &buf})
	require.NoError(t, err)
	assert.Len(t, recs, 2, "malformed line skipped, valid lines retained")
	assert.Contains(t, buf.String(), MsgMalformedSkip)
}

// TestStore_ReadRecords_SkipsFutureSchemaVersion locks AC 01-03 Edge Case 2.
func TestStore_ReadRecords_SkipsFutureSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-06.jsonl")
	v1, _ := json.Marshal(sampleRecord("2026-06-14T10:00:00Z-abc123"))
	future := sampleRecord("2026-06-14T10:00:00Z-abc123")
	future.SchemaVersion = SchemaVersion + 1
	v2, _ := json.Marshal(future)
	require.NoError(t, os.WriteFile(path, []byte(string(v1)+"\n"+string(v2)+"\n"), 0o600))

	var buf bytes.Buffer
	recs, err := ReadRecords(path, ReadOpts{Writer: &buf})
	require.NoError(t, err)
	require.Len(t, recs, 1, "future-schema record must be skipped, not read as v1")
	assert.Equal(t, SchemaVersion, recs[0].SchemaVersion)
	assert.Contains(t, buf.String(), "schema_version")
}

// TestStore_ReadRecords_SkipsOverLongLine locks AC 01-03 Edge Case 3.
func TestStore_ReadRecords_SkipsOverLongLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-06.jsonl")
	good1, _ := json.Marshal(sampleRecord("2026-06-14T10:00:00Z-a"))
	good2, _ := json.Marshal(sampleRecord("2026-06-14T10:00:00Z-b"))
	huge := bytes.Repeat([]byte("x"), maxLineBytes+1024)

	var content []byte
	content = append(content, good1...)
	content = append(content, '\n')
	content = append(content, huge...)
	content = append(content, '\n')
	content = append(content, good2...)
	content = append(content, '\n')
	require.NoError(t, os.WriteFile(path, content, 0o600))

	var buf bytes.Buffer
	recs, err := ReadRecords(path, ReadOpts{Writer: &buf})
	require.NoError(t, err, "an over-long line must be skipped, not abort the read")
	require.Len(t, recs, 2, "valid records before AND after the over-long line are retained")
	assert.Contains(t, buf.String(), "over-long line")
}

// TestStore_ReadRecords_SkipsStructurallyValidButEmptyRecords locks the malformed-
// skip contract for inputs that json.Unmarshal accepts but lack the minimal
// identity fields required by the v1 schema (RunID and ID). A literal null, an
// empty object, or an unrelated object must not surface as a phantom record.
func TestStore_ReadRecords_SkipsStructurallyValidButEmptyRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-06.jsonl")
	good, _ := json.Marshal(sampleRecord("2026-06-14T10:00:00Z-abc123"))
	content := string(good) + "\nnull\n{}\n{\"foo\":1}\n" + string(good) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	var buf bytes.Buffer
	recs, err := ReadRecords(path, ReadOpts{Writer: &buf})
	require.NoError(t, err)
	assert.Len(t, recs, 2, "only the two valid records are retained")
	assert.Contains(t, buf.String(), MsgMalformedSkip, "empty/unrelated lines are reported as malformed")
}

// TestStore_ReadRecords_MissingFile locks AC 01-03 Error Scenario 1: a missing file
// surfaces the raw os error so callers can use os.IsNotExist.
func TestStore_ReadRecords_MissingFile(t *testing.T) {
	_, err := ReadRecords(filepath.Join(t.TempDir(), "2026-06.jsonl"), ReadOpts{})
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err), "a genuinely missing file must surface as the raw os error")
}

// TestStore_ReadRecords_NilWriterDoesNotPanic locks AC 01-03 Edge Case 5.
func TestStore_ReadRecords_NilWriterDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-06.jsonl")
	good, _ := json.Marshal(sampleRecord("2026-06-14T10:00:00Z-a"))
	require.NoError(t, os.WriteFile(path, []byte(string(good)+"\n{bad\n"), 0o600))

	recs, err := ReadRecords(path, ReadOpts{}) // nil Writer → os.Stderr, must not panic
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}

// TestStore_ConcurrentAppend_SameMonthFile locks AC 01-04 Scenario 1: 50 goroutines
// appending to one shard produce 50 intact, individually parseable lines with no
// torn/lost/duplicated writes. Run under -race.
func TestStore_ConcurrentAppend_SameMonthFile(t *testing.T) {
	dir := t.TempDir()
	runID := "2026-06-14T10:00:00Z-abc123"
	const n = 50

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			rec := sampleRecord(runID)
			rec.EstMinutes = i // unique sentinel per record
			_ = Append(dir, rec)
		}(i)
	}
	wg.Wait()

	raw, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	lines := bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n"))
	require.Len(t, lines, n, "every concurrent append must land as exactly one line")

	seen := make(map[int]bool, n)
	for _, line := range lines {
		var r Record
		require.NoError(t, json.Unmarshal(line, &r), "each line must be an intact record: %q", string(line))
		assert.False(t, seen[r.EstMinutes], "sentinel %d appeared twice", r.EstMinutes)
		seen[r.EstMinutes] = true
	}
	for i := 0; i < n; i++ {
		assert.True(t, seen[i], "sentinel %d was lost", i)
	}
}

// TestCompact locks AC1, AC2, AC3, AC4, AC5:
// - AC1: folds append-only store by id, drops superseded records.
// - AC2: bounded on-disk size after compaction.
// - AC3: concurrency-safe rewrite.
// - AC4: compacted shards remain readable.
// - AC5: effective open backlog is identical.
func TestCompact(t *testing.T) {
	dir := t.TempDir()

	// 1. Create a few open findings.
	rec1 := sampleRecord("2026-06-14T10:00:00Z-a")
	rec1.Problem = "finding 1"
	rec1.StampID()

	rec2 := sampleRecord("2026-06-15T10:00:00Z-b")
	rec2.Problem = "finding 2"
	rec2.StampID()

	require.NoError(t, Append(dir, rec1))
	require.NoError(t, Append(dir, rec2))

	// 2. Add multiple resolve/wontfix cycles of finding 1 (simulating churn/history).
	now := "2026-06-16T10:00:00Z"
	for i := 0; i < 5; i++ {
		resolved := rec1
		resolved.RunID = fmt.Sprintf("2026-06-16T10:0%d:00Z-resolved", i)
		resolved.Timestamp = now
		resolved.Status = "resolved"
		resolved.ResolvedAt = now
		require.NoError(t, Append(dir, resolved))
	}

	// 3. Keep finding 2 open but update it once (simulating a drift/manual append).
	drifted := rec2
	drifted.Line = 100
	drifted.StampID() // new ID due to line drift
	require.NoError(t, Append(dir, drifted))

	// Check file size / count before compaction
	path := filepath.Join(dir, "2026-06.jsonl")
	beforeBytes, err := os.ReadFile(path)
	require.NoError(t, err)
	beforeRecs, err := ReadRecords(path, ReadOpts{})
	require.NoError(t, err)
	// We appended: rec1, rec2, 5x resolved, drifted = 8 records.
	require.Len(t, beforeRecs, 8)

	// Keep a copy of the open backlog before compaction
	openBefore := FoldRecords(beforeRecs)

	// 4. Run compaction.
	_, err = Compact(dir, ReadOpts{})
	require.NoError(t, err)

	// Check file size / count after compaction
	afterBytes, err := os.ReadFile(path)
	require.NoError(t, err)
	afterRecs, err := ReadRecords(path, ReadOpts{})
	require.NoError(t, err)

	// After compaction, we should only have:
	// - the highest-precedence terminal record for rec1 (1 record).
	// - the original rec2 (1 record) - wait, it has ID from rec2.
	// - the drifted rec2 (1 record) - it has a different ID.
	// Total = 3 records.
	assert.Len(t, afterRecs, 3)
	assert.Less(t, len(afterBytes), len(beforeBytes), "compacted size must be smaller")

	// AC5: Verify open backlog is identical
	openAfter := FoldRecords(afterRecs)
	assert.Equal(t, len(openBefore), len(openAfter))

	// AC3: Test concurrency safety by running concurrent Appends and Compacts
	const workers = 10
	var wg sync.WaitGroup
	wg.Add(workers * 2)

	for i := 0; i < workers; i++ {
		// Appenders
		go func(i int) {
			defer wg.Done()
			r := sampleRecord("2026-06-17T10:00:00Z-c")
			r.Problem = fmt.Sprintf("concurrent finding %d", i)
			r.StampID()
			_ = Append(dir, r)
		}(i)

		// Compacters
		go func() {
			defer wg.Done()
			_, _ = Compact(dir, ReadOpts{})
		}()
	}
	wg.Wait()

	// Verify we can still read all records cleanly without corruption
	finalRecs, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	assert.NotEmpty(t, finalRecs)
}

// --- Plan 35.13 TD-004: compaction must not destroy newer-schema records ----

// futureLine builds a raw JSONL line one schema version ahead of this binary —
// structurally valid, but forward-incompatible, so decodeRecord skips it. Derived
// from SchemaVersion so it stays one ahead across bumps.
func futureLine(id, runID string) string {
	return fmt.Sprintf(
		`{"schema_version":%d,"id":%q,"run_id":%q,"ts":"2026-06-20T10:00:00Z","severity":"HIGH","file":"future.go","line":7,"problem":"written by a newer binary","unknown_future_key":"keep me"}`,
		SchemaVersion+1, id, runID)
}

// writeShard writes lines verbatim as one shard file.
func writeShard(t *testing.T, dir, month string, lines ...string) {
	t.Helper()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, month+".jsonl"),
		[]byte(strings.Join(lines, "\n")+"\n"), 0o600))
}

// TestCompact_PreservesForwardIncompatibleRecords locks TD-004, the destructive
// half of the downgrade contract. Compaction rebuilds each shard from the records
// it could decode, so before this guard an older binary compacting a store written
// by a newer one did not merely hide the newer records — it deleted them. A
// forward-incompatible line must survive the rewrite byte-for-byte, including keys
// this binary has no field for.
func TestCompact_PreservesForwardIncompatibleRecords(t *testing.T) {
	dir := t.TempDir()

	// Two records sharing an id, so the fold genuinely rewrites the shard.
	older := sampleRecord("2026-06-14T10:00:00Z-a")
	older.Problem = "same finding"
	older.Timestamp = "2026-06-14T10:00:00Z"
	older.StampID()
	newer := older
	newer.Timestamp = "2026-06-15T10:00:00Z"
	newer.EstMinutes = 99

	oldJSON, err := json.Marshal(older)
	require.NoError(t, err)
	newJSON, err := json.Marshal(newer)
	require.NoError(t, err)
	future := futureLine("futureid", "2026-06-20T10:00:00Z-f")

	writeShard(t, dir, "2026-06", string(oldJSON), string(newJSON), future)

	res, err := Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.Equal(t, 2, res.RecordsBefore, "only decodable records are counted")
	assert.Equal(t, 1, res.RecordsAfter, "the duplicate id folds to one")

	raw, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), future,
		"the forward-incompatible line must survive compaction verbatim")
	assert.Contains(t, string(raw), `"unknown_future_key":"keep me"`,
		"keys this binary has no field for must not be dropped")
	assert.Equal(t, 2, len(strings.Split(strings.TrimRight(string(raw), "\n"), "\n")),
		"one folded record plus one preserved line")
}

// TestCompact_KeepsShardHoldingOnlyForwardIncompatibleRecords locks the second
// destruction path: Compact removes shards left with no live records, so a shard
// containing nothing this binary understands was deleted outright.
func TestCompact_KeepsShardHoldingOnlyForwardIncompatibleRecords(t *testing.T) {
	dir := t.TempDir()

	rec := sampleRecord("2026-06-14T10:00:00Z-a")
	recJSON, err := json.Marshal(rec)
	require.NoError(t, err)
	writeShard(t, dir, "2026-06", string(recJSON))

	future := futureLine("futureid", "2026-07-20T10:00:00Z-f")
	writeShard(t, dir, "2026-07", future)

	_, err = Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, "2026-07.jsonl"),
		"a shard holding only forward-incompatible records must not be removed")
	raw, err := os.ReadFile(filepath.Join(dir, "2026-07.jsonl"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), future, "its contents must survive intact")
}

// TestCompact_ReportsPreservedCount locks the reporting half: a caller can tell
// that compaction left records it could not fold, which explains why the store did
// not shrink as far as the fold counts suggest.
func TestCompact_ReportsPreservedCount(t *testing.T) {
	dir := t.TempDir()

	rec := sampleRecord("2026-06-14T10:00:00Z-a")
	recJSON, err := json.Marshal(rec)
	require.NoError(t, err)
	writeShard(t, dir, "2026-06",
		string(recJSON),
		futureLine("f1", "2026-06-20T10:00:00Z-f"),
		futureLine("f2", "2026-06-21T10:00:00Z-f"))

	res, err := Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.Equal(t, 2, res.Preserved, "both forward-incompatible lines are reported")
	assert.Equal(t, 1, res.RecordsBefore, "preserved lines are not counted as records")
}

// TestCompact_PreservedOnlyStoreIsNotRewritten locks the no-op path: when this
// binary can decode nothing, compaction must touch no file at all rather than
// rewriting shards from an empty fold.
func TestCompact_PreservedOnlyStoreIsNotRewritten(t *testing.T) {
	dir := t.TempDir()
	future := futureLine("futureid", "2026-06-20T10:00:00Z-f")
	writeShard(t, dir, "2026-06", future)

	res, err := Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.False(t, res.StoreFound, "no foldable records is still a no-op")
	assert.Equal(t, 1, res.Preserved, "but the caller is told why")

	raw, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, future+"\n", string(raw), "the shard is left byte-identical")
}

// TestCompact_MalformedLinesAreStillDropped pins the deliberate scope boundary of
// the TD-004 fix: a forward-incompatible line is valid data this binary cannot yet
// interpret and is preserved, whereas a malformed line is corrupt and compaction
// remains the place it is cleaned up. Preserving corrupt bytes forever would grow
// the store without bound and is a separate decision.
func TestCompact_MalformedLinesAreStillDropped(t *testing.T) {
	dir := t.TempDir()

	older := sampleRecord("2026-06-14T10:00:00Z-a")
	older.Problem = "same finding"
	older.StampID()
	newer := older
	newer.Timestamp = "2026-06-15T10:00:00Z"

	oldJSON, err := json.Marshal(older)
	require.NoError(t, err)
	newJSON, err := json.Marshal(newer)
	require.NoError(t, err)
	writeShard(t, dir, "2026-06", string(oldJSON), string(newJSON), `{"broken":`)

	_, err = Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `{"broken":`,
		"a corrupt line is still dropped by compaction (deliberate: only forward-incompatible lines are preserved)")
}

// overLongLine builds a single JSONL line larger than maxLineBytes, carrying a
// forward-incompatible schema version. The read path cannot buffer it, so it is
// skipped without ever reaching decodeRecord — the case a naive "preserve what
// decodeRecord rejected" fix misses entirely.
func overLongLine(id string) string {
	return fmt.Sprintf(
		`{"schema_version":%d,"id":%q,"run_id":"2026-07-02T10:00:00Z-r","ts":"2026-07-02T10:00:00Z","severity":"HIGH","problem":"newer schema with a blob","blob":%q}`,
		SchemaVersion+1, id, strings.Repeat("X", maxLineBytes+1024))
}

// TestCompact_KeepsShardHoldingOnlyAnOverLongLine locks the second half of TD-004.
// An over-long line is skipped by the buffer guard before any decode happens, so it
// yields neither a record nor a preserved line — leaving its shard eligible for the
// "no live records" removal. A future schema that embeds a blob, diff, or log makes
// oversized records ordinary rather than exotic, and this binary cannot know how big
// a future record is entitled to be.
func TestCompact_KeepsShardHoldingOnlyAnOverLongLine(t *testing.T) {
	dir := t.TempDir()

	rec := sampleRecord("2026-06-14T10:00:00Z-a")
	recJSON, err := json.Marshal(rec)
	require.NoError(t, err)
	writeShard(t, dir, "2026-06", string(recJSON))

	big := overLongLine("bigfuture")
	writeShard(t, dir, "2026-07", big)
	before, err := os.ReadFile(filepath.Join(dir, "2026-07.jsonl"))
	require.NoError(t, err)

	_, err = Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(dir, "2026-07.jsonl"),
		"a shard whose only line is over-long must not be removed")
	after, err := os.ReadFile(filepath.Join(dir, "2026-07.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, before, after, "its bytes must be untouched")
}

// TestCompact_LeavesShardWithOverLongLineUncompacted locks the conservative
// degradation: a shard holding a line this binary cannot even buffer is left
// entirely alone rather than rewritten from the subset it could read. Skipping the
// fold for that one shard costs disk; rewriting it costs data.
func TestCompact_LeavesShardWithOverLongLineUncompacted(t *testing.T) {
	dir := t.TempDir()

	older := sampleRecord("2026-07-14T10:00:00Z-a")
	older.Problem = "same finding"
	older.StampID()
	newer := older
	newer.Timestamp = "2026-07-15T10:00:00Z"
	oldJSON, err := json.Marshal(older)
	require.NoError(t, err)
	newJSON, err := json.Marshal(newer)
	require.NoError(t, err)

	writeShard(t, dir, "2026-07", string(oldJSON), overLongLine("bigfuture"), string(newJSON))
	before, err := os.ReadFile(filepath.Join(dir, "2026-07.jsonl"))
	require.NoError(t, err)

	// A second, clean shard so Compact takes the rewrite path rather than the
	// nothing-to-do early return.
	clean := sampleRecord("2026-06-14T10:00:00Z-b")
	clean.Problem = "unrelated"
	clean.StampID()
	cleanJSON, err := json.Marshal(clean)
	require.NoError(t, err)
	writeShard(t, dir, "2026-06", string(cleanJSON))

	_, err = Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)

	after, err := os.ReadFile(filepath.Join(dir, "2026-07.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, before, after,
		"a shard containing an unbufferable line is left byte-identical, superseded records and all")
}

// TestCompact_PreservesAcrossBufferRefills is the guard for the single line the
// whole preservation fix rests on: the retained line must be a COPY, because
// bufio.Reader.ReadSlice returns a view into a buffer the next read overwrites.
// Every other preservation test uses a shard far smaller than the 1 MiB buffer, so
// the buffer never refills and an aliased slice stays valid by accident — they pass
// with or without the copy. This one forces many refills, so a missing copy writes
// fragments of unrelated records into the shard as invalid JSON: silent corruption
// of the very data the fix exists to protect, which is worse than the deletion it
// replaced.
func TestCompact_PreservesAcrossBufferRefills(t *testing.T) {
	dir := t.TempDir()

	// ~2.5 MiB of decodable records, so the reader refills its buffer repeatedly,
	// with forward-incompatible lines interleaved between them.
	var lines []string
	var futures []string
	for i := 0; i < 6; i++ {
		rec := sampleRecord("2026-06-14T10:00:00Z-a")
		rec.Problem = strings.Repeat("P", 400*1024) + fmt.Sprintf("-%d", i)
		rec.StampID()
		recJSON, err := json.Marshal(rec)
		require.NoError(t, err)
		lines = append(lines, string(recJSON))

		f := futureLine(fmt.Sprintf("future%d", i), "2026-06-20T10:00:00Z-f")
		futures = append(futures, f)
		lines = append(lines, f)
	}
	writeShard(t, dir, "2026-06", lines...)

	res, err := Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	require.Equal(t, len(futures), res.Preserved)

	raw, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	for i, f := range futures {
		assert.Contains(t, string(raw), f,
			"preserved line %d must survive verbatim across buffer refills", i)
	}
	// Every line must still be parseable JSON — a truncated alias would not be.
	for i, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		var probe map[string]any
		assert.NoError(t, json.Unmarshal([]byte(line), &probe),
			"line %d must be intact JSON, not a fragment of a neighbouring record", i)
	}
}

// TestCompact_SweepsStaleTempFiles locks the crash-reaping contract: temp files
// (.<month>.jsonl.tmp-*) leaked by a Compact killed between CreateTemp and rename
// are removed at the start of the next Compact, while non-matching files are left
// untouched.
func TestCompact_SweepsStaleTempFiles(t *testing.T) {
	dir := t.TempDir()

	rec := sampleRecord("2026-06-14T10:00:00Z-a")
	require.NoError(t, Append(dir, rec))

	// Pre-seed leaked temps exactly as a SIGKILLed Compact leaves them (CreateTemp
	// pattern: "."+month+".jsonl.tmp-*"), plus a lookalike the sweep must not touch.
	stale1 := filepath.Join(dir, ".2026-06.jsonl.tmp-111")
	stale2 := filepath.Join(dir, ".2026-07.jsonl.tmp-222")
	keepFile := filepath.Join(dir, "2026-08.jsonl.tmp-333") // no leading dot: not a compaction temp
	for _, p := range []string{stale1, stale2, keepFile} {
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))
	}

	_, cerr := Compact(dir, ReadOpts{})
	require.NoError(t, cerr)

	for _, p := range []string{stale1, stale2} {
		_, err := os.Stat(p)
		assert.True(t, os.IsNotExist(err), "stale compaction temp must be swept: %s", filepath.Base(p))
	}
	_, err := os.Stat(keepFile)
	assert.NoError(t, err, "non-matching file must survive the sweep")
}

// --- Plan 35.13 T3: FoldRecords is recency-aware, wontfix is unconditional ---

// foldRec builds a bare record for the fold tests: only id, timestamp, and
// status matter to the precedence rule.
func foldRec(id, ts, status string) Record {
	return Record{SchemaVersion: SchemaVersion, ID: id, RunID: ts + "-r", Timestamp: ts,
		File: "a.go", Line: 1, Problem: "p", Status: status}
}

func TestFoldRecords_RecencyByStatus(t *testing.T) {
	cases := []struct {
		name       string
		in         []Record
		wantStatus string
		wantTS     string
	}{
		{
			name:       "open then resolved folds to the resolution",
			in:         []Record{foldRec("a", "2026-07-01T00:00:00Z", ""), foldRec("a", "2026-07-02T00:00:00Z", "resolved")},
			wantStatus: "resolved", wantTS: "2026-07-02T00:00:00Z",
		},
		{
			name:       "resolved then re-detected folds back to the open regression",
			in:         []Record{foldRec("a", "2026-07-01T00:00:00Z", ""), foldRec("a", "2026-07-02T00:00:00Z", "resolved"), foldRec("a", "2026-07-03T00:00:00Z", "")},
			wantStatus: "", wantTS: "2026-07-03T00:00:00Z",
		},
		{
			name:       "deferred then re-detected re-surfaces",
			in:         []Record{foldRec("a", "2026-07-01T00:00:00Z", ""), foldRec("a", "2026-07-02T00:00:00Z", "deferred"), foldRec("a", "2026-07-03T00:00:00Z", "")},
			wantStatus: "", wantTS: "2026-07-03T00:00:00Z",
		},
		{
			name:       "wontfix survives a later re-detection unconditionally",
			in:         []Record{foldRec("a", "2026-07-01T00:00:00Z", ""), foldRec("a", "2026-07-02T00:00:00Z", "wontfix"), foldRec("a", "2026-07-03T00:00:00Z", "")},
			wantStatus: "wontfix", wantTS: "2026-07-02T00:00:00Z",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FoldRecords(tc.in)
			require.Len(t, got, 1)
			assert.Equal(t, tc.wantStatus, got[0].Status)
			assert.Equal(t, tc.wantTS, got[0].Timestamp)
		})
	}
}

// A resolution appended in the same second as the finding must still close it:
// at an equal timestamp the terminal record outranks the open one.
func TestFoldRecords_EqualTimestampTerminalWins(t *testing.T) {
	got := FoldRecords([]Record{
		foldRec("a", "2026-07-01T00:00:00Z", ""),
		foldRec("a", "2026-07-01T00:00:00Z", "resolved"),
	})
	require.Len(t, got, 1)
	assert.Equal(t, "resolved", got[0].Status)

	// ...and read order must not flip it.
	got = FoldRecords([]Record{
		foldRec("a", "2026-07-01T00:00:00Z", "resolved"),
		foldRec("a", "2026-07-01T00:00:00Z", ""),
	})
	require.Len(t, got, 1)
	assert.Equal(t, "resolved", got[0].Status)
}

// Divergent terminal records for one id (the no-lock window) still resolve by
// precedence, not read order — wontfix outranks resolved either way.
func TestFoldRecords_DivergentTerminalsPreferWontfixEitherOrder(t *testing.T) {
	for _, order := range [][]Record{
		{foldRec("a", "2026-07-01T00:00:00Z", "resolved"), foldRec("a", "2026-07-02T00:00:00Z", "wontfix")},
		{foldRec("a", "2026-07-01T00:00:00Z", "wontfix"), foldRec("a", "2026-07-02T00:00:00Z", "resolved")},
	} {
		got := FoldRecords(order)
		require.Len(t, got, 1)
		assert.Equal(t, "wontfix", got[0].Status, "wontfix wins regardless of append order")
	}
}

// Compaction must fold a regressed id to its re-opened record WITHOUT destroying
// the superseded resolution: that record holds the ResolvedAt and the human-typed
// --reason justification, which exist nowhere else. Retention is bounded at two
// records per id, and the fold over what survives still yields the same effective
// record every reader saw before compaction.
func TestCompact_RegressedIDKeepsItsResolutionTrail(t *testing.T) {
	dir := t.TempDir()
	resolved := foldRec("a", "2026-07-02T00:00:00Z", "resolved")
	resolved.ResolvedAt = "2026-07-02T00:00:00Z"
	resolved.Justification = "closed after the retry cap landed"
	regressed := []Record{
		foldRec("a", "2026-07-01T00:00:00Z", ""),
		resolved,
		foldRec("a", "2026-07-03T00:00:00Z", ""),
	}
	dismissed := []Record{
		foldRec("b", "2026-07-01T00:00:00Z", ""),
		foldRec("b", "2026-07-02T00:00:00Z", "wontfix"),
		foldRec("b", "2026-07-03T00:00:00Z", ""),
	}
	for _, r := range append(regressed, dismissed...) {
		require.NoError(t, Append(dir, r))
	}

	res, err := Compact(dir, ReadOpts{})
	require.NoError(t, err)
	assert.Equal(t, 6, res.RecordsBefore)
	assert.Equal(t, 3, res.RecordsAfter, "the regressed id keeps 2 records, the dismissed id 1")

	after, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, after, 3)

	// The resolution trail survived, justification and all.
	var trail *Record
	for i := range after {
		if after[i].ID == "a" && after[i].Status == "resolved" {
			trail = &after[i]
		}
	}
	require.NotNil(t, trail, "the superseded resolution must not be destroyed")
	assert.Equal(t, "closed after the retry cap landed", trail.Justification)
	assert.Equal(t, "2026-07-02T00:00:00Z", trail.ResolvedAt)

	// Readers still see exactly what they saw before compaction.
	byID := map[string]Record{}
	for _, r := range FoldRecords(after) {
		byID[r.ID] = r
	}
	require.Len(t, byID, 2)
	assert.Equal(t, "", byID["a"].Status, "the regressed id is still effectively open")
	assert.Equal(t, "2026-07-03T00:00:00Z", byID["a"].Timestamp)
	assert.Equal(t, "wontfix", byID["b"].Status, "the dismissed id compacts to its suppressing record")
}

// Compaction is idempotent under the retention rule: a second pass over an
// already-compacted store drops nothing further.
func TestCompact_RetentionIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	for _, r := range []Record{
		foldRec("a", "2026-07-01T00:00:00Z", ""),
		foldRec("a", "2026-07-02T00:00:00Z", "resolved"),
		foldRec("a", "2026-07-03T00:00:00Z", ""),
	} {
		require.NoError(t, Append(dir, r))
	}

	first, err := Compact(dir, ReadOpts{})
	require.NoError(t, err)
	require.Equal(t, 2, first.RecordsAfter)

	second, err := Compact(dir, ReadOpts{})
	require.NoError(t, err)
	assert.Equal(t, 2, second.RecordsBefore)
	assert.Equal(t, 2, second.RecordsAfter)
	assert.Equal(t, 0, second.Dropped, "a second pass has nothing left to drop")
}

// A still-open id with no terminal record keeps exactly one record: retention
// adds a second record only where a resolution trail actually exists.
func TestCompact_OpenOnlyIDKeepsOneRecord(t *testing.T) {
	dir := t.TempDir()
	for _, r := range []Record{
		foldRec("a", "2026-07-01T00:00:00Z", ""),
		foldRec("a", "2026-07-02T00:00:00Z", ""),
	} {
		require.NoError(t, Append(dir, r))
	}

	res, err := Compact(dir, ReadOpts{})
	require.NoError(t, err)
	assert.Equal(t, 1, res.RecordsAfter)
	assert.Equal(t, 1, res.Dropped)
}

// A later `deferred` must not displace an earlier `resolved` as the retained
// trail: only the resolution can carry a human-typed --reason.
func TestCompact_RetainsTheHighestRankedTerminal(t *testing.T) {
	dir := t.TempDir()
	resolved := foldRec("a", "2026-07-02T00:00:00Z", "resolved")
	resolved.Justification = "why it was closed"
	for _, r := range []Record{
		foldRec("a", "2026-07-01T00:00:00Z", ""),
		resolved,
		foldRec("a", "2026-07-03T00:00:00Z", "deferred"),
		foldRec("a", "2026-07-04T00:00:00Z", ""),
	} {
		require.NoError(t, Append(dir, r))
	}

	_, err := Compact(dir, ReadOpts{})
	require.NoError(t, err)

	after, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, after, 2)
	var trail *Record
	for i := range after {
		if IsClosedStatus(after[i].Status) {
			trail = &after[i]
		}
	}
	require.NotNil(t, trail)
	assert.Equal(t, "resolved", trail.Status, "rank outranks recency when choosing the retained trail")
	assert.Equal(t, "why it was closed", trail.Justification)
}
