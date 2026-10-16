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

// Epic 35.16.14 fix AC1a. On the 2026-10-15 coned re-run the litellm breaker
// opened when litellm restarted and never closed again, and every bin of the
// four reviewers on it stopped. These tests drive a real circuitbreaker.Breaker
// on the queue's fake clock, so the cooldown, the half-open probe and the
// requeue backoff all run on one timeline.

// trippedBreaker installs a fresh breaker for provider in the process-wide
// registry (the one llmclient and the lease throttle both read), on clock.
// The provider name must be unique to the test: the registry is shared.
func trippedBreaker(t *testing.T, provider string, clock *fakeUnitClock, open bool) *circuitbreaker.Breaker {
	t.Helper()
	b := circuitbreaker.New(provider, circuitbreaker.DefaultThreshold, circuitbreaker.DefaultCooldown)
	b.SetClock(clock.Now)
	if open {
		for range circuitbreaker.DefaultThreshold {
			require.True(t, b.Allow())
			b.RecordFailure()
		}
		require.Equal(t, circuitbreaker.StateOpen, b.State())
	}
	circuitbreaker.DefaultRegistry.Set(provider, b)
	t.Cleanup(func() { circuitbreaker.DefaultRegistry.Set(provider, circuitbreaker.New(provider, 0, 0)) })
	return b
}

// breakerProvider answers through each call's provider breaker exactly as
// llmclient.send does: an open circuit fails fast with a CircuitOpenError and
// no call; otherwise the call is made and its outcome recorded. The provider is
// down (503) until down has passed on clock, and each real call takes took.
type breakerProvider struct {
	clock *fakeUnitClock
	down  time.Duration
	took  map[string]time.Duration
	mu    sync.Mutex
	// calls counts real calls per agent; fast counts fail-fasts per agent.
	calls, fast map[string]int
	// order is every real call's provider, in the order they were made.
	order []string
}

func newBreakerProvider(clock *fakeUnitClock, down time.Duration) *breakerProvider {
	return &breakerProvider{clock: clock, down: down, took: map[string]time.Duration{}, calls: map[string]int{}, fast: map[string]int{}}
}

func (c *breakerProvider) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	p := circuitbreaker.ProviderFromContext(ctx)
	b := circuitbreaker.DefaultRegistry.Get(p)
	if !b.Allow() {
		c.mu.Lock()
		c.fast[inv.Model]++
		c.mu.Unlock()
		return "", &llmclient.CircuitOpenError{Provider: p}
	}
	c.mu.Lock()
	c.calls[inv.Model]++
	c.order = append(c.order, p)
	took := c.took[p]
	c.mu.Unlock()
	if c.clock.elapsed() < c.down {
		b.RecordFailure()
		return "", exhausted503()
	}
	if took > 0 {
		c.clock.After(took)
	}
	b.RecordSuccess()
	return "NO FINDINGS", nil
}

// The board's failure: a provider outage that outlasts one unit's retry budget
// (a restart takes minutes; four leases spaced 20s, 40s and 80s apart last
// about two). Every lease that met the open breaker failed fast and spent one
// of the unit's four attempts, so every unit on the provider was marked failed
// before the provider came back. With nothing left to call it, nothing asked
// the breaker to move to half-open, so it stayed open for the rest of the run.
//
// A fail-fast made no call and says nothing about the unit, so it must not
// spend the unit's attempts: the unit waits for the breaker's cooldown instead,
// the half-open probe finds the provider healthy, the breaker closes, and every
// bin is reviewed on its primary.
func TestBreakerRecovery_OutageLongerThanTheUnitBudgetLosesNoUnit(t *testing.T) {
	clock := newFakeUnitClock()
	provider := "litellm-" + t.Name()
	b := trippedBreaker(t, provider, clock, false)
	const outage = 30 * time.Minute
	c := newBreakerProvider(clock, outage)
	var slots []Slot
	for n := range 8 {
		for _, a := range []string{"archer", "brad", "otto", "ronin"} {
			s := agentSlot(fmt.Sprintf("%s%d", a, n))
			s.Primary.Provider = provider
			slots = append(slots, s)
		}
	}

	// One worker: the fake clock moves only when the worker waits, so the run
	// is one deterministic timeline.
	results := NewEngine(c, WithMaxParallel(1), retryEveryUnit(), withUnitClock(clock)).Run(context.Background(), slots)

	for i, r := range results {
		require.Equalf(t, StatusOK, r.Status, "unit %d (%s): %v", i, r.Agent, r.Err)
		assert.LessOrEqualf(t, c.calls[r.Agent], unitMaxAttempts, "unit %d made no more real calls than its attempt budget", i)
	}
	assert.Equal(t, circuitbreaker.StateClosed, b.State(), "the probe found the provider healthy and closed the breaker")
	assert.GreaterOrEqual(t, clock.elapsed(), outage)
}

// A unit whose leases keep failing fast is still bounded: it waits out the
// breaker for at most unitCircuitWaitBudget, then is marked failed with one
// warn line, so a provider that never recovers cannot requeue a unit forever.
func TestBreakerRecovery_CircuitWaitIsBounded(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&lockedWriter{w: &buf}, nil))
	fake := newFake()
	fake.failFor["a"] = &llmclient.CircuitOpenError{Provider: "p"}
	clock := newFakeUnitClock()

	results := NewEngine(fake, retryEveryUnit(), withUnitClock(clock), WithLogger(logger)).
		Run(context.Background(), []Slot{agentSlot("a")})

	require.Len(t, results, 1)
	waits := int(unitCircuitWaitBudget / circuitOpenBackoff)
	assert.Equal(t, StatusFailed, results[0].Status)
	assert.Equal(t, waits, results[0].Requeues, "one requeue per cooldown inside the budget")
	assert.Equal(t, waits+1, fake.calls["a"])
	assert.Equal(t, unitCircuitWaitBudget, clock.elapsed())
	assert.Equal(t, 1, strings.Count(buf.String(), "unit exhausted its retry budget"))
	assert.Contains(t, buf.String(), "class=circuit_open")
}

