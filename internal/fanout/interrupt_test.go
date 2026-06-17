package fanout

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cancelAfterCompleter succeeds for the first cancelAt completions and cancels
// the parent context on the cancelAt-th call, simulating a SIGINT landing after
// that many agents finished. The engine checks ctx.Err() before each invocation,
// so once cancelled it starts no further agents. Driven serially (MaxParallel=1),
// this makes the succeeded count deterministic regardless of slot scheduling.
type cancelAfterCompleter struct {
	mu       sync.Mutex
	count    int
	cancelAt int
	cancel   context.CancelFunc
}

func (c *cancelAfterCompleter) Complete(ctx context.Context, _ llmclient.Invocation) (string, error) {
	c.mu.Lock()
	c.count++
	n := c.count
	c.mu.Unlock()
	if n > c.cancelAt {
		// Defensive: unreached under MaxParallel=1 because the engine short-circuits
		// post-cancel without calling Complete. Respect cancellation if it ever is.
		return "", ctx.Err()
	}
	if n == c.cancelAt {
		c.cancel() // the interrupt arrives just as the cancelAt-th agent completes
	}
	return "CRITICAL|auth.go:3|Unchecked call|Guard it|security|15|b() unchecked", nil
}

// TestExecuteReview_InterruptPreservesPartialAndMarksInterrupted is the epic 4.1
// AC9 integration test: a review is interrupted (root context cancelled) after 2
// agents complete. The 2 completed agents' results are preserved on disk, and the
// run is marked interrupted in the manifest and derived as the "interrupted"
// state by ReadReviewStatus — distinguishable from a clean completion. No new
// agents start after the interrupt (AC3); completed results are never lost (AC4,
// AC5 success criteria).
func TestExecuteReview_InterruptPreservesPartialAndMarksInterrupted(t *testing.T) {
	dir := t.TempDir()
	names := []string{"greta", "kai", "mira", "otto"}

	m := &payload.Manifest{
		Base: "a", Head: "b", Roster: names,
		StartedAt: time.Now().UTC(), TimeoutSecs: 600, PayloadMode: "blocks",
		PerAgentPayload: map[string]string{}, Stages: []string{"review"},
	}
	require.NoError(t, WriteManifest(dir, m))

	var slots []Slot
	for _, n := range names {
		slots = append(slots, Slot{Primary: Agent{
			Name:        n,
			Invocation:  llmclient.Invocation{Model: n},
			PayloadMode: "blocks",
		}})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &cancelAfterCompleter{cancelAt: 2, cancel: cancel}

	// MaxParallel=1 serializes the fan-out so exactly 2 agents complete before the
	// cancellation lands and the remaining slots short-circuit to timeout.
	prep := &PreparedReview{
		ID: "2026-06-17_interrupt", Dir: dir,
		Slots: slots, MaxParallel: 1, manifest: m,
	}

	res, err := ExecuteReview(ctx, fake, prep)
	require.NoError(t, err, "≥1 agent succeeded, so no run-level error despite the interrupt")
	require.NotNil(t, res)

	// Partial results preserved: exactly the 2 agents that ran before the interrupt.
	assert.Equal(t, 2, res.Summary.Succeeded, "AC4: the 2 completed agents' results are preserved")
	assert.Equal(t, 2, res.Summary.Failed, "the 2 not-yet-started agents short-circuit (AC3: no new agents start)")
	assert.True(t, res.Summary.Partial, "an interrupted run with partial success is partial")

	// Manifest records the interrupt (the durable marker).
	mdata, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var got payload.Manifest
	require.NoError(t, json.Unmarshal(mdata, &got))
	assert.True(t, got.Interrupted, "AC4: manifest records the interrupt")

	// Derived status reports interrupted — the single source of truth read by
	// `atcr status` and the MCP handler.
	st, err := ReadReviewStatus(dir, "2026-06-17_interrupt")
	require.NoError(t, err)
	assert.Equal(t, RunInterrupted, st.Status, "AC9: status derives to interrupted, not completed")

	// Concrete on-disk evidence: exactly 2 per-agent status.json marked ok.
	matches, err := filepath.Glob(filepath.Join(dir, "sources", "pool", "raw", "agent", "*", statusFile))
	require.NoError(t, err)
	okCount := 0
	for _, p := range matches {
		b, rerr := os.ReadFile(p)
		require.NoError(t, rerr)
		var as AgentStatus
		require.NoError(t, json.Unmarshal(b, &as))
		if as.Status == StatusOK {
			okCount++
		}
	}
	assert.Equal(t, 2, okCount, "AC9: exactly 2 agent results persisted to disk")
}
