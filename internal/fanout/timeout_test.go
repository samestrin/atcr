package fanout

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
)

// deadlineCaptureCompleter records how much time was left on the context deadline
// at the moment each model's call ran. It lets a test assert the timeout-scaling
// seams (Epic 19.10 F6) applied the SCALED deadline without waiting real seconds:
// the deadline duration itself is the observable, not an elapsed sleep.
type deadlineCaptureCompleter struct {
	mu          sync.Mutex
	remaining   map[string]time.Duration
	hadDeadline map[string]bool
}

func (d *deadlineCaptureCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	dl, ok := ctx.Deadline()
	d.mu.Lock()
	if d.remaining == nil {
		d.remaining = map[string]time.Duration{}
		d.hadDeadline = map[string]bool{}
	}
	d.hadDeadline[inv.Model] = ok
	if ok {
		d.remaining[inv.Model] = time.Until(dl)
	}
	d.mu.Unlock()
	return "review by " + inv.Model, nil
}

func (d *deadlineCaptureCompleter) get(model string) (time.Duration, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.remaining[model], d.hadDeadline[model]
}

// TestScaledTimeoutSecs_NoOpWhenUnchunked verifies the base timeout is returned
// unchanged for chunkTotal 0 and 1 (the unchunked/bulk case) — zero regression for
// every non-chunked caller (bulk path, doctor/direct construction, fallbacks).
func TestScaledTimeoutSecs_NoOpWhenUnchunked(t *testing.T) {
	for _, chunkTotal := range []int{0, 1} {
		assert.Equal(t, 600, scaledTimeoutSecs(600, chunkTotal),
			"chunkTotal=%d must not change the base timeout", chunkTotal)
	}
	// A base of 0 ("global deadline only") stays 0 regardless of chunk count.
	assert.Equal(t, 0, scaledTimeoutSecs(0, 8))
	assert.Equal(t, 0, scaledTimeoutSecs(0, 1))
}

// TestScaledTimeoutSecs_MonotonicAndLargerWhenChunked verifies the scaled value is
// strictly larger for chunkTotal=6 than for the unchunked case, and is
// monotonically non-decreasing across increasing chunkTotal.
func TestScaledTimeoutSecs_MonotonicAndLargerWhenChunked(t *testing.T) {
	base := 600
	assert.Greater(t, scaledTimeoutSecs(base, 6), scaledTimeoutSecs(base, 1),
		"6 chunks must get a strictly larger deadline than 1")

	prev := scaledTimeoutSecs(base, 1)
	for ct := 2; ct <= 50; ct++ {
		cur := scaledTimeoutSecs(base, ct)
		assert.GreaterOrEqual(t, cur, prev, "scaled timeout must be non-decreasing in chunkTotal (ct=%d)", ct)
		prev = cur
	}
}

// TestScaledTimeoutSecs_ClampsToSchemaCeiling verifies a pathological chunk count
// can never produce an unbounded deadline — it clamps to registry.MaxTimeoutSecs.
func TestScaledTimeoutSecs_ClampsToSchemaCeiling(t *testing.T) {
	// A base already near the ceiling with any multiplier > 1 clamps.
	assert.Equal(t, registry.MaxTimeoutSecs, scaledTimeoutSecs(registry.MaxTimeoutSecs, 6))
	// A pathologically large chunk count clamps rather than overflowing.
	assert.Equal(t, registry.MaxTimeoutSecs, scaledTimeoutSecs(registry.MaxTimeoutSecs, 1<<30))
	// The ceiling factor caps growth well below the schema max for a normal base:
	// 600 * min(1000, 8) = 4800, not 600*1000.
	assert.Equal(t, 600*chunkTimeoutCeilingFactor, scaledTimeoutSecs(600, 1000))
}

// TestScaledTimeoutSecs_Deterministic verifies the scaled value depends only on
// (baseSecs, chunkTotal) — no live/network/time input.
func TestScaledTimeoutSecs_Deterministic(t *testing.T) {
	for i := 0; i < 5; i++ {
		assert.Equal(t, scaledTimeoutSecs(600, 6), scaledTimeoutSecs(600, 6))
		assert.Equal(t, scaledTimeoutSecs(123, 4), scaledTimeoutSecs(123, 4))
	}
}

