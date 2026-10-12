package fanout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRun_PoolOf6Drains700UnitsWithBoundedWorkers is the T2 gate (Epic 35.16.14):
// a 700-unit plan through max_parallel=6 never has more than 6 units in flight
// and never more than ~6 live worker goroutines. Before the pool, Run spawned one
// goroutine per slot, so the goroutine count tracked the plan size (700) and only
// the semaphore bounded execution. Not parallel: it samples the process's
// goroutine count, which another test's goroutines would inflate.
func TestRun_PoolOf6Drains700UnitsWithBoundedWorkers(t *testing.T) {
	const units, workers = 700, 6
	slots := make([]Slot, units)
	for i := range slots {
		slots[i] = agentSlot(fmt.Sprintf("u%03d", i))
	}
	fake := newFake()
	fake.delay = time.Millisecond
	baseline := runtime.NumGoroutine()
	var peakG atomic.Int64
	fake.onStart = func(string) {
		n := int64(runtime.NumGoroutine())
		for {
			p := peakG.Load()
			if n <= p || peakG.CompareAndSwap(p, n) {
				return
			}
		}
	}

	results := NewEngine(fake, WithMaxParallel(workers)).Run(context.Background(), slots)

	require.Len(t, results, units)
	for i, r := range results {
		require.Equalf(t, StatusOK, r.Status, "unit %d", i)
		require.Equalf(t, slots[i].Primary.Name, r.Agent, "results[%d] must stay aligned with slots[%d]", i, i)
	}
	assert.LessOrEqual(t, atomic.LoadInt32(&fake.peak), int32(workers), "in-flight units never exceed max_parallel")
	assert.Equal(t, int32(workers), atomic.LoadInt32(&fake.peak), "the pool keeps every worker busy")
	// Slack of 2 covers runtime helpers that may start while the run is live; the
	// goroutine-per-slot engine this replaces sat near baseline+700 here.
	assert.LessOrEqual(t, peakG.Load()-int64(baseline), int64(workers+2),
		"live goroutines follow the worker count, not the unit count")
}

// TestRun_UnboundedLaneKeepsOneWorkerPerUnit pins maxParallel <= 0 as today's
// unbounded lane: every unit is in flight at once.
func TestRun_UnboundedLaneKeepsOneWorkerPerUnit(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 5, poolWorkers(5, 0))
	assert.Equal(t, 5, poolWorkers(5, -1))
	assert.Equal(t, 5, poolWorkers(5, 9), "a pool wider than the plan runs one worker per unit")
	assert.Equal(t, 3, poolWorkers(5, 3))
	assert.Equal(t, 0, poolWorkers(0, 3), "no units, no workers")
}

// TestRun_PoolRecoversPanicAndKeepsDraining: a panic in one unit fails that unit
// only; the worker that recovered it goes on to lease the rest of the queue.
func TestRun_PoolRecoversPanicAndKeepsDraining(t *testing.T) {
	t.Parallel()
	fake := newFake()
	fake.onStart = func(model string) {
		if model == "boom" {
			panic("unit exploded")
		}
	}
	slots := []Slot{agentSlot("a"), agentSlot("boom"), agentSlot("b"), agentSlot("c")}
	var mu sync.Mutex
	doneOrder := []int{}
	results := NewEngine(fake, WithMaxParallel(1), WithSlotDone(func(i int, _ Result) {
		mu.Lock()
		doneOrder = append(doneOrder, i)
		mu.Unlock()
	})).Run(context.Background(), slots)

	assert.Equal(t, StatusFailed, results[1].Status)
	for _, i := range []int{0, 2, 3} {
		assert.Equalf(t, StatusOK, results[i].Status, "unit %d", i)
	}
	assert.Equal(t, []int{0, 1, 2, 3}, doneOrder, "one worker drains the queue in order, past the panic")
}

// deadlineCompleter records how much deadline each model's call had left when
// it started; model "slow" holds its worker for hold first.
type deadlineCompleter struct {
	hold time.Duration
	mu   sync.Mutex
	left map[string]time.Duration
}

