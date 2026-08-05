package localdebt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordLine marshals rec to its on-disk JSONL form, for composing shard fixtures
// with store_test.go's writeShard helper.
func recordLine(t *testing.T, rec Record) string {
	t.Helper()
	line, err := json.Marshal(rec)
	require.NoError(t, err)
	return string(line)
}

// shardPath is the on-disk path of a month shard under dir.
func shardPath(dir, month string) string {
	return filepath.Join(dir, month+".jsonl")
}

// idRecord is sampleRecord with a distinct Problem (hence a distinct stamped id)
// so a fixture can hold many independent findings.
func idRecord(runID, problem string) Record {
	rec := sampleRecord(runID)
	rec.Problem = problem
	rec.StampID()
	return rec
}

// TestStreamSummaries_MatchesReadAllIDs pins full id-set parity between the
// minimal-decode walker and ReadAll on a corpus that mixes valid, malformed, and
// forward-incompatible lines across shards — the property that makes the streaming
// dedup seed safe. Order matters too: the fold's full-tie rule is positional
// ("last append wins"), so both paths must visit shards in os.ReadDir lexical
// order and lines in file order.
func TestStreamSummaries_MatchesReadAllIDs(t *testing.T) {
	dir := t.TempDir()

	future := sampleRecord("2026-06-14T10:00:00Z-future")
	future.SchemaVersion = SchemaVersion + 1
	future.Problem = "from a newer binary"
	future.StampID()

	writeShard(t, dir, "2026-06",
		recordLine(t, idRecord("2026-06-14T10:00:00Z-a", "jun first")),
		"{not json",
		recordLine(t, future),
		recordLine(t, idRecord("2026-06-20T10:00:00Z-b", "jun second")),
	)
	writeShard(t, dir, "2026-07",
		recordLine(t, idRecord("2026-07-01T10:00:00Z-c", "jul first")),
	)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore\n"), 0o600))

	recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	wantIDs := make([]string, 0, len(recs))
	for _, r := range recs {
		wantIDs = append(wantIDs, r.ID)
	}
	require.Len(t, wantIDs, 3, "the malformed and forward-incompatible lines are skipped")

	var gotIDs []string
	require.NoError(t, StreamSummaries(dir, ReadOpts{Writer: io.Discard}, func(s Summary) error {
		gotIDs = append(gotIDs, s.ID)
		return nil
	}))
	assert.Equal(t, wantIDs, gotIDs,
		"the streaming path yields exactly ReadAll's ids, in the same shard and line order")
}

