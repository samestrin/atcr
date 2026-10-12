package fanout

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/atomicfs"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeCoverageRecord writes one persona's chunks.json under reviewDir's pool.
func writeCoverageRecord(t *testing.T, reviewDir, agent string, bins []chunkRecordBin) {
	t.Helper()
	path := filepath.Join(reviewDir, "sources", "pool", poolRawAgentDir, agent, chunksFile)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, atomicfs.WriteJSON(path, chunkRecord{SchemaVersion: chunksSchemaVersion, Agent: agent, Bins: bins}))
}

// TestReadCoverage_ReviewedPredicate pins "reviewed" = ok and not
// contributedNothing, bins summed per persona and files as a cross-persona union:
// pending, failed, timeout and deferred bins are planned but not reviewed, an ok
// bin that contributed nothing covers none of its files, and a re-packed bin
// covers only the files its server kept.
func TestReadCoverage_ReviewedPredicate(t *testing.T) {
	dir := t.TempDir()
	writeCoverageRecord(t, dir, "brad", []chunkRecordBin{
		{Index: 0, Files: []string{"a", "b"}, Outcome: StatusOK},
		{Index: 1, Files: []string{"c"}, Outcome: StatusOK, ContributedNothing: true},
		{Index: 2, Files: []string{"d", "e"}, Outcome: StatusOK, RePacked: true, ServedFiles: []string{"d"}},
		{Index: 3, Files: []string{"f"}, Outcome: StatusFailed},
		{Index: 4, Files: []string{"g"}, Outcome: StatusTimeout},
		{Index: 5, Files: []string{"h"}, Outcome: chunkOutcomePending},
		{Index: 6, Files: []string{"i"}, Outcome: chunkOutcomeDeferred},
		{Index: 7, Files: []string{"j"}, Outcome: StatusOK, RePacked: true}, // kept nothing
	})
	// kai reviews f, which brad failed: the union covers it.
	writeCoverageRecord(t, dir, "kai", []chunkRecordBin{
		{Index: 0, Files: []string{"f"}, Outcome: StatusOK},
		{Index: 1, Files: []string{"a"}, Outcome: StatusOK},
	})

	cov, ok, err := ReadCoverage(dir)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, Coverage{BinsReviewed: 5, BinsPlanned: 10, FilesCovered: 4, FilesInScope: 10}, cov,
		"reviewed bins: brad 0, 2, 7 + kai 0, 1; covered files: a, b, d, f")
	assert.InDelta(t, 40.0, cov.FilePercent(), 1e-9)
}

// A review whose personas wrote no chunks.json (bulk or diff) has no bin-level
// coverage: ok is false and nothing is counted.
func TestReadCoverage_NoRecordsIsUnmeasured(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool", poolRawAgentDir, "bruce"), 0o755))
	cov, ok, err := ReadCoverage(dir)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, Coverage{}, cov)
}

