package fanout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/samestrin/atcr/internal/hookobs"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/metrics"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCompleter is a deterministic Completer for concurrency and failure tests.
// It tracks live concurrency (peak observed) and can delay, fail, or block on a
// release channel keyed by agent model.
type fakeCompleter struct {
	mu      sync.Mutex
	live    int32
	peak    int32
	calls   map[string]int // model -> call count
	delay   time.Duration
	failFor map[string]error         // model -> error to return
	block   map[string]chan struct{} // model -> release gate
	onStart func(model string)
}

func newFake() *fakeCompleter {
	return &fakeCompleter{calls: map[string]int{}, failFor: map[string]error{}, block: map[string]chan struct{}{}}
}

func (f *fakeCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	n := atomic.AddInt32(&f.live, 1)
	for {
		p := atomic.LoadInt32(&f.peak)
		if n <= p || atomic.CompareAndSwapInt32(&f.peak, p, n) {
			break
		}
	}
	defer atomic.AddInt32(&f.live, -1)

	f.mu.Lock()
	f.calls[inv.Model]++
	gate := f.block[inv.Model]
	onStart := f.onStart
	f.mu.Unlock()
	if onStart != nil {
		onStart(inv.Model)
	}

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	f.mu.Lock()
	err := f.failFor[inv.Model]
	f.mu.Unlock()
	if err != nil {
		return "", err
	}
	return "review by " + inv.Model, nil
}

func (f *fakeCompleter) callCount(model string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[model]
}

// agentSlot builds a non-serial slot with a single primary agent.
func agentSlot(name string) Slot {
	return Slot{Primary: Agent{
		Name:        name,
		Invocation:  llmclient.Invocation{Model: name},
		PayloadMode: "blocks",
	}}
}

// TestEngine_WithLogger verifies the WithLogger option stores the injected
// logger so logger() returns it (the review_id-correlated logger seeded by
// ExecuteReview reaches every agent invocation).
func TestEngine_WithLogger(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	e := NewEngine(newFake(), WithLogger(logger))
	require.Same(t, logger, e.logger(), "WithLogger must store the injected logger")
}

// TestEngine_NilLogger_ReturnsDiscard verifies the nil-safe fallback: an engine
// constructed without WithLogger returns a usable no-op discard logger — never
// nil, never the global slog default — and logging through it does not panic.
func TestEngine_NilLogger_ReturnsDiscard(t *testing.T) {
	t.Parallel()
	e := NewEngine(newFake())
	got := e.logger()
	require.NotNil(t, got, "logger() must never return nil")
	assert.NotPanics(t, func() { got.Info("no logger injected") }, "discard fallback must not panic")
}

// TestEngine_NilLogger_NoAlloc verifies the discard fallback returns the shared
// singleton with zero allocations, rather than constructing a fresh discard
// logger on every call (logger() is invoked once per agent in invokeAgent).
func TestEngine_NilLogger_NoAlloc(t *testing.T) {
	// No t.Parallel(): testing.AllocsPerRun panics if called during a parallel test.
	e := NewEngine(newFake())
	var sink *slog.Logger
	allocs := testing.AllocsPerRun(100, func() {
		sink = e.logger()
	})
	_ = sink
	assert.Zero(t, allocs, "logger() must not allocate on the discard path")
}

// TestInvokeAgent_AttachesAgentName verifies AC10: every log line emitted while
// an agent runs carries agent_name. The per-agent logger is scoped via
// log.WithAgent before the invocation and threaded through ctx.
func TestInvokeAgent_AttachesAgentName(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e := NewEngine(newFake(), WithLogger(logger))

	r := e.invokeAgent(context.Background(), agentSlot("security").Primary)
	require.Equal(t, StatusOK, r.Status)
	assert.Contains(t, buf.String(), log.AttrAgentName+"=security",
		"every log line during an agent invocation must carry agent_name (AC10)")
}

func TestRun_ParallelAgentsRunConcurrently(t *testing.T) {
	f := newFake()
	f.delay = 30 * time.Millisecond
	e := NewEngine(f)

	slots := []Slot{agentSlot("a"), agentSlot("b"), agentSlot("c")}
	results := e.Run(context.Background(), slots)

	require.Len(t, results, 3)
	for _, r := range results {
		assert.Equal(t, StatusOK, r.Status)
	}
	assert.Equal(t, int32(3), atomic.LoadInt32(&f.peak),
		"all three parallel agents should be in flight at once")
}

func TestRun_SerialLaneRunsSequentially(t *testing.T) {
	// Note: this test verifies serial lane serialization, not the max_parallel
	// semaphore cap. Serial slots always run in a single goroutine sequentially
	// regardless of cap. For cap coverage, see TestRun_MaxParallelBoundsPeakConcurrency.
	f := newFake()
	f.delay = 20 * time.Millisecond
	e := NewEngine(f)

	slots := []Slot{
		{Primary: Agent{Name: "s1", Invocation: llmclient.Invocation{Model: "s1"}}, Serial: true},
		{Primary: Agent{Name: "s2", Invocation: llmclient.Invocation{Model: "s2"}}, Serial: true},
		{Primary: Agent{Name: "s3", Invocation: llmclient.Invocation{Model: "s3"}}, Serial: true},
	}
	results := e.Run(context.Background(), slots)

	require.Len(t, results, 3)
	assert.Equal(t, int32(1), atomic.LoadInt32(&f.peak),
		"serial lane must never run two agents at once")
}

func TestRun_SerialAndParallelLanesRunConcurrently(t *testing.T) {
	f := newFake()
	// Gate every agent so they all park inside Complete; then peak concurrency
	// reflects how many lanes overlap.
	models := []string{"p1", "p2", "s1"}
	for _, m := range models {
		f.block[m] = make(chan struct{})
	}
	started := make(chan string, 5) // buffer for all agents so onStart never blocks
	f.onStart = func(m string) { started <- m }
	e := NewEngine(f)

	slots := []Slot{
		agentSlot("p1"),
		agentSlot("p2"),
		{Primary: Agent{Name: "s1", Invocation: llmclient.Invocation{Model: "s1"}}, Serial: true},
	}

	done := make(chan []Result, 1)
	go func() { done <- e.Run(context.Background(), slots) }()

	// Wait until all three (2 parallel + 1 serial) have entered Complete: proves
	// the serial lane runs concurrently with the parallel lane.
	seen := map[string]bool{}
	for len(seen) < 3 {
		select {
		case m := <-started:
			seen[m] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d agents started; lanes did not overlap", len(seen))
		}
	}
	for _, m := range models {
		close(f.block[m])
	}
	results := <-done
	require.Len(t, results, 3)
}

