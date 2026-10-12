package fanout

import (
	"context"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/registry"
)

// Work queue (Epic 35.16.14 T2). Engine.Run no longer spawns a goroutine per
// slot: a fixed pool of workers pulls slot indices from a unitQueue, runs each
// unit to its result, and only then pulls the next. Goroutine count and peak
// payload memory follow the worker count, not the number of units.

// defaultRunCeiling is the run-level wall-clock ceiling runEngine places over a
// whole fan-out when none is set. It replaces the wave-scaled aggregate deadline
// (aggregateTimeoutFactor), which modelled a fixed slot list running in
// ceil(slots/max_parallel) waves and so cancelled units that had not yet been
// leased. A drained queue has no wave count: each unit carries its own deadline
// from lease time (unitDeadline), and this ceiling only stops a hung run.
const defaultRunCeiling = time.Duration(registry.MaxTimeoutSecs) * time.Second

// unitClock is the queue's time source. The engine uses the wall clock; tests
// substitute a fake so a minutes-long backoff runs in no real time.
type unitClock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type wallClock struct{}

func (wallClock) Now() time.Time                         { return time.Now() }
func (wallClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// unitQueue hands out slot indices, one per lease. It is safe for concurrent
// use by every worker of one lane.
//
// A unit is pending (ready to lease), held (a worker has it), or delayed (it
// failed retryably and was requeued, Epic 35.16.14 T3, and waits out its
// backoff). A delayed unit becomes pending once its backoff has passed, behind
// every unit already pending. The queue is drained only when nothing is pending,
// delayed or held, since a held unit may still come back.
type unitQueue struct {
	clock   unitClock
	mu      sync.Mutex
	pending []int
	delayed []delayedUnit
	held    int
	// changed is closed and replaced whenever a held unit is released or
	// requeued, waking workers waiting for one.
	changed chan struct{}
	// throttle, when set, caps each provider's leases (Epic 35.16.14 T4): a
	// pending unit whose provider is at its cap is skipped, and a throttled
	// provider's units wait behind every healthy provider's.
	throttle *providerThrottle
}

type delayedUnit struct {
	i  int
	at time.Time
}

func newUnitQueue(idx []int, clock unitClock) *unitQueue {
	if clock == nil {
		clock = wallClock{}
	}
	return &unitQueue{clock: clock, pending: idx, changed: make(chan struct{})}
}

// lease takes the next pending unit, waiting while every remaining unit is
// delayed or held by another worker. ok is false once the queue is drained.
// Once ctx is done every delayed unit is pending at once: its backoff no longer
// buys anything, and the worker resolves it without a call.
func (q *unitQueue) lease(ctx context.Context) (i int, ok bool) {
	for {
		q.mu.Lock()
		q.promote(ctx.Err() != nil)
		if i, ok := q.take(ctx.Err() != nil); ok {
			q.held++
			q.mu.Unlock()
			return i, true
		}
		// Nothing leasable. A pending unit the throttle skipped has a unit of its
		// provider held (the throttle always admits a provider with none in
		// flight), so its release is a change to wait for.
		if len(q.pending) == 0 && len(q.delayed) == 0 && q.held == 0 {
			q.mu.Unlock()
			return 0, false
		}
		changed := q.changed
		var wake <-chan time.Time
		if len(q.delayed) > 0 {
			wake = q.clock.After(q.nextReady().Sub(q.clock.Now()))
		}
		q.mu.Unlock()
		// A done ctx is not waited on: it is already closed, and promote has moved
		// every delayed unit, so only a held unit's return can change anything.
		cancelled := ctx.Done()
		if ctx.Err() != nil {
			cancelled = nil
		}
		select {
		case <-changed:
		case <-wake:
		case <-cancelled:
		}
	}
}

// take removes and returns the next unit to lease: the first pending one, or
// with a throttle the one it picks. A done run (all) leases in order, past every
// cap, since its units resolve without a call. Caller holds mu.
func (q *unitQueue) take(all bool) (int, bool) {
	if len(q.pending) == 0 {
		return 0, false
	}
	at := 0
	if q.throttle != nil && !all {
		if at = q.throttle.pick(q.pending); at < 0 {
			return 0, false
		}
	}
	i := q.pending[at]
	q.pending = append(q.pending[:at], q.pending[at+1:]...)
	if q.throttle != nil {
		q.throttle.acquire(i)
	}
	return i, true
}

// promote moves every delayed unit whose backoff has passed (all of them when
// all is set) to the back of the pending list, in the order they were delayed.
// Caller holds mu.
func (q *unitQueue) promote(all bool) {
	if len(q.delayed) == 0 {
		return
	}
	now := q.clock.Now()
	kept := q.delayed[:0]
	for _, d := range q.delayed {
		if all || !d.at.After(now) {
			q.pending = append(q.pending, d.i)
		} else {
			kept = append(kept, d)
		}
	}
	q.delayed = kept
}

// nextReady is when the first delayed unit's backoff ends. Caller holds mu and
// has checked delayed is non-empty.
func (q *unitQueue) nextReady() time.Time {
	at := q.delayed[0].at
	for _, d := range q.delayed[1:] {
		if d.at.Before(at) {
			at = d.at
		}
	}
	return at
}

// release returns held unit i as resolved with r: it will not be leased again.
func (q *unitQueue) release(i int, r Result) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.held--
	q.settle(i, r)
	q.signal()
}

// requeue returns held unit i, whose attempt ended in r, to the queue, leasable
// again after backoff.
func (q *unitQueue) requeue(i int, backoff time.Duration, r Result) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.held--
	q.settle(i, r)
	q.delayed = append(q.delayed, delayedUnit{i: i, at: q.clock.Now().Add(backoff)})
	q.signal()
}

// settle hands unit i's attempt to the throttle. Caller holds mu.
func (q *unitQueue) settle(i int, r Result) {
	if q.throttle != nil {
		q.throttle.done(i, r)
	}
}

// signal wakes every waiting worker. Caller holds mu.
func (q *unitQueue) signal() {
	close(q.changed)
	q.changed = make(chan struct{})
}

// poolWorkers is how many workers drain n units: maxParallel when it bounds the
// lane, otherwise one per unit (maxParallel <= 0 is the unbounded lane, every
// unit in flight at once, as before the pool).
func poolWorkers(n, maxParallel int) int {
	if maxParallel > 0 && maxParallel < n {
		return maxParallel
	}
	return n
}

// unitDeadline bounds one leased unit — its whole fallback chain — from the
// moment a worker takes it. The budget is the run's base timeout scaled by the
// unit's chunk count, the same scaling invokeAgent gives each call, so the unit
// deadline never cuts a call short of its own per-call deadline. A unit leased
// late in a long run gets the same budget as the first one leased. A
// non-positive base timeout leaves ctx unchanged.
func unitDeadline(ctx context.Context, baseSecs int, s Slot) (context.Context, context.CancelFunc) {
	secs := scaledTimeoutSecs(baseSecs, s.Primary.ChunkTotal)
	if secs <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(secs)*time.Second)
}
