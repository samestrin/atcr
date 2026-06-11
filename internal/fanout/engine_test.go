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
	started := make(chan string, len(models))
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