func TestRun_GlobalTimeoutCancelsAndDrains(t *testing.T) {
	f := newFake()
	f.delay = time.Hour // would block forever without cancellation
	e := NewEngine(f)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	slots := []Slot{agentSlot("a"), agentSlot("b")}

	done := make(chan []Result, 1)
	go func() { done <- e.Run(ctx, slots) }()

	select {
	case results := <-done:
		require.Len(t, results, 2)
		for _, r := range results {
			assert.Equal(t, StatusTimeout, r.Status, "deadline exceeded must surface as timeout")
			assert.Error(t, r.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context timeout — WaitGroup leaked")
	}
}

func TestRun_OneSlotFailsOthersStillComplete(t *testing.T) {
	f := newFake()
	f.failFor["b"] = errors.New("boom")
	e := NewEngine(f)

	results := e.Run(context.Background(), []Slot{agentSlot("a"), agentSlot("b"), agentSlot("c")})

	byName := map[string]Result{}
	for _, r := range results {
		byName[r.Agent] = r
	}
	assert.Equal(t, StatusOK, byName["a"].Status)
	assert.Equal(t, StatusFailed, byName["b"].Status)
	assert.Equal(t, StatusOK, byName["c"].Status)
}

func TestClassifyStatus(t *testing.T) {
	assert.Equal(t, StatusTimeout, classifyStatus(context.DeadlineExceeded))
	assert.Equal(t, StatusTimeout, classifyStatus(context.Canceled))
	assert.Equal(t, StatusTimeout, classifyStatus(fmt.Errorf("request failed: %w", context.DeadlineExceeded)))
	assert.Equal(t, StatusFailed, classifyStatus(errors.New("HTTP 401 unauthorized")))
	assert.Equal(t, StatusFailed, classifyStatus(errors.New("failed to parse response: unexpected EOF")))
}

// A genuine (non-context) error must be classified failed even when the ambient
// context is already cancelled — proving we classify on the returned error, not
// on ctx state. This is the race the old `if ctx.Err() != nil` check mislabeled.
func TestInvokeAgent_RealErrorUnderCancelledCtxIsFailedNotTimeout(t *testing.T) {
	f := newFake()
	f.failFor["a"] = errors.New("HTTP 401 unauthorized") // returned immediately, ignores ctx
	e := NewEngine(f)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // ambient context already done

	r := e.invokeAgent(ctx, Agent{Name: "a", Invocation: llmclient.Invocation{Model: "a"}})
	assert.Equal(t, StatusFailed, r.Status)
	assert.ErrorContains(t, r.Err, "401")
}

// A nil completer is a programming error; it must fail loudly at construction
// rather than nil-panicking deep inside the first agent invocation.
func TestNewEngine_NilCompleterPanics(t *testing.T) {
	assert.Panics(t, func() { NewEngine(nil) },
		"NewEngine(nil) must panic at construction, not inside invokeAgent")
}

// A serial slot short-circuited by cancellation must record the wall-clock that
// elapsed before the cancellation, not 0.
func TestRun_SerialShortCircuitStampsElapsedDuration(t *testing.T) {
	f := newFake()
	f.delay = time.Hour // first serial slot blocks until the deadline fires
	e := NewEngine(f)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	slots := []Slot{
		{Primary: Agent{Name: "s1", Invocation: llmclient.Invocation{Model: "s1"}}, Serial: true},
		{Primary: Agent{Name: "s2", Invocation: llmclient.Invocation{Model: "s2"}}, Serial: true},
	}
	results := e.Run(ctx, slots)

	require.Len(t, results, 2)
	assert.Equal(t, StatusTimeout, results[1].Status)
	assert.Greater(t, results[1].DurationMS, int64(0),
		"short-circuited slot must record elapsed wall-clock, not 0")
}

// A run whose context is cancelled before Run starts still reports every slot
// to WithSlotDone, but leases none of them: slotDone fires for slots whose
// slotStart never ran, in both lanes. Characterizes the behaviour WithSlotStart
// documents (Epic 71 T2); a hook pair that assumes start-then-done would break.
func TestRun_CancelledBeforeRunFiresSlotDoneWithoutSlotStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	serial := agentSlot("s")
	serial.Serial = true
	slots := []Slot{agentSlot("a"), agentSlot("b"), serial}

	var mu sync.Mutex
	started := map[int]int{}
	done := map[int]string{}
	NewEngine(newFake(),
		WithSlotStart(func(i int) {
			mu.Lock()
			defer mu.Unlock()
			started[i]++
		}),
		WithSlotDone(func(i int, r Result) {
			mu.Lock()
			defer mu.Unlock()
			done[i] = r.Status
		})).Run(ctx, slots)

	assert.Empty(t, started, "no slot is leased once the run is cancelled")
	assert.Equal(t, map[int]string{0: StatusTimeout, 1: StatusTimeout, 2: StatusTimeout}, done,
		"every slot still reports its final Result to slotDone")
}

// A serial slot short-circuited by cancellation must carry the primary's review
// constraints (MinSeverity/MaxFindings) so the cancelled slot's Result is
// structurally identical to one produced by invokeSlot. Without this, a future
// change stamping synthetic findings onto cancelled slots would skip guardrails
// only on the serial lane.
func TestRun_SerialShortCircuitStampsConstraints(t *testing.T) {
	f := newFake()
	f.delay = time.Hour
	e := NewEngine(f)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	maxFindings := 3
	slots := []Slot{
		{Primary: Agent{Name: "s1", Invocation: llmclient.Invocation{Model: "s1"}}, Serial: true},
		{Primary: Agent{
			Name: "s2", Invocation: llmclient.Invocation{Model: "s2"},
			MinSeverity: "HIGH", MaxFindings: &maxFindings,
		}, Serial: true},
	}
	results := e.Run(ctx, slots)

	require.Len(t, results, 2)
	assert.Equal(t, "HIGH", results[1].MinSeverity,
		"cancelled serial slot must carry MinSeverity from primary")
	require.NotNil(t, results[1].MaxFindings)
	assert.Equal(t, 3, *results[1].MaxFindings,
		"cancelled serial slot must carry MaxFindings from primary")
}

// DurationMS is slot wall time: a slot whose primary burned time before
// failing must report primary + fallback duration, not just the winner's.
func TestInvokeSlot_DurationCoversWholeChain(t *testing.T) {
	f := newFake()
	f.delay = 30 * time.Millisecond // every attempt takes ~30ms
	f.failFor["primary"] = errors.New("boom")
	e := NewEngine(f)

	slot := Slot{
		Primary:   Agent{Name: "primary", Invocation: llmclient.Invocation{Model: "primary"}},
		Fallbacks: []Agent{{Name: "fb", Invocation: llmclient.Invocation{Model: "fb"}}},
	}
	r := e.invokeSlot(context.Background(), slot)

	require.Equal(t, StatusOK, r.Status)
	require.True(t, r.FallbackUsed)
	assert.GreaterOrEqual(t, r.DurationMS, int64(60),
		"slot duration must cover the failed primary attempt plus the fallback, not just the winner")
}

func TestRun_EmptyRosterReturnsNoResults(t *testing.T) {
	e := NewEngine(newFake())
	results := e.Run(context.Background(), nil)
	assert.Empty(t, results)
}

func TestRun_ResultsPreserveInputOrder(t *testing.T) {
	f := newFake()
	e := NewEngine(f)
	var slots []Slot
	for i := 0; i < 8; i++ {
		slots = append(slots, agentSlot(fmt.Sprintf("a%d", i)))
	}
	results := e.Run(context.Background(), slots)
	require.Len(t, results, 8)
	for i, r := range results {
		assert.Equal(t, fmt.Sprintf("a%d", i), r.Agent, "result %d out of order", i)
	}
}

// panicCompleter wraps a fakeCompleter and panics for configured models.
type panicCompleter struct {
	f        *fakeCompleter
	panicFor map[string]bool
}

func (p *panicCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	if p.panicFor[inv.Model] {
		panic("simulated agent panic")
	}
	return p.f.Complete(ctx, inv)
}

// A panicking parallel slot must be isolated to a failed result instead of
// crashing the process.
func TestRun_ParallelSlotPanicRecovers(t *testing.T) {
	f := newFake()
	p := &panicCompleter{f: f, panicFor: map[string]bool{"b": true}}
	e := NewEngine(p)

	slots := []Slot{agentSlot("a"), agentSlot("b"), agentSlot("c")}
	results := e.Run(context.Background(), slots)

	require.Len(t, results, 3)
	byName := map[string]Result{}
	for _, r := range results {
		byName[r.Agent] = r
	}
	assert.Equal(t, StatusOK, byName["a"].Status)
	assert.Equal(t, StatusFailed, byName["b"].Status)
	assert.ErrorContains(t, byName["b"].Err, "panic")
	assert.Equal(t, StatusOK, byName["c"].Status)
}

// A panicking serial slot must be isolated to a failed result.
func TestRun_SerialSlotPanicRecovers(t *testing.T) {
	f := newFake()
	p := &panicCompleter{f: f, panicFor: map[string]bool{"s2": true}}
	e := NewEngine(p)

	slots := []Slot{
		{Primary: Agent{Name: "s1", Invocation: llmclient.Invocation{Model: "s1"}}, Serial: true},
		{Primary: Agent{Name: "s2", Invocation: llmclient.Invocation{Model: "s2"}}, Serial: true},
	}
	results := e.Run(context.Background(), slots)

	require.Len(t, results, 2)
	assert.Equal(t, StatusOK, results[0].Status)
	assert.Equal(t, StatusFailed, results[1].Status)
	assert.ErrorContains(t, results[1].Err, "panic")
}

// A recovered agent panic must still record the agent's terminal metrics. The
// panic unwinds past invokeAgent's duration Observe + recordAgentOutcome
// (engine.go:433-434), so without recover-site instrumentation the agent is
// counted in atcr_agents_total but in no outcome counter — silently breaking the
// invariant agents_total == succeeded + failed + timed_out. It must be counted
// exactly once (agents_total must not double-count).
func TestRun_RecoveredPanicRecordsAgentMetrics(t *testing.T) {
	f := newFake()
	p := &panicCompleter{f: f, panicFor: map[string]bool{"boom": true}}
	e := NewEngine(p)

	total0 := metrics.Counter(metrics.NameAgentsTotal).Value()
	failed0 := metrics.Counter(metrics.NameAgentsFailed).Value()
	durCount0 := metrics.Histogram(metrics.NameAgentDurationSeconds).Count()

	results := e.Run(context.Background(), []Slot{agentSlot("boom")})

	require.Len(t, results, 1)
	require.Equal(t, StatusFailed, results[0].Status)
	require.ErrorContains(t, results[0].Err, "panic")

	assert.Equal(t, int64(1), metrics.Counter(metrics.NameAgentsTotal).Value()-total0,
		"panicking agent must be counted in agents_total exactly once")
	assert.Equal(t, int64(1), metrics.Counter(metrics.NameAgentsFailed).Value()-failed0,
		"recovered panic must record a failed outcome so the agents_total invariant holds")
	assert.Equal(t, int64(1), metrics.Histogram(metrics.NameAgentDurationSeconds).Count()-durCount0,
		"recovered panic must observe agent duration")
}

func parallelSlots(n int) []Slot {
	var slots []Slot
	for i := 0; i < n; i++ {
		slots = append(slots, agentSlot(fmt.Sprintf("a%d", i)))
	}
	return slots
}

func TestRun_MaxParallelBoundsPeakConcurrency(t *testing.T) {
	f := newFake()
	// Gate EVERY agent so the cap fills deterministically regardless of which
	// agents win the two semaphore permits. With max_parallel=2 exactly two park
	// inside Complete (peak reaches 2) while the rest block on the semaphore. The
	// old version gated only a0/a1 and assumed those two were admitted first, so
	// it flaked under load when a non-gated agent won a permit and finished before
	// the two gated agents were ever live together.
	models := []string{"a0", "a1", "a2", "a3", "a4"}
	for _, m := range models {
		f.block[m] = make(chan struct{})
	}
	started := make(chan string, len(models)) // buffer all so onStart never blocks
	f.onStart = func(m string) { started <- m }
	e := NewEngine(f, WithMaxParallel(2))

	done := make(chan []Result, 1)
	go func() { done <- e.Run(context.Background(), parallelSlots(len(models))) }()

	// onStart fires after the peak update, so once two distinct agents have
	// started they are both parked on their gates holding both permits — peak is
	// exactly 2 (cap reached) and no third agent can be admitted until release.
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case m := <-started:
			seen[m] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d agents started; semaphore did not admit cap agents", len(seen))
		}
	}
	assert.Equal(t, int32(2), atomic.LoadInt32(&f.peak),
		"max_parallel=2 must cap a 5-agent roster at 2 concurrent calls")

	// Release every gate so all agents complete.
	for _, m := range models {
		close(f.block[m])
	}

	results := <-done
	require.Len(t, results, 5)
	for _, r := range results {
		assert.Equal(t, StatusOK, r.Status)
	}
}

func TestRun_MaxParallelZeroIsUnbounded(t *testing.T) {
	f := newFake()
	f.delay = 30 * time.Millisecond
	e := NewEngine(f, WithMaxParallel(0))

	results := e.Run(context.Background(), parallelSlots(5))

	require.Len(t, results, 5)
	assert.Equal(t, int32(5), atomic.LoadInt32(&f.peak),
		"max_parallel=0 is unbounded: all five agents run at once (current behavior)")
}

func TestRun_MaxParallelLargerThanRosterIsUnbounded(t *testing.T) {
	f := newFake()
	f.delay = 30 * time.Millisecond
	e := NewEngine(f, WithMaxParallel(50))

	results := e.Run(context.Background(), parallelSlots(3))

	require.Len(t, results, 3)
	assert.Equal(t, int32(3), atomic.LoadInt32(&f.peak),
		"a cap above the roster size never blocks: all three run at once")
}