// A unit that met an open breaker and then a real failure has spent one real
// attempt, not two: the fail-fast is not counted against unitMaxAttempts.
func TestBreakerRecovery_FailFastDoesNotSpendAnAttempt(t *testing.T) {
	t.Parallel()
	clock := newFakeUnitClock()
	c := &scripted{errs: []error{
		&llmclient.CircuitOpenError{Provider: "p"},
		&llmclient.CircuitOpenError{Provider: "p"},
		exhausted503(), exhausted503(), exhausted503(), exhausted503(),
	}}
	results := NewEngine(c, retryEveryUnit(), withUnitClock(clock)).Run(context.Background(), []Slot{agentSlot("a")})
	require.Len(t, results, 1)
	assert.Equal(t, StatusFailed, results[0].Status)
	assert.Equal(t, 6, c.n, "two fail-fasts plus the four real attempts the budget allows")
	assert.Equal(t, 6, results[0].UnitAttempts, "every lease is still reported")
}

// scripted answers its calls with errs in order, then reviews.
type scripted struct {
	mu   sync.Mutex
	errs []error
	n    int
}

func (s *scripted) Complete(context.Context, llmclient.Invocation) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	if s.n <= len(s.errs) {
		return "", s.errs[s.n-1]
	}
	return "NO FINDINGS", nil
}

// Once the cooldown has passed, the breaker waits for a probe. A unit of that
// provider must be leased then, not after every healthy provider's backlog has
// drained: the breaker moves only when something calls it, so a deferred probe
// leaves it open for as long as the healthy providers have work.
func TestThrottle_HalfOpenProbeLeasesAheadOfTheHealthyBacklog(t *testing.T) {
	t.Parallel()
	breakers := &breakerStates{}
	q := throttledQueue([]string{"beta", "beta", "alpha", "alpha", "beta"}, 4, newFakeUnitClock(), breakers)
	breakers.set("alpha", circuitbreaker.StateHalfOpen)

	i, ok := tryLease(q)
	require.True(t, ok)
	assert.Equal(t, 2, i, "the probe goes first")
	var leased []int
	for {
		i, ok := tryLease(q)
		if !ok {
			break
		}
		leased = append(leased, i)
	}
	assert.Equal(t, []int{0, 1, 4}, leased, "one probe at a time; the healthy units follow")
}

// End to end: alpha's breaker is open when the run starts and alpha is healthy
// again; beta has a long backlog of slow calls. Alpha's probe runs as soon as
// the cooldown has passed, closes the breaker, and alpha drains alongside beta,
// rather than waiting behind beta's whole backlog.
func TestBreakerRecovery_ProbeIsNotStarvedByAHealthyBacklog(t *testing.T) {
	clock := newFakeUnitClock()
	alpha, beta := "alpha-"+t.Name(), "beta-"+t.Name()
	b := trippedBreaker(t, alpha, clock, true)
	trippedBreaker(t, beta, clock, false)
	c := newBreakerProvider(clock, 0)
	c.took[beta] = 5 * time.Minute
	var slots []Slot
	for n := range 12 {
		s := agentSlot(fmt.Sprintf("b%d", n))
		s.Primary.Provider = beta
		slots = append(slots, s)
	}
	for n := range 4 {
		s := agentSlot(fmt.Sprintf("a%d", n))
		s.Primary.Provider = alpha
		slots = append(slots, s)
	}

	results := NewEngine(c, WithMaxParallel(2), retryEveryUnit(), withUnitClock(clock)).Run(context.Background(), slots)

	for i, r := range results {
		require.Equalf(t, StatusOK, r.Status, "unit %d: %v", i, r.Err)
	}
	assert.Equal(t, circuitbreaker.StateClosed, b.State())
	first := -1
	for k, p := range c.order {
		if p == alpha {
			first = k
			break
		}
	}
	require.GreaterOrEqual(t, first, 0)
	assert.Lessf(t, first, 4, "alpha's probe ran within the first few calls, not after beta's twelve (order %v)", c.order)
}

// Only a lease on which no chain member made a call waits for free. A primary
// that made a real call and failed spent an attempt, even when its fallback then
// failed fast and the walk ended on the open breaker: the unit keeps the attempt
// budget's bound, not the thirty leases of the circuit-wait budget.
func TestBreakerRecovery_RealCallThenFailFastSpendsAnAttempt(t *testing.T) {
	t.Parallel()
	fake := newFake()
	fake.failFor["primary"] = exhausted503()
	fake.failFor["fb"] = &llmclient.CircuitOpenError{Provider: "p"}
	slot := Slot{
		Primary:   Agent{Name: "primary", Provider: "q", Invocation: llmclient.Invocation{Model: "primary"}},
		Fallbacks: []Agent{{Name: "fb", Provider: "p", Invocation: llmclient.Invocation{Model: "fb"}}},
	}

	results := NewEngine(fake, retryEveryUnit(), withUnitClock(newFakeUnitClock())).Run(context.Background(), []Slot{slot})

	require.Len(t, results, 1)
	assert.Equal(t, StatusFailed, results[0].Status)
	assert.Equal(t, unitMaxAttempts, fake.calls["primary"])
	assert.Equal(t, unitMaxAttempts, results[0].UnitAttempts)
}
