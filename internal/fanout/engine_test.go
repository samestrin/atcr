package fanout

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
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

func parallelSlots(n int) []Slot {
	var slots []Slot
	for i := 0; i < n; i++ {
		slots = append(slots, agentSlot(fmt.Sprintf("a%d", i)))
	}
	return slots
}

func TestRun_MaxParallelBoundsPeakConcurrency(t *testing.T) {
	f := newFake()
	// Gate the first cap agents so peak deterministically reaches the cap
	// before any agent completes, removing timing dependence on delay.
	models := []string{"a0", "a1"}
	for _, m := range models {
		f.block[m] = make(chan struct{})
	}
	started := make(chan string, 5) // buffer all agents so onStart never blocks
	f.onStart = func(m string) { started <- m }
	e := NewEngine(f, WithMaxParallel(2))

	done := make(chan []Result, 1)
	go func() { done <- e.Run(context.Background(), parallelSlots(5)) }()

	// Wait for both capped agents to enter Complete — peak is deterministically 2.
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case m := <-started:
			seen[m] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d agents started; semaphore did not admit cap agents", len(seen))
		}
	}
	// Release the gates so all agents can complete.
	for _, m := range models {
		close(f.block[m])
	}

	results := <-done
	require.Len(t, results, 5)
	for _, r := range results {
		assert.Equal(t, StatusOK, r.Status)
	}
	assert.Equal(t, int32(2), atomic.LoadInt32(&f.peak),
		"max_parallel=2 must cap a 5-agent roster at 2 concurrent calls")
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
}