// A record that cannot be read faithfully is an error, never a silent skip:
// dropping one persona's planned bins would overstate coverage.
func TestReadCoverage_BadRecordIsAnError(t *testing.T) {
	for name, body := range map[string]string{
		"corrupt":        "{not json",
		"unknown schema": `{"schema_version": 99, "agent": "brad", "bins": []}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeCoverageRecord(t, dir, "kai", []chunkRecordBin{{Files: []string{"a"}, Outcome: StatusOK}})
			path := filepath.Join(dir, "sources", "pool", poolRawAgentDir, "brad", chunksFile)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
			_, ok, err := ReadCoverage(dir)
			require.Error(t, err)
			assert.False(t, ok)
		})
	}
}

// slotDone stamps the two signals the coverage reader needs onto the bin: a
// contributed-nothing ok result, and the subset a re-packing fallback served.
func TestChunkLedger_SlotDoneStampsCoverageSignals(t *testing.T) {
	poolDir := t.TempDir()
	slot := func() Slot {
		return Slot{Primary: Agent{Name: "brad", ChunkTotal: 3, chunkFiles: []string{"a", "b"}}}
	}
	l := newChunkLedger(log.Discard(), poolDir, []Slot{slot(), slot(), slot()})
	require.NotNil(t, l)
	l.slotDone(0, Result{Status: StatusOK})
	l.slotDone(1, Result{Status: StatusOK, ThinkSuppressed: true})
	l.slotDone(2, Result{Status: StatusOK, servedRePacked: true, servedChunkFiles: []string{"a"}})

	rec := readChunkRecord(t, poolDir, "brad")
	require.Len(t, rec.Bins, 3)
	assert.Equal(t, chunkRecordBin{Index: 0, Files: []string{"a", "b"}, Outcome: StatusOK}, rec.Bins[0],
		"a plain ok bin is written exactly as before")
	assert.True(t, rec.Bins[1].ContributedNothing)
	assert.True(t, rec.Bins[2].RePacked)
	assert.Equal(t, []string{"a"}, rec.Bins[2].ServedFiles)

	// A failed bin never carries contributed_nothing: the flag qualifies ok only.
	l.slotDone(1, Result{Status: StatusFailed, ThinkSuppressed: true})
	assert.False(t, readChunkRecord(t, poolDir, "brad").Bins[1].ContributedNothing)
}

// open is the resume's side of ReadCoverage's predicate: a bin is open when any
// of its files is not covered, so every bin coverage leaves a file of
// unreviewed is one a resume re-dispatches.
func TestChunkRecordBin_OpenMatchesCoverage(t *testing.T) {
	for _, tc := range []struct {
		bin  chunkRecordBin
		open bool
	}{
		{chunkRecordBin{Files: []string{"a", "b"}, Outcome: StatusOK}, false},
		{chunkRecordBin{Files: []string{"a"}, Outcome: StatusOK, ContributedNothing: true}, true},
		{chunkRecordBin{Files: []string{"a", "b"}, Outcome: StatusOK, RePacked: true, ServedFiles: []string{"a"}}, true},
		{chunkRecordBin{Files: []string{"a", "b"}, Outcome: StatusOK, RePacked: true, ServedFiles: []string{"b", "a"}}, false},
		{chunkRecordBin{Files: []string{"a"}, Outcome: StatusOK, RePacked: true}, true},
		{chunkRecordBin{Files: []string{"a"}, Outcome: StatusFailed}, true},
		{chunkRecordBin{Files: []string{"a"}, Outcome: StatusTimeout}, true},
		{chunkRecordBin{Files: []string{"a"}, Outcome: chunkOutcomePending}, true},
		{chunkRecordBin{Files: []string{"a"}, Outcome: chunkOutcomeDeferred}, true},
		{chunkRecordBin{Files: []string{"a"}, Outcome: "unknown"}, true},
	} {
		assert.Equal(t, tc.open, tc.bin.open(), "%+v", tc.bin)
	}
}

func TestCoverage_FilePercentEmptyScopeIsZero(t *testing.T) {
	assert.Zero(t, Coverage{}.FilePercent(), "no files in scope is never reported as covered")
}

// A record over maxChunksBytes is refused, not read: an error, never a figure.
func TestReadCoverage_OversizedRecordIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources", "pool", poolRawAgentDir, "brad", chunksFile)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, make([]byte, maxChunksBytes+1), 0o644))
	_, ok, err := ReadCoverage(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
	assert.False(t, ok)
}

// A deferred bin as T2's ledger really writes it (riding the persona's last
// slot, never dispatched) is planned but not reviewed: it adds to bins planned
// and files in scope, and covers none of its files even after every dispatched
// bin resolves ok.
func TestReadCoverage_LedgerDeferredBinIsPlannedNotReviewed(t *testing.T) {
	reviewDir := t.TempDir()
	poolDir := filepath.Join(reviewDir, "sources", "pool")
	slots := []Slot{
		{Primary: Agent{Name: "brad", ChunkTotal: 2, chunkFiles: []string{"a"}}},
		{Primary: Agent{Name: "brad", ChunkTotal: 2, chunkFiles: []string{"b"}}, deferredBins: [][]string{{"c", "d"}}},
	}
	l := newChunkLedger(log.Discard(), poolDir, slots)
	require.NotNil(t, l)
	l.writePlan()
	l.slotDone(0, Result{Status: StatusOK})
	l.slotDone(1, Result{Status: StatusOK})
	require.Equal(t, chunkOutcomeDeferred, readChunkRecord(t, poolDir, "brad").Bins[2].Outcome)

	cov, ok, err := ReadCoverage(reviewDir)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, Coverage{BinsReviewed: 2, BinsPlanned: 3, FilesCovered: 2, FilesInScope: 4}, cov)
	assert.InDelta(t, 50.0, cov.FilePercent(), 1e-9)
}

// A review dir whose path holds glob metacharacters ([, *, ?) still has its
// records found: discovery lists the pool's agent directory instead of matching
// a pattern built from the user-supplied path, which would match nothing and
// read the run as unbinned (EST-936).
func TestReadCoverage_GlobCharactersInReviewDir(t *testing.T) {
	for _, name := range []string{"rev[1]", "rev*", "rev?"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), name)
			writeCoverageRecord(t, dir, "brad", []chunkRecordBin{
				{Index: 0, Files: []string{"a"}, Outcome: StatusOK},
				{Index: 1, Files: []string{"b"}, Outcome: StatusFailed},
			})
			cov, ok, err := ReadCoverage(dir)
			require.NoError(t, err)
			require.True(t, ok, "the record is found, so the run is measured")
			assert.Equal(t, Coverage{BinsReviewed: 1, BinsPlanned: 2, FilesCovered: 1, FilesInScope: 2}, cov)
		})
	}
}

// A chunks.json write that fails leaves no record, which ReadCoverage alone
// would read as "binned nothing". The ledger keeps the failure and the review
// result carries it, so the coverage gate can fail closed (EST-936).
func TestChunkLedger_WriteFailureIsSurfaced(t *testing.T) {
	t.Run("ledger", func(t *testing.T) {
		notADir := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(notADir, nil, 0o644))
		slot := Slot{Primary: Agent{Name: "brad", ChunkTotal: 2, chunkFiles: []string{"a"}}}
		l := newChunkLedger(log.Discard(), filepath.Join(notADir, "pool"), []Slot{slot, slot})
		require.NotNil(t, l)
		require.NoError(t, l.writeErr())
		l.writePlan()
		require.Error(t, l.writeErr(), "a failed plan write is kept")
		l.slotDone(0, Result{Status: StatusOK})
		assert.Error(t, l.writeErr(), "a later write never clears it")
		assert.NoError(t, (*chunkLedger)(nil).writeErr(), "no ledger (bulk or diff) is no failure")
	})

	t.Run("resume result", func(t *testing.T) {
		entries := sixtyFourFiles()
		dir := originalRun(t, entries, 4)
		// chunks.json as a non-empty directory: the atomic rename onto it fails.
		path := filepath.Join(dir, "sources", "pool", poolRawAgentDir, "greta", chunksFile)
		require.NoError(t, os.Remove(path))
		require.NoError(t, os.MkdirAll(filepath.Join(path, "block"), 0o755))

		p := &PreparedReview{ID: "r", Dir: dir, Slots: gretaBaselineSlots(t, entries[4:]), manifest: &payload.Manifest{Roster: []string{"greta"}}}
		res, err := ExecuteResume(context.Background(), baselineChunkFindingCompleter{}, p)
		require.NoError(t, err, "losing the record never costs the review")
		require.NotNil(t, res)
		assert.Error(t, res.ChunkRecordErr)
	})
}
