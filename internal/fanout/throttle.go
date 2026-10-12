package fanout

import (
	"log/slog"
	"time"

	"github.com/samestrin/atcr/internal/circuitbreaker"
)

// Adaptive leasing (Epic 35.16.14 T4). max_parallel is the pool's width, but it
// is a ceiling per provider, not a fixed share: a provider that answers 429 gets
// fewer concurrent leases and its units are deferred behind every healthy
// provider's, so one throttling upstream does not stall the run or burn the
// fallback chain while the others still have capacity.
//
// The two signals come from different places. The circuit breaker counts only
// 5xx, provider stalls and transport errors; it records a 429 as a healthy
// reply (llmclient's send). So the rate-limit signal is the error class of the
// unit that failed, and the breaker is read, never fed, for the outage case.
//
//   - A unit whose primary answered 429 halves its provider's lease cap (never
//     below 1) and opens a rate-limit window of throttleWindow.
//   - A success on the primary once the window has passed raises the cap by one;
//     back at the ceiling the provider is no longer throttled.
//   - A provider whose breaker is open or half-open is held to one lease until
//     the breaker closes.
//
// A throttled provider always keeps one lease, so its units still drain and the
// queue cannot wedge on a provider with nothing in flight.

// throttleWindow is how long a 429 keeps its provider's cap lowered before
// successes may raise it again: the rate-limit backoff, the same estimate of how
// long an upstream's rate-limit window lasts.
const throttleWindow = rateLimitBackoff

// providerThrottle caps leases per provider for one unitQueue. The queue's
// mutex guards it; it has no lock of its own.
type providerThrottle struct {
	clock   unitClock
	ceiling int
	window  time.Duration
	logger  *slog.Logger
	// provider names unit i's provider: its primary's, which is where a held 429
	// came from. An empty provider is never throttled.
	provider func(i int) string
	// breaker reads a provider's circuit state.
	breaker func(provider string) circuitbreaker.State
	lanes   map[string]*providerLane
}

// providerLane is one provider's lease state.
type providerLane struct {
	inFlight int
	// limit is the lowered cap; 0 means the provider runs at the ceiling.
	limit int
	// until is when the latest rate-limit window ends.
	until time.Time
}

func newProviderThrottle(ceiling int, clock unitClock, logger *slog.Logger, provider func(int) string, breaker func(string) circuitbreaker.State) *providerThrottle {
	if breaker == nil {
		breaker = func(p string) circuitbreaker.State { return circuitbreaker.DefaultRegistry.Get(p).State() }
	}
	return &providerThrottle{
		clock: clock, ceiling: max(ceiling, 1), window: throttleWindow, logger: logger,
		provider: provider, breaker: breaker, lanes: map[string]*providerLane{},
	}
}

func (t *providerThrottle) lane(p string) *providerLane {
	l, ok := t.lanes[p]
	if !ok {
		l = &providerLane{}
		t.lanes[p] = l
	}
	return l
}

// capFor is how many leases provider p may hold now, and whether p is throttled.
func (t *providerThrottle) capFor(p string) (int, bool) {
	if p == "" {
		return t.ceiling, false
	}
	limit, throttled := t.ceiling, false
	if l := t.lane(p); l.limit > 0 {
		limit, throttled = l.limit, true
	}
	if t.breaker(p) != circuitbreaker.StateClosed {
		limit, throttled = 1, true
	}
	return limit, throttled
}

// pick is the position in pending of the unit to lease next, or -1 when every
// pending unit's provider is at its cap. The first unit of an unthrottled
// provider wins; a throttled provider's unit is taken only when no healthy one
// can be.
func (t *providerThrottle) pick(pending []int) int {
	type verdict struct{ admit, throttled bool }
	seen := map[string]verdict{}
	deferred := -1
	for k, i := range pending {
		p := t.provider(i)
		v, ok := seen[p]
		if !ok {
			limit, throttled := t.capFor(p)
			v = verdict{admit: p == "" || t.lane(p).inFlight < limit, throttled: throttled}
			seen[p] = v
		}
		switch {
		case !v.admit:
		case !v.throttled:
			return k
		case deferred < 0:
			deferred = k
		}
	}
	return deferred
}

// acquire counts a lease of unit i against its provider.
func (t *providerThrottle) acquire(i int) {
	if p := t.provider(i); p != "" {
		t.lane(p).inFlight++
	}
}

// done returns unit i's lease and feeds the attempt's outcome to its provider's
// cap. Only an attempt the primary itself answered says anything about the
// primary's provider: a fallback-served or chain-wide failure is not counted.
func (t *providerThrottle) done(i int, r Result) {
	p := t.provider(i)
	if p == "" {
		return
	}
	l := t.lane(p)
	l.inFlight--
	if r.FallbackUsed {
		return
	}
	now := t.clock.Now()
	switch {
	case r.Status != StatusOK && classifyUnitFailure(r.Err) == failureRateLimit:
		limit := l.limit
		if limit == 0 {
			limit = t.ceiling
		}
		l.limit = max(1, limit/2)
		l.until = now.Add(t.window)
		t.logger.Info("provider rate-limited; lowering its lease cap",
			"provider", p, "lease_cap", l.limit, "ceiling", t.ceiling, "window", t.window.String())
	case r.Status == StatusOK && l.limit > 0 && !now.Before(l.until):
		l.limit++
		if l.limit >= t.ceiling {
			l.limit = 0
			t.logger.Info("provider recovered; lease cap back at the ceiling", "provider", p, "ceiling", t.ceiling)
		}
	}
}
