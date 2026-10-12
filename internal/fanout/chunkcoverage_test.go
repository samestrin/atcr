package fanout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/atomicfs"
	"github.com/samestrin/atcr/internal/log"
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

func TestCoverage_FilePercentEmptyScopeIsZero(t *testing.T) {
	assert.Zero(t, Coverage{}.FilePercent(), "no files in scope is never reported as covered")
}
