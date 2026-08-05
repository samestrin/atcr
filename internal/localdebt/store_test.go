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

// The nil-able kinds a caller can hand in as an io.Writer beyond a pointer. Writing
// to any of them when nil fails — a panic for func/map/slice, a permanent block for
// a chan — which is exactly what a diagnostics path must never do.
type (
	writerFunc  func([]byte) (int, error)
	writerMap   map[string]int
	writerSlice []byte
	writerChan  chan int
)

func (f writerFunc) Write(p []byte) (int, error)  { return f(p) }
func (m writerMap) Write(p []byte) (int, error)   { m["n"] += len(p); return len(p), nil }
func (s writerSlice) Write(p []byte) (int, error) { _ = s[0]; return len(p), nil }
func (c writerChan) Write(p []byte) (int, error)  { c <- len(p); return len(p), nil }

// TestDiagWriter_EveryNilableKindFallsBack pins the ACTUAL bound of the
// diagnostics-sink guard (TD internal/localdebt/store.go:57). Its doc claimed it
// preserved a "never panic in a diagnostics path" contract, while the check tested
// reflect.Pointer alone — so a nil func-, map-, slice- or chan-kinded writer sailed
// past it and failed on the first Write exactly as an unguarded one would. A guard
// that upholds less than its comment asserts is worse for the next reader than no
// guard at all; either the claim or the code had to move, and the claim is the one
// worth keeping.
func TestDiagWriter_EveryNilableKindFallsBack(t *testing.T) {
	for name, w := range map[string]io.Writer{
		"untyped nil": nil,
		"nil pointer": (*bytes.Buffer)(nil),
		"nil func":    writerFunc(nil),
		"nil map":     writerMap(nil),
		"nil slice":   writerSlice(nil),
		"nil chan":    writerChan(nil),
	} {
		t.Run(name, func(t *testing.T) {
			// require, not assert: the returned sink is written to below, and a
			// wrong answer here means writing to the very value that hangs or panics.
			got := diagWriter(w)
			require.Equal(t, os.Stderr, got, "an unusable sink must fall back to os.Stderr")
			assert.NotPanics(t, func() { _, _ = got.Write(nil) })
		})
	}

	t.Run("a usable writer is passed through untouched", func(t *testing.T) {
		var buf bytes.Buffer
		require.Same(t, &buf, diagWriter(&buf))
	})
}

// TestReadPathDiagnostics_CarryNoAbsoluteShardPath pins the package SECURITY
// contract over RAW PATH STRINGS (TD internal/localdebt/store.go:203). The contract
// used to rest on callers following the DefaultDir(".") relative-root convention, but
// the store dir is now DefaultDir(ResolveStoreRoot(...)) and both the explicit and
// manifest tiers resolve to ABSOLUTE paths — so the malformed-record and over-long-
// line warnings, which format their path with a bare %s rather than through
// basePathErr, print a full username-bearing shard path. On the MCP path these go to
// server stderr, which a calling agent typically captures into model context.
func TestReadPathDiagnostics_CarryNoAbsoluteShardPath(t *testing.T) {
	dir := t.TempDir()
	longLine := strings.Repeat("x", maxLineBytes+16)
	writeShard(t, dir, "2026-06",
		"{not json",
		`{"schema_version":3,"run_id":"2026-06-14T10:00:00Z-a"}`, // missing id
		longLine,
	)

	t.Run("ReadRecords", func(t *testing.T) {
		var diag bytes.Buffer
		_, _ = ReadRecords(filepath.Join(dir, "2026-06.jsonl"), ReadOpts{Writer: &diag})
		assertShardDiagIsRedacted(t, diag.String(), dir)
	})

	t.Run("StreamSummaries", func(t *testing.T) {
		var diag bytes.Buffer
		_ = StreamSummaries(dir, ReadOpts{Writer: &diag}, func(Summary) error { return nil })
		assertShardDiagIsRedacted(t, diag.String(), dir)
	})
}

// assertShardDiagIsRedacted checks the warnings still identify WHICH shard (the base
// name is what a human needs) while carrying none of the absolute path above it.
func assertShardDiagIsRedacted(t *testing.T, diag, dir string) {
	t.Helper()
	require.Contains(t, diag, MsgMalformedSkip, "the malformed-line warning must still fire")
	require.Contains(t, diag, "over-long line", "the over-long-line warning must still fire")
	assert.Contains(t, diag, "2026-06.jsonl", "the shard base name still names the damaged file")
	assert.NotContains(t, diag, dir, "the absolute store path must never reach the sink:\n%s", diag)
}

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
	id, err := ManualRunID(fixed)
	require.NoError(t, err)
	month, err := monthFromRunID(id)
	require.NoError(t, err, "a ManualRunID must always resolve to a month shard")
	assert.Equal(t, "2026-06", month)

	// 2026-07-01T00:30:00+02:00 is 2026-06-30T22:30:00Z — the June shard.
	crossing := time.Date(2026, 7, 1, 0, 30, 0, 0, time.FixedZone("UTC+2", 2*60*60))
	id, err = ManualRunID(crossing)
	require.NoError(t, err)
	month, err = monthFromRunID(id)
	require.NoError(t, err)
	assert.Equal(t, "2026-06", month,
		"ManualRunID normalizes to UTC, so a local-time month boundary does not misfile the shard")
}

// TestManualRunID_SuffixDistinguishesManual locks the provenance-legibility half:
// a manual entry's run_id is visibly distinct from a reconcile run_id
// (<RFC3339>-<review-dir base>) in the raw JSONL, not only in the origin field.
func TestManualRunID_SuffixDistinguishesManual(t *testing.T) {
	id, err := ManualRunID(time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC))
	require.NoError(t, err)
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

	id, err := ManualRunID(fixed)
	require.NoError(t, err)
	rec := sampleRecord(id)
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