func TestRun_MaxParallelDrainsUnderCancellation(t *testing.T) {
	// Verify no goroutine leak: count goroutines before and after Run.
	before := runtime.NumGoroutine()

	// The semaphore must not defeat the WaitGroup drain guarantee: with a roster
	// larger than the cap, queued goroutines whose ctx-aware acquire loses to
	// cancellation still resolve to a timeout result and Done() — no leak.
	f := newFake()
	f.delay = time.Hour // every started agent blocks until the deadline fires
	e := NewEngine(f, WithMaxParallel(2))

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	done := make(chan []Result, 1)
	go func() { done <- e.Run(ctx, parallelSlots(5)) }()

	select {
	case results := <-done:
		require.Len(t, results, 5)
		for _, r := range results {
			assert.Equal(t, StatusTimeout, r.Status, "every slot must surface timeout under the cap")
			assert.Error(t, r.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return under the cap — semaphore blocked the WaitGroup drain")
	}

	// Poll for goroutine cleanup (similar to goleak): give the runtime a short
	// window to reclaim goroutines, then assert no persistent leak.
	var after int
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		after = runtime.NumGoroutine()
		if after <= before+2 {
			break
		}
	}
	assert.LessOrEqual(t, after, before+2,
		"goroutine count must not permanently increase after Run — WaitGroup drain guarantee")
}

// ctxRecordingCompleter records the audit call identity from the context of
// each invocation, so a test can assert what invokeAgent stamped.
type ctxRecordingCompleter struct {
	mu   sync.Mutex
	seen []hookobs.Call
}

func (c *ctxRecordingCompleter) Complete(ctx context.Context, _ llmclient.Invocation) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, hookobs.CallFrom(ctx))
	return "HIGH: a finding", nil
}

func (c *ctxRecordingCompleter) calls() []hookobs.Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]hookobs.Call, len(c.seen))
	copy(out, c.seen)
	return out
}

// TestInvokeAgent_StampsAgentIdentity covers the chokepoint that attributes a
// model call to the persona that made it (Epic 35.0). Without it an audit
// observer receives a stream in which every reviewer's invocations are
// indistinguishable — the fan-out engine is the only layer that knows the agent
// name, so nothing downstream can recover it.
func TestInvokeAgent_StampsAgentIdentity(t *testing.T) {
	t.Parallel()
	rec := &ctxRecordingCompleter{}
	e := NewEngine(rec)

	res := e.invokeAgent(context.Background(), Agent{
		Name:       "security-reviewer",
		Invocation: llmclient.Invocation{Model: "test/model-a", Prompt: "p"},
	})
	require.Empty(t, res.Err)

	calls := rec.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "security-reviewer", calls[0].AgentName,
		"invokeAgent must attribute the call to its agent")
}

// TestInvokeAgent_PreservesOuterCallIdentity: the engine contributes only the
// agent name. The run id and stage come from the layer above (cli, verify,
// debate) and must survive, or a record cannot be tied back to its review.
func TestInvokeAgent_PreservesOuterCallIdentity(t *testing.T) {
	t.Parallel()
	rec := &ctxRecordingCompleter{}
	e := NewEngine(rec)
	ctx := hookobs.WithCall(context.Background(), hookobs.Call{RunID: "2026-07-25_main", Stage: "review"})

	res := e.invokeAgent(ctx, Agent{
		Name:       "security-reviewer",
		Invocation: llmclient.Invocation{Model: "test/model-a", Prompt: "p"},
	})
	require.Empty(t, res.Err)

	calls := rec.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, hookobs.Call{RunID: "2026-07-25_main", AgentName: "security-reviewer", Stage: "review"},
		calls[0], "the engine must add the agent name without disturbing run or stage")
}

// --- T6 (sprint 35.16.11.2.2.4): refuse a salvaged reply as findings ----------
//
// TEST MAP: parseFindings/ParsedFindingCount's cases are split across three files,
// each covering one layer. The strip and content-preservation cases live in
// response_truncation_test.go (TestResult_ParseFindings_*); the merge-level,
// per-bin ones in chunker_test.go (TestMergeResultGroup_*Salvaged*); and the
// invokeSlot/salvage cases HERE. The split is deliberate (each file already owns
// its layer) but was unnamed — change parseFindings and you must touch all three.
// Fixtures (draft/real finding rows, "fresh Result per assertion: ParsedFindingCount
// memoizes") are duplicated across the halves rather than shared.

// The salvage path (the empty-content branch of llmclient.CompleteWithMeta)
// promotes a reply's chain-of-thought into Content when the provider returns empty
// content. Verify (verify/invoke.go:150) and every debate seat
// (debate/protocol.go:157) already refuse it; the diff cache refuses it (the store
// gate in invokeCachedSingleShot). The findings lane had no
// guard at all, so a DRAFT finding written inside abandoned reasoning could be
// parsed and written to the pool as real. parseFindings is the single choke point
// both ParsedFindingCount and findingsFor share, so one guard there covers both.
func TestResult_ParseFindings_SalvagedReplyYieldsNoFindings(t *testing.T) {
	// Well-formed on purpose: pre-fix this content parses to one finding, which is
	// what makes its refusal the thing under test rather than a parsing accident.
	const draft = "HIGH|a.go:1|draft finding from abandoned reasoning|f|correctness|5|e"
	// A committed finding from a bin that did NOT salvage, for the per-chunk cases.
	const real = "MEDIUM|b.go:2|real finding|f|correctness|2|e"

	t.Run("unchunked", func(t *testing.T) {
		assert.Equal(t, 1, (&Result{Content: draft}).ParsedFindingCount(),
			"the same content without the Salvaged marker really does parse to one finding")

		// ResponseTruncated is part of the fixture, not an extra condition under test:
		// since TD internal/fanout/engine.go:604 the unchunked refusal reads Salvaged
		// AND ResponseTruncated, so truncation is what makes this reasoning ABANDONED
		// rather than an answer that merely came back on the other channel. The
		// assertions below are unchanged; only the shape that earns them is now stated
		// in full. The inverse shape is pinned by
		// TestResult_ParseFindings_SalvagedOnStopKeepsItsFindings.
		//
		// Fresh Result per assertion: ParsedFindingCount memoizes on first use.
		assert.Equal(t, 0, (&Result{Content: draft, Salvaged: true, ResponseTruncated: true}).ParsedFindingCount(),
			"a salvaged reply cut off mid-thought must not be counted as findings")

		fr := findingsFor(Result{Agent: "bruce", Status: StatusOK, Content: draft,
			Salvaged: true, ResponseTruncated: true}, nil)
		assert.Empty(t, fr.Findings,
			"findingsFor must see the guard too, not just the count gate — it is the path to the pool")
	})

	t.Run("chunked, only the salvaged bin is refused", func(t *testing.T) {
		// Built THROUGH mergeResultGroup so the fixture is a shape the chunked path
		// really emits — hand-setting chunkContents beside a stale Content pins
		// parseFindings against a Result no code produces. The refusal is per chunk:
		// bin 2 salvaged, so its draft is dropped, but bin 1's committed finding
		// survives. chunkSalvaged is index-aligned with chunkContents by construction
		// (chunkBin).
		merged := mergeResultGroup([]Result{
			{Agent: "bruce", Status: StatusOK, Content: real},
			{Agent: "bruce", Status: StatusOK, Content: draft, Salvaged: true},
		}, nil)
		require.Equal(t, []bool{false, true}, merged.chunkSalvaged)
		assert.Equal(t, 1, merged.ParsedFindingCount(), "the clean bin's finding must survive its sibling's salvage")
		fr := findingsFor(merged, nil)
		require.Len(t, fr.Findings, 1)
		assert.Equal(t, "real finding", fr.Findings[0].Problem)
	})

	t.Run("chunked, every bin salvaged yields nothing", func(t *testing.T) {
		merged := mergeResultGroup([]Result{
			{Agent: "bruce", Status: StatusOK, Content: draft, Salvaged: true},
			{Agent: "bruce", Status: StatusOK, Content: draft, Salvaged: true},
		}, nil)
		require.Equal(t, []bool{true, true}, merged.chunkSalvaged)
		assert.Equal(t, 0, merged.ParsedFindingCount())
		assert.Empty(t, findingsFor(merged, nil).Findings)
	})

	t.Run("chunked with no per-chunk flags fails closed", func(t *testing.T) {
		// Deliberately a shape mergeResultGroup does NOT emit: some other assembling
		// path left the per-chunk flags absent while the persona-wide bit is set, so
		// the result cannot say WHICH bin salvaged. Refusing the whole result is the
		// safe reading: it never parses salvaged reasoning as a finding.
		r := Result{
			Agent:         "bruce",
			Status:        StatusOK,
			Content:       real + "\n" + draft,
			chunkContents: []string{real, draft},
			Salvaged:      true,
		}
		assert.Equal(t, 0, r.ParsedFindingCount(), "without per-chunk flags the persona-wide bit refuses everything")
		assert.Empty(t, findingsFor(r, nil).Findings)
	})

	t.Run("chunked, mismatched flags with NO salvage parses every bin", func(t *testing.T) {
		// The COMPLEMENTARY half of the length-mismatch branch, previously unpinned: a
		// future change that stopped OR-folding Salvaged in mergeResultGroup would
		// silently turn the fail-closed guard above into a fail-open one with the suite
		// still green. Here the flags are misaligned AND no bin is marked salvaged, so
		// there is no reasoning to protect and parsing every bin is the correct
		// reading — the guard exists to refuse SALVAGED content, not to reject
		// misaligned shapes for their own sake. Pinned so that reading is a decision on
		// the record.
		r := Result{
			Agent:         "bruce",
			Status:        StatusOK,
			Content:       real + "\n" + draft,
			chunkContents: []string{real, draft},
			chunkSalvaged: []bool{false}, // misaligned on purpose
			Salvaged:      false,
		}
		assert.Equal(t, 2, r.ParsedFindingCount(),
			"with no salvage recorded there is nothing to refuse, so both bins parse")
		assert.Len(t, findingsFor(r, nil).Findings, 2)
	})
}

