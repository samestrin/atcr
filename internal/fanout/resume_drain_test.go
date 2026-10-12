package fanout

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/samestrin/atcr/internal/atomicfs"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.14 T6: --resume is a drain of the work ledger.

// countingCompleter counts the calls it forwards.
type countingCompleter struct {
	inner Completer
	n     atomic.Int64
}

func (c *countingCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	c.n.Add(1)
	return c.inner.Complete(ctx, inv)
}

// The measured 2026-09-09 coned run left every bin state the ledger knows: bins
// reviewed, bins that failed after their outer budget, timed out, were killed
// mid-backoff, were never leased, were cut by the old 64-bin ceiling, and ok
// bins that still left files unreviewed. A resume re-leases exactly the bins
// that are not done, replays the done ones without a call, and the re-leased
// bins' attempts carry on from the prior record.
func TestResumeDrain_ReleasesExactlyTheNonDoneUnits(t *testing.T) {
	cfg := chunkResumeConfig()
	repo, paths := chunkResumeRepo(t, 8)
	dir := partialBaselineReview(t, cfg, repo, paths, 2)
	poolDir := filepath.Join(dir, "sources", "pool")

	// Rewrite greta's ledger into the coned run's mix of states. Bin i holds
	// paths[i]; only bin 0 is done.
	rec := readChunkRecord(t, poolDir, "greta")
	require.Len(t, rec.Bins, 8)
	type state struct {
		outcome   string
		attempts  int
		nothing   bool
		rePacked  bool
		wantLease bool
	}
	states := []state{
		{outcome: StatusOK, attempts: 2},                                    // reviewed after one requeue
		{outcome: StatusOK, attempts: 1, nothing: true, wantLease: true},    // lane refused its content
		{outcome: StatusFailed, attempts: unitMaxAttempts, wantLease: true}, // outer budget exhausted
		{outcome: StatusTimeout, attempts: 2, wantLease: true},
		{outcome: chunkOutcomePending, attempts: 1, wantLease: true},      // killed during the backoff
		{outcome: chunkOutcomePending, wantLease: true},                   // never leased
		{outcome: chunkOutcomeDeferred, wantLease: true},                  // cut by the legacy ceiling
		{outcome: StatusOK, attempts: 1, rePacked: true, wantLease: true}, // fallback served none of it
	}
	var wantLeased []string
	for i, s := range states {
		b := &rec.Bins[i]
		require.Equal(t, []string{paths[i]}, b.Files)
		b.Outcome, b.Attempts, b.ContributedNothing, b.RePacked, b.ServedFiles = s.outcome, s.attempts, s.nothing, s.rePacked, nil
		if s.wantLease {
			wantLeased = append(wantLeased, paths[i])
		}
	}
	require.NoError(t, atomicfs.WriteJSON(filepath.Join(poolDir, poolRawAgentDir, "greta", chunksFile), rec))

	rprep, info, err := PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
	require.NoError(t, err)
	assert.Equal(t, []string{"greta"}, info.Pending)
	assert.Equal(t, []string{"kai"}, info.Completed, "a persona with no open bin is not re-paid for")
	assert.Equal(t, len(wantLeased), info.Units)
	require.Len(t, rprep.Slots, len(wantLeased))
	assert.Equal(t, wantLeased, slotFiles(rprep.Slots, "greta"), "exactly the non-done bins are re-leased")

	// Cache reads off, so the count is the re-leased units: bin 1's lane was
	// called in the first run and would otherwise be served from the cache.
	rprep.cacheNoRead = true
	calls := &countingCompleter{inner: baselineChunkFindingCompleter{}}
	_, err = ExecuteResume(context.Background(), calls, rprep)
	require.NoError(t, err)
	assert.Equal(t, int64(len(wantLeased)), calls.n.Load(), "one call per re-leased unit, none for the replayed bin")

	after := readChunkRecord(t, poolDir, "greta")
	require.Len(t, after.Bins, 8, "the drained ledger still names every planned unit")
	byFile := map[string]chunkRecordBin{}
	for _, b := range after.Bins {
		require.Len(t, b.Files, 1)
		byFile[b.Files[0]] = b
	}
	for i, s := range states {
		b := byFile[paths[i]]
		assert.Equal(t, StatusOK, b.Outcome, "bin %d", i)
		assert.False(t, b.open(), "bin %d", i)
		want := s.attempts
		if s.wantLease {
			want++
		}
		assert.Equal(t, want, b.Attempts, "bin %d: attempts carry on from the prior record", i)
	}
	assertCoverage(t, dir, Coverage{BinsReviewed: 16, BinsPlanned: 16, FilesCovered: 8, FilesInScope: 8})

	// Drained: the next resume has nothing to lease.
	_, info, err = PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
	require.NoError(t, err)
	assert.True(t, info.AllComplete())
	assert.Zero(t, info.Units)
}

// inheritAttempts takes a bin's count from the prior open bin that named its
// files, ignores done bins (they are replayed, not re-leased), and leaves a
// fresh run's ledger alone.
func TestInheritAttempts(t *testing.T) {
	l := &chunkLedger{
		order: []string{"greta"},
		records: map[string]*chunkRecord{"greta": {Bins: []chunkRecordBin{
			{Files: []string{"a.go"}},
			{Files: []string{"b.go"}},
			{Files: []string{"c.go"}},
		}}},
	}
	l.inheritAttempts(nil)
	for _, b := range l.records["greta"].Bins {
		assert.Zero(t, b.Attempts)
	}
	l.inheritAttempts(map[string]*chunkRecord{
		"greta": {Bins: []chunkRecordBin{
			{Files: []string{"a.go", "b.go"}, Outcome: StatusFailed, Attempts: 3}, // split on resume
			{Files: []string{"c.go"}, Outcome: StatusOK, Attempts: 2},             // done
		}},
		"kai": {Bins: []chunkRecordBin{{Files: []string{"a.go"}, Outcome: StatusFailed, Attempts: 4}}},
	})
	bins := l.records["greta"].Bins
	assert.Equal(t, 3, bins[0].Attempts)
	assert.Equal(t, 3, bins[1].Attempts, "both halves of a split bin keep its count")
	assert.Zero(t, bins[2].Attempts, "a done bin's count is not inherited")
}