// TestStreamSummaries_ProjectsFoldFields asserts the Summary carries every field
// the fold and the resolve selection read — id, status, severity, ts, and whether
// a File is present — and nothing that would retain the record's bulk.
func TestStreamSummaries_ProjectsFoldFields(t *testing.T) {
	dir := t.TempDir()
	rec := idRecord("2026-06-14T10:00:00Z-a", "projected")
	rec.Status = "deferred"
	fileless := idRecord("2026-06-14T10:00:00Z-b", "no file")
	fileless.File = ""
	fileless.StampID()
	writeShard(t, dir, "2026-06", recordLine(t, rec), recordLine(t, fileless))

	sums, err := ReadSummaries(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	require.Len(t, sums, 2)

	assert.Equal(t, Summary{
		ID:        rec.ID,
		Status:    "deferred",
		Severity:  rec.Severity,
		Timestamp: rec.Timestamp,
		HasFile:   true,
	}, sums[0])
	assert.False(t, sums[1].HasFile, "an empty File is projected as HasFile false, without retaining the path")
}

// TD: `debt list` and `debt dashboard` read every record in full (ReadAll +
// FoldRecords) — 327/355 MB RSS on a 100k-record store versus 145 MB for
// `debt resolve --list`, which selects on summaries and hydrates only the
// survivors. Selecting the same way needs the fields those two filter and sort
// on: File and Line (the --component filter and sortDebt's location tiebreak),
// Category (--category) and EstMinutes (--sort=est). The projection carries
// them; it still carries nothing of the record's bulk (problem, fix, evidence,
// justification, reviewers, source_report).
func TestStreamSummaries_ProjectsTheFieldsListAndDashboardSelectOn(t *testing.T) {
	dir := t.TempDir()
	rec := idRecord("2026-06-14T10:00:00Z-a", "projected")
	rec.File = "internal/autofix/apply.go"
	rec.Line = 108
	rec.Category = "correctness"
	rec.EstMinutes = 45
	rec.StampID()
	writeShard(t, dir, "2026-06", recordLine(t, rec))

	sums, err := ReadSummaries(dir, ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	require.Len(t, sums, 1)

	assert.Equal(t, "internal/autofix/apply.go", sums[0].File, "--component filters on the path")
	assert.Equal(t, 108, sums[0].Line, "sortDebt's location tiebreak needs the line")
	assert.Equal(t, "correctness", sums[0].Category, "--category filters on it")
	assert.Equal(t, 45, sums[0].EstMinutes, "--sort=est orders on it")
	assert.True(t, sums[0].HasFile, "and the existing projection is unchanged")
}

// TestDecodeSummary_SkipsMalformedLine locks skip-and-warn parity with
// decodeRecord on a corrupt line: same outcome, byte-identical warning.
func TestDecodeSummary_SkipsMalformedLine(t *testing.T) {
	line := []byte("{not json")
	var sumDiag, recDiag bytes.Buffer

	_, ok := decodeSummary(line, "2026-06.jsonl", &sumDiag)
	assert.False(t, ok, "a malformed line is skipped")
	_, outcome := decodeRecord(line, "2026-06.jsonl", &recDiag)
	require.Equal(t, decodeMalformed, outcome)

	assert.Contains(t, sumDiag.String(), MsgMalformedSkip)
	assert.Equal(t, recDiag.String(), sumDiag.String(),
		"both decode paths emit the same warning for the same line")
}

// TestDecodeSummary_SkipsFutureSchemaVersion pins cross-path consistency on the
// schema gate. A record visible to one decoder and invisible to the other would
// corrupt dedup: an id present in the store but absent from the seed re-appends.
func TestDecodeSummary_SkipsFutureSchemaVersion(t *testing.T) {
	rec := sampleRecord("2026-06-14T10:00:00Z-future")
	rec.SchemaVersion = SchemaVersion + 1
	rec.StampID()
	line := []byte(recordLine(t, rec))

	var sumDiag, recDiag bytes.Buffer
	_, ok := decodeSummary(line, "2026-06.jsonl", &sumDiag)
	assert.False(t, ok)
	_, outcome := decodeRecord(line, "2026-06.jsonl", &recDiag)
	assert.Equal(t, decodeForwardIncompatible, outcome,
		"the same line is forward-incompatible to the full decoder")

	assert.Contains(t, sumDiag.String(), fmt.Sprintf("unsupported schema_version %d (> %d)", SchemaVersion+1, SchemaVersion))
	assert.Equal(t, recDiag.String(), sumDiag.String())
}

// TestDecodeSummary_SkipsMissingRequiredField covers the third gate: a record
// missing run_id or id is malformed to both decoders.
func TestDecodeSummary_SkipsMissingRequiredField(t *testing.T) {
	for name, mutate := range map[string]func(*Record){
		"missing run_id": func(r *Record) { r.RunID = "" },
		"missing id":     func(r *Record) { r.ID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			rec := sampleRecord("2026-06-14T10:00:00Z-a")
			rec.StampID()
			mutate(&rec)
			line := []byte(recordLine(t, rec))

			var sumDiag, recDiag bytes.Buffer
			_, ok := decodeSummary(line, "2026-06.jsonl", &sumDiag)
			assert.False(t, ok)
			_, outcome := decodeRecord(line, "2026-06.jsonl", &recDiag)
			assert.Equal(t, decodeMalformed, outcome)
			assert.Contains(t, sumDiag.String(), "missing required field (run_id or id)")
			assert.Equal(t, recDiag.String(), sumDiag.String())
		})
	}
}

// TestStreamSummaries_SkipsOverLongLine asserts an oversized line is drained (not
// buffered) and the shard keeps yielding — the bufio.Reader-over-Scanner property
// the full read path relies on.
func TestStreamSummaries_SkipsOverLongLine(t *testing.T) {
	dir := t.TempDir()
	good := idRecord("2026-06-14T10:00:00Z-a", "after the giant line")
	writeShard(t, dir, "2026-06",
		strings.Repeat("x", maxLineBytes+16),
		recordLine(t, good),
	)

	var diag bytes.Buffer
	var got []string
	require.NoError(t, StreamSummaries(dir, ReadOpts{Writer: &diag}, func(s Summary) error {
		got = append(got, s.ID)
		return nil
	}))
	assert.Equal(t, []string{good.ID}, got, "the valid line after an over-long one is still yielded")
	assert.Contains(t, diag.String(), "skipping over-long line")
}

// TestStreamSummaries_MissingDir matches ReadAll's "no backlog yet" convention.
func TestStreamSummaries_MissingDir(t *testing.T) {
	var got int
	err := StreamSummaries(filepath.Join(t.TempDir(), "nope"), ReadOpts{}, func(Summary) error {
		got++
		return nil
	})
	require.NoError(t, err, "a missing store directory is empty, not an error")
	assert.Zero(t, got)
}

// TestStreamSummaries_ErrorDoesNotLeakAbsolutePath keeps the new walker inside the
// package's path-redaction contract (store.go SECURITY block).
func TestStreamSummaries_ErrorDoesNotLeakAbsolutePath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a permission-denied open cannot be provoked as root")
	}
	dir := t.TempDir()
	writeShard(t, dir, "2026-06", "{}")
	path := shardPath(dir, "2026-06")
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	err := StreamSummaries(dir, ReadOpts{Writer: io.Discard}, func(Summary) error { return nil })
	require.Error(t, err)
	assert.False(t, os.IsNotExist(err), "EACCES is a real failure, not a missing-file signal")
	assert.NotContains(t, err.Error(), dir,
		"a shard error must not embed the absolute (username-bearing) store path")
}

