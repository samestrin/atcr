package fanout

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/circuitbreaker"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// breakerStates is a settable breaker read for the throttle; providers not in
// the map are closed.
type breakerStates struct {
	mu sync.Mutex
	m  map[string]circuitbreaker.State
}

func (b *breakerStates) set(p string, s circuitbreaker.State) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		b.m = map[string]circuitbreaker.State{}
	}
	b.m[p] = s
}

func (b *breakerStates) state(p string) circuitbreaker.State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.m[p]
}

// throttledQueue is a queue over units whose providers are named by providers,
// throttled at ceiling.
func throttledQueue(providers []string, ceiling int, clock unitClock, breakers *breakerStates) *unitQueue {
	idx := make([]int, len(providers))
	for i := range idx {
		idx[i] = i
	}
	q := newUnitQueue(idx, clock)
	q.throttle = newProviderThrottle(ceiling, clock, slog.New(slog.DiscardHandler),
		func(i int) leaseKey { return leaseKey{provider: providers[i]} }, breakers.state)
	return q
}

// onLane puts every unit on one provider and model.
func onLane(provider, model string) func(int) leaseKey {
	return func(int) leaseKey { return leaseKey{provider: provider, model: model} }
}

// tryLease leases without waiting: ok is false when nothing is leasable now.
func tryLease(q *unitQueue) (int, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.promote(false)
	i, ok := q.take(false)
	if ok {
		q.held++
	}
	return i, ok
}

func rateLimited() Result { return Result{Status: StatusFailed, Err: exhausted429()} }

// A 429 halves the provider's cap, never below one lease.
func TestThrottle_RateLimitHalvesTheCap(t *testing.T) {
	t.Parallel()
	th := newProviderThrottle(6, newFakeUnitClock(), slog.New(slog.DiscardHandler),
		onLane("alpha", ""), (&breakerStates{}).state)
	for _, want := range []int{3, 1, 1} {
		th.acquire(0)
		th.done(0, rateLimited())
		got, throttled := th.capFor(leaseKey{provider: "alpha"})
		assert.Equal(t, want, got)
		assert.True(t, throttled)
	}
	got, throttled := th.capFor(leaseKey{provider: "beta"})
	assert.Equal(t, 6, got, "another provider keeps the ceiling")
	assert.False(t, throttled)
}

// A rate-limited provider's units wait behind a healthy provider's, and it gets
// no more leases than its cap while the healthy one leases at the ceiling.
func TestThrottle_DefersAndCapsTheRateLimitedProvider(t *testing.T) {
	t.Parallel()
	providers := []string{"alpha", "alpha", "beta", "alpha", "beta", "beta", "alpha", "beta"}
	q := throttledQueue(providers, 4, newFakeUnitClock(), &breakerStates{})

	i, ok := tryLease(q)
	require.True(t, ok)
	require.Equal(t, 0, i, "nothing is throttled yet: leases in order")
	q.requeue(i, time.Hour, rateLimited()) // alpha: cap 4 -> 2

	i, _ = tryLease(q)
	assert.Equal(t, 2, i, "the healthy provider's unit goes first")
	q.release(i, Result{Status: StatusOK})
	var leased []int
	for {
		i, ok := tryLease(q)
		if !ok {
			break
		}
		leased = append(leased, i)
	}
	assert.Equal(t, []int{4, 5, 7, 1, 3}, leased, "every beta unit, then alpha up to its cap of two")
}

// A throttled provider with nothing in flight always gets a lease, so the queue
// never wedges on it.
func TestThrottle_ProviderWithNothingInFlightIsAdmitted(t *testing.T) {
	t.Parallel()
	q := throttledQueue([]string{"alpha", "alpha"}, 4, newFakeUnitClock(), &breakerStates{})
	for range 3 {
		i, _ := tryLease(q)
		q.requeue(i, time.Hour, rateLimited())
		q.mu.Lock()
		q.pending = append(q.pending, i) // as if the backoff had passed
		q.delayed = nil
		q.mu.Unlock()
	}
	_, ok := tryLease(q)
	assert.True(t, ok, "cap 1, nothing in flight")
	_, ok = tryLease(q)
	assert.False(t, ok, "cap 1, one in flight")
}

