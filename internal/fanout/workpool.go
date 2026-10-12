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

// unitQueue hands out slot indices in order, one per lease. It is safe for
// concurrent use by every worker of one Run.
type unitQueue struct {
	mu      sync.Mutex
	pending []int
}

func newUnitQueue(idx []int) *unitQueue {
	return &unitQueue{pending: idx}
}

// lease takes the next pending unit. ok is false once the queue is drained.
func (q *unitQueue) lease() (i int, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return 0, false
	}
	i = q.pending[0]
	q.pending = q.pending[1:]
	return i, true
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