func (c *deadlineCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	dl, ok := ctx.Deadline()
	c.mu.Lock()
	if ok {
		c.left[inv.Model] = time.Until(dl)
	} else {
		c.left[inv.Model] = -1
	}
	c.mu.Unlock()
	if inv.Model == "slow" {
		select {
		case <-time.After(c.hold):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "review by " + inv.Model, nil
}

// TestRunEngine_UnitLeasedPastOldAggregateGetsFullDeadline is the deadline half
// of the T2 gate. Two chunked units (ChunkTotal 2, so 2s each from a 1s base)
// share one worker. The wave model sized the run at 1s x max(2 chunks, 2 waves)
// = 2s from the start, so the second unit, leased at ~1.5s, would have had about
// 0.5s left. Under the pool it gets its full 2s from lease time.
func TestRunEngine_UnitLeasedPastOldAggregateGetsFullDeadline(t *testing.T) {
	t.Parallel()
	const hold = 1500 * time.Millisecond
	unit := func(name string) Slot {
		s := agentSlot(name)
		s.Primary.TimeoutSecs = 1
		s.Primary.ChunkTotal = 2
		return s
	}
	c := &deadlineCompleter{hold: hold, left: map[string]time.Duration{}}
	p := &PreparedReview{Slots: []Slot{unit("slow"), unit("late")}, TimeoutSec: 1, MaxParallel: 1}

	start := time.Now()
	results, _ := runEngine(context.Background(), c, p, t.TempDir())
	require.GreaterOrEqual(t, time.Since(start), hold)

	require.Equal(t, StatusOK, results[0].Status)
	require.Equal(t, StatusOK, results[1].Status, "the late unit must not be cancelled by a run-wide deadline")
	c.mu.Lock()
	defer c.mu.Unlock()
	assert.Greater(t, c.left["late"], 1800*time.Millisecond,
		"a unit leased after the old wave-model deadline still gets its full per-call timeout")
	assert.LessOrEqual(t, c.left["late"], 2*time.Second, "the unit deadline is its scaled timeout, not the run ceiling")
}

// TestRunEngine_RunCeilingStopsHungRun: the run-level ceiling, not a unit
// deadline, ends a run whose units would otherwise wait far longer.
func TestRunEngine_RunCeilingStopsHungRun(t *testing.T) {
	t.Parallel()
	fake := newFake()
	fake.block["hang"] = make(chan struct{}) // never released
	p := &PreparedReview{
		Slots:       []Slot{agentSlot("hang"), agentSlot("queued")},
		TimeoutSec:  600,
		MaxParallel: 1,
		runCeiling:  50 * time.Millisecond,
	}
	start := time.Now()
	results, _ := runEngine(context.Background(), fake, p, t.TempDir())
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.Equal(t, StatusTimeout, results[0].Status)
	assert.Equal(t, StatusTimeout, results[1].Status, "a unit the ceiling reached first times out")
	assert.Zero(t, fake.callCount("queued"), "and makes no provider call")
}

// TestDefaultRunCeilingIsRegistryMax pins the ceiling's default.
func TestDefaultRunCeilingIsRegistryMax(t *testing.T) {
	t.Parallel()
	assert.Equal(t, time.Duration(registry.MaxTimeoutSecs)*time.Second, defaultRunCeiling)
}

// TestRunReview_BulkOutputIndependentOfPoolWidth: a bulk (non-chunked) review
// writes the same artifacts whether the pool is one worker per unit (the
// unbounded lane, the shape goroutine-per-slot had) or narrower than the roster.
// Only wall-clock fields may differ.
func TestRunReview_BulkOutputIndependentOfPoolWidth(t *testing.T) {
	roster := []string{"greta", "kai", "dax", "otto", "mira", "penny", "bruce"}
	run := func(maxParallel int) map[string]string {
		t.Setenv("ATCR_TEST_KEY", "secret")
		repo, base, head := initRepo(t)
		agents := map[string]registry.AgentConfig{}
		for _, n := range roster {
			agents[n] = registry.AgentConfig{Provider: "p", Model: "m-" + n, Persona: n, Temperature: ptrF(0.7)}
		}
		cfg := &ReviewConfig{
			Registry: &registry.Registry{
				Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: "http://unused.invalid"}},
				Agents:    agents,
			},
			Project:  &registry.ProjectConfig{Agents: roster},
			Settings: registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600, MaxParallel: maxParallel},
		}
		res, err := RunReview(context.Background(), newFake(), cfg, reviewReq(repo, repo, base, head))
		require.NoError(t, err)
		return poolArtifacts(t, filepath.Join(res.Dir, "sources", "pool"))
	}
	unbounded, pooled := run(0), run(2)
	require.NotEmpty(t, unbounded)
	assert.Equal(t, unbounded, pooled)
}

var wallClockField = regexp.MustCompile(`"duration_ms": \d+`)

// poolArtifacts reads every file under dir, keyed by relative path, with
// wall-clock durations blanked.
func poolArtifacts(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out[rel] = wallClockField.ReplaceAllString(string(data), `"duration_ms": 0`)
		return nil
	}))
	return out
}