// TestStreamSummaries_NilWriterDoesNotPanic covers both unset-writer shapes: a
// zero ReadOpts and a typed-nil io.Writer, which is non-nil as an interface yet
// panics on first Write.
func TestStreamSummaries_NilWriterDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	writeShard(t, dir, "2026-06", "{not json")

	for name, opts := range map[string]ReadOpts{
		"zero opts": {},
		"typed nil": {Writer: (*bytes.Buffer)(nil)},
	} {
		t.Run(name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				_ = StreamSummaries(dir, opts, func(Summary) error { return nil })
			})
		})
	}
}

// TestStreamSummaries_CallbackErrorAborts asserts a callback error stops the walk
// and propagates unwrapped, so a caller can use it as an early-exit signal.
func TestStreamSummaries_CallbackErrorAborts(t *testing.T) {
	dir := t.TempDir()
	writeShard(t, dir, "2026-06",
		recordLine(t, idRecord("2026-06-14T10:00:00Z-a", "first")),
		recordLine(t, idRecord("2026-06-15T10:00:00Z-b", "second")),
	)
	writeShard(t, dir, "2026-07",
		recordLine(t, idRecord("2026-07-01T10:00:00Z-c", "third")),
	)

	sentinel := errors.New("stop here")
	seen := 0
	err := StreamSummaries(dir, ReadOpts{Writer: io.Discard}, func(Summary) error {
		seen++
		return sentinel
	})
	require.ErrorIs(t, err, sentinel, "the callback's error propagates unwrapped")
	assert.Equal(t, 1, seen, "the walk stops at the first callback error")
}

// TestReadSummaries_ReturnsPartialResultsOnError locks the documented shape:
// summaries decoded before a mid-walk error are returned ALONGSIDE the error
// rather than discarded, so a caller can salvage what was readable.
//
// Note what this does NOT license. Folding a partial result yields wrong effective
// statuses, so the dedup seed deliberately throws it away — see
// TestPersistLocalDebt_PartialReadDoesNotSuppressAReopen in cli/, which is the
// guard against re-introducing a partial seed as an optimization.
func TestReadSummaries_ReturnsPartialResultsOnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a permission-denied open cannot be provoked as root")
	}
	dir := t.TempDir()
	readable := idRecord("2026-06-14T10:00:00Z-a", "readable shard")
	writeShard(t, dir, "2026-06", recordLine(t, readable))
	writeShard(t, dir, "2026-07", recordLine(t, idRecord("2026-07-01T10:00:00Z-b", "blocked shard")))
	blocked := shardPath(dir, "2026-07")
	require.NoError(t, os.Chmod(blocked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o600) })

	sums, err := ReadSummaries(dir, ReadOpts{Writer: io.Discard})
	require.Error(t, err)
	require.Len(t, sums, 1, "summaries read before the failing shard are returned with the error")
	assert.Equal(t, readable.ID, sums[0].ID)
}