// THE BOUNDARY, and the whole reason the guard reads Salvaged ONLY. Verify and
// debate check ResponseTruncated too, because a verdict or a statement is either
// whole or worthless. Findings are not: a truncated review's partial findings are
// real, and the truncationFailover gate in invokeSlot is built to tell
// truncated-with-findings (keep) from truncated-with-nothing (fail over). Adding
// ResponseTruncated here would zero the count for EVERY truncated review and fire
// that gate on reviews that did raise findings. Pinned so the next reader cannot
// quietly "fix" the asymmetry with invoke.go/protocol.go.
func TestResult_ParseFindings_TruncatedButNotSalvagedKeepsItsFindings(t *testing.T) {
	const real = "MEDIUM|b.go:2|real finding from a cut-off review|f|correctness|2|e"
	r := Result{Agent: "bruce", Status: StatusOK, Content: real, ResponseTruncated: true}

	assert.Equal(t, 1, r.ParsedFindingCount(),
		"a truncated-but-not-salvaged reply's partial findings are real and must survive")
	fr := findingsFor(r, nil)
	require.Len(t, fr.Findings, 1)
	assert.Equal(t, "real finding from a cut-off review", fr.Findings[0].Problem)
}

// THE OTHER BOUNDARY, and the one the Salvaged-only guard got wrong.
// llmclient.CompleteWithMeta sets Salvaged purely on empty content plus non-empty
// reasoning (client.go:428-455) — a CHANNEL fact, not an ABANDONMENT fact. On
// finish_reason "stop" the model finished normally and its answer simply arrived on
// reasoning_content, which is the standard shape for several reasoning deployments.
// ResponseTruncated is FALSE there, so the Salvaged-only guard discarded a committed
// review with no failover, no log line and FindingsCount 0 in status.json.
//
// Pairing the two flags is what separates the two cases: a salvage WITH truncation is
// a thought the provider cut off mid-sentence (refuse it, as before), while a salvage
// on a stop reason is a finished answer on the other channel (parse it). The sibling
// test above pins Salvaged=false/Truncated=true; this one pins the inverse, so neither
// flag can be dropped from the pair without a red test.
func TestResult_ParseFindings_SalvagedOnStopKeepsItsFindings(t *testing.T) {
	const real = "HIGH|d.go:4|committed finding delivered on the reasoning channel|f|correctness|3|e"

	t.Run("stop reason: the answer is finished, just on the other channel", func(t *testing.T) {
		r := Result{Agent: "bruce", Status: StatusOK, Content: real, Salvaged: true}

		assert.Equal(t, 1, r.ParsedFindingCount(),
			"a salvage with no truncation is a completed answer on the reasoning channel, not an abandoned draft")
		fr := findingsFor(r, nil)
		require.Len(t, fr.Findings, 1)
		assert.Equal(t, "committed finding delivered on the reasoning channel", fr.Findings[0].Problem)
	})

	t.Run("cut off mid-thought: still refused, exactly as before", func(t *testing.T) {
		r := Result{Agent: "bruce", Status: StatusOK, Content: real, Salvaged: true, ResponseTruncated: true}

		assert.Equal(t, 0, r.ParsedFindingCount(),
			"a salvage the provider cut off IS an abandoned draft and must still yield nothing")
		assert.Empty(t, findingsFor(r, nil).Findings)
	})
}

// No regression on the ordinary row: neither flag set, findings parse as before.
func TestResult_ParseFindings_UnflaggedReplyIsUnaffected(t *testing.T) {
	const real = "LOW|c.go:3|ordinary finding|f|correctness|1|e"
	assert.Equal(t, 1, (&Result{Content: real}).ParsedFindingCount())
	assert.Len(t, findingsFor(Result{Agent: "bruce", Status: StatusOK, Content: real}, nil).Findings, 1)
}

// The whole thing composing, end to end, and the row the 4.1.A review was filed
// over. Bin 1 returned a committed finding; bin 2 salvaged. The merged persona must
// keep bin 1's finding and drop bin 2's draft.
//
// Refusing on the persona-wide fold instead would discard bin 1's real finding,
// score the persona unparseable for a finding it did produce, and falsify
// docs/findings-format.md's chunk contract ("one garbled chunk beside a chunk with
// findings is counted there without marking the persona unparseable"). Both
// assertions on UnparseableResponse below exist to pin that the contract holds.
func TestMergeResultGroup_SalvagedLaterChunkKeepsSiblingFindings(t *testing.T) {
	g := []Result{
		{Agent: "bruce", Status: StatusOK, Content: "MEDIUM|b.go:2|real finding|f|correctness|2|e"},
		{Agent: "bruce", Status: StatusOK, Content: "HIGH|a.go:1|draft|f|correctness|5|e",
			Salvaged: true, UnparseableResponse: true},
	}
	merged := mergeResultGroup(g, nil)

	require.True(t, merged.Salvaged,
		"the persona-wide bit still records that a salvage happened, for status and the cache")
	require.Equal(t, []bool{false, true}, merged.chunkSalvaged,
		"the per-chunk flags must stay index-aligned with the chunk contents")

	assert.Equal(t, 1, merged.ParsedFindingCount(), "bin 1's committed finding must survive")
	fr := findingsFor(merged, nil)
	require.Len(t, fr.Findings, 1)
	assert.Equal(t, "real finding", fr.Findings[0].Problem)
	assert.Equal(t, "b.go", fr.Findings[0].File)

	assert.Equal(t, 1, merged.UnparseableChunks, "the salvaged bin is still counted as unparseable")
	assert.False(t, merged.UnparseableResponse,
		"a persona is not scored unparseable for findings it did produce")
}

// THE INVARIANT THE WHOLE PER-BIN DESIGN RESTS ON, and it was unpinned until the
// Phase 4 gate asked for it. chunkSalvaged and chunkContents must stay index-aligned,
// which mergeResultGroup achieves by appending to both inside the SAME
// non-empty-content branch. Hoisting the flag append out of that branch leaves the
// whole suite green but misaligns the slices whenever a bin returns empty content —
// and misalignment is not benign: parseFindings reads the lengths as a mismatch,
// falls back to the persona-wide bit, and refuses EVERYTHING, silently dropping the
// clean bin's real findings. So the empty bin in the middle is the point.
func TestMergeResultGroup_EmptyChunkKeepsSalvageFlagsAligned(t *testing.T) {
	g := []Result{
		{Agent: "bruce", Status: StatusOK, Content: "MEDIUM|b.go:2|real finding|f|correctness|2|e"},
		{Agent: "bruce", Status: StatusOK, Content: "   "}, // dropped from BOTH slices
		{Agent: "bruce", Status: StatusOK, Content: "HIGH|a.go:1|draft|f|correctness|5|e", Salvaged: true},
	}
	merged := mergeResultGroup(g, nil)

	require.Len(t, merged.chunkContents, 2, "the whitespace-only bin contributes no content")
	require.Equal(t, []bool{false, true}, merged.chunkSalvaged,
		"the flags must drop the same bin the contents did, or every index after it names the wrong bin")
	assert.Equal(t, 1, merged.ParsedFindingCount(),
		"misalignment would make parseFindings fail closed and lose this finding")
	assert.Equal(t, "real finding", findingsFor(merged, nil).Findings[0].Problem)
}

// The doc's two halves, at the merge level. docs/findings-format.md says where a
// refused bin is counted depends on WHY it salvaged, so both arms are pinned:
// a stop-reason salvage stays ok and lands in unparseable_chunks, while a
// length-cutoff salvage was already demoted to StatusFailed by the failover gate
// and lands in unreviewed_chunks instead. Getting this wrong is how the published
// sentence silently stops matching the code.
func TestMergeResultGroup_SalvagedChunkCountedByWhyItSalvaged(t *testing.T) {
	clean := Result{Agent: "bruce", Status: StatusOK, Content: "MEDIUM|b.go:2|real finding|f|correctness|2|e"}

	t.Run("stop-reason salvage is unparseable", func(t *testing.T) {
		// A salvage on a stop reason (SalvagedOnStop) stays StatusOK; its content is
		// prose nothing could parse, so invokeSlot's marker block sets
		// UnparseableResponse, and mergeResultGroup counts it.
		merged := mergeResultGroup([]Result{clean, {
			Agent: "bruce", Status: StatusOK, Content: "chain of thought only",
			Salvaged: true, SalvagedOnStop: true, UnparseableResponse: true,
		}}, nil)

		require.Equal(t, []bool{false, true}, merged.chunkSalvagedOnStop,
			"the fixture must really be a stop-reason salvage, or the label lies")
		assert.Equal(t, 1, merged.UnparseableChunks)
		assert.Equal(t, 0, merged.UnreviewedChunks)
		assert.Equal(t, 1, merged.ParsedFindingCount(), "the clean bin still contributes")
	})

	t.Run("length-cutoff salvage is unreviewed, not unparseable", func(t *testing.T) {
		// The failover gate already demoted this bin to StatusFailed, so the
		// UnparseableResponse block (gated on StatusOK) never ran for it and
		// UnreviewedChunks counts it as len(g) - okCount instead.
		merged := mergeResultGroup([]Result{clean, {
			Agent: "bruce", Status: StatusFailed, Content: "chain of thought only",
			Salvaged: true, ResponseTruncated: true, Err: errTruncatedZeroFindings,
		}}, nil)

		assert.Equal(t, 0, merged.UnparseableChunks,
			"a failed bin never reaches the unparseable marker — it is gated on StatusOK")
		assert.Equal(t, 1, merged.UnreviewedChunks)
		assert.Equal(t, 1, merged.ParsedFindingCount(), "the clean bin still contributes")
	})
}

// The other half of the same fold: every bin salvaged means the persona really does
// contribute nothing, and it IS scored unparseable.
func TestMergeResultGroup_AllChunksSalvagedYieldsNoFindings(t *testing.T) {
	g := []Result{
		{Agent: "bruce", Status: StatusOK, Content: "HIGH|a.go:1|draft one|f|correctness|5|e",
			Salvaged: true, UnparseableResponse: true},
		{Agent: "bruce", Status: StatusOK, Content: "HIGH|a.go:2|draft two|f|correctness|5|e",
			Salvaged: true, UnparseableResponse: true},
	}
	merged := mergeResultGroup(g, nil)

	assert.Equal(t, 0, merged.ParsedFindingCount())
	assert.Empty(t, findingsFor(merged, nil).Findings)
	assert.True(t, merged.UnparseableResponse, "no bin produced anything a parser could use")
}

