package llmclient

import (
	"context"
	"time"
)

// retryOverride is a per-call retry budget carried on the context so the
// fan-out can apply each agent's effective max_retries / initial_backoff
// (Epic 4.6) without rebuilding the shared *Client. It mirrors how the
// circuitbreaker threads its per-provider state through the context: the value
// is set far from the call site and read inside dispatch.
type retryOverride struct {
	maxRetries     int
	initialBackoff time.Duration
}

// retryOverrideKey is the unexported context key for retryOverride; the
// unexported type guarantees no collision with other packages' context values.
type retryOverrideKey struct{}

// WithRetryOverride returns a context carrying a per-call retry budget that
// dispatch prefers over the client's own fields. A negative maxRetries is
// clamped to 0 (a single attempt) — the same guard WithRetry applies — so the
// retry loop never falls through to a nil-cause "exhausted retries" error.
func WithRetryOverride(ctx context.Context, maxRetries int, initialBackoff time.Duration) context.Context {
	if maxRetries < 0 {
		maxRetries = 0
	}
	return context.WithValue(ctx, retryOverrideKey{}, retryOverride{
		maxRetries:     maxRetries,
		initialBackoff: initialBackoff,
	})
}

// retryOverrideFromContext returns the per-call retry override and whether one
// was set.
func retryOverrideFromContext(ctx context.Context) (retryOverride, bool) {
	o, ok := ctx.Value(retryOverrideKey{}).(retryOverride)
	return o, ok
}