// TD: StoreFound answers "does the store exist", NOT "did a fold happen". It
// reported false for a store that exists and holds records (all newer-schema), so
// every caller had to special-case Preserved>0 to avoid printing "No local TD
// store" over them — an ambiguous-ownership contract the CLI was compensating for
// in two branches. Whether anything was foldable is RecordsBefore's job, and what
// this binary could not decode is Preserved's; the two counts are separate.
func TestCompact_StoreFoundReportsPresenceNotFoldability(t *testing.T) {
	t.Run("existing store with only preserved records", func(t *testing.T) {
		dir := t.TempDir()
		writeShard(t, dir, "2026-06", futureLine("futureid", "2026-06-20T10:00:00Z-f"))

		res, err := Compact(dir, ReadOpts{Writer: io.Discard})

		require.NoError(t, err)
		assert.True(t, res.StoreFound, "the store plainly exists — it holds a record")
		assert.Zero(t, res.RecordsBefore, "nothing this binary can fold is RecordsBefore's answer")
		assert.Equal(t, 1, res.Preserved, "and the undecodable line is counted separately")
	})

	t.Run("existing store with only malformed lines", func(t *testing.T) {
		dir := t.TempDir()
		writeShard(t, dir, "2026-06", "{not json", "{also not json")

		res, err := Compact(dir, ReadOpts{Writer: io.Discard})

		require.NoError(t, err)
		assert.True(t, res.StoreFound, "a shard on disk is a store, however unreadable its contents")
		assert.Zero(t, res.RecordsBefore)
	})

	t.Run("missing store", func(t *testing.T) {
		res, err := Compact(filepath.Join(t.TempDir(), "absent"), ReadOpts{Writer: io.Discard})

		require.NoError(t, err)
		assert.False(t, res.StoreFound, "an absent store is not found")
		assert.Zero(t, res.Preserved)
	})

	t.Run("existing but empty store directory", func(t *testing.T) {
		res, err := Compact(t.TempDir(), ReadOpts{Writer: io.Discard})

		require.NoError(t, err)
		assert.False(t, res.StoreFound,
			"a directory holding no shard holds no records either — the CLI's 'no store' line is accurate here")
	})
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
	assert.Zero(t, res.RecordsBefore, "no foldable records is still a no-op")
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

// The retained trail record must not carry the id's aggregate counters: those
// live on the effective record only, so a second compaction cannot double-count
// them once T5 carries Occurrences/FirstSeen through the fold.
func TestCompact_RetainedTrailCarriesNoAggregateCounters(t *testing.T) {
	dir := t.TempDir()
	resolved := foldRec("a", "2026-07-02T00:00:00Z", "resolved")
	resolved.Occurrences = 5
	resolved.FirstSeen = "2026-07-01T00:00:00Z"
	for _, r := range []Record{
		foldRec("a", "2026-07-01T00:00:00Z", ""),
		resolved,
		foldRec("a", "2026-07-03T00:00:00Z", ""),
	} {
		require.NoError(t, Append(dir, r))
	}

	_, err := Compact(dir, ReadOpts{})
	require.NoError(t, err)

	after, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, after, 2)
	for _, r := range after {
		if IsClosedStatus(r.Status) {
			assert.Zero(t, r.Occurrences, "the trail entry carries no occurrence count")
			assert.Empty(t, r.FirstSeen, "the trail entry carries no first-seen stamp")
		}
	}
}

// A `deferred` effective record is not a resolution: it carries no justification,
// so treating it as the trail would discard an earlier `resolved` record and the
// --reason text only that record holds.
func TestCompact_DeferredEffectiveRecordStillKeepsTheResolutionTrail(t *testing.T) {
	dir := t.TempDir()
	resolved := foldRec("a", "2026-07-02T00:00:00Z", "resolved")
	resolved.Justification = "fixed by the retry cap"
	for _, r := range []Record{
		foldRec("a", "2026-07-01T00:00:00Z", ""),
		resolved,
		foldRec("a", "2026-07-03T00:00:00Z", "deferred"),
	} {
		require.NoError(t, Append(dir, r))
	}

	_, err := Compact(dir, ReadOpts{})
	require.NoError(t, err)

	after, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, after, 2, "the deferred effective record plus its resolution trail")

	var trail, eff *Record
	for i := range after {
		if after[i].Status == "resolved" {
			trail = &after[i]
		}
		if after[i].Status == "deferred" {
			eff = &after[i]
		}
	}
	require.NotNil(t, eff, "the deferred record is still the effective one")
	require.NotNil(t, trail, "the resolution must survive a later deferral")
	assert.Equal(t, "fixed by the retry cap", trail.Justification)
}

// A deferred-only id has no resolution to preserve, so it keeps exactly one
// record — the effective one must never be retained as its own trail.
func TestCompact_DeferredOnlyIDKeepsOneRecord(t *testing.T) {
	dir := t.TempDir()
	for _, r := range []Record{
		foldRec("a", "2026-07-01T00:00:00Z", ""),
		foldRec("a", "2026-07-02T00:00:00Z", "deferred"),
	} {
		require.NoError(t, Append(dir, r))
	}

	res, err := Compact(dir, ReadOpts{})
	require.NoError(t, err)
	assert.Equal(t, 1, res.RecordsAfter, "no duplicate of the effective record")
}

// --- T5: occurrence/first-seen carry-through and automatic compaction ---------

// detection builds an open (no-status) record for id-group tests: a fresh sighting
// with no carried counters, exactly what the reconcile hook appends.
func detection(problem, ts string) Record {
	r := sampleRecord(ts + "-run")
	r.Problem = problem
	r.Timestamp = ts
	r.StampID()
	return r
}

func TestFoldRecords_CarriesOccurrencesAndFirstSeen(t *testing.T) {
	a := detection("recurring finding", "2026-06-01T00:00:00Z")
	b, c := a, a
	b.Timestamp = "2026-06-02T00:00:00Z"
	c.Timestamp = "2026-06-03T00:00:00Z"

	folded := FoldRecords([]Record{a, b, c})
	require.Len(t, folded, 1)
	assert.Equal(t, 3, folded[0].Occurrences, "three detections of one id count as three occurrences")
	assert.Equal(t, "2026-06-01T00:00:00Z", folded[0].FirstSeen, "the earliest sighting, not the winner's own ts")
	assert.Equal(t, "2026-06-03T00:00:00Z", folded[0].Timestamp, "the winning record is still the latest")
}

func TestFoldRecords_ResolutionRecordDoesNotInflateOccurrences(t *testing.T) {
	open := detection("fixed once", "2026-06-01T00:00:00Z")
	resolved := open
	resolved.Timestamp = "2026-06-02T00:00:00Z"
	resolved.Status = "resolved"

	folded := FoldRecords([]Record{open, resolved})
	require.Len(t, folded, 1)
	assert.Equal(t, 1, folded[0].Occurrences,
		"a resolution is a status marker, not a second sighting")
}

func TestFoldRecords_DeferredOnlyGroupCountsAsOneSighting(t *testing.T) {
	// `atcr debt add --status deferred` files an item that is not a detection; the
	// floor of 1 keeps it from reporting zero occurrences.
	rec := detection("filed straight to deferred", "2026-06-01T00:00:00Z")
	rec.Status = "deferred"

	folded := FoldRecords([]Record{rec})
	require.Len(t, folded, 1)
	assert.Equal(t, 1, folded[0].Occurrences)
	assert.Equal(t, "2026-06-01T00:00:00Z", folded[0].FirstSeen)
}

