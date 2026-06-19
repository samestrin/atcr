// Package circuitbreaker implements a per-provider circuit breaker so a failing
// LLM provider is detected once and skipped by every subsequent agent, instead
// of each agent independently timing out against the same outage (Epic 4.5).
//
// The breaker is a three-state machine — closed (normal), open (fail fast), and
// half-open (testing recovery). A provider that returns the configured number of
// consecutive breaker-failures (5xx, timeouts, connection-level transport
// errors; 4xx including 429/401 never count) trips the circuit open. After a
// cooldown the circuit admits a single probe (half-open); one success closes it,
// one failure reopens it and restarts the cooldown. Every state transition is
// pushed to the atcr_circuit_breaker_state gauge, labelled by provider.
//
// All state is in-memory and resets on process exit (persistence is out of
// scope). Every method is safe for concurrent use: one mutex guards the whole
// state machine, and the in-memory transitions are far cheaper than the provider
// call they gate, so the lock is never the hot path.
package circuitbreaker

import (
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/metrics"
)

// State is the circuit breaker's current mode.
type State int

const (
	// StateClosed is normal operation: requests are allowed.
	StateClosed State = iota
	// StateOpen means the circuit tripped: requests fail fast without a call.
	StateOpen
	// StateHalfOpen means the cooldown elapsed and a single probe is admitted to
	// test whether the provider recovered.
	StateHalfOpen
)

// String renders the state as the gauge/diagnostic label. The numeric value
// (0/1/2) is what the gauge stores; this is for logs and the String() metric
// note. An out-of-range value renders "unknown" rather than panicking.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Default tuning (hardcoded per Epic 4.5; configurable thresholds are explicitly
// out of scope). Three consecutive failures is conservative enough to ride out a
// transient blip; a 60s cooldown is short enough that a recovered provider is
// retried quickly via the half-open probe.
const (
	DefaultThreshold = 3
	DefaultCooldown  = 60 * time.Second
)

// Breaker is one provider's circuit. Construct it with New (or obtain a shared
// one from a Registry). The zero value is not usable — it has no clock.
type Breaker struct {
	mu           sync.Mutex
	provider     string
	state        State
	failureCount int
	openedAt     time.Time
	// probeInFlight gates half-open to a single trial request: once a probe is
	// admitted, concurrent callers fail fast until the probe resolves (success
	// closes the circuit, failure reopens it). This bounds the recovery burst to
	// one request instead of letting every waiting agent hit a still-down provider.
	probeInFlight bool
	threshold     int
	cooldown      time.Duration
	// now is the clock, injectable so tests drive cooldown transitions without
	// real sleeps. Production uses time.Now.
	now func() time.Time
}

// New builds a closed breaker for provider with the given failure threshold and
// cooldown. It seeds the per-provider gauge to closed (0) so the series exists
// before the first transition.
func New(provider string, threshold int, cooldown time.Duration) *Breaker {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	if cooldown <= 0 {
		cooldown = DefaultCooldown
	}
	b := &Breaker{
		provider:  provider,
		state:     StateClosed,
		threshold: threshold,
		cooldown:  cooldown,
		now:       time.Now,
	}
	b.setMetric(StateClosed)
	return b
}

// Allow reports whether a request may proceed. It returns true when the circuit
// is closed, or when it is half-open and no probe is currently in flight (the
// caller becomes the probe). It returns false when the circuit is open, or
// half-open with a probe already running. A time-elapsed open circuit is first
// rolled forward to half-open so the cooldown is honoured lazily without a timer.
//
// A caller that wins the half-open probe MUST report the outcome exactly once via
// RecordSuccess, RecordFailure, or ReleaseProbe — otherwise the probe slot leaks
// and the circuit wedges half-open forever. The slot is held for the caller's
// whole call (which may include a retry/backoff schedule spanning several
// seconds), so during a probe every other agent for the provider fails fast even
// if the provider has already recovered; this single-probe gate is intentional
// (it bounds the recovery burst to one request). The maximum slot-hold equals the
// caller's I/O timeout (typically the HTTP client deadline); a context
// cancellation that short-circuits the probe before any response must call
// ReleaseProbe, not RecordFailure, so the slot is freed for the next caller.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refresh()
	switch b.state {
	case StateClosed:
		return true
	case StateHalfOpen:
		if b.probeInFlight {
			return false
		}
		b.probeInFlight = true
		return true
	default: // StateOpen
		return false
	}
}

// RecordSuccess records a successful provider call. In half-open it closes the
// circuit (the provider recovered); in closed it resets the consecutive-failure
// run. A success observed while open (an in-flight call that started before the
// trip) only clears the failure run; the cooldown-driven half-open probe still
// governs reopening.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case StateHalfOpen:
		b.transition(StateClosed)
		b.failureCount = 0
		b.probeInFlight = false
	default: // StateClosed, StateOpen
		b.failureCount = 0
	}
}

// RecordFailure records a breaker-failure (the caller has already filtered out
// 4xx/cancellations). In closed it advances the consecutive-failure run and trips
// the circuit at the threshold; in half-open it reopens immediately and restarts
// the cooldown. A failure observed while already open is ignored so a pre-trip
// in-flight call cannot extend the cooldown indefinitely.
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case StateClosed:
		b.failureCount++
		if b.failureCount >= b.threshold {
			b.open()
		}
	case StateHalfOpen:
		b.open()
		b.probeInFlight = false
	default: // StateOpen
	}
}

// ReleaseProbe frees a half-open probe slot without recording a health verdict.
// It is for outcomes that prove nothing about the provider — a caller-initiated
// cancellation that cut the probe short before any response. The circuit stays
// half-open so the next caller can retry the probe; no failure is counted (the
// provider did not actually fail) and the circuit is not closed (no success was
// observed). The method unconditionally clears probeInFlight regardless of
// state; outside half-open probeInFlight is always false, so the clear is a
// harmless no-op in practice.
func (b *Breaker) ReleaseProbe() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.probeInFlight = false
}

// State returns the current state, rolling a cooldown-elapsed open circuit
// forward to half-open first so the reported state never lags the clock.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refresh()
	return b.state
}

// refresh applies the time-based transition: an open circuit whose cooldown has
// elapsed becomes half-open, ready to admit a probe. probeInFlight stays false so
// the next Allow admits the trial. Caller must hold b.mu.
func (b *Breaker) refresh() {
	if b.state == StateOpen && b.now().Sub(b.openedAt) >= b.cooldown {
		b.transition(StateHalfOpen)
	}
}

// open trips the circuit and anchors the cooldown at the current time, clearing
// the failure run so a later half-open→closed starts clean. Caller must hold b.mu.
func (b *Breaker) open() {
	b.transition(StateOpen)
	b.openedAt = b.now()
	b.failureCount = 0
}

// transition sets the state and pushes the new value to the per-provider gauge so
// operators see the change in /metrics. Caller must hold b.mu. The metrics call
// takes the registry's own lock briefly; metrics never calls back into this
// package, so there is no lock-ordering cycle.
func (b *Breaker) transition(s State) {
	b.state = s
	b.setMetric(s)
}

// setMetric writes the state's numeric value (0/1/2) to the provider-labelled
// gauge. Caller must hold b.mu.
func (b *Breaker) setMetric(s State) {
	metrics.Gauge(metrics.Key(metrics.NameCircuitBreakerState, metrics.LabelProvider, b.provider)).Set(float64(s))
}
