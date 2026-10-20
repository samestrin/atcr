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
//   - A provider whose breaker is half-open with none of its units in flight is
//     waiting for its probe, so its next unit is leased first, ahead of the
//     healthy providers' (Epic 35.16.14 fix AC1a). The breaker only moves when
//     a call reaches it: a probe deferred behind a long healthy backlog left it
//     open for as long as that backlog lasted, and its units unleased.
//
// A throttled provider always keeps one lease, so its units still drain and the
// queue cannot wedge on a provider with nothing in flight.

// throttleWindow is how long a 429 keeps its lane's cap lowered before
// successes may raise it again: the rate-limit backoff, the same estimate of how
// long an upstream's rate-limit window lasts.
const throttleWindow = rateLimitBackoff

// leaseKey names the lane a unit is leased on: its primary's provider and model.
type leaseKey struct{ provider, model string }

// providerThrottle caps leases per lane for one unitQueue. The queue's mutex
// guards it; it has no lock of its own.
type providerThrottle struct {
	clock   unitClock
	ceiling int
	window  time.Duration
	logger  *slog.Logger
	// key names unit i's lane: its primary's provider and model, which is where a
	// held 429 came from. A unit with an empty provider is never throttled.
	key func(i int) leaseKey
	// breaker reads a provider's circuit state.
	breaker func(provider string) circuitbreaker.State
	// lanes is the rate-limit state of each lane; inFlight counts each
	// provider's leases across all its lanes, for the breaker.
	lanes    map[leaseKey]*providerLane
	inFlight map[string]int
}

// providerLane is one lane's lease state.
type providerLane struct {
	inFlight int
	// limit is the lowered cap; 0 means the lane runs at the ceiling.
	limit int
	// until is when the latest rate-limit window ends.
	until time.Time
}

func newProviderThrottle(ceiling int, clock unitClock, logger *slog.Logger, key func(int) leaseKey, breaker func(string) circuitbreaker.State) *providerThrottle {
	if breaker == nil {
		breaker = func(p string) circuitbreaker.State { return circuitbreaker.DefaultRegistry.Get(p).State() }
	}
	return &providerThrottle{
		clock: clock, ceiling: max(ceiling, 1), window: throttleWindow, logger: logger,
		key: key, breaker: breaker, lanes: map[leaseKey]*providerLane{}, inFlight: map[string]int{},
	}
}

// rateKey is the lane whose rate-limit state k shares.
func rateKey(k leaseKey) leaseKey { return leaseKey{provider: k.provider} }

func (t *providerThrottle) lane(k leaseKey) *providerLane {
	k = rateKey(k)
	l, ok := t.lanes[k]
	if !ok {
		l = &providerLane{}
		t.lanes[k] = l
	}
	return l
}

// capFor is how many leases lane k may hold now, and whether it is throttled.
func (t *providerThrottle) capFor(k leaseKey) (int, bool) {
	if k.provider == "" {
		return t.ceiling, false
	}
	limit, throttled := t.ceiling, false
	if l := t.lane(k); l.limit > 0 {
		limit, throttled = l.limit, true
	}
	if t.breaker(k.provider) != circuitbreaker.StateClosed {
		limit, throttled = 1, true
	}
	return limit, throttled
}

// pick is the position in pending of the unit to lease next, or -1 when every
// pending unit's lane is at its cap. The first unit of an unthrottled lane, or
// of a provider whose half-open breaker awaits its probe, wins; a throttled
// lane's unit is taken only when no healthy one can be.
func (t *providerThrottle) pick(pending []int) int {
	seen := map[leaseKey]leaseVerdict{}
	healthy, deferred := -1, -1
	for k, i := range pending {
		key := t.key(i)
		v, ok := seen[key]
		if !ok {
			v = t.verdict(key)
			seen[key] = v
		}
		switch {
		case !v.admit:
		case v.probe:
			return k
		case !v.throttled:
			if healthy < 0 {
				healthy = k
			}
		case deferred < 0:
			deferred = k
		}
	}
	if healthy >= 0 {
		return healthy
	}
	return deferred
}

// leaseVerdict is whether lane k may take a lease now, whether it is
// throttled, and whether its next lease is the probe its provider's half-open
// breaker is waiting for.
type leaseVerdict struct{ admit, throttled, probe bool }

func (t *providerThrottle) verdict(k leaseKey) leaseVerdict {
	if k.provider == "" {
		return leaseVerdict{admit: true}
	}
	limit, throttled := t.capFor(k)
	state := t.breaker(k.provider)
	providerInFlight := t.inFlight[k.provider]
	admit := t.lane(k).inFlight < limit
	if state != circuitbreaker.StateClosed {
		// An outage is provider-wide: one lease across all its models.
		admit = providerInFlight < 1
	}
	return leaseVerdict{
		admit:     admit,
		throttled: throttled,
		probe:     providerInFlight == 0 && state == circuitbreaker.StateHalfOpen,
	}
}

// acquire counts a lease of unit i against its lane and provider.
func (t *providerThrottle) acquire(i int) {
	if k := t.key(i); k.provider != "" {
		t.lane(k).inFlight++
		t.inFlight[k.provider]++
	}
}

// done returns unit i's lease and feeds the attempt's outcome to its lane's
// cap. Only an attempt the primary itself answered says anything about the
// primary's lane: a fallback-served or chain-wide failure is not counted.
func (t *providerThrottle) done(i int, r Result) {
	k := t.key(i)
	if k.provider == "" {
		return
	}
	l := t.lane(k)
	l.inFlight--
	t.inFlight[k.provider]--
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
			"provider", k.provider, "model", k.model, "lease_cap", l.limit, "ceiling", t.ceiling, "window", t.window.String())
	case r.Status == StatusOK && l.limit > 0 && !now.Before(l.until):
		l.limit++
		if l.limit >= t.ceiling {
			l.limit = 0
			t.logger.Info("provider recovered; lease cap back at the ceiling",
				"provider", k.provider, "model", k.model, "ceiling", t.ceiling)
		}
	}
}