// Once the window has passed, each success on the primary raises the cap by one
// until the provider is back at the ceiling. A success inside the window does
// not.
func TestThrottle_RecoversAsTheWindowPasses(t *testing.T) {
	t.Parallel()
	clock := newFakeUnitClock()
	var buf bytes.Buffer
	th := newProviderThrottle(4, clock, slog.New(slog.NewTextHandler(&buf, nil)),
		onLane("alpha", ""), (&breakerStates{}).state)
	ok := Result{Status: StatusOK}
	th.acquire(0)
	th.done(0, rateLimited())
	th.acquire(0)
	th.done(0, rateLimited())
	got, _ := th.capFor(leaseKey{provider: "alpha"})
	require.Equal(t, 1, got)

	th.acquire(0)
	th.done(0, ok)
	got, _ = th.capFor(leaseKey{provider: "alpha"})
	assert.Equal(t, 1, got, "a success inside the window does not raise the cap")

	clock.After(throttleWindow)
	for _, want := range []int{2, 3} {
		th.acquire(0)
		th.done(0, ok)
		got, throttled := th.capFor(leaseKey{provider: "alpha"})
		assert.Equal(t, want, got)
		assert.True(t, throttled)
	}
	th.acquire(0)
	th.done(0, ok)
	got, throttled := th.capFor(leaseKey{provider: "alpha"})
	assert.Equal(t, 4, got)
	assert.False(t, throttled, "back at the ceiling")
	assert.Equal(t, 2, strings.Count(buf.String(), "provider rate-limited; lowering its lease cap"))
	assert.Equal(t, 1, strings.Count(buf.String(), "provider recovered; lease cap back at the ceiling"))
	assert.Equal(t, 0, th.lanes[leaseKey{provider: "alpha"}].inFlight)
}

// An open or half-open breaker holds its provider to one lease, deferred behind
// healthy providers, and closing it restores the ceiling. The breaker is only
// read: the throttle's own cap is untouched.
func TestThrottle_OpenBreakerHoldsProviderToOneLease(t *testing.T) {
	t.Parallel()
	breakers := &breakerStates{}
	providers := []string{"alpha", "alpha", "beta", "alpha"}
	q := throttledQueue(providers, 4, newFakeUnitClock(), breakers)

	breakers.set("alpha", circuitbreaker.StateOpen)
	var leased []int
	for {
		i, ok := tryLease(q)
		if !ok {
			break
		}
		leased = append(leased, i)
	}
	assert.Equal(t, []int{2, 0}, leased, "beta first, then one alpha probe")

	breakers.set("alpha", circuitbreaker.StateHalfOpen)
	_, ok := tryLease(q)
	assert.False(t, ok, "half-open still admits one")

	breakers.set("alpha", circuitbreaker.StateClosed)
	i, ok := tryLease(q)
	assert.True(t, ok)
	assert.Equal(t, 1, i, "closed: back to the ceiling")
	assert.Zero(t, q.throttle.lanes[leaseKey{provider: "alpha"}].limit)
}

// laneQueue is a queue over units whose lanes are named by keys, throttled at
// ceiling.
func laneQueue(keys []leaseKey, ceiling int, breakers *breakerStates) *unitQueue {
	idx := make([]int, len(keys))
	for i := range idx {
		idx[i] = i
	}
	q := newUnitQueue(idx, newFakeUnitClock())
	q.throttle = newProviderThrottle(ceiling, q.clock, slog.New(slog.DiscardHandler),
		func(i int) leaseKey { return keys[i] }, breakers.state)
	return q
}

// Epic 35.16.14 fix AC1c: every reviewer behind one litellm shares a provider,
// so a 429 from one model must lower only that model's cap. Two 429s from a
// one-slot local model held the whole panel to one lease.
func TestThrottle_RateLimitLowersOnlyThatModelsCap(t *testing.T) {
	t.Parallel()
	slow := leaseKey{provider: "litellm", model: "gpt-oss-20b"}
	fast := leaseKey{provider: "litellm", model: "qwen3-coder"}
	keys := []leaseKey{slow, slow, slow, fast, fast, fast, fast, fast, fast}
	q := laneQueue(keys, 6, &breakerStates{})

	for range 2 {
		q.throttle.acquire(0)
		q.throttle.done(0, rateLimited()) // slow: cap 6 -> 3 -> 1
	}
	got, throttled := q.throttle.capFor(slow)
	assert.Equal(t, 1, got, "the rate-limited model is held to one lease")
	assert.True(t, throttled)
	got, throttled = q.throttle.capFor(fast)
	assert.Equal(t, 6, got, "another model behind the same provider keeps the ceiling")
	assert.False(t, throttled)

	var leased []int
	for {
		i, ok := tryLease(q)
		if !ok {
			break
		}
		leased = append(leased, i)
	}
	assert.Equal(t, []int{3, 4, 5, 6, 7, 8, 0}, leased,
		"every fast unit at full width, then the slow model's one lease")
}

