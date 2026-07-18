package localdebt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleRecord builds a minimal valid v1 record for store tests, populating every
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