// The chunked arm now knows WHY each bin salvaged. A bin that salvaged on a stop
// reason is a finished answer on the reasoning channel, so its findings count, while
// a bin cut off mid-thought is still refused. Before the per-chunk reason existed the
// chunked arm refused every salvaged bin, so the mixed persona parsed 0.
func TestMergeResultGroup_StopReasonSalvagedChunkKeepsItsFindings(t *testing.T) {
	const draft = "HIGH|a.go:1|draft|f|correctness|5|e"
	const real = "MEDIUM|b.go:2|real finding on the reasoning channel|f|correctness|2|e"

	t.Run("truncated bin refused, stop-reason bin kept", func(t *testing.T) {
		merged := mergeResultGroup([]Result{
			{Agent: "bruce", Status: StatusOK, Content: draft, Salvaged: true, ResponseTruncated: true},
			{Agent: "bruce", Status: StatusOK, Content: real, Salvaged: true, SalvagedOnStop: true},
		}, nil)

		require.Equal(t, []bool{true, true}, merged.chunkSalvaged)
		require.Equal(t, []bool{false, true}, merged.chunkSalvagedOnStop)
		assert.Equal(t, 1, merged.ParsedFindingCount())
		fr := findingsFor(merged, nil)
		require.Len(t, fr.Findings, 1)
		assert.Equal(t, "real finding on the reasoning channel", fr.Findings[0].Problem)
	})

	t.Run("both bins truncated: still nothing", func(t *testing.T) {
		merged := mergeResultGroup([]Result{
			{Agent: "bruce", Status: StatusOK, Content: draft, Salvaged: true, ResponseTruncated: true},
			{Agent: "bruce", Status: StatusOK, Content: real, Salvaged: true, ResponseTruncated: true},
		}, nil)

		assert.Equal(t, 0, merged.ParsedFindingCount())
		assert.Empty(t, findingsFor(merged, nil).Findings)
	})

	t.Run("empty middle bin keeps all three slices aligned", func(t *testing.T) {
		merged := mergeResultGroup([]Result{
			{Agent: "bruce", Status: StatusOK, Content: draft, Salvaged: true, ResponseTruncated: true},
			{Agent: "bruce", Status: StatusOK, Content: "   "}, // dropped from all three slices
			{Agent: "bruce", Status: StatusOK, Content: real, Salvaged: true, SalvagedOnStop: true},
		}, nil)

		require.Len(t, merged.chunkContents, 2)
		require.Equal(t, []bool{true, true}, merged.chunkSalvaged)
		require.Equal(t, []bool{false, true}, merged.chunkSalvagedOnStop)
		assert.Equal(t, 1, merged.ParsedFindingCount())
	})

	t.Run("misaligned reason slice reads as not-on-stop", func(t *testing.T) {
		r := Result{
			Agent: "bruce", Status: StatusOK, Salvaged: true,
			chunkContents:       []string{real},
			chunkSalvaged:       []bool{true},
			chunkSalvagedOnStop: nil,
		}
		assert.Equal(t, 0, r.ParsedFindingCount(), "no per-chunk reason means fail closed")
	})
}

// salvageReasonCompleter returns a fixed Completion (or error) on the Meta path, so
// invokeSlot's copy of the per-reply salvage reason can be observed directly.
type salvageReasonCompleter struct {
	comp llmclient.Completion
	err  error
}

func (s salvageReasonCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return s.comp.Content, s.err
}

func (s salvageReasonCompleter) CompleteWithMeta(_ context.Context, _ llmclient.Invocation) (llmclient.Completion, error) {
	return s.comp, s.err
}

// invokeSlot carries the client's reason onto the Result, and the ErrSalvagedReply
// arm, which has no Completion to read a reason from, leaves it false so that arm
// stays fail-closed.
func TestInvokeSlot_CarriesSalvagedOnStop(t *testing.T) {
	slot := Slot{Primary: Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}}}

	t.Run("stop-reason salvage sets it", func(t *testing.T) {
		e := NewEngine(salvageReasonCompleter{comp: llmclient.Completion{
			Content:  "HIGH|a.go:1|finding|f|correctness|5|e",
			Salvaged: true, SalvagedOnStop: true,
		}})
		r := e.invokeSlot(context.Background(), slot)
		assert.True(t, r.Salvaged)
		assert.True(t, r.SalvagedOnStop)
	})

	t.Run("truncated salvage leaves it false", func(t *testing.T) {
		e := NewEngine(salvageReasonCompleter{comp: llmclient.Completion{
			Content:  "HIGH|a.go:1|draft|f|correctness|5|e",
			Salvaged: true, SalvagedTruncated: true, Truncated: true,
		}})
		r := e.invokeSlot(context.Background(), slot)
		assert.True(t, r.Salvaged)
		assert.False(t, r.SalvagedOnStop)
	})

	t.Run("ErrSalvagedReply arm leaves it false", func(t *testing.T) {
		e := NewEngine(salvageReasonCompleter{err: llmclient.ErrSalvagedReply})
		r := e.invokeSlot(context.Background(), slot)
		assert.True(t, r.Salvaged)
		assert.False(t, r.SalvagedOnStop, "no Completion reached this arm, so there is no reason to carry")
	})
}

// PINNED, not asserted-unchanged (task-06 Test Strategy): the guard necessarily
// MOVES what a salvaged row means downstream, so both shapes are recorded here.
//
// Salvage does NOT imply truncation — llmclient.CompleteWithMeta salvages inside
// its content == "" branch on ANY finish reason, so a stop-reason reply with empty
// content and reasoning present is salvaged with ResponseTruncated false. The two
// shapes therefore land in different places and must not be collapsed.
func TestInvokeSlot_SalvagedReply_ContributesNoFindings(t *testing.T) {
	// AMENDED by TD internal/fanout/engine.go:604 (2026-10-05). The subtest below used
	// to assert this shape contributed NOTHING and scored unparseable. That was T6's
	// deliberate choice and it is now deliberately reversed: Salvaged is a CHANNEL
	// fact, so on a stop reason this is a FINISHED review that merely arrived on
	// reasoning_content — the standard shape for several reasoning deployments — and
	// refusing it destroyed those reviewers' entire real output with no failover and
	// no log line. The assertions move with the behaviour rather than being relaxed:
	// each one is now the opposite claim, stated and tested, not dropped.
	//
	// The sibling subtest (salvaged AND truncated) is untouched, which is the point —
	// the pair is what separates a cut-off thought from a completed answer.
	t.Run("salvaged on a stop reason: a finished review on the other channel", func(t *testing.T) {
		// StatusOK survives as before. What changed is the count and the marker: the
		// reply parsed, so it is neither empty nor unparseable, and ReviewerOutcome
		// scores it as the real review it is.
		e := NewEngine(&metaTruncatingCompleter{
			content:  "HIGH|a.go:1|committed finding delivered on the reasoning channel|f|correctness|5|e",
			salvaged: true,
		}, WithTruncationFailover())
		r := e.invokeSlot(context.Background(), Slot{Primary: Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}}})

		assert.Equal(t, StatusOK, r.Status)
		require.True(t, r.Salvaged, "the disclosure marker still records which channel the answer came on")
		require.False(t, r.ResponseTruncated, "precondition: a stop reason is not a truncation")
		assert.Equal(t, 1, r.ParsedFindingCount(),
			"a completed answer on the reasoning channel is a real review, not an abandoned draft")
		assert.False(t, r.UnparseableResponse,
			"it parsed, so it is not unparseable — scoring it so hid a reviewer's whole contribution")
	})

	t.Run("salvaged and truncated: demoted to failover", func(t *testing.T) {
		// Zero findings now trips the truncation-failover gate, where before the
		// guard this reply could pass it on the strength of its draft row. The cost
		// is one backup-model call; the alternative is a draft counted as real.
		e := NewEngine(&metaTruncatingCompleter{
			content:   "HIGH|a.go:1|draft from abandoned reasoning|f|correctness|5|e",
			salvaged:  true,
			truncated: true,
		}, WithTruncationFailover())
		r := e.invokeSlot(context.Background(), Slot{Primary: Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}}})

		assert.Equal(t, 0, r.ParsedFindingCount())
		assert.Equal(t, StatusFailed, r.Status, "a salvaged reply with nothing parseable is a runaway")
		assert.ErrorIs(t, r.Err, errTruncatedZeroFindings)
		assert.False(t, r.UnparseableResponse,
			"the unparseable marker is gated on StatusOK, so the failover path never sets it")
	})
}

// TestResult_ParseFindings_MemoizesTheParsedSlice pins the slice cache: a
// second parseFindings call on the same Result must return the cached slice
// (shared backing array), not re-run SplitThink + ParseModelOutput. The
// truncation-failover gate parses via ParsedFindingCount and findingsFor parses
// again for every result that HAS findings — caching the slice is what makes
// the two share one parse instead of only the zero case sharing one.
func TestResult_ParseFindings_MemoizesTheParsedSlice(t *testing.T) {
	r := &Result{Content: "HIGH|a.go:1|x|f|correctness|1|e\nLOW|b.go:2|y|f|correctness|1|e"}
	first := r.parseFindings()
	require.NotEmpty(t, first)
	second := r.parseFindings()
	if unsafe.SliceData(first) != unsafe.SliceData(second) {
		t.Fatalf("parseFindings must memoize the parsed slice across calls on the same Result; got two separate parses")
	}
}

