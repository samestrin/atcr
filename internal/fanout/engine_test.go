package fanout

import (
	"bytes"
	"context"
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

		// Fresh Result per assertion: ParsedFindingCount memoizes on first use.
		assert.Equal(t, 0, (&Result{Content: draft, Salvaged: true}).ParsedFindingCount(),
			"a salvaged reply's reasoning must not be counted as findings")

		fr := findingsFor(Result{Agent: "bruce", Status: StatusOK, Content: draft, Salvaged: true}, nil)
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
		// StatusOK with content nothing could parse: invokeSlot's marker block sets
		// UnparseableResponse, and mergeResultGroup counts it.
		merged := mergeResultGroup([]Result{clean, {
			Agent: "bruce", Status: StatusOK, Content: "chain of thought only",
			Salvaged: true, UnparseableResponse: true,
		}}, nil)

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

// PINNED, not asserted-unchanged (task-06 Test Strategy): the guard necessarily
// MOVES what a salvaged row means downstream, so both shapes are recorded here.
//
// Salvage does NOT imply truncation — llmclient.CompleteWithMeta salvages inside
// its content == "" branch on ANY finish reason, so a stop-reason reply with empty
// content and reasoning present is salvaged with ResponseTruncated false. The two
// shapes therefore land in different places and must not be collapsed.
func TestInvokeSlot_SalvagedReply_ContributesNoFindings(t *testing.T) {
	t.Run("salvaged, not truncated: recorded unparseable", func(t *testing.T) {
		// StatusOK survives (only findings are refused, not the call), the count is
		// zero, and the reasoning is not the clean-review sentinel — so the row reads
		// unparseable, which ReviewerOutcome ranks above clean. Intended: "reviewed
		// and found nothing" and "emitted reasoning no parser should trust" score the
		// same and this marker is the only thing that tells them apart.
		e := NewEngine(&metaTruncatingCompleter{
			content:  "HIGH|a.go:1|draft from abandoned reasoning|f|correctness|5|e",
			salvaged: true,
		}, WithTruncationFailover())
		r := e.invokeSlot(context.Background(), Slot{Primary: Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}}})

		assert.Equal(t, StatusOK, r.Status)
		require.True(t, r.Salvaged)
		assert.Equal(t, 0, r.ParsedFindingCount(), "the draft inside salvaged reasoning is refused")
		assert.True(t, r.UnparseableResponse, "salvaged reasoning is not the clean-review sentinel")
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

// TestInvokeSlot_SalvagedSentinelShapedReply_IsNotACleanReview is the complement
// of the non-sentinel subtest in TestInvokeSlot_SalvagedReply_ContributesNoFindings:
// a salvaged reply whose promoted reasoning is literally the clean-review
// sentinel ("NO FINDINGS") must not score as a genuine clean review. A salvaged
// reply has ParsedFindingCount 0 by construction, so control reaches the
// sentinel gate; if the gate read the sentinel-shaped salvage, the slot would be
// recorded StatusOK with zero findings and no marker — the silent false
// no-issues-found. A reply the design declares uncommitted (T6) cannot be a
// committed no-findings report whatever its text.
func TestInvokeSlot_SalvagedSentinelShapedReply_IsNotACleanReview(t *testing.T) {
	e := NewEngine(&metaTruncatingCompleter{
		content:  "NO FINDINGS",
		salvaged: true,
	}, WithTruncationFailover())
	r := e.invokeSlot(context.Background(), Slot{Primary: Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}}})

	assert.Equal(t, StatusOK, r.Status)
	require.True(t, r.Salvaged)
	assert.True(t, r.UnparseableResponse,
		"a salvaged reply is never a committed clean review, sentinel-shaped or not")
}
