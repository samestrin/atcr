package fanout

import (
	"context"
	"errors"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/circuitbreaker"
	"github.com/samestrin/atcr/internal/llmclient"
)

// breakerGatedOutage is a provider behind a real circuit breaker, gated the way
// llmclient.send gates it: an open circuit fails fast with no call, a call made
// before window has passed on clock fails at the transport (connection refused,
// as from a restarting proxy) and counts against the breaker, and a call after
// it reviews the chunk and closes a half-open probe. The breaker runs on the
// same fake clock as the queue, so its cooldown passes only while workers wait.
type breakerGatedOutage struct {
	clock    *fakeUnitClock
	window   time.Duration
	breaker  *circuitbreaker.Breaker
	calls    atomic.Int32
	fastFail atomic.Int32
}

func newBreakerGatedOutage(clock *fakeUnitClock, window time.Duration) *breakerGatedOutage {
	return &breakerGatedOutage{
		clock: clock, window: window,
		breaker: circuitbreaker.NewWithClock("p", circuitbreaker.DefaultThreshold, circuitbreaker.DefaultCooldown, clock.Now),
	}
}

func (c *breakerGatedOutage) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	if !c.breaker.Allow() {
		c.fastFail.Add(1)
		return "", &llmclient.CircuitOpenError{Provider: "p"}
	}
	c.calls.Add(1)
	if c.clock.elapsed() < c.window {
		c.breaker.RecordFailure()
		return "", &url.Error{Op: "Post", URL: "http://litellm/chat/completions", Err: errors.New("connection refused")}
	}
	c.breaker.RecordSuccess()
	return baselineChunkFindingCompleter{}.Complete(ctx, inv)
}

func (c *breakerGatedOutage) breakerFor(string) *circuitbreaker.Breaker { return c.breaker }

func withBreakers(f func(provider string) *circuitbreaker.Breaker) EngineOption {
	return func(e *Engine) { e.breakers = f }
}

// F2 (Epic 35.16.14, AC1): the provider goes down, its breaker opens, and it
// comes back minutes later. Every unit queued behind the open breaker must be
// reviewed once it recovers. Before the fix a fail-fast lease was charged as a
// transport failure, so each unit spent its four attempts (about 140s of
// backoff) on fail-fasts inside one outage and was marked failed; with every
// unit spent nothing was left to make the half-open probe, so the breaker never
// closed again.
func TestUnitRetry_OpenBreakerRecoversWhenTheProviderRecovers(t *testing.T) {
	t.Parallel()
	slots := sixteenBaselineSlots(t)
	for _, s := range slots {
		require.Equal(t, "p", s.Primary.Provider)
	}
	clock := newFakeUnitClock()
	c := newBreakerGatedOutage(clock, 5*time.Minute)

	results, poolDir := ledgerEngine(t, c, slots, true, withUnitClock(clock), withBreakers(c.breakerFor))

	for i, r := range results {
		require.Equalf(t, StatusOK, r.Status, "unit %d: %v", i, r.Err)
		assert.LessOrEqualf(t, r.UnitAttempts-r.circuitWaits, unitMaxAttempts, "unit %d stays inside its charged budget", i)
	}
	for _, agent := range []string{"greta", "kai"} {
		for _, b := range readChunkRecord(t, poolDir, agent).Bins {
			assert.Equal(t, StatusOK, b.Outcome, "%s bin %d", agent, b.Index)
		}
	}
	assert.Positive(t, c.fastFail.Load(), "the breaker opened and units failed fast on it")
	assert.GreaterOrEqual(t, clock.elapsed(), 5*time.Minute, "the run outlasted the outage")
	assert.Equal(t, circuitbreaker.StateClosed, c.breaker.State(), "the half-open probe closed the breaker")
}

// A failed probe is a real call and costs its unit an attempt; a fail-fast does
// not. Only the attempts the provider actually answered count against
// unitMaxAttempts, so units that only ever failed fast end with every charged
// attempt still in hand.
func TestUnitRetry_FailFastIsNotChargedAnAttempt(t *testing.T) {
	t.Parallel()
	slots := sixteenBaselineSlots(t)
	clock := newFakeUnitClock()
	c := newBreakerGatedOutage(clock, 5*time.Minute)

	results, _ := ledgerEngine(t, c, slots, true, withUnitClock(clock), withBreakers(c.breakerFor))

	var charged, waits int
	for _, r := range results {
		require.Equal(t, StatusOK, r.Status)
		charged += r.UnitAttempts - r.circuitWaits
		waits += r.circuitWaits
	}
	assert.Equal(t, int(c.calls.Load()), charged, "every charged attempt is a call the provider answered")
	assert.Equal(t, int(c.fastFail.Load()), waits, "every fail-fast was a free requeue")
}

// The bound: a provider that never recovers does not hold its units forever. A
// unit may wait on an open breaker for at most unitCircuitWaitBudget of summed
// backoff; past it a fail-fast is charged like any transport failure, so the
// unit then fails inside its ordinary budget.
func TestUnitRetry_DeadProviderBoundsTheCircuitWait(t *testing.T) {
	t.Parallel()
	slots := sixteenBaselineSlots(t)
	clock := newFakeUnitClock()
	c := newBreakerGatedOutage(clock, 1000*time.Hour)

	done := make(chan []Result, 1)
	go func() {
		r, _ := ledgerEngine(t, c, slots, true, withUnitClock(clock), withBreakers(c.breakerFor))
		done <- r
	}()
	var results []Result
	select {
	case results = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("a run against a provider that never recovers did not end")
	}
	for i, r := range results {
		assert.Equalf(t, StatusFailed, r.Status, "unit %d", i)
		assert.LessOrEqualf(t, r.UnitAttempts-r.circuitWaits, unitMaxAttempts, "unit %d", i)
	}
	assert.Less(t, clock.elapsed(), unitCircuitWaitBudget+unitBackoffBudget+2*circuitbreaker.DefaultCooldown,
		"each unit's wait is bounded by the circuit budget plus its ordinary budget")
}