// TestMaxLaneChunkTotal verifies the aggregate-seam input: the largest ChunkTotal
// across ALL slots, serial AND parallel. Parallel personas MUST count — the
// per-call deadline is a child of the aggregate runCtx and cannot extend past it,
// so a parallel chunked persona (the production roster runs serial_agents: []) is
// only covered when the aggregate parent itself scales.
func TestMaxLaneChunkTotal(t *testing.T) {
	slots := []Slot{
		{Primary: Agent{Name: "dax", ChunkTotal: 6}, Serial: true},    // serial, chunked
		{Primary: Agent{Name: "otto", ChunkTotal: 12}, Serial: false}, // PARALLEL — must be COUNTED
		{Primary: Agent{Name: "vera", ChunkTotal: 3}, Serial: false},  // parallel, fewer chunks
		{Primary: Agent{Name: "kai", ChunkTotal: 0}, Serial: true},    // unchunked
	}
	assert.Equal(t, 12, maxLaneChunkTotal(slots),
		"covers the worst chunked persona in EITHER lane — the parallel greta/vera/brad case is not ignored")

	// A lone parallel chunked persona still counts (regression guard for the
	// serial-only bug that pinned parallel personas to the flat wall).
	assert.Equal(t, 9, maxLaneChunkTotal([]Slot{{Primary: Agent{ChunkTotal: 9}, Serial: false}}))
	// The helper returns the raw max; the unchunked no-op (<=1) is scaledTimeoutSecs'
	// job, so ChunkTotal 1 returns 1 here (and scaledTimeoutSecs then leaves the base
	// unchanged), while an empty roster returns 0.
	assert.Equal(t, 1, maxLaneChunkTotal([]Slot{{Primary: Agent{ChunkTotal: 1}, Serial: false}}))
	assert.Equal(t, 0, maxLaneChunkTotal(nil))
}

// TestInvokeAgent_ScaledPerCallDeadlineFromChunkTotal verifies the per-call seam
// (invokeAgent): a chunked agent's context deadline reflects the SCALED value,
// while an unchunked agent keeps the flat base deadline. Uses the deadline itself
// as the observable so the test is instant and deterministic (no real waiting).
func TestInvokeAgent_ScaledPerCallDeadlineFromChunkTotal(t *testing.T) {
	// Chunked persona: base 2s x 4 chunks -> ~8s deadline, not the flat 2s.
	cap := &deadlineCaptureCompleter{}
	NewEngine(cap).invokeAgent(context.Background(), Agent{
		Name:        "dax",
		TimeoutSecs: 2,
		ChunkTotal:  4,
		Invocation:  llmclient.Invocation{Model: "dax"},
	})
	rem, had := cap.get("dax")
	assert.True(t, had, "a scaled per-call deadline must be set")
	assert.Greater(t, rem, 5*time.Second, "per-call deadline scaled to ~8s (base 2 x 4 chunks), not the flat 2s")
	assert.LessOrEqual(t, rem, 8*time.Second)

	// Unchunked persona: ChunkTotal <= 1 -> the flat 2s deadline is preserved.
	cap2 := &deadlineCaptureCompleter{}
	NewEngine(cap2).invokeAgent(context.Background(), Agent{
		Name:        "otto",
		TimeoutSecs: 2,
		ChunkTotal:  1,
		Invocation:  llmclient.Invocation{Model: "otto"},
	})
	rem2, had2 := cap2.get("otto")
	assert.True(t, had2)
	assert.LessOrEqual(t, rem2, 2*time.Second, "an unchunked agent keeps the flat base deadline")
	assert.Greater(t, rem2, 500*time.Millisecond)
}