// TestFoldSummaries_MatchesFoldRecords pins the two instantiations of the generic
// fold together. The corpus deliberately includes a resolved record followed by a
// LATER open record (the AC3 regression shape) and a wontfix group, because those
// are the cases where a divergent precedence rule would silently revert Task 03 on
// the dedup-seed side while FoldRecords' own tests stayed green.
func TestFoldSummaries_MatchesFoldRecords(t *testing.T) {
	mk := func(runID, problem, status, ts, sev string) Record {
		r := idRecord(runID, problem)
		r.Status = status
		r.Timestamp = ts
		r.Severity = sev
		return r
	}

	regressedOpen := mk("2026-06-01T00:00:00Z-a", "regresses", "", "2026-06-01T00:00:00Z", "HIGH")
	regressedFix := regressedOpen
	regressedFix.Status = "resolved"
	regressedFix.Timestamp = "2026-06-02T00:00:00Z"
	regressedAgain := regressedOpen
	regressedAgain.Timestamp = "2026-06-03T00:00:00Z"
	regressedAgain.Severity = "CRITICAL"

	dismissedOpen := mk("2026-06-01T00:00:00Z-b", "dismissed", "", "2026-06-01T00:00:00Z", "LOW")
	dismissed := dismissedOpen
	dismissed.Status = "wontfix"
	dismissed.Timestamp = "2026-06-02T00:00:00Z"
	dismissedAgain := dismissedOpen
	dismissedAgain.Timestamp = "2026-06-04T00:00:00Z"

	// A same-timestamp pair: rank breaks the tie, so the terminal record wins.
	tiedOpen := mk("2026-06-05T00:00:00Z-c", "tied", "", "2026-06-05T00:00:00Z", "MEDIUM")
	tiedTerm := tiedOpen
	tiedTerm.Status = "deferred"

	stillOpen := mk("2026-06-06T00:00:00Z-d", "plain open", "", "2026-06-06T00:00:00Z", "LOW")

	recs := []Record{
		regressedOpen, regressedFix, regressedAgain,
		dismissedOpen, dismissed, dismissedAgain,
		tiedOpen, tiedTerm,
		stillOpen,
	}
	sums := make([]Summary, 0, len(recs))
	for _, r := range recs {
		sums = append(sums, Summary{ID: r.ID, Status: r.Status, Severity: r.Severity, Timestamp: r.Timestamp, HasFile: r.File != ""})
	}

	foldedRecs := FoldRecords(recs)
	foldedSums := FoldSummaries(sums)
	require.Len(t, foldedSums, len(foldedRecs))
	for i := range foldedRecs {
		assert.Equal(t, foldedRecs[i].ID, foldedSums[i].ID, "identical id ordering")
		assert.Equal(t, foldedRecs[i].Status, foldedSums[i].Status, "identical winning status per id")
		assert.Equal(t, foldedRecs[i].Timestamp, foldedSums[i].Timestamp, "identical winning occurrence per id")
	}

	// Spot-check the two cases the seed's correctness turns on.
	assert.Empty(t, foldedSums[0].Status, "a resolved id re-detected later folds back to OPEN")
	assert.Equal(t, "wontfix", foldedSums[1].Status, "a dismissal survives a later re-detection")
}

// generateAllocFixture writes n ~500-byte records across three month shards
// directly (not via Append, whose per-record lock would dominate the setup), and
// returns the store dir.
func generateAllocFixture(tb testing.TB, n int) string {
	tb.Helper()
	dir := tb.TempDir()
	months := []string{"2026-05", "2026-06", "2026-07"}
	pad := strings.Repeat("detail ", 12) // ~84 bytes per padded field
	bufs := make([]bytes.Buffer, len(months))
	for i := 0; i < n; i++ {
		m := i % len(months)
		rec := Record{
			SchemaVersion: SchemaVersion,
			RunID:         months[m] + "-14T10:00:00Z-run",
			Timestamp:     fmt.Sprintf("%s-14T10:%02d:%02dZ", months[m], i/60%60, i%60),
			Severity:      "HIGH",
			File:          fmt.Sprintf("internal/pkg%02d/file%04d.go", i%20, i),
			Line:          i%500 + 1,
			Problem:       fmt.Sprintf("finding %d: %s", i, pad),
			Fix:           "apply the documented fix: " + pad,
			Category:      "correctness",
			EstMinutes:    30,
			Evidence:      "evidence for the finding: " + pad,
			Reviewers:     []string{"claude", "security", "performance"},
			Confidence:    "HIGH",
			Justification: "justification text: " + pad,
		}
		rec.StampID()
		line, err := json.Marshal(rec)
		require.NoError(tb, err)
		bufs[m].Write(line)
		bufs[m].WriteByte('\n')
	}
	for m, month := range months {
		require.NoError(tb, os.WriteFile(filepath.Join(dir, month+".jsonl"), bufs[m].Bytes(), 0o600))
	}
	return dir
}

