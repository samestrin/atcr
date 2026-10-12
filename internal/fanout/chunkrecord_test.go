package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eightBinEntries returns eight 100-byte files that a 100-byte budget packs one
// per bin, so a baseline persona plans exactly eight bins in file order.
func eightBinEntries() ([]payload.FileEntry, []string) {
	var entries []payload.FileEntry
	var files []string
	for i := 0; i < 8; i++ {
		f := fmt.Sprintf("f%d.go", i)
		files = append(files, f)
		entries = append(entries, baselineEntry(f, 100))
	}
	return entries, files
}

func readChunkRecord(t *testing.T, poolDir, agent string) chunkRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(poolDir, poolRawAgentDir, agent, chunksFile))
	require.NoError(t, err)
	var rec chunkRecord
	require.NoError(t, json.Unmarshal(data, &rec))
	return rec
}

// killAfterCompleter answers the first n calls and then parks every later call
// until its context is cancelled, signalling reached when call n+1 arrives. On a
// serial lane, call n+1 cannot start until bin n's completion hook has returned,
// so reached marks the instant a process kill would leave exactly n bins
// resolved on disk.
type killAfterCompleter struct {
	n       int32
	calls   atomic.Int32
	reached chan struct{}
	once    sync.Once
}

func (c *killAfterCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	if c.calls.Add(1) <= c.n {
		return baselineChunkFindingCompleter{}.Complete(ctx, inv)
	}
	c.once.Do(func() { close(c.reached) })
	<-ctx.Done()
	return "", ctx.Err()
}

// Epic 35.16.13 T1 success criterion: a fan-out killed after bin 3 of 8 leaves a
// chunks.json holding all 8 planned bins, exactly 3 resolved with their file
// sets and the other 5 still pending — the record is written before dispatch
// and updated per bin, not assembled at merge time.
func TestChunkRecord_KilledAfterBinThreeOfEight(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	// Serial lane: bins dispatch one at a time in order, so "after bin 3" is exact.
	cfg.Project = &registry.ProjectConfig{SerialAgents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 100
	entries, files := eightBinEntries()

	slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", false, true)
	require.NoError(t, err)
	require.Len(t, slots, 8)

	poolDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &killAfterCompleter{n: 3, reached: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runEngine(ctx, c, &PreparedReview{Slots: slots}, poolDir)
	}()
	<-c.reached

	// The on-disk record at the moment of the "kill".
	rec := readChunkRecord(t, poolDir, "greta")
	assert.Equal(t, chunksSchemaVersion, rec.SchemaVersion)
	assert.Equal(t, "greta", rec.Agent)
	require.Len(t, rec.Bins, 8, "every planned bin is on disk, not only the resolved ones")
	var resolved, pending int
	for i, b := range rec.Bins {
		assert.Equal(t, i, b.Index)
		assert.Equal(t, []string{files[i]}, b.Files, "bin %d names its file set", i)
		switch b.Outcome {
		case StatusOK:
			resolved++
			assert.Less(t, i, 3, "only the first three bins resolved")
		case chunkOutcomePending:
			pending++
		default:
			t.Fatalf("bin %d: unexpected outcome %q", i, b.Outcome)
		}
	}
	assert.Equal(t, 3, resolved)
	assert.Equal(t, 5, pending)

	// Cutting the run off resolves the in-flight bin and the unstarted ones as
	// timeouts; the record follows them.
	cancel()
	<-done
	rec = readChunkRecord(t, poolDir, "greta")
	require.Len(t, rec.Bins, 8)
	for i, b := range rec.Bins {
		want := StatusTimeout
		if i < 3 {
			want = StatusOK
		}
		assert.Equal(t, want, b.Outcome, "bin %d", i)
	}
}

// Concurrent per-bin completions on the parallel lane are serialised into one
// consistent record per persona: every bin ends resolved with its own outcome,
// and a failed bin is recorded as failed rather than lost.
func TestChunkRecord_ParallelLaneRecordsEveryBin(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = 100
	entries, files := eightBinEntries()

	slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", false, true)
	require.NoError(t, err)
	require.Len(t, slots, 16, "two personas × eight bins")

	poolDir := t.TempDir()
	runEngine(context.Background(), baselinePartialFailCompleter{failMarker: "f5.go"}, &PreparedReview{Slots: slots, MaxParallel: 3}, poolDir)

	for _, agent := range []string{"greta", "kai"} {
		rec := readChunkRecord(t, poolDir, agent)
		require.Len(t, rec.Bins, 8, agent)
		for i, b := range rec.Bins {
			want := StatusOK
			if files[i] == "f5.go" {
				want = StatusFailed
			}
			assert.Equal(t, want, b.Outcome, "%s bin %d", agent, i)
			assert.Equal(t, []string{files[i]}, b.Files, "%s bin %d", agent, i)
		}
	}
}

// AC7: a run that is not a baseline multi-chunk fan-out writes no chunks.json —
// neither a single-chunk baseline (which falls through to one bulk slot per
// persona) nor a full bulk diff review.
func TestChunkRecord_BulkRunWritesNoFile(t *testing.T) {
	t.Run("single-chunk baseline", func(t *testing.T) {
		cfg := twoAgentConfig("http://unused")
		entries, _ := eightBinEntries()
		slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", false, true)
		require.NoError(t, err)
		require.Len(t, slots, 2, "one bulk slot per persona")

		poolDir := t.TempDir()
		runEngine(context.Background(), baselineChunkFindingCompleter{}, &PreparedReview{Slots: slots}, poolDir)
		assertNoChunkRecord(t, poolDir)
	})

	t.Run("bulk diff review", func(t *testing.T) {
		t.Setenv("ATCR_TEST_KEY", "secret")
		repo, base, head := initRepo(t)
		srv := mockProvider(t)
		res, err := RunReview(context.Background(), llmclient.New(), twoAgentConfig(srv.URL), reviewReq(repo, repo, base, head))
		require.NoError(t, err)
		assertNoChunkRecord(t, res.Dir)
	})
}

func assertNoChunkRecord(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		assert.False(t, strings.HasSuffix(path, chunksFile), "unexpected %s", path)
		return nil
	})
	require.NoError(t, err)
}

// WithSlotDone fires exactly once per slot, on both lanes and on the recovered
// panic path, with the Result Run itself returns for that slot.
func TestEngineRun_SlotDoneOncePerSlot(t *testing.T) {
	slots := []Slot{agentSlot("a"), agentSlot("b"), agentSlot("c"), agentSlot("d")}
	slots[2].Serial, slots[3].Serial = true, true
	p := &panicCompleter{f: newFake(), panicFor: map[string]bool{"b": true, "d": true}}
	var mu sync.Mutex
	got := map[int][]Result{}
	results := NewEngine(p, WithSlotDone(func(i int, r Result) {
		mu.Lock()
		defer mu.Unlock()
		got[i] = append(got[i], r)
	})).Run(context.Background(), slots)

	require.Len(t, got, len(slots))
	for i := range slots {
		require.Len(t, got[i], 1, "slot %d reported once", i)
		assert.Equal(t, results[i].Agent, got[i][0].Agent)
		assert.Equal(t, results[i].Status, got[i][0].Status, "slot %d", i)
	}
	assert.Equal(t, StatusFailed, got[1][0].Status, "a recovered panic is reported as failed")
	assert.Equal(t, StatusFailed, got[3][0].Status)
}