func TestFoldRecords_FirstSeenPrefersEarlierCarriedValue(t *testing.T) {
	// A compacted carrier holds the id's original FirstSeen even though its own
	// Timestamp is much later; the aggregate must not regress to the later value.
	carrier := detection("long-lived", "2026-09-01T00:00:00Z")
	carrier.Occurrences = 4
	carrier.FirstSeen = "2026-01-01T00:00:00Z"
	fresh := detection("long-lived", "2026-09-02T00:00:00Z")

	folded := FoldRecords([]Record{carrier, fresh})
	require.Len(t, folded, 1)
	assert.Equal(t, "2026-01-01T00:00:00Z", folded[0].FirstSeen)
	assert.Equal(t, 5, folded[0].Occurrences, "carried 4 plus the one uncounted detection")
}

func TestFoldRecords_FirstSeenFallsBackToLexicalOnUnparseableTimestamp(t *testing.T) {
	a := detection("garbage ts", "not-a-timestamp")
	b := a
	b.Timestamp = "2026-06-02T00:00:00Z"

	folded := FoldRecords([]Record{a, b})
	require.Len(t, folded, 1)
	assert.Equal(t, "2026-06-02T00:00:00Z", folded[0].FirstSeen,
		"an unparseable value falls back to a byte comparison, deterministically")
}

func TestEarlierTimestamp_ComparesInstantsNotBytes(t *testing.T) {
	// The offset-bearing value is the EARLIER instant while sorting later
	// lexically; FirstSeen is durable, so it is parsed rather than byte-compared.
	assert.Equal(t, "2026-01-01T01:00:00+05:00",
		earlierTimestamp("2026-01-01T00:00:00Z", "2026-01-01T01:00:00+05:00"))
	assert.Equal(t, "2026-01-01T00:00:00Z",
		earlierTimestamp("2026-01-01T00:00:00Z", "2026-06-01T00:00:00Z"))
	assert.Equal(t, "aaa", earlierTimestamp("aaa", "bbb"), "unparseable falls back to bytes")
}

// TestCompact_RegressionCountSurvivesCompactionCycle is AC4's named assertion:
// detect -> resolve -> compact -> re-detect -> compact leaves the regression count
// and the original sighting intact at O(1) store size.
func TestCompact_RegressionCountSurvivesCompactionCycle(t *testing.T) {
	dir := t.TempDir()
	first := detection("regresses after a fix", "2026-06-01T00:00:00Z")
	first.RunID = "2026-06-01T00:00:00Z-run"
	require.NoError(t, Append(dir, first))

	resolved := first
	resolved.RunID = "2026-06-02T00:00:00Z-resolved"
	resolved.Timestamp = "2026-06-02T00:00:00Z"
	resolved.Status = "resolved"
	resolved.ResolvedAt = "2026-06-02T00:00:00Z"
	resolved.Justification = "fixed in PR #1"
	require.NoError(t, Append(dir, resolved))

	_, err := Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)

	regressed := first
	regressed.RunID = "2026-07-01T00:00:00Z-run"
	regressed.Timestamp = "2026-07-01T00:00:00Z"
	require.NoError(t, Append(dir, regressed))

	_, err = Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)

	recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	folded := FoldRecords(recs)
	require.Len(t, folded, 1)
	assert.Equal(t, 2, folded[0].Occurrences, "the regression is the second occurrence")
	assert.Equal(t, "2026-06-01T00:00:00Z", folded[0].FirstSeen, "the original sighting survives")
	assert.Empty(t, folded[0].Status, "the id is open again after the regression")

	// The superseded resolution and its human-typed justification are still on disk.
	var trail *Record
	for i := range recs {
		if recs[i].Status == "resolved" {
			trail = &recs[i]
		}
	}
	require.NotNil(t, trail, "compaction retains the resolution trail")
	assert.Equal(t, "fixed in PR #1", trail.Justification)
}