// TestRunEngine_SerialLaneAggregateDeadlineScalesWithChunkTotal is the AC6
// regression: a serial-lane persona fanned into many chunk-Slots runs them
// sequentially, so the run's aggregate deadline must scale with the lane's chunk
// count — otherwise the sum of chunk calls hits the flat 600s wall (the confirmed
// greta/vera/brad failure). Agents carry no per-call timeout (TimeoutSecs=0) so
// the completer observes the aggregate runCtx deadline directly.
func TestRunEngine_SerialLaneAggregateDeadlineScalesWithChunkTotal(t *testing.T) {
	const chunks = 6
	var slots []Slot
	for i := 0; i < chunks; i++ {
		slots = append(slots, Slot{
			Primary: Agent{
				Name:        "dax",
				Invocation:  llmclient.Invocation{Model: "dax"},
				PayloadMode: "blocks",
				ChunkTotal:  chunks, // every chunk-Slot carries the persona's full chunk count
			},
			Serial: true,
		})
	}
	cap := &deadlineCaptureCompleter{}
	prep := &PreparedReview{ID: "t", Dir: t.TempDir(), Slots: slots, MaxParallel: 1, TimeoutSec: 600}

	runEngine(context.Background(), cap, prep, t.TempDir())

	rem, had := cap.get("dax")
	assert.True(t, had, "the aggregate deadline must be set")
	// 600 base x min(6, ceiling) = 3600s aggregate, not the flat 600s wall.
	assert.Greater(t, rem, 601*time.Second,
		"a serial chunked lane gets an aggregate deadline scaled by its chunk count, not the flat 600s wall")
}

// TestRunEngine_ParallelLaneAggregateDeadlineScalesWithChunkTotal is the AC6
// regression for the PRODUCTION roster (serial_agents: [], so greta/vera/brad run
// parallel). A parallel chunked persona's chunks queue behind max_parallel / a slow
// backend, and the per-call deadline cannot exceed the aggregate runCtx — so the
// aggregate MUST scale for parallel personas too. This test fails against a
// serial-only aggregate seam (the runCtx would stay the flat 600s).
func TestRunEngine_ParallelLaneAggregateDeadlineScalesWithChunkTotal(t *testing.T) {
	const chunks = 6
	var slots []Slot
	for i := 0; i < chunks; i++ {
		slots = append(slots, Slot{
			Primary: Agent{
				Name:        "greta",
				Invocation:  llmclient.Invocation{Model: "greta"},
				PayloadMode: "blocks",
				ChunkTotal:  chunks,
			},
			Serial: false, // PARALLEL — matches serial_agents: [] production config
		})
	}
	cap := &deadlineCaptureCompleter{}
	prep := &PreparedReview{ID: "t", Dir: t.TempDir(), Slots: slots, MaxParallel: 6, TimeoutSec: 600}

	runEngine(context.Background(), cap, prep, t.TempDir())

	rem, had := cap.get("greta")
	assert.True(t, had, "the aggregate deadline must be set")
	assert.Greater(t, rem, 601*time.Second,
		"a PARALLEL chunked persona (greta/vera/brad) must get an aggregate deadline scaled by chunk count, not the flat 600s wall")
}

// TestRunEngine_UnchunkedAggregateDeadlineUnchanged verifies zero regression: a
// bulk (unchunked) run keeps the flat aggregate deadline — scaling is a no-op when
// no serial lane is chunked.
func TestRunEngine_UnchunkedAggregateDeadlineUnchanged(t *testing.T) {
	slots := []Slot{{
		Primary: Agent{Name: "otto", Invocation: llmclient.Invocation{Model: "otto"}, PayloadMode: "blocks"},
		Serial:  true,
	}}
	cap := &deadlineCaptureCompleter{}
	prep := &PreparedReview{ID: "t", Dir: t.TempDir(), Slots: slots, MaxParallel: 1, TimeoutSec: 600}

	runEngine(context.Background(), cap, prep, t.TempDir())

	rem, had := cap.get("otto")
	assert.True(t, had)
	assert.LessOrEqual(t, rem, 600*time.Second, "an unchunked run keeps the flat 600s aggregate deadline")
}