// TestInvokeSlot_SalvagedSentinelShapedReply_IsCleanOnlyOnAStopReasonSalvage is
// the complement of TestInvokeSlot_SalvagedReply_ContributesNoFindings for the
// clean-review sentinel: a salvaged reply whose promoted reasoning is literally
// "NO FINDINGS". The sentinel parses to zero findings, so control always reaches
// the sentinel arm in invokeSlot; what the arm decides is whether the reply
// COMMITTED that answer. A reply the provider cut off did not, so scoring it clean
// would be the silent false no-issues-found — StatusOK, zero findings, no marker.
// Only a stop-reason salvage of the exact sentinel scores clean; every other
// subtest pins an unparseable outcome.
func TestInvokeSlot_SalvagedSentinelShapedReply_IsCleanOnlyOnAStopReasonSalvage(t *testing.T) {
	slot := Slot{Primary: Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}}}

	t.Run("truncated salvage: not a committed clean review", func(t *testing.T) {
		// No WithTruncationFailover, so the truncation gate does not demote it first
		// and the sentinel arm is what decides.
		e := NewEngine(salvageReasonCompleter{comp: llmclient.Completion{
			Content:  "NO FINDINGS",
			Salvaged: true, SalvagedTruncated: true, Truncated: true,
		}})
		r := e.invokeSlot(context.Background(), slot)

		assert.Equal(t, StatusOK, r.Status)
		require.True(t, r.Salvaged)
		require.False(t, r.SalvagedOnStop)
		assert.True(t, r.UnparseableResponse,
			"a cut-off reply is never a committed clean review, sentinel-shaped or not")
	})

	t.Run("salvage with no recorded reason: fail-closed as before", func(t *testing.T) {
		// The fixture this test pinned before the flag split: Salvaged with neither
		// reason set reads as abandoned.
		e := NewEngine(&metaTruncatingCompleter{
			content:  "NO FINDINGS",
			salvaged: true,
		}, WithTruncationFailover())
		r := e.invokeSlot(context.Background(), slot)

		assert.Equal(t, StatusOK, r.Status)
		require.True(t, r.Salvaged)
		assert.True(t, r.UnparseableResponse,
			"without a stop reason a salvage is never a committed clean review")
	})

	// AMENDED by Epic 35.16.11.2.2.4.5 AC4 (2026-10-08). This test used to assert
	// that EVERY sentinel-shaped salvage scored unparseable. That is now deliberately
	// reversed for a stop-reason salvage, as TD internal/fanout/engine.go:604 reversed
	// it for one that carries findings: the provider finished the reply, so the
	// sentinel is the model's committed answer on the reasoning channel. The claim is
	// replaced by its opposite, stated and tested, not dropped.
	t.Run("stop-reason salvage: a committed clean review", func(t *testing.T) {
		e := NewEngine(salvageReasonCompleter{comp: llmclient.Completion{
			Content:  "NO FINDINGS",
			Salvaged: true, SalvagedOnStop: true,
		}}, WithTruncationFailover())
		r := e.invokeSlot(context.Background(), slot)

		assert.Equal(t, StatusOK, r.Status)
		require.True(t, r.Salvaged, "the disclosure marker still records which channel the answer came on")
		require.True(t, r.SalvagedOnStop)
		assert.False(t, r.UnparseableResponse,
			"a finished NO FINDINGS is the specified clean report, whichever channel carried it")
		assert.False(t, r.ThinkSuppressed)
		// This arm no longer scores it unparseable, and WholePersonaSalvaged counts
		// only abandoned salvages (AC5), so the published outcome is clean.
		assert.Equal(t, "clean", ReviewerOutcome(statusFor(r, findingsResult{}), 0))
	})

	t.Run("stop-reason salvage of prose: still unparseable", func(t *testing.T) {
		// Falling through to the sentinel read is not a pass: IsNoFindings accepts the
		// sentinel alone, so prose on the reasoning channel scores exactly as prose on
		// the content channel does.
		e := NewEngine(salvageReasonCompleter{comp: llmclient.Completion{
			Content:  "I looked at the diff and nothing seems wrong to me.",
			Salvaged: true, SalvagedOnStop: true,
		}}, WithTruncationFailover())
		r := e.invokeSlot(context.Background(), slot)

		assert.Equal(t, StatusOK, r.Status)
		require.True(t, r.SalvagedOnStop)
		assert.True(t, r.UnparseableResponse)
	})
}

// countingMetaCompleter is mapMetaCompleter with a per-model call count, so a test
// can assert a fallback was never invoked rather than only that it did not win.
type countingMetaCompleter struct {
	mu      sync.Mutex
	byModel map[string]llmclient.Completion
	calls   map[string]int
}

func (c *countingMetaCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	comp, err := c.CompleteWithMeta(ctx, inv)
	return comp.Content, err
}

func (c *countingMetaCompleter) CompleteWithMeta(_ context.Context, inv llmclient.Invocation) (llmclient.Completion, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[inv.Model]++
	return c.byModel[inv.Model], nil
}

// failoverLogCapture returns a ctx whose logger writes Warn-and-above text lines,
// time-stripped, to the returned buffer, so a line can be compared byte for byte.
func failoverLogCapture() (context.Context, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelWarn,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
	return log.NewContext(context.Background(), logger), &buf
}

// The truncation-failover line names the salvage when the truncated reply was a
// promoted reasoning draft (Epic 35.16.11.2.2.4.5.1 T5). Without it the operator
// sees "truncated with zero findings" and cannot tell a reviewer that ran out of
// tokens mid-answer from one whose answer channel was empty and whose cut-off
// thinking was promoted in its place.
func TestInvokeSlot_TruncationFailoverLine_NamesTheSalvage(t *testing.T) {
	slot := Slot{
		Primary:   Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "primary"}},
		Fallbacks: []Agent{{Name: "bruce-fb", Invocation: llmclient.Invocation{Model: "fallback"}}},
	}

	t.Run("truncated salvage: walked to the backup, line names the salvage", func(t *testing.T) {
		c := &countingMetaCompleter{byModel: map[string]llmclient.Completion{
			"primary": {
				Content:  "HIGH|a.go:1|draft from abandoned reasoning|f|correctness|5|e",
				Salvaged: true, SalvagedTruncated: true, Truncated: true,
			},
			"fallback": {Content: "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce"},
		}}
		ctx, buf := failoverLogCapture()
		r := NewEngine(c, WithTruncationFailover()).invokeSlot(ctx, slot)

		assert.Equal(t, StatusOK, r.Status, "the backup rescued the slot")
		assert.True(t, r.FallbackUsed)
		assert.Equal(t, 1, c.calls["fallback"])
		assert.Equal(t,
			`level=WARN msg="reviewer response truncated with zero findings; failing over" agent=bruce model=primary `+
				`salvaged=true reason="truncated reply was a promoted reasoning draft"`+"\n",
			buf.String())
	})

	t.Run("unsalvaged truncation: line is unchanged", func(t *testing.T) {
		c := &countingMetaCompleter{byModel: map[string]llmclient.Completion{
			"primary":  {Content: "I was thinking hard but never emitted a finding", Truncated: true},
			"fallback": {Content: "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce"},
		}}
		ctx, buf := failoverLogCapture()
		r := NewEngine(c, WithTruncationFailover()).invokeSlot(ctx, slot)

		assert.True(t, r.FallbackUsed)
		assert.Equal(t,
			`level=WARN msg="reviewer response truncated with zero findings; failing over" agent=bruce model=primary`+"\n",
			buf.String())
	})

	// The decision this slice records: a stop-reason salvage is a FINISHED reply, so
	// prose on the reasoning channel is scored exactly as prose on the content channel
	// is — unparseable, and not retried. Failing it over would spend the backup on
	// every plausible clean review that arrived on that channel.
	t.Run("stop-reason salvage of prose: not failed over", func(t *testing.T) {
		c := &countingMetaCompleter{byModel: map[string]llmclient.Completion{
			"primary": {
				Content:  "I looked at the diff and nothing seems wrong to me.",
				Salvaged: true, SalvagedOnStop: true,
			},
			"fallback": {Content: "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce"},
		}}
		ctx, buf := failoverLogCapture()
		r := NewEngine(c, WithTruncationFailover()).invokeSlot(ctx, slot)

		assert.Equal(t, StatusOK, r.Status)
		assert.True(t, r.UnparseableResponse)
		assert.False(t, r.FallbackUsed)
		assert.Zero(t, c.calls["fallback"], "the backup must never be called")
		assert.Empty(t, buf.String(), "no failover, so no failover line")
	})
}

// --- Epic 89 T5: retry a zero-finding truncation once on the primary ---------

// sequenceMetaCompleter scripts each model's replies in call order (the last
// entry repeats once the script runs out) and records the max_tokens every call
// asked for, so a test can see the primary asked twice and at what caps.
type sequenceMetaCompleter struct {
	mu      sync.Mutex
	byModel map[string][]llmclient.Completion
	// errs scripts an error beside the same call's completion; nil or short
	// means the call succeeds.
	errs      map[string][]error
	calls     map[string]int
	maxTokens map[string][]int
}

func (c *sequenceMetaCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	comp, err := c.CompleteWithMeta(ctx, inv)
	return comp.Content, err
}

func (c *sequenceMetaCompleter) CompleteWithMeta(_ context.Context, inv llmclient.Invocation) (llmclient.Completion, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls == nil {
		c.calls = map[string]int{}
		c.maxTokens = map[string][]int{}
	}
	n := c.calls[inv.Model]
	c.calls[inv.Model]++
	mt := 0
	if inv.MaxTokens != nil {
		mt = *inv.MaxTokens
	}
	c.maxTokens[inv.Model] = append(c.maxTokens[inv.Model], mt)
	var err error
	if errs := c.errs[inv.Model]; n < len(errs) {
		err = errs[n]
	}
	script := c.byModel[inv.Model]
	if len(script) == 0 {
		return llmclient.Completion{}, err
	}
	if n >= len(script) {
		n = len(script) - 1
	}
	return script[n], err
}

// sizedPrimary is a primary sized the way buildSlots sizes one: a resolved
// output cap, the same cap on the wire, and a resolved context window.
func sizedPrimary(maxTokens, window int) Agent {
	mt := maxTokens
	return Agent{
		Name:              "bruce",
		Invocation:        llmclient.Invocation{Model: "primary", MaxTokens: &mt},
		ResolvedMaxTokens: maxTokens,
		ResolvedWindow:    window,
	}
}

const (
	retryRamble  = "I was thinking hard but never emitted a finding"
	retryFinding = "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce"
)