// TestStreamSummaries_AllocatesFarLessThanReadAll is AC6's second clause: the
// minimal path must be materially cheaper, not merely different.
//
// It asserts a RATIO rather than absolute byte counts, skips under -short, avoids
// t.Parallel, and forces a GC before each measurement, so it pins the property
// without degrading into a flaky micro-benchmark across Go releases. The 5x floor
// is deliberately conservative — the expected gap is roughly an order of
// magnitude — and both measured values are printed on failure so a regression is
// diagnosable without re-running by hand.
func TestStreamSummaries_AllocatesFarLessThanReadAll(t *testing.T) {
	if testing.Short() {
		t.Skip("allocation fixture is ~10 MB")
	}
	dir := generateAllocFixture(t, 20000)

	// measure runs one read path and reports the bytes it churned and the bytes it
	// still holds with its result live.
	//
	// Both snapshots are taken immediately after a forced GC, which is what makes
	// the retained figure reproducible: without the second GC, HeapAlloc also
	// counts whatever garbage happened not to have been collected yet, and the
	// ratio swings by double digits between runs. With it, both figures are stable
	// to three significant figures.
	//
	// run returns its result so measure can hold it across that second GC —
	// KeepAlive inside run would not, because run has already returned by the time
	// the GC happens, and the whole result would be collected before it is
	// measured. That mistake reads as a ~2 KB retained figure for both paths.
	measure := func(run func() (any, int)) (total, retained uint64, n int) {
		var m0, m1 runtime.MemStats
		var result any
		runtime.GC()
		runtime.ReadMemStats(&m0)
		result, n = run()
		runtime.GC()
		runtime.ReadMemStats(&m1)
		// TotalAlloc is cumulative and unaffected by the GC above. HeapAlloc is a
		// level, not a counter, and the delta is computed signed then clamped:
		// unsigned subtraction would underflow to an astronomical value if the
		// baseline heap happened to sit above the end heap (the previous path's
		// result becoming collectable between the two snapshots), turning a
		// measurement artifact into a spurious pass.
		total = m1.TotalAlloc - m0.TotalAlloc
		if d := int64(m1.HeapAlloc) - int64(m0.HeapAlloc); d > 0 {
			retained = uint64(d)
		}
		runtime.KeepAlive(result)
		return total, retained, n
	}

	readAllTotal, readAllRetained, readAllN := measure(func() (any, int) {
		recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
		require.NoError(t, err)
		return recs, len(recs)
	})
	require.Equal(t, 20000, readAllN)

	// The literal dedup-seeding shape: stream, retain only the id set.
	streamTotal, streamRetained, streamN := measure(func() (any, int) {
		seen := make(map[string]bool)
		require.NoError(t, StreamSummaries(dir, ReadOpts{Writer: io.Discard}, func(s Summary) error {
			seen[s.ID] = true
			return nil
		}))
		return seen, len(seen)
	})
	require.Equal(t, 20000, streamN)

	// A ratio, not an absolute byte count, so the test pins the property without
	// degrading into a flaky micro-benchmark across Go releases. The 5x floor is
	// deliberately conservative against the measured ~6x churn and ~17x retention.
	assert.Less(t, streamTotal*5, readAllTotal,
		"total allocated bytes: stream=%d readAll=%d (want stream*5 < readAll)", streamTotal, readAllTotal)
	assert.Less(t, streamRetained*5, readAllRetained,
		"retained bytes: stream=%d readAll=%d (want stream*5 < readAll)", streamRetained, readAllRetained)
}

// BenchmarkDedupSeed_ReadAll and BenchmarkDedupSeed_StreamSummaries report the
// same comparison as B/op and allocs/op. They are a diagnostic complement to the
// allocation test above, not a gate.
func BenchmarkDedupSeed_ReadAll(b *testing.B) {
	dir := generateAllocFixture(b, 5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recs, err := ReadAll(dir, ReadOpts{Writer: io.Discard})
		if err != nil {
			b.Fatal(err)
		}
		seen := make(map[string]bool, len(recs))
		for _, r := range recs {
			seen[r.ID] = true
		}
	}
}