func TestCompact_OccurrencesIdempotentAcrossRepeatedCompaction(t *testing.T) {
	dir := t.TempDir()
	first := detection("stable count", "2026-06-01T00:00:00Z")
	first.RunID = "2026-06-01T00:00:00Z-run"
	require.NoError(t, Append(dir, first))
	resolved := first
	resolved.RunID = "2026-06-02T00:00:00Z-resolved"
	resolved.Timestamp = "2026-06-02T00:00:00Z"
	resolved.Status = "resolved"
	require.NoError(t, Append(dir, resolved))
	regressed := first
	regressed.RunID = "2026-07-01T00:00:00Z-run"
	regressed.Timestamp = "2026-07-01T00:00:00Z"
	require.NoError(t, Append(dir, regressed))

	read := func() Record {
		recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		folded := FoldRecords(recs)
		require.Len(t, folded, 1)
		return folded[0]
	}

	_, err := Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	once := read()
	for i := 0; i < 3; i++ {
		_, err := Compact(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		again := read()
		assert.Equal(t, once.Occurrences, again.Occurrences, "repeated compaction must not inflate or decay the count")
		assert.Equal(t, once.FirstSeen, again.FirstSeen)
	}
	assert.Equal(t, 2, once.Occurrences)
}

func TestStoreStats_CountsRecordsAndBytes(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Append(dir, detection("a", "2026-06-01T00:00:00Z")))
	require.NoError(t, Append(dir, detection("b", "2026-07-01T00:00:00Z")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored\n"), 0o600))

	records, size, err := StoreStats(dir)
	require.NoError(t, err)
	assert.Equal(t, 2, records, "one line per record; non-.jsonl files ignored")

	var want int64
	for _, m := range []string{"2026-06", "2026-07"} {
		info, err := os.Stat(filepath.Join(dir, m+".jsonl"))
		require.NoError(t, err)
		want += info.Size()
	}
	assert.Equal(t, want, size)
}

func TestStoreStats_MissingDirIsZero(t *testing.T) {
	records, size, err := StoreStats(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err, "a missing store is the no-backlog state, not an error")
	assert.Zero(t, records)
	assert.Zero(t, size)
}

// seedChurn writes n superseded detections of ONE id, so a store has something for
// compaction to actually drop (n records fold to 1).
func seedChurn(t *testing.T, dir string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		rec := detection("churning finding", fmt.Sprintf("2026-06-%02dT00:00:00Z", i+1))
		rec.RunID = fmt.Sprintf("2026-06-%02dT00:00:00Z-run", i+1)
		require.NoError(t, Append(dir, rec))
	}
}

// seedIDs writes ids distinct findings, each with occ superseded detections, so a
// store compacts to a known FLOOR of `ids` records rather than to 1. The watermark
// tests need that floor to be large enough that one further append is not 50%
// growth — which is the whole point of the damping.
func seedIDs(t *testing.T, dir string, ids, occ int) {
	t.Helper()
	for i := 0; i < ids; i++ {
		for j := 0; j < occ; j++ {
			rec := detection(fmt.Sprintf("finding %d", i), fmt.Sprintf("2026-06-%02dT00:00:00Z", j+1))
			rec.RunID = fmt.Sprintf("2026-06-%02dT00:00:00Z-run", j+1)
			require.NoError(t, Append(dir, rec))
		}
	}
}

func TestMaybeCompact_TriggersOnRecordThreshold(t *testing.T) {
	dir := t.TempDir()
	seedChurn(t, dir, 4)

	res, triggered, err := MaybeCompact(dir, CompactPolicy{MaxRecords: 3}, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	require.True(t, triggered, "4 records against a 3-record threshold must compact")
	assert.Equal(t, 4, res.RecordsBefore)
	assert.Equal(t, 1, res.RecordsAfter, "the four occurrences fold to one live record")
}

func TestMaybeCompact_TriggersOnByteThreshold(t *testing.T) {
	dir := t.TempDir()
	seedChurn(t, dir, 2)

	// Well under any record threshold: the byte clause is what fires.
	_, triggered, err := MaybeCompact(dir, CompactPolicy{MaxRecords: 1_000_000, MaxBytes: 1}, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.True(t, triggered, "whichever threshold trips first wins")
}

func TestMaybeCompact_NoOpBelowThresholds(t *testing.T) {
	dir := t.TempDir()
	seedChurn(t, dir, 2)
	before, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)

	res, triggered, err := MaybeCompact(dir, CompactPolicy{MaxRecords: 100, MaxBytes: 1 << 30}, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.False(t, triggered)
	assert.Equal(t, CompactResult{}, res)

	after, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, before, after, "a below-threshold store is not rewritten at all")
}

func TestMaybeCompact_ZeroPolicyUsesProductionDefaults(t *testing.T) {
	assert.Equal(t, 100_000, DefaultAutoCompactMaxRecords, "the production record threshold is pinned")
	assert.Equal(t, int64(100<<20), int64(DefaultAutoCompactMaxBytes), "the production byte threshold is pinned")

	var zero CompactPolicy
	assert.Equal(t, DefaultAutoCompactMaxRecords, zero.maxRecords())
	assert.Equal(t, int64(DefaultAutoCompactMaxBytes), zero.maxBytes())

	dir := t.TempDir()
	seedChurn(t, dir, 3)
	_, triggered, err := MaybeCompact(dir, CompactPolicy{}, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.False(t, triggered, "a tiny store is nowhere near the production defaults")
}

// TestMaybeCompact_WatermarkSuppressesReCompactionOfACompactedStore is the TD-013
// guard, and the reason the trigger is not a bare absolute threshold.
//
// Compact retains up to TWO records per id, so a store's post-compaction floor can
// sit above the threshold. Without the watermark, every subsequent append re-trips
// it and rewrites every shard under the cross-process lock to drop nothing —
// forever.
func TestMaybeCompact_WatermarkSuppressesReCompactionOfACompactedStore(t *testing.T) {
	dir := t.TempDir()
	// 10 live ids x 3 occurrences: the store compacts to a floor of 10 records,
	// which the 1-record threshold still exceeds — the exact shape TD-013 describes.
	seedIDs(t, dir, 10, 3)
	policy := CompactPolicy{MaxRecords: 1}

	_, triggered, err := MaybeCompact(dir, policy, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	require.True(t, triggered, "the first pass compacts")
	records, _, err := StoreStats(dir)
	require.NoError(t, err)
	require.Equal(t, 10, records, "the post-compaction floor still exceeds the threshold")

	require.FileExists(t, filepath.Join(dir, compactWatermarkFile),
		"a successful compaction records its post-compaction size")

	// One more append, then re-check: the store is still above the threshold but has
	// not grown materially, so nothing should happen.
	fresh := detection("a different finding", "2026-07-01T00:00:00Z")
	fresh.RunID = "2026-07-01T00:00:00Z-run"
	require.NoError(t, Append(dir, fresh))

	before, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	_, triggered, err = MaybeCompact(dir, policy, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.False(t, triggered,
		"an already-compacted store above an absolute threshold must not re-compact on every append")
	after, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, before, after, "and it must not rewrite the shards either")
}

func TestMaybeCompact_ResumesOnceTheStoreGrowsPastTheWatermark(t *testing.T) {
	dir := t.TempDir()
	seedIDs(t, dir, 10, 3)
	policy := CompactPolicy{MaxRecords: 1}
	_, triggered, err := MaybeCompact(dir, policy, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	require.True(t, triggered)

	// Grow well past 1.5x the watermark: the guard damps re-compaction, it does not
	// disable it.
	for i := 0; i < 12; i++ {
		rec := detection("churning finding", fmt.Sprintf("2026-07-%02dT00:00:00Z", i+1))
		rec.RunID = fmt.Sprintf("2026-07-%02dT00:00:00Z-run", i+1)
		require.NoError(t, Append(dir, rec))
	}
	_, triggered, err = MaybeCompact(dir, policy, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.True(t, triggered, "a store that genuinely grew is compacted again")
}

func TestMaybeCompact_CorruptWatermarkStillCompacts(t *testing.T) {
	// The guard degrades toward compacting: it may delay a redundant compaction, it
	// must never prevent a needed one.
	dir := t.TempDir()
	seedChurn(t, dir, 4)
	require.NoError(t, os.WriteFile(filepath.Join(dir, compactWatermarkFile), []byte("{not json"), 0o600))

	_, triggered, err := MaybeCompact(dir, CompactPolicy{MaxRecords: 3}, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.True(t, triggered, "an unreadable watermark falls back to the thresholds alone")
}

func TestMaybeCompact_WatermarkFileIsInvisibleToTheReadPath(t *testing.T) {
	dir := t.TempDir()
	seedChurn(t, dir, 4)
	_, _, err := MaybeCompact(dir, CompactPolicy{MaxRecords: 1}, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(dir, compactWatermarkFile))

	recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.Len(t, recs, 1, "the watermark is not a shard and never decodes as a record")

	// sweepStaleTemps reaps crash debris on every Compact; it must not reap this.
	_, err = Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, compactWatermarkFile),
		"the watermark does not match the .jsonl.tmp- debris pattern")
}

// TestMaybeCompact_DoesNotDeadlockAgainstCompactLock proves MaybeCompact takes no
// lock of its own: withLock is mkdir-based and NOT reentrant, so a nested acquire
// would spin for the full lockWait before failing.
func TestMaybeCompact_DoesNotDeadlockAgainstCompactLock(t *testing.T) {
	dir := t.TempDir()
	seedChurn(t, dir, 4)

	// Hold the store's lock for the duration, so MaybeCompact runs AGAINST a real
	// competing holder rather than merely against itself. Its stats read and
	// threshold check take no lock and must complete regardless; only the delegated
	// Compact contends, and it must not deadlock.
	held := make(chan struct{})
	released := make(chan struct{})
	go func() {
		_ = withLock(dir, "test-holder", func() error {
			close(held)
			<-released
			return nil
		})
	}()
	<-held

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = MaybeCompact(dir, CompactPolicy{MaxRecords: 1}, ReadOpts{Writer: io.Discard})
	}()

	// A NESTED withLock would block on the holder AND on itself, so it could never
	// finish even after the holder releases. Release, then require completion.
	close(released)
	select {
	case <-done:
	case <-time.After(lockWait / 3):
		t.Fatal("MaybeCompact did not finish well inside lockWait: it is nesting withLock")
	}
}

// TestCompact_OccurrencesStableWhenAShardIsSkipped is the 3.2.A HIGH-1 guard.
//
// Compact does not reach every record: a shard holding a line longer than
// maxLineBytes is left whole rather than rewritten, so that shard's superseded
// detections survive every compaction. An "already counted" test that infers
// counted-ness from a record having been DROPPED therefore re-counts them forever,
// and Occurrences climbs by one per compaction — unbounded, silent, and unattended
// now that compaction is automatic.
func TestCompact_OccurrencesStableWhenAShardIsSkipped(t *testing.T) {
	dir := t.TempDir()
	base := detection("survives in a protected shard", "2026-06-01T00:00:00Z")

	// June: two real detections plus an over-long line, which protects the shard.
	first := base
	first.RunID = "2026-06-01T00:00:00Z-run"
	second := base
	second.RunID = "2026-06-02T00:00:00Z-run"
	second.Timestamp = "2026-06-02T00:00:00Z"
	writeShard(t, dir, "2026-06",
		recordLine(t, first),
		strings.Repeat("x", maxLineBytes+16),
		recordLine(t, second),
	)
	// July: the latest detection, in a shard Compact CAN rewrite.
	third := base
	third.RunID = "2026-07-01T00:00:00Z-run"
	third.Timestamp = "2026-07-01T00:00:00Z"
	writeShard(t, dir, "2026-07", recordLine(t, third))

	occurrences := func() int {
		recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		folded := FoldRecords(recs)
		require.Len(t, folded, 1)
		return folded[0].Occurrences
	}
	require.Equal(t, 3, occurrences(), "three detections before any compaction")

	for i := 1; i <= 5; i++ {
		_, err := Compact(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		assert.Equal(t, 3, occurrences(),
			"compaction %d: a skipped shard's surviving records must not be re-counted", i)
	}
}

// TestCompact_OccurrencesStableWithANonMonthShard covers the second case Compact
// cannot rewrite: a stray .jsonl file that is not a month shard is read for records
// but never rewritten or removed, so its records survive as duplicates.
func TestCompact_OccurrencesStableWithANonMonthShard(t *testing.T) {
	dir := t.TempDir()
	rec := detection("lives in a stray shard", "2026-06-01T00:00:00Z")
	rec.RunID = "2026-06-01T00:00:00Z-run"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "archive.jsonl"),
		[]byte(recordLine(t, rec)+"\n"), 0o600))

	occurrences := func() int {
		recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		folded := FoldRecords(recs)
		require.Len(t, folded, 1)
		return folded[0].Occurrences
	}
	require.Equal(t, 1, occurrences())

	for i := 1; i <= 5; i++ {
		_, err := Compact(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		assert.Equal(t, 1, occurrences(),
			"compaction %d: a non-month shard's duplicate must not be re-counted", i)
	}
}

// TestMaybeCompact_WatermarkRecordedEvenWhenNothingFolds is the 3.2.A HIGH-2 guard.
// A store of only malformed or forward-incompatible lines compacts to a no-op
// (StoreFound false). Without a recorded baseline the growth gate reads a zero
// watermark forever, so every subsequent append re-takes the cross-process lock and
// re-reads the whole store to drop nothing.
func TestMaybeCompact_WatermarkRecordedEvenWhenNothingFolds(t *testing.T) {
	dir := t.TempDir()
	future := sampleRecord("2026-06-14T10:00:00Z-future")
	future.SchemaVersion = SchemaVersion + 1
	future.StampID()
	writeShard(t, dir, "2026-06",
		"{not json",
		"{also not json",
		recordLine(t, future),
		recordLine(t, future),
	)
	policy := CompactPolicy{MaxRecords: 2}

	res, triggered, err := MaybeCompact(dir, policy, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	require.True(t, triggered)
	require.Zero(t, res.RecordsBefore, "nothing foldable: the fold is a no-op")
	require.FileExists(t, filepath.Join(dir, compactWatermarkFile),
		"a no-op fold still records a baseline, or the trigger re-fires forever")

	_, triggered, err = MaybeCompact(dir, policy, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.False(t, triggered, "an unchanged unfoldable store must not re-compact")
}

// TestCompact_ManualCompactionRefreshesTheWatermark pins that the baseline is
// written by Compact rather than by MaybeCompact, so a manual `atcr debt compact`
// cannot leave a stale watermark that provokes a redundant automatic compaction.
func TestCompact_ManualCompactionRefreshesTheWatermark(t *testing.T) {
	dir := t.TempDir()
	seedIDs(t, dir, 10, 3)

	_, err := Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)

	w := readCompactWatermark(dir)
	records, size, err := StoreStats(dir)
	require.NoError(t, err)
	assert.Equal(t, records, w.Records, "the watermark measures the store the same way the trigger does")
	assert.Equal(t, size, w.Bytes)

	_, triggered, err := MaybeCompact(dir, CompactPolicy{MaxRecords: 1}, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.False(t, triggered, "a store the user just compacted is not immediately re-compacted")
}

func TestMaybeCompact_ShrinkingStoreDoesNotTrigger(t *testing.T) {
	dir := t.TempDir()
	seedIDs(t, dir, 10, 3)
	_, err := Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)

	// Remove a shard behind the store's back: current size is now BELOW the
	// watermark, which must read as "no growth", not as an unsigned surprise.
	require.NoError(t, os.Remove(filepath.Join(dir, "2026-06.jsonl")))
	_, triggered, err := MaybeCompact(dir, CompactPolicy{MaxRecords: 1}, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.False(t, triggered)
}

func TestGrewPast_MeasuresFiftyPercentWithoutTruncation(t *testing.T) {
	// Cross-multiplied rather than dividing the watermark: integer division would
	// demand 66% growth at a watermark of 3 and 100% at a watermark of 1.
	for _, tc := range []struct {
		current, watermark int64
		want               bool
	}{
		{current: 3, watermark: 2, want: false}, // exactly +50% is not PAST +50%...
		{current: 4, watermark: 2, want: true},  // ...but this clearly is
		{current: 5, watermark: 3, want: true},  // 66%
		{current: 4, watermark: 3, want: false}, // 33% — below the bar
		{current: 15, watermark: 10, want: false},
		{current: 16, watermark: 10, want: true},
		{current: 1, watermark: 0, want: true}, // never compacted: thresholds decide
		{current: 0, watermark: 0, want: false},
	} {
		assert.Equalf(t, tc.want, grewPast(tc.current, tc.watermark),
			"grewPast(%d, %d)", tc.current, tc.watermark)
	}
}

// TestMaybeCompact_ByteThresholdBoundary pins the byte clause at its actual
// boundary rather than with a MaxBytes any non-empty store trips.
func TestMaybeCompact_ByteThresholdBoundary(t *testing.T) {
	dir := t.TempDir()
	seedChurn(t, dir, 3)
	_, size, err := StoreStats(dir)
	require.NoError(t, err)

	_, triggered, err := MaybeCompact(dir,
		CompactPolicy{MaxRecords: 1_000_000, MaxBytes: size + 1}, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.False(t, triggered, "one byte above the store size does not trip")

	_, triggered, err = MaybeCompact(dir,
		CompactPolicy{MaxRecords: 1_000_000, MaxBytes: size}, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.True(t, triggered, "the threshold is inclusive: size >= MaxBytes trips")
}

// TestCompact_PreservesQualitySignalModelRecovery is the Phase 3 gate's HIGH guard.
//
// AggregateQualitySignal recovers a missing Model from an earlier same-id terminal
// record, which is how a wontfix that outranks an attributed resolution still
// reports a dismissal. Compaction used to drop that donor, so the whole outcome
// vanished from the signal — and T5 made compaction automatic inside the very
// reconcile that emits the signal, so one invocation could compact and then emit
// something different from what it would have emitted moments earlier.
func TestCompact_PreservesQualitySignalModelRecovery(t *testing.T) {
	dir := t.TempDir()
	base := detection("attributed then dismissed", "2026-06-01T10:00:00Z")
	base.Reviewers = []string{"bruce"}
	base.Model = "claude-x"

	open := base
	open.RunID = "2026-06-01T10:00:00Z-run"
	resolved := base
	resolved.RunID = "2026-06-01T11:00:00Z-resolved"
	resolved.Timestamp = "2026-06-01T11:00:00Z"
	resolved.Status = "resolved"
	// The dismissal outranks the resolution and carries NO model attribution.
	dismissed := base
	dismissed.RunID = "2026-06-01T12:00:00Z-wontfix"
	dismissed.Timestamp = "2026-06-01T12:00:00Z"
	dismissed.Status = "wontfix"
	dismissed.Model = ""
	for _, r := range []Record{open, resolved, dismissed} {
		require.NoError(t, Append(dir, r))
	}

	signal := func() []QualityRow {
		recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		return AggregateQualitySignal(recs)
	}
	want := signal()
	require.Equal(t, []QualityRow{{Persona: "bruce", Model: "claude-x", DismissedCount: 1}}, want,
		"before compaction the model is recovered from the earlier resolution")

	for i := 1; i <= 3; i++ {
		_, err := Compact(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		assert.Equal(t, want, signal(),
			"compaction %d must not change what the quality signal reports", i)
	}

	// Retention is still bounded: the effective record plus one donor, not a
	// growing trail.
	recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.Len(t, recs, 2, "an id retains at most two records after compaction")
}

// TestCompact_TiedTerminalDonorDoesNotDisplaceTheEffectiveRecord is the gate
// pass-2 HIGH guard.
//
// The model donor and the effective record are both terminal, so they can tie on
// BOTH timestamp and ClosedStatusRank — and latestItem breaks a full tie by append
// order, last wins. Emitting the donor after the effective record therefore handed
// it the fold: readers saw a different record than before compaction, and the NEXT
// compaction deleted the displaced one along with the human-typed --reason only it
// carried.
func TestCompact_TiedTerminalDonorDoesNotDisplaceTheEffectiveRecord(t *testing.T) {
	dir := t.TempDir()
	const tied = "2026-06-01T10:00:00Z"
	base := detection("tied terminals", tied)
	base.Reviewers = []string{"bruce"}

	// Appended first: attributed, with its own rationale.
	donor := base
	donor.RunID = tied + "-donor"
	donor.Status = "wontfix"
	donor.Model = "claude-x"
	donor.Justification = "DONOR-REASON"
	// Appended last at the SAME timestamp and rank: this one is effective.
	eff := base
	eff.RunID = tied + "-eff"
	eff.Status = "wontfix"
	eff.Model = ""
	eff.Justification = "EFFECTIVE-REASON"
	require.NoError(t, Append(dir, donor))
	require.NoError(t, Append(dir, eff))

	effective := func() Record {
		recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		folded := FoldRecords(recs)
		require.Len(t, folded, 1)
		return folded[0]
	}
	signal := func() []QualityRow {
		recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		return AggregateQualitySignal(recs)
	}
	wantJustification := effective().Justification
	require.Equal(t, "EFFECTIVE-REASON", wantJustification, "the last-appended record wins a full tie")
	wantSignal := signal()

	for i := 1; i <= 3; i++ {
		_, err := Compact(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		assert.Equal(t, wantJustification, effective().Justification,
			"compaction %d must not swap which record readers see as effective", i)
		assert.Equal(t, wantSignal, signal(),
			"compaction %d must not change the aggregated signal", i)
	}
}

// TestFoldRecords_ManualFilingIsASighting covers both halves of the sighting rule:
// a hand-filed item counts even when it carries a status, and filing one for an id
// that already has history ADDS to the count rather than resetting it.
func TestFoldRecords_ManualFilingIsASighting(t *testing.T) {
	filed := detection("filed by hand", "2026-06-01T00:00:00Z")
	filed.Status = "deferred"
	filed.Origin = OriginManual

	folded := FoldRecords([]Record{filed})
	require.Len(t, folded, 1)
	assert.Equal(t, 1, folded[0].Occurrences, "a filed item is its own first sighting")

	redetected := filed
	redetected.Status = ""
	redetected.Origin = ""
	redetected.Timestamp = "2026-06-02T00:00:00Z"
	folded = FoldRecords([]Record{filed, redetected})
	require.Len(t, folded, 1)
	assert.Equal(t, 2, folded[0].Occurrences,
		"a re-detection after a manual filing is the second sighting, so it reports one regression")
}

// TestFoldRecords_ManualFilingNeverDecreasesAnExistingCount pins the failure the
// carrier-stamp approach introduced: filing an item whose file/line/problem hashes
// to an id the store already holds must not erase that id's history.
func TestFoldRecords_ManualFilingNeverDecreasesAnExistingCount(t *testing.T) {
	first := detection("already tracked", "2026-06-01T00:00:00Z")
	second := first
	second.Timestamp = "2026-06-02T00:00:00Z"
	require.Equal(t, 2, FoldRecords([]Record{first, second})[0].Occurrences)

	filed := first
	filed.Timestamp = "2026-06-03T00:00:00Z"
	filed.Status = "deferred"
	filed.Origin = OriginManual

	folded := FoldRecords([]Record{first, second, filed})
	require.Len(t, folded, 1)
	assert.Equal(t, 3, folded[0].Occurrences,
		"filing a duplicate of a tracked id adds a sighting; it must never decrease the count")
}

// TestFoldRecords_ResolutionOfAManuallyFiledItemIsNotASighting guards the
// discriminator's precision: `atcr debt resolve` copies the folded record and so
// inherits Origin, but a resolution always stamps ResolvedAt and a filing never
// does.
func TestFoldRecords_ResolutionOfAManuallyFiledItemIsNotASighting(t *testing.T) {
	filed := detection("filed then resolved", "2026-06-01T00:00:00Z")
	filed.Status = "deferred"
	filed.Origin = OriginManual

	resolution := filed
	resolution.Timestamp = "2026-06-02T00:00:00Z"
	resolution.Status = "resolved"
	resolution.ResolvedAt = "2026-06-02T00:00:00Z"

	folded := FoldRecords([]Record{filed, resolution})
	require.Len(t, folded, 1)
	assert.Equal(t, 1, folded[0].Occurrences,
		"resolving a manually filed item is a marker on it, not a second sighting")
}

// TestCompact_OccurrencesStableWhenASuppressingRecordWinsTheFold is the gate
// pass-3 HIGH guard.
//
// Rule 1 makes a wontfix record win unconditionally, so the carrier is NOT the
// group's newest record. A sighting newer than it that survives compaction — in a
// shard the rewrite cannot reach — is then newer than the carrier's own timestamp
// too, so a boundary derived from that timestamp re-counts it on every fold.
// CountedThrough is what makes the boundary independent of fold precedence.
func TestCompact_OccurrencesStableWhenASuppressingRecordWinsTheFold(t *testing.T) {
	for _, tc := range []struct {
		name    string
		survive func(t *testing.T, dir string, rec Record)
	}{
		{
			name: "protected shard",
			survive: func(t *testing.T, dir string, rec Record) {
				t.Helper()
				writeShard(t, dir, "2026-06", recordLine(t, rec), strings.Repeat("x", maxLineBytes+16))
			},
		},
		{
			name: "non-month shard",
			survive: func(t *testing.T, dir string, rec Record) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "archive.jsonl"),
					[]byte(recordLine(t, rec)+"\n"), 0o600))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			base := detection("dismissed then re-sighted", "2026-05-01T00:00:00Z")

			// The wontfix is OLDER, and rule 1 makes it effective regardless.
			dismissed := base
			dismissed.RunID = "2026-05-01T00:00:00Z-wontfix"
			dismissed.Status = "wontfix"
			dismissed.Justification = "accepted pattern"
			writeShard(t, dir, "2026-05", recordLine(t, dismissed))

			// A NEWER sighting, parked where compaction cannot rewrite it.
			later := base
			later.RunID = "2026-06-01T00:00:00Z-run"
			later.Timestamp = "2026-06-01T00:00:00Z"
			tc.survive(t, dir, later)

			occurrences := func() int {
				recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
				require.NoError(t, err)
				folded := FoldRecords(recs)
				require.Len(t, folded, 1)
				require.Equal(t, "wontfix", folded[0].Status, "rule 1: the suppressing record wins")
				return folded[0].Occurrences
			}
			want := occurrences()

			for i := 1; i <= 5; i++ {
				_, err := Compact(dir, ReadOpts{Writer: io.Discard})
				require.NoError(t, err)
				assert.Equal(t, want, occurrences(),
					"compaction %d: a survivor NEWER than the carrier must not be re-counted", i)
			}
		})
	}
}

// TestCompact_DeferredEffectiveRecordEmitsNoSignalEitherWay pins why modelDonor's
// SETTLED-only gate is exactly right rather than accidentally narrow.
//
// foldTerminalByID admits any CLOSED effective record, `deferred` included, and
// recovers a Model for it — which reads as though a deferred-effective id could
// depend on the donor. It cannot: AggregateQualitySignal's status switch maps
// anything that is neither wontfix nor resolved to no counter and no group, so such
// an id emits no row whatever its attribution. Nothing for compaction to preserve,
// and nothing for the donor to be needed for.
func TestCompact_DeferredEffectiveRecordEmitsNoSignalEitherWay(t *testing.T) {
	dir := t.TempDir()
	base := detection("resolved then deferred", "2026-06-01T10:00:00Z")
	base.Reviewers = []string{"bruce"}
	base.Model = "claude-x"

	resolved := base
	resolved.RunID = "2026-06-01T11:00:00Z-resolved"
	resolved.Timestamp = "2026-06-01T11:00:00Z"
	resolved.Status = "resolved"
	resolved.ResolvedAt = resolved.Timestamp
	// A later deferral with no attribution becomes the effective record.
	deferred := base
	deferred.RunID = "2026-06-01T12:00:00Z-deferred"
	deferred.Timestamp = "2026-06-01T12:00:00Z"
	deferred.Status = "deferred"
	deferred.Model = ""
	for _, r := range []Record{base, resolved, deferred} {
		require.NoError(t, Append(dir, r))
	}

	signal := func() []QualityRow {
		recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		return AggregateQualitySignal(recs)
	}
	require.Empty(t, signal(),
		"a deferred-effective id contributes to neither counter, so it emits no row")

	for i := 1; i <= 3; i++ {
		_, err := Compact(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		assert.Empty(t, signal(), "compaction %d: still no row, so there is nothing to preserve", i)
	}

	// Re-settling the id DOES put it back in the signal, and that path is settled,
	// so the donor covers it — which is the case modelDonor is gated on.
	dismissed := deferred
	dismissed.RunID = "2026-06-01T13:00:00Z-wontfix"
	dismissed.Timestamp = "2026-06-01T13:00:00Z"
	dismissed.Status = "wontfix"
	require.NoError(t, Append(dir, dismissed))
	want := signal()
	require.Equal(t, []QualityRow{{Persona: "bruce", Model: "claude-x", DismissedCount: 1}}, want)
	for i := 1; i <= 3; i++ {
		_, err := Compact(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		assert.Equal(t, want, signal(), "compaction %d must not change the settled id's signal", i)
	}
}

// TestCompact_AggregateSurvivesAProtectedEffectiveShard is the gate pass-4 HIGH
// guard — the deflation mirror of the CountedThrough inflation case.
//
// When the effective record's month is protected, Compact never writes it, so the
// Occurrences/FirstSeen that live only on the folded record are thrown away — while
// the id's other shards, holding the sightings that aggregate summarized, are still
// rewritten or removed. The count deflates unrecomputably.
func TestCompact_AggregateSurvivesAProtectedEffectiveShard(t *testing.T) {
	t.Run("bare sighting pair", func(t *testing.T) {
		dir := t.TempDir()
		base := detection("older sighting elsewhere", "2026-05-01T00:00:00Z")
		older := base
		older.RunID = "2026-05-01T00:00:00Z-run"
		writeShard(t, dir, "2026-05", recordLine(t, older))

		// The NEWER sighting — the effective record — shares its shard with an
		// over-long line, so that shard cannot be rewritten.
		newer := base
		newer.RunID = "2026-06-01T00:00:00Z-run"
		newer.Timestamp = "2026-06-01T00:00:00Z"
		writeShard(t, dir, "2026-06", recordLine(t, newer), strings.Repeat("x", maxLineBytes+16))

		fold := func() Record {
			recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
			require.NoError(t, err)
			folded := FoldRecords(recs)
			require.Len(t, folded, 1)
			return folded[0]
		}
		want := fold()
		require.Equal(t, 2, want.Occurrences)
		require.Equal(t, "2026-05-01T00:00:00Z", want.FirstSeen)

		for i := 1; i <= 4; i++ {
			_, err := Compact(dir, ReadOpts{Writer: io.Discard})
			require.NoError(t, err)
			got := fold()
			assert.Equal(t, want.Occurrences, got.Occurrences, "compaction %d must not deflate the count", i)
			assert.Equal(t, want.FirstSeen, got.FirstSeen, "compaction %d must not lose the first sighting", i)
		}
	})

	t.Run("carrier elsewhere, regression in the protected shard", func(t *testing.T) {
		dir := t.TempDir()
		base := detection("carrier elsewhere", "2026-05-01T00:00:00Z")

		// A compacted carrier in a rewritable shard.
		carrier := base
		carrier.RunID = "2026-05-01T00:00:00Z-resolved"
		carrier.Status = "resolved"
		carrier.ResolvedAt = "2026-05-01T00:00:00Z"
		carrier.Occurrences = 4
		carrier.FirstSeen = "2026-04-01T00:00:00Z"
		carrier.CountedThrough = "2026-04-01T00:00:00Z"
		writeShard(t, dir, "2026-05", recordLine(t, carrier))

		regression := base
		regression.RunID = "2026-06-01T00:00:00Z-run"
		regression.Timestamp = "2026-06-01T00:00:00Z"
		writeShard(t, dir, "2026-06", recordLine(t, regression), strings.Repeat("x", maxLineBytes+16))

		fold := func() Record {
			recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
			require.NoError(t, err)
			folded := FoldRecords(recs)
			require.Len(t, folded, 1)
			return folded[0]
		}
		want := fold()
		require.Equal(t, 5, want.Occurrences, "the carrier's 4 plus the regression")
		require.Equal(t, "2026-04-01T00:00:00Z", want.FirstSeen)

		for i := 1; i <= 4; i++ {
			_, err := Compact(dir, ReadOpts{Writer: io.Discard})
			require.NoError(t, err)
			got := fold()
			assert.Equal(t, want.Occurrences, got.Occurrences, "compaction %d must not deflate the count", i)
			assert.Equal(t, want.FirstSeen, got.FirstSeen, "compaction %d must not lose the first sighting", i)
		}
	})
}

// TestCompact_UncompactableIDDoesNotGrowTheStore guards the pass-through against
// the growth shape the gate found: a record whose physical .jsonl file is not its
// run_id's month shard already exists twice on disk (readAllPreserving re-emits it
// into the month shard without rewriting the original). Passing BOTH copies through
// would write a third next run, then a fourth — unbounded, with every duplicate
// counted as another sighting.
func TestCompact_UncompactableIDDoesNotGrowTheStore(t *testing.T) {
	dir := t.TempDir()
	rec := detection("duplicated across shards", "2026-05-01T00:00:00Z")
	rec.RunID = "2026-05-01T00:00:00Z-run"
	// A stray non-month file holding a record whose run_id month is 2026-05.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "archive.jsonl"),
		[]byte(recordLine(t, rec)+"\n"), 0o600))

	// And the id's effective record parked in a protected shard, which is what makes
	// the id uncompactable.
	effective := rec
	effective.RunID = "2026-06-01T00:00:00Z-run"
	effective.Timestamp = "2026-06-01T00:00:00Z"
	writeShard(t, dir, "2026-06", recordLine(t, effective), strings.Repeat("x", maxLineBytes+16))

	stats := func() (int, int) {
		t.Helper()
		lines, _, err := StoreStats(dir)
		require.NoError(t, err)
		recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		folded := FoldRecords(recs)
		require.Len(t, folded, 1)
		return lines, folded[0].Occurrences
	}

	_, err := Compact(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	wantLines, wantOcc := stats()

	for i := 2; i <= 6; i++ {
		_, err := Compact(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		gotLines, gotOcc := stats()
		assert.Equal(t, wantLines, gotLines, "compaction %d must not grow the store", i)
		assert.Equal(t, wantOcc, gotOcc, "compaction %d must not inflate the count", i)
	}
}

// BenchmarkAppendBatch500 measures a 500-finding reconcile run's append phase
// under ONE lock cycle (TD internal/localdebt/reconcile.go:204): the per-record
// Append path it replaces paid a withLock acquisition — MkdirAll, lock-dir
// mkdir, owner-file write, RemoveAll — per finding, roughly 6 syscalls each.
func BenchmarkAppendBatch500(b *testing.B) {
	recs := make([]Record, 500)
	for i := range recs {
		r := Record{
			SchemaVersion: SchemaVersion,
			RunID:         "2026-08-01T00:00:00Z-bench",
			Timestamp:     "2026-08-01T00:00:00Z",
			Severity:      "LOW",
			File:          "f.go",
			Line:          i + 1,
			Problem:       "benchmark finding",
		}
		r.StampID()
		recs[i] = r
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dir := filepath.Join(b.TempDir(), "store")
		if _, err := appendBatch(dir, recs); err != nil {
			b.Fatal(err)
		}
	}
}