// A rate-limited model's own cap still recovers as the window passes, one
// success at a time, without touching its neighbour's.
func TestThrottle_RateLimitedModelRecoversOnItsOwnLane(t *testing.T) {
	t.Parallel()
	clock := newFakeUnitClock()
	slow := leaseKey{provider: "litellm", model: "gpt-oss-20b"}
	fast := leaseKey{provider: "litellm", model: "qwen3-coder"}
	keys := []leaseKey{slow, fast}
	var buf bytes.Buffer
	th := newProviderThrottle(4, clock, slog.New(slog.NewTextHandler(&buf, nil)),
		func(i int) leaseKey { return keys[i] }, (&breakerStates{}).state)
	th.acquire(0)
	th.done(0, rateLimited())
	got, _ := th.capFor(slow)
	require.Equal(t, 2, got)

	clock.After(throttleWindow)
	for _, want := range []int{3, 4} {
		before := got
		th.acquire(1)
		th.done(1, Result{Status: StatusOK})
		got, _ = th.capFor(slow)
		assert.Equal(t, before, got, "a success on another model does not raise this one's cap")
		th.acquire(0)
		th.done(0, Result{Status: StatusOK})
		got, _ = th.capFor(slow)
		assert.Equal(t, want, got)
	}
	_, throttled := th.capFor(slow)
	assert.False(t, throttled, "back at the ceiling")
	out := buf.String()
	assert.Contains(t, out, "provider=litellm model=gpt-oss-20b lease_cap=2")
	assert.Contains(t, out, `msg="provider recovered; lease cap back at the ceiling" provider=litellm model=gpt-oss-20b`)
	assert.NotContains(t, out, "qwen3-coder")
}

// An outage is provider-wide: an open breaker holds every model of that
// provider to one lease between them, and a half-open breaker's probe goes to
// whichever model's unit is first.
func TestThrottle_OpenBreakerHoldsEveryModelOfTheProvider(t *testing.T) {
	t.Parallel()
	breakers := &breakerStates{}
	a := leaseKey{provider: "litellm", model: "gpt-oss-20b"}
	b := leaseKey{provider: "litellm", model: "qwen3-coder"}
	other := leaseKey{provider: "openai", model: "gpt-5"}
	keys := []leaseKey{a, b, other, a, b}
	q := laneQueue(keys, 4, breakers)

	breakers.set("litellm", circuitbreaker.StateOpen)
	var leased []int
	for {
		i, ok := tryLease(q)
		if !ok {
			break
		}
		leased = append(leased, i)
	}
	assert.Equal(t, []int{2, 0}, leased, "the other provider first, then one lease for all of litellm")

	q.release(0, Result{Status: StatusFailed})
	breakers.set("litellm", circuitbreaker.StateHalfOpen)
	i, ok := tryLease(q)
	require.True(t, ok)
	assert.Equal(t, 1, i, "the probe goes to the next litellm unit, whatever its model")
	_, ok = tryLease(q)
	assert.False(t, ok, "half-open: one lease across the provider")
}

// Only the primary's own answer feeds its provider: a unit a fallback served,
// or whose whole chain failed, says nothing about the primary.
func TestThrottle_FallbackOutcomeIsNotCounted(t *testing.T) {
	t.Parallel()
	th := newProviderThrottle(4, newFakeUnitClock(), slog.New(slog.DiscardHandler),
		onLane("alpha", ""), (&breakerStates{}).state)
	r := rateLimited()
	r.FallbackUsed = true
	th.acquire(0)
	th.done(0, r)
	got, throttled := th.capFor(leaseKey{provider: "alpha"})
	assert.Equal(t, 4, got)
	assert.False(t, throttled)
	assert.Equal(t, 0, th.lanes[leaseKey{provider: "alpha"}].inFlight, "the lease is still returned")
}

// Only a 429 lowers the cap: a 5xx is the breaker's to judge, and a final
// failure says nothing about load.
func TestThrottle_OnlyRateLimitLowersTheCap(t *testing.T) {
	t.Parallel()
	th := newProviderThrottle(4, newFakeUnitClock(), slog.New(slog.DiscardHandler),
		onLane("alpha", ""), (&breakerStates{}).state)
	for _, err := range []error{exhausted503(), &llmclient.HTTPStatusError{Status: 400}, fmt.Errorf("call: %w", context.DeadlineExceeded)} {
		th.acquire(0)
		th.done(0, Result{Status: StatusFailed, Err: err})
	}
	got, throttled := th.capFor(leaseKey{provider: "alpha"})
	assert.Equal(t, 4, got)
	assert.False(t, throttled)
}

