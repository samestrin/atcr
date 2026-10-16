package fanout

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
)

// Outer per-unit retry (Epic 35.16.14 T3). The per-call retry in
// internal/llmclient is sized for one call inside one deadline: six attempts and
// about 6.6s of backoff at the registry defaults, longer only when the upstream
// sends Retry-After. A quota-limited upstream's rate-limit window lasts minutes,
// so a unit that fails retryably is returned to the work queue instead of being
// recorded terminal, and is leased again once its backoff has passed. Units the
// ledger does not record (bulk, chunked diff) are never requeued, so their runs
// are unchanged.
//
// The two budgets bound the outer loop on their own, so nested retries cannot
// multiply into an unbounded wait: at most unitMaxAttempts leases per unit
// (with six inner attempts each, 24 calls) and at most unitBackoffBudget of
// backoff between them. A unit that exhausts either is marked failed.
const (
	// unitMaxAttempts is how many times one unit may be leased.
	unitMaxAttempts = 4
	// unitBackoffBudget caps the backoff one unit may wait out across its
	// requeues, summed.
	unitBackoffBudget = 10 * time.Minute
	// unitCircuitWaitBudget caps how long one unit may wait, summed, on an open
	// circuit breaker without being charged an attempt.
	unitCircuitWaitBudget = 30 * time.Minute
)

// Base backoffs by failure class, doubled per attempt. A rate limit waits
// longer, because its window is minutes long and retrying inside it only
// spends quota: 75s, 150s, 300s (525s in all, inside the budget). An outage
// class waits 20s, 40s, 80s.
const (
	rateLimitBackoff = 75 * time.Second
	outageBackoff    = 20 * time.Second
)

// failureClass is why a unit's attempt failed, as far as requeueing cares.
type failureClass int

const (
	// failureFinal is any failure a later attempt would repeat: a 4xx other than
	// 429, a malformed or truncated reply, a cancelled run.
	failureFinal failureClass = iota
	// failureRateLimit is an HTTP 429. It requeues on the primary without
	// walking the fallback chain.
	failureRateLimit
	// failureServer is an HTTP 5xx.
	failureServer
	// failureTransport is a request that got no HTTP response, or a provider
	// whose circuit breaker is open.
	failureTransport
	// failureTimeout is a call or unit deadline that fired.
	failureTimeout
)

func (c failureClass) String() string {
	switch c {
	case failureRateLimit:
		return "rate_limit"
	case failureServer:
		return "server_error"
	case failureTransport:
		return "transport"
	case failureTimeout:
		return "timeout"
	default:
		return "final"
	}
}

// classifyUnitFailure maps an attempt's error to its failure class. The
// *HTTPStatusError stays errors.As-reachable through llmclient's
// exhausted-retries wrap, so a 429 that outlasted the per-call budget still
// reads as a 429 here.
func classifyUnitFailure(err error) failureClass {
	if err == nil || errors.Is(err, context.Canceled) {
		return failureFinal
	}
	var he *llmclient.HTTPStatusError
	if errors.As(err, &he) {
		switch {
		case he.Status == 429:
			return failureRateLimit
		case he.Status >= 500:
			return failureServer
		default:
			return failureFinal
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return failureTimeout
	}
	var co *llmclient.CircuitOpenError
	var ue *url.Error
	if errors.As(err, &co) || errors.As(err, &ue) {
		return failureTransport
	}
	return failureFinal
}

// unitRetryPolicy is the outer budget. The engine always runs the constants
// above; tests shrink it.
type unitRetryPolicy struct {
	maxAttempts int
	budget      time.Duration
	backoff     func(c failureClass, attempt int) time.Duration
}

func defaultUnitRetryPolicy() unitRetryPolicy {
	return unitRetryPolicy{maxAttempts: unitMaxAttempts, budget: unitBackoffBudget, backoff: unitBackoff}
}

// unitBackoff is the wait before lease attempt+1, after attempt (1-based)
// failed with class c.
func unitBackoff(c failureClass, attempt int) time.Duration {
	base := outageBackoff
	if c == failureRateLimit {
		base = rateLimitBackoff
	}
	return base << (attempt - 1)
}

// next reports the backoff before lease attempt+1 after attempt failed with
// class c, having already waited spent. ok is false when either budget is
// exhausted.
func (p unitRetryPolicy) next(c failureClass, attempt int, spent time.Duration) (time.Duration, bool) {
	if attempt >= p.maxAttempts {
		return 0, false
	}
	d := p.backoff(c, attempt)
	if spent+d > p.budget {
		return 0, false
	}
	return d, true
}

// unitRetryState is one unit's progress through its outer budget. Only the
// worker holding the unit touches it; the queue's mutex orders the handoff
// between workers.
type unitRetryState struct {
	attempts int
	requeues int
	spent    time.Duration
}
