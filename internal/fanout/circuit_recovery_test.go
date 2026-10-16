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
		// UnitAttempts stays zero for a unit that never requeued: its one lease.
		charged += max(r.UnitAttempts, 1) - r.circuitWaits
		waits += r.circuitWaits
	}
	assert.Equal(t, int(c.calls.Load()), charged, "every charged attempt is a call the provider answered")
	assert.Equal(t, int(c.fastFail.Load()), waits, "every fail-fast was a free requeue")
}

// The first bound: a provider that never recovers does not hold its units
// forever. Each half-open probe is a real call, charged to the unit that made
// it, so once every unit has spent its attempts on failed probes the run ends
// with every unit failed and none over its charged budget.
func TestUnitRetry_DeadProviderEndsOnChargedProbes(t *testing.T) {
	t.Parallel()
	slots := sixteenBaselineSlots(t)
	clock := newFakeUnitClock()
	c := newBreakerGatedOutage(clock, 1000*time.Hour)

	results, _ := ledgerEngine(t, c, slots, true, withUnitClock(clock), withBreakers(c.breakerFor))

	for i, r := range results {
		assert.Equalf(t, StatusFailed, r.Status, "unit %d", i)
		assert.Equalf(t, unitMaxAttempts, r.UnitAttempts-r.circuitWaits, "unit %d spent exactly its charged budget", i)
		assert.LessOrEqualf(t, r.circuitWaited, unitCircuitWaitBudget, "unit %d", i)
	}
	assert.Equal(t, int32(len(slots)*unitMaxAttempts), c.calls.Load(), "every charged attempt was a real call")
}

// heldProbe is a provider whose breaker is half-open with its probe held by some
// other caller for good: every call fails fast and none ever reaches it, so no
// probe can be charged to the unit.
type heldProbe struct{ breaker *circuitbreaker.Breaker }

func (c *heldProbe) Complete(context.Context, llmclient.Invocation) (string, error) {
	return "", &llmclient.CircuitOpenError{Provider: "p"}
}

// The second bound, for a unit that is never the probe: it may wait on an open
// breaker for at most unitCircuitWaitBudget, summed. Past that its fail-fasts
// are charged like any transport failure, so it then fails inside its ordinary
// budget instead of requeueing forever.
func TestUnitRetry_CircuitWaitBudgetBoundsAUnitThatNeverProbes(t *testing.T) {
	t.Parallel()
	slots := sixteenBaselineSlots(t)[:1]
	br := circuitbreaker.NewWithClock("p", 1, circuitbreaker.DefaultCooldown, newFakeUnitClock().Now)
	br.ForceHalfOpen()
	require.True(t, br.Allow(), "someone else takes the probe and never resolves it")
	clock := newFakeUnitClock()
	c := &heldProbe{breaker: br}

	results, _ := ledgerEngine(t, c, slots, true, withUnitClock(clock),
		withBreakers(func(string) *circuitbreaker.Breaker { return br }))

	r := results[0]
	require.Equal(t, StatusFailed, r.Status)
	var co *llmclient.CircuitOpenError
	assert.ErrorAs(t, r.Err, &co)
	assert.LessOrEqual(t, r.circuitWaited, unitCircuitWaitBudget)
	assert.Greater(t, r.circuitWaited, unitCircuitWaitBudget-outageBackoff, "it waited out the whole circuit budget")
	assert.Equal(t, unitMaxAttempts, r.UnitAttempts-r.circuitWaits, "then spent its ordinary budget")
}

// circuitWait waits out an open breaker's remaining cooldown, rechecks a
// half-open one whose probe is held after outageBackoff, counts at least
// minCircuitWait, and refuses once the budget would be exceeded.
func TestEngine_CircuitWait(t *testing.T) {
	t.Parallel()
	clock := newFakeUnitClock()
	br := circuitbreaker.NewWithClock("p", 1, time.Minute, clock.Now)
	e := NewEngine(newFake(), withBreakers(func(string) *circuitbreaker.Breaker { return br }))

	d, ok := e.circuitWait("p", 0)
	assert.True(t, ok)
	assert.Equal(t, minCircuitWait, d, "closed: lease again almost at once")

	br.RecordFailure() // open
	clock.After(20 * time.Second)
	d, ok = e.circuitWait("p", 0)
	assert.True(t, ok)
	assert.Equal(t, 40*time.Second, d, "open: the cooldown left")

	clock.After(40 * time.Second)
	require.True(t, br.Allow(), "half-open: someone takes the probe")
	d, ok = e.circuitWait("p", 0)
	assert.True(t, ok)
	assert.Equal(t, outageBackoff, d, "half-open with the probe held: recheck")

	_, ok = e.circuitWait("p", unitCircuitWaitBudget-outageBackoff+time.Second)
	assert.False(t, ok, "past the budget")
	d, ok = e.circuitWait("p", unitCircuitWaitBudget-outageBackoff)
	assert.True(t, ok, "exactly at the budget")
	assert.Equal(t, outageBackoff, d)
}