// A cancelled run leases past every cap: its units resolve without a call.
func TestThrottle_CancelledRunLeasesPastTheCap(t *testing.T) {
	t.Parallel()
	q := throttledQueue([]string{"alpha", "alpha", "alpha"}, 4, newFakeUnitClock(), &breakerStates{})
	q.throttle.lane(leaseKey{provider: "alpha"}).limit = 1
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 3 {
		_, ok := q.lease(ctx)
		require.True(t, ok)
	}
}

// providerSplit is a completer for two providers, keyed on the provider the
// engine threads onto the call context: alpha answers 429 (as llmclient reports
// an exhausted per-call budget) until window has passed on clock, beta always
// reviews.
type providerSplit struct {
	clock  *fakeUnitClock
	window time.Duration
	mu     sync.Mutex
	calls  map[string]int
	limits int
}

func (c *providerSplit) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	p := circuitbreaker.ProviderFromContext(ctx)
	elapsed := c.clock.elapsed()
	c.mu.Lock()
	c.calls[p]++
	limited := p == "alpha" && elapsed < c.window
	if limited {
		c.limits++
	}
	c.mu.Unlock()
	if limited {
		return "", exhausted429()
	}
	return "", nil
}

// T4 success criterion (Epic 35.16.14): with one of two providers rate-limiting,
// the healthy provider's units drain at full rate — never throttled, never
// requeued, one call each — while the rate-limited provider's are throttled to
// one lease and requeued, and the run completes on its primaries without the
// fallback chain. (The fake clock jumps whenever any worker waits, so when a
// healthy call starts is not a stable measure; the lease order is pinned by
// TestThrottle_DefersAndCapsTheRateLimitedProvider.)
func TestThrottle_TwoProvidersOneRateLimited(t *testing.T) {
	t.Parallel()
	clock := newFakeUnitClock()
	c := &providerSplit{clock: clock, window: 90 * time.Second, calls: map[string]int{}}
	var slots []Slot
	for n := range 8 {
		for _, p := range []string{"alpha", "beta"} {
			s := agentSlot(fmt.Sprintf("%s%d", p, n))
			s.Primary.Provider = p
			// Every unit has a backup on the healthy provider: the run must not need it.
			s.Fallbacks = []Agent{{Name: "backup", Provider: "beta", Invocation: llmclient.Invocation{Model: "backup"}}}
			slots = append(slots, s)
		}
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&lockedWriter{w: &buf}, nil))
	e := NewEngine(c, WithMaxParallel(4), retryEveryUnit(), withUnitClock(clock), WithLogger(logger))
	e.breakerState = (&breakerStates{}).state

	results := e.Run(context.Background(), slots)

	alphaRequeues := 0
	for i, r := range results {
		require.Equalf(t, StatusOK, r.Status, "unit %d", i)
		assert.Equalf(t, slots[i].Primary.Name, r.Agent, "results[%d] stays aligned", i)
		assert.Falsef(t, r.FallbackUsed, "unit %d served by its primary", i)
		if slots[i].Primary.Provider == "beta" {
			assert.Zerof(t, r.Requeues, "healthy unit %d is never requeued", i)
		} else {
			alphaRequeues += r.Requeues
		}
	}
	assert.Equal(t, 8, c.calls["beta"], "one call per healthy unit; the backup was never called")
	assert.Equal(t, c.limits, alphaRequeues, "every 429 was requeued")
	assert.Positive(t, alphaRequeues)
	assert.GreaterOrEqual(t, clock.elapsed(), 90*time.Second)

	out := buf.String()
	assert.Contains(t, out, `msg="provider rate-limited; lowering its lease cap" provider=alpha`)
	assert.Contains(t, out, "lease_cap=1", "the rate-limited provider was held to one lease")
	assert.NotContains(t, out, "provider=beta", "the healthy provider was never throttled")
}

// Without the outer retry (a bulk run) the pool has no throttle: it leases in
// order exactly as before.
func TestThrottle_OffWithoutUnitRetry(t *testing.T) {
	t.Parallel()
	fake := newFake()
	var slots []Slot
	for n := range 4 {
		s := agentSlot(fmt.Sprintf("a%d", n))
		s.Primary.Provider = "alpha"
		fake.failFor[s.Primary.Name] = exhausted429()
		slots = append(slots, s)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&lockedWriter{w: &buf}, nil))
	results := NewEngine(fake, WithMaxParallel(4), WithLogger(logger)).Run(context.Background(), slots)
	for _, r := range results {
		assert.Equal(t, StatusFailed, r.Status)
	}
	assert.NotContains(t, buf.String(), "lease cap")
}