func BenchmarkDedupSeed_StreamSummaries(b *testing.B) {
	dir := generateAllocFixture(b, 5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		seen := make(map[string]bool)
		if err := StreamSummaries(dir, ReadOpts{Writer: io.Discard}, func(s Summary) error {
			seen[s.ID] = true
			return nil
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// TestDecodeSummary_SkipsWrongTypedNumericField pins the parity gap the 3.1
// adversarial review found: encoding/json type-checks only the fields the TARGET
// struct declares, so a field Record declares and summaryLine omitted was rejected
// by decodeRecord and silently ACCEPTED here — putting an id in the dedup seed
// that no rendering path can display, which suppresses the finding permanently.
//
// summaryLine now declares the numeric fields for exactly this reason. The warning
// TEXT necessarily differs (encoding/json names the target struct), so the
// assertion is on the shared SKIP DECISION, which is the property that matters.
func TestDecodeSummary_SkipsWrongTypedNumericField(t *testing.T) {
	for _, field := range []string{"line", "est_minutes", "occurrences", "source_report.line"} {
		t.Run(field, func(t *testing.T) {
			rec := sampleRecord("2026-06-14T10:00:00Z-a")
			rec.StampID()
			var obj map[string]any
			require.NoError(t, json.Unmarshal([]byte(recordLine(t, rec)), &obj))
			if field == "source_report.line" {
				obj["source_report"] = map[string]any{"path": "sources/x/review.md", "line": "42"}
			} else {
				obj[field] = "12" // a number written as a string: the realistic corruption
			}
			line, err := json.Marshal(obj)
			require.NoError(t, err)

			var sumDiag, recDiag bytes.Buffer
			_, ok := decodeSummary(line, "2026-06.jsonl", &sumDiag)
			_, outcome := decodeRecord(line, "2026-06.jsonl", &recDiag)

			assert.False(t, ok, "the summary path must not accept a line the full path rejects")
			assert.Equal(t, decodeMalformed, outcome)
			assert.Contains(t, sumDiag.String(), MsgMalformedSkip)
		})
	}
}

// TestStreamSummaries_CallbackErrorWrappingNotExistStillAborts guards the walk's
// error triage: a caller sentinel that happens to wrap fs.ErrNotExist must NOT be
// mistaken for a shard that disappeared mid-walk and swallowed as success.
func TestStreamSummaries_CallbackErrorWrappingNotExistStillAborts(t *testing.T) {
	dir := t.TempDir()
	writeShard(t, dir, "2026-06", recordLine(t, idRecord("2026-06-14T10:00:00Z-a", "first")))
	writeShard(t, dir, "2026-07", recordLine(t, idRecord("2026-07-01T10:00:00Z-b", "second")))

	sentinel := fmt.Errorf("caller gave up: %w", fs.ErrNotExist)
	seen := 0
	err := StreamSummaries(dir, ReadOpts{Writer: io.Discard}, func(Summary) error {
		seen++
		return sentinel
	})
	require.Error(t, err, "an aborted walk must not report success")
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, 1, seen)
}

// TestStreamSummaries_OverLongWarningMatchesFullReadPath pins the one gate whose
// warning both readers emit from a shared constant rather than inline literals.
func TestStreamSummaries_OverLongWarningMatchesFullReadPath(t *testing.T) {
	// One dir for both readers: the warning embeds the shard path, so separate
	// fixtures would differ on the path alone and the assertion would be vacuous.
	dir := t.TempDir()
	writeShard(t, dir, "2026-06",
		strings.Repeat("x", maxLineBytes+16),
		recordLine(t, idRecord("2026-06-14T10:00:00Z-a", "after the giant line")),
	)

	var streamDiag bytes.Buffer
	require.NoError(t, StreamSummaries(dir, ReadOpts{Writer: &streamDiag}, func(Summary) error { return nil }))
	var readDiag bytes.Buffer
	_, err := ReadAll(dir, ReadOpts{Writer: &readDiag})
	require.NoError(t, err)

	assert.Equal(t, readDiag.String(), streamDiag.String(),
		"both readers emit the over-long warning from one shared constant")
}