// The first reply truncates with zero findings; the retry on the primary, at
// twice the resolved cap, answers. The backup is never called, and status.json
// records the doubled cap the serving attempt actually ran under.
func TestInvokeSlot_TruncatedZeroFindings_RetriesThePrimaryAtTwiceTheCap(t *testing.T) {
	c := &sequenceMetaCompleter{byModel: map[string][]llmclient.Completion{
		"primary":  {{Content: retryRamble, Truncated: true}, {Content: retryFinding}},
		"fallback": {{Content: retryFinding}},
	}}
	slot := Slot{
		Primary:   sizedPrimary(8192, 131072),
		Fallbacks: []Agent{{Name: "bruce-fb", Invocation: llmclient.Invocation{Model: "fallback"}}},
	}
	ctx, buf := failoverLogCapture()
	r := NewEngine(c, WithTruncationFailover()).invokeSlot(ctx, slot)

	require.Equal(t, StatusOK, r.Status, "the retry rescued the slot on the primary")
	assert.NoError(t, r.Err)
	assert.False(t, r.FallbackUsed, "a retry that answers must not walk the chain")
	assert.Zero(t, c.calls["fallback"], "the backup must never be called")
	assert.Equal(t, []int{8192, 16384}, c.maxTokens["primary"], "retried once, at twice ResolvedMaxTokens")
	assert.Equal(t, 1, r.ParsedFindingCount())
	assert.Equal(t, 16384, r.ResolvedMaxTokens)
	assert.Equal(t, 8192, *slot.Primary.Invocation.MaxTokens,
		"the retry must not mutate the slot's primary: a requeue starts again at the resolved cap")

	st := statusFor(r, findingsFor(r, nil))
	assert.Equal(t, 16384, st.ResolvedMaxTokens)
	raw, err := json.Marshal(st)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"resolved_max_tokens":16384`, "status.json shows the doubled cap")

	assert.Equal(t,
		`level=WARN msg="reviewer response truncated with zero findings; retrying on the primary at a larger output cap" `+
			`agent=bruce model=primary max_tokens=8192 retry_max_tokens=16384`+"\n",
		buf.String())
}

// Both the first reply and the retry truncate with zero findings: the second
// truncation falls through to today's failover unchanged, so the chain is walked
// and the backup serves the slot. The retry happens once, never twice.
func TestInvokeSlot_TruncatedZeroFindings_RetryAlsoTruncates_WalksTheChain(t *testing.T) {
	c := &sequenceMetaCompleter{byModel: map[string][]llmclient.Completion{
		"primary":  {{Content: retryRamble, Truncated: true}},
		"fallback": {{Content: retryFinding}},
	}}
	slot := Slot{
		Primary:   sizedPrimary(8192, 131072),
		Fallbacks: []Agent{{Name: "bruce-fb", Invocation: llmclient.Invocation{Model: "fallback"}}},
	}
	ctx, buf := failoverLogCapture()
	r := NewEngine(c, WithTruncationFailover()).invokeSlot(ctx, slot)

	require.Equal(t, StatusOK, r.Status, "the backup rescued the slot")
	assert.True(t, r.FallbackUsed, "the second truncation must walk the chain")
	assert.Equal(t, []int{8192, 16384}, c.maxTokens["primary"], "exactly one retry on the primary")
	assert.Equal(t, 1, c.calls["fallback"])
	assert.Equal(t,
		`level=WARN msg="reviewer response truncated with zero findings; retrying on the primary at a larger output cap" `+
			`agent=bruce model=primary max_tokens=8192 retry_max_tokens=16384`+"\n"+
			`level=WARN msg="reviewer response truncated with zero findings; failing over" agent=bruce model=primary`+"\n",
		buf.String(), "the failover line after the retry is today's line, unchanged")
}

// Every member truncates: the slot fails with errTruncatedZeroFindings exactly as
// before, and the failed slot records the doubled cap its primary last ran at.
func TestInvokeSlot_TruncatedZeroFindings_AllTruncate_FailsWithTheDoubledCap(t *testing.T) {
	c := &sequenceMetaCompleter{byModel: map[string][]llmclient.Completion{
		"primary":  {{Content: retryRamble, Truncated: true}},
		"fallback": {{Content: "ramble two", Truncated: true}},
	}}
	slot := Slot{
		Primary:   sizedPrimary(8192, 131072),
		Fallbacks: []Agent{{Name: "bruce-fb", Invocation: llmclient.Invocation{Model: "fallback"}}},
	}
	r := NewEngine(c, WithTruncationFailover()).invokeSlot(context.Background(), slot)

	assert.Equal(t, StatusFailed, r.Status)
	assert.ErrorIs(t, r.Err, errTruncatedZeroFindings)
	assert.Equal(t, 2, c.calls["primary"])
	assert.Equal(t, 1, c.calls["fallback"], "a fallback's truncation is not retried")
	assert.Equal(t, 16384, r.ResolvedMaxTokens)
	assert.Equal(t, 16384, statusFor(r, findingsFor(r, nil)).ResolvedMaxTokens)
}

// A salvaged truncation (the answer channel was empty and the cut-off reasoning
// was promoted) is retried too: it is the same reasoning model running out of
// output tokens.
func TestInvokeSlot_SalvagedTruncation_IsRetriedOnThePrimary(t *testing.T) {
	c := &sequenceMetaCompleter{byModel: map[string][]llmclient.Completion{
		"primary": {
			{Content: "HIGH|a.go:1|draft from abandoned reasoning|f|correctness|5|e", Salvaged: true, SalvagedTruncated: true, Truncated: true},
			{Content: retryFinding},
		},
	}}
	ctx, buf := failoverLogCapture()
	r := NewEngine(c, WithTruncationFailover()).invokeSlot(ctx, Slot{Primary: sizedPrimary(8192, 131072)})

	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, []int{8192, 16384}, c.maxTokens["primary"])
	assert.False(t, r.Salvaged, "the served reply is the retry's, not the salvage")
	assert.Equal(t,
		`level=WARN msg="reviewer response truncated with zero findings; retrying on the primary at a larger output cap" `+
			`agent=bruce model=primary max_tokens=8192 retry_max_tokens=16384 `+
			`salvaged=true reason="truncated reply was a promoted reasoning draft"`+"\n",
		buf.String())
}

// The doubled cap is clamped: by registry.MaxTokensCap, and by the context
// window less the prompt tokens the first single-shot call reported (input and
// output share the window). When the clamp leaves no room above the current cap
// there is nothing larger to ask for, so the slot fails over at once.
func TestInvokeSlot_TruncationRetry_CapIsClamped(t *testing.T) {
	cases := []struct {
		name      string
		maxTokens int
		window    int
		tokensIn  int
		want      []int
	}{
		{"window caps the doubled cap", 8192, 12000, 0, []int{8192, 12000}},
		{"window less the reported prompt tokens", 8192, 32768, 20000, []int{8192, 12768}},
		{"MaxTokensCap caps the doubled cap", 600000, 2000000, 0, []int{600000, registry.MaxTokensCap}},
		{"no room above the cap: no retry", 8192, 32768, 26000, []int{8192}},
		{"already at MaxTokensCap: no retry", registry.MaxTokensCap, 2000000, 0, []int{registry.MaxTokensCap}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &sequenceMetaCompleter{byModel: map[string][]llmclient.Completion{
				"primary":  {{Content: retryRamble, Truncated: true, Usage: llmclient.UsageData{PromptTokens: tc.tokensIn}}},
				"fallback": {{Content: retryFinding}},
			}}
			slot := Slot{
				Primary:   sizedPrimary(tc.maxTokens, tc.window),
				Fallbacks: []Agent{{Name: "bruce-fb", Invocation: llmclient.Invocation{Model: "fallback"}}},
			}
			r := NewEngine(c, WithTruncationFailover()).invokeSlot(context.Background(), slot)

			assert.Equal(t, tc.want, c.maxTokens["primary"])
			assert.True(t, r.FallbackUsed, "a truncating retry, or no retry, walks the chain")
		})
	}
}

// No retry where there is nothing to double, or no gate to retry in: an unsized
// agent (no resolved cap or window), and an engine without truncation failover.
func TestInvokeSlot_TruncationRetry_NotAttempted(t *testing.T) {
	t.Run("unsized primary", func(t *testing.T) {
		c := &sequenceMetaCompleter{byModel: map[string][]llmclient.Completion{
			"primary":  {{Content: retryRamble, Truncated: true}},
			"fallback": {{Content: retryFinding}},
		}}
		slot := Slot{
			Primary:   Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "primary"}},
			Fallbacks: []Agent{{Name: "bruce-fb", Invocation: llmclient.Invocation{Model: "fallback"}}},
		}
		r := NewEngine(c, WithTruncationFailover()).invokeSlot(context.Background(), slot)
		assert.Equal(t, 1, c.calls["primary"])
		assert.True(t, r.FallbackUsed)
	})
	t.Run("truncation failover off", func(t *testing.T) {
		c := &sequenceMetaCompleter{byModel: map[string][]llmclient.Completion{
			"primary": {{Content: retryRamble, Truncated: true}},
		}}
		r := NewEngine(c).invokeSlot(context.Background(), Slot{Primary: sizedPrimary(8192, 131072)})
		assert.Equal(t, 1, c.calls["primary"])
		assert.Equal(t, StatusOK, r.Status)
	})
	t.Run("truncated reply with findings", func(t *testing.T) {
		c := &sequenceMetaCompleter{byModel: map[string][]llmclient.Completion{
			"primary": {{Content: retryFinding, Truncated: true}},
		}}
		r := NewEngine(c, WithTruncationFailover()).invokeSlot(context.Background(), Slot{Primary: sizedPrimary(8192, 131072)})
		assert.Equal(t, 1, c.calls["primary"], "a truncated reply that parsed findings stays StatusOK and is not retried")
		assert.Equal(t, StatusOK, r.Status)
	})
}

// The retry asks a different max_tokens than the primary's cache key was derived
// from (cacheKeyInputs.MaxTokens), so its reply is never stored under that key.
func TestInvokeSlot_TruncationRetry_IsNotCachedUnderThePrimaryKey(t *testing.T) {
	c := &sequenceMetaCompleter{byModel: map[string][]llmclient.Completion{
		"primary": {{Content: retryRamble, Truncated: true}, {Content: retryFinding}},
	}}
	cache := &memCache{m: map[string]string{}}
	p := sizedPrimary(8192, 131072)
	p.CacheKey = "primary-key"
	r := NewEngine(c, WithTruncationFailover(), WithCache(cache, false)).invokeSlot(context.Background(), Slot{Primary: p})

	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 2, c.calls["primary"])
	assert.Empty(t, cache.m, "the doubled-cap reply must not be cached under the resolved-cap key")
}

// A one-agent slot whose truncated reply and retry are both think-only must not
// claim a fallback chain was exhausted: no chain was walked. The discarded reply
// is recorded by the retry line, not by the think-only walk total.
func TestInvokeSlot_TruncationRetry_ThinkOnlyRetryDoesNotClaimAChainWalk(t *testing.T) {
	c := &sequenceMetaCompleter{byModel: map[string][]llmclient.Completion{
		"primary": {
			{Content: "<think>still going", Truncated: true},
			{Content: "<think>checked every file</think>\n"},
		},
	}}
	ctx, buf := failoverLogCapture()
	r := NewEngine(c, WithTruncationFailover()).invokeSlot(ctx, Slot{Primary: sizedPrimary(8192, 131072)})

	assert.Equal(t, 2, c.calls["primary"])
	assert.True(t, r.ThinkSuppressed)
	assert.Equal(t, 1, r.ThinkOnlyAttempts)
	assert.NotContains(t, buf.String(), "exhausted the fallback chain")
	assert.Contains(t, buf.String(), "the reply was a think-only run")
}

// Only the primary is retried. A fallback sized the way buildSlots sizes one
// (review.go's buildFallbackAgent stamps its own ResolvedMaxTokens and window)
// that truncates with zero findings is called once, at its own cap, and the
// slot then fails exactly as it did before the retry existed.
func TestInvokeSlot_TruncationRetry_SizedFallbackIsNotRetried(t *testing.T) {
	c := &sequenceMetaCompleter{byModel: map[string][]llmclient.Completion{
		"primary":  {{Content: retryRamble, Truncated: true}},
		"fallback": {{Content: "ramble two", Truncated: true}},
	}}
	fbMax := 4096
	slot := Slot{
		Primary: sizedPrimary(8192, 131072),
		Fallbacks: []Agent{{
			Name:              "bruce-fb",
			Invocation:        llmclient.Invocation{Model: "fallback", MaxTokens: &fbMax},
			ResolvedMaxTokens: fbMax,
			ResolvedWindow:    65536,
		}},
	}
	r := NewEngine(c, WithTruncationFailover()).invokeSlot(context.Background(), slot)

	assert.Equal(t, []int{8192, 16384}, c.maxTokens["primary"], "the primary is retried once")
	assert.Equal(t, []int{4096}, c.maxTokens["fallback"], "a sized fallback is called once, at its own cap, and never retried")
	assert.Equal(t, StatusFailed, r.Status)
	assert.ErrorIs(t, r.Err, errTruncatedZeroFindings)
}

// maxTokensChat records the max_tokens each tool-loop turn asked for and
// delegates the reply to scriptedChat.
type maxTokensChat struct {
	*scriptedChat
	seen []int
}

func (m *maxTokensChat) Chat(ctx context.Context, inv llmclient.Invocation, msgs []llmclient.Message, defs []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	mt := 0
	if inv.MaxTokens != nil {
		mt = *inv.MaxTokens
	}
	m.mu.Lock()
	m.seen = append(m.seen, mt)
	m.mu.Unlock()
	return m.scriptedChat.Chat(ctx, inv, msgs, defs)
}

// A tool loop's TokensIn sums every turn, so it is no single call's input and
// must not be taken off the window: the retry cap is min(2×cap, window), not
// window − TokensIn. Here the summed input (120000) would shrink the retry from
// 16384 to 11072 if the single-shot clamp were applied.
func TestInvokeSlot_TruncationRetry_ToolLoopClampIsTheWindowAlone(t *testing.T) {
	sc := &maxTokensChat{scriptedChat: &scriptedChat{turns: []chatTurn{
		{content: retryRamble, truncated: true, usage: llmclient.UsageData{PromptTokens: 120000}},
		{content: retryFinding},
	}}}
	p := sizedPrimary(8192, 131072)
	p.Tools, p.SupportsFC, p.Provider = true, true, "prov"
	r := NewEngine(sc, WithDispatcher(&fakeDispatcher{}), WithTruncationFailover()).invokeSlot(context.Background(), Slot{Primary: p})

	require.Equal(t, StatusOK, r.Status, "the retried loop answered")
	assert.Equal(t, []int{8192, 16384}, sc.seen, "retry at min(2×cap, window), the summed TokensIn not subtracted")
	assert.Equal(t, 16384, r.ResolvedMaxTokens)
}

// A 5xx on the truncation retry is the primary's lane answering 5xx, so the walk
// is marked for the throttle's streak exactly as a 5xx on the first call is,
// whether a fallback then serves the slot or the chain ends there.
func TestInvokeSlot_TruncationRetry_ServerErrorMarksThePrimary(t *testing.T) {
	primary := func() *sequenceMetaCompleter {
		return &sequenceMetaCompleter{
			byModel: map[string][]llmclient.Completion{
				"primary":  {{Content: retryRamble, Truncated: true}, {}},
				"fallback": {{Content: retryFinding}},
			},
			errs: map[string][]error{"primary": {nil, &llmclient.HTTPStatusError{Status: 503}}},
		}
	}
	t.Run("fallback served", func(t *testing.T) {
		c := primary()
		slot := Slot{
			Primary:   sizedPrimary(8192, 131072),
			Fallbacks: []Agent{{Name: "bruce-fb", Invocation: llmclient.Invocation{Model: "fallback"}}},
		}
		r := NewEngine(c, WithTruncationFailover()).invokeSlot(context.Background(), slot)
		require.Equal(t, StatusOK, r.Status)
		require.True(t, r.FallbackUsed)
		assert.Equal(t, 2, c.calls["primary"], "the retry ran")
		assert.True(t, r.primaryServerErr, "the retry's 5xx counts toward the primary's lane")
	})
	t.Run("chain failed", func(t *testing.T) {
		c := primary()
		r := NewEngine(c, WithTruncationFailover()).invokeSlot(context.Background(), Slot{Primary: sizedPrimary(8192, 131072)})
		require.Equal(t, StatusFailed, r.Status)
		assert.Equal(t, 2, c.calls["primary"], "the retry ran")
		assert.True(t, r.primaryServerErr, "the retry's 5xx counts toward the primary's lane")
	})
}

// The reply the truncation retry discards was a real, billed call: its tokens and
// call records stay in the totals the walk returns, beside the retry's own, so
// the run's cost and API-call metrics count both calls. The silent-lane check
// still reads the one attempt's tokens, not the sum.
func TestInvokeSlot_TruncationRetry_DiscardedReplyIsCounted(t *testing.T) {
	discarded := llmclient.Completion{
		Content: retryRamble, Truncated: true,
		Usage:       llmclient.UsageData{PromptTokens: 20000, CompletionTokens: 8192},
		CallRecords: []llmclient.CallRecord{{ReachedWire: true, Duration: time.Second}},
	}
	retryRec := llmclient.CallRecord{ReachedWire: true, Duration: 2 * time.Second}

	t.Run("retry served", func(t *testing.T) {
		c := &sequenceMetaCompleter{byModel: map[string][]llmclient.Completion{
			"primary": {discarded, {
				Content:     retryFinding,
				Usage:       llmclient.UsageData{PromptTokens: 20000, CompletionTokens: 300},
				CallRecords: []llmclient.CallRecord{retryRec},
			}},
		}}
		r := NewEngine(c, WithTruncationFailover()).invokeSlot(context.Background(), Slot{Primary: sizedPrimary(8192, 131072)})
		require.Equal(t, StatusOK, r.Status)
		assert.Equal(t, 40000, r.TokensIn)
		assert.Equal(t, 8492, r.TokensOut)
		assert.Equal(t, []llmclient.CallRecord{discarded.CallRecords[0], retryRec}, r.CallRecords)
	})
	t.Run("retry also truncated and the chain failed", func(t *testing.T) {
		c := &sequenceMetaCompleter{byModel: map[string][]llmclient.Completion{
			"primary": {discarded, {
				Content: retryRamble, Truncated: true,
				Usage:       llmclient.UsageData{PromptTokens: 20000, CompletionTokens: 16384},
				CallRecords: []llmclient.CallRecord{retryRec},
			}},
		}}
		r := NewEngine(c, WithTruncationFailover()).invokeSlot(context.Background(), Slot{Primary: sizedPrimary(8192, 131072)})
		require.Equal(t, StatusFailed, r.Status)
		assert.Equal(t, 40000, r.TokensIn)
		assert.Equal(t, 24576, r.TokensOut)
		assert.Equal(t, []llmclient.CallRecord{discarded.CallRecords[0], retryRec}, r.CallRecords)
	})
	t.Run("silent lane reads the retry's own tokens", func(t *testing.T) {
		p := sizedPrimary(8192, 131072)
		p.Invocation.Thinking = registry.ThinkingOn
		c := &sequenceMetaCompleter{byModel: map[string][]llmclient.Completion{
			"primary": {discarded, {Content: "NO FINDINGS", Usage: llmclient.UsageData{PromptTokens: 20000, CompletionTokens: 10}}},
		}}
		r := NewEngine(c, WithTruncationFailover()).invokeSlot(context.Background(), Slot{Primary: p})
		require.Equal(t, StatusOK, r.Status)
		assert.Equal(t, 1, r.SilentChunks, "the discarded reply's 8192 output tokens must not hide a silent retry")
		assert.Equal(t, 8202, r.TokensOut)
	})
}

// truncationRetryMaxTokens, directly: the doubled cap, each clamp, and each
// reason there is nothing larger to ask for.
func TestTruncationRetryMaxTokens(t *testing.T) {
	agent := func(maxTokens, window int) Agent {
		return Agent{ResolvedMaxTokens: maxTokens, ResolvedWindow: window}
	}
	cases := []struct {
		name   string
		agent  Agent
		reply  Result
		want   int
		wantOK bool
	}{
		{"doubled", agent(8192, 131072), Result{}, 16384, true},
		{"unsized: no cap", agent(0, 131072), Result{}, 0, false},
		{"unsized: no window", agent(8192, 0), Result{}, 0, false},
		{"MaxTokensCap clamps", agent(600000, 2000000), Result{}, registry.MaxTokensCap, true},
		{"window clamps", agent(8192, 12000), Result{}, 12000, true},
		{"TokensIn clamps a single call", agent(8192, 32768), Result{TokensIn: 20000}, 12768, true},
		{"tool loop: the window alone", agent(8192, 131072), Result{Tools: true, TokensIn: 120000}, 16384, true},
		{"degraded tool loop is a single call", agent(8192, 131072), Result{Tools: true, ToolsDegraded: true, TokensIn: 120000}, 11072, true},
		{"clamp equal to the cap", agent(8192, 32768), Result{TokensIn: 32768 - 8192}, 0, false},
		{"clamp one above the cap", agent(8192, 32768), Result{TokensIn: 32768 - 8193}, 8193, true},
		{"clamp below the cap", agent(8192, 32768), Result{TokensIn: 30000}, 0, false},
		{"already at MaxTokensCap", agent(registry.MaxTokensCap, 2000000), Result{}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := truncationRetryMaxTokens(tc.agent, tc.reply)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}
