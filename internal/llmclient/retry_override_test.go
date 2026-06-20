package llmclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 4.6: a per-call retry override carried on the context lets the fan-out
// apply each agent's effective retry budget without rebuilding the shared
// client. dispatch prefers the override over the client's own fields; absent an
// override the client default applies.

func TestRetryOverride_RaisesBudgetAboveClientDefault(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		okResponse(w, "ok after override retries")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	// Client built with NO retry budget (1 attempt); the override raises it to 3.
	c := New(WithHTTPClient(srv.Client()), WithRetry(0, time.Millisecond, 1.5))
	ctx := WithRetryOverride(context.Background(), 3, time.Millisecond)

	out, err := c.Complete(ctx, Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"})
	require.NoError(t, err)
	assert.Equal(t, "ok after override retries", out)
	assert.Equal(t, int32(3), calls.Load(), "override budget (3) drove 1 + 2 retries")
}

func TestRetryOverride_LowersBudgetBelowClientDefault(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	// Client default would retry 5×; the override forces a single attempt.
	c := New(WithHTTPClient(srv.Client()), WithRetry(5, time.Millisecond, 1.5))
	ctx := WithRetryOverride(context.Background(), 0, time.Millisecond)

	_, err := c.Complete(ctx, Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"})
	require.Error(t, err)
	assert.Equal(t, int32(1), calls.Load(), "override budget (0) made exactly one attempt")
}

func TestRetryOverride_AbsentUsesClientDefault(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		okResponse(w, "ok")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	// No override on the context → the client's own budget (2) governs.
	c := New(WithHTTPClient(srv.Client()), WithRetry(2, time.Millisecond, 1.5))
	out, err := c.Complete(context.Background(), Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"})
	require.NoError(t, err)
	assert.Equal(t, "ok", out)
	assert.Equal(t, int32(3), calls.Load())
}

func TestRetryOverride_ContextRoundTrip(t *testing.T) {
	base := context.Background()
	_, ok := retryOverrideFromContext(base)
	assert.False(t, ok, "no override on a bare context")

	ctx := WithRetryOverride(base, 4, 250*time.Millisecond)
	o, ok := retryOverrideFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, 4, o.maxRetries)
	assert.Equal(t, 250*time.Millisecond, o.initialBackoff)
}

func TestRetryOverride_NegativeBudgetClamped(t *testing.T) {
	// A negative budget would make the attempt loop fall through to a nil-cause
	// "exhausted retries"; clamp to 0 (a single attempt) like WithRetry does.
	ctx := WithRetryOverride(context.Background(), -3, time.Millisecond)
	o, ok := retryOverrideFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, 0, o.maxRetries, "negative override budget is clamped to 0")
}

func TestRetryOverride_NegativeBackoffFloored(t *testing.T) {
	// Symmetric with the maxRetries clamp: a negative base from a direct caller is
	// floored to 0 rather than flowing into the backoff schedule as a negative
	// (fire-immediately) duration. Config callers are validated (>=1ms) so this
	// guards only direct WithRetryOverride callers.
	ctx := WithRetryOverride(context.Background(), 2, -5*time.Second)
	o, ok := retryOverrideFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, time.Duration(0), o.initialBackoff, "negative override base is floored to 0")
}

func TestRetryOverride_FirstSleepClampedToMaxBackoff(t *testing.T) {
	// A direct WithRetryOverride base above maxBackoff (30s) is the only input the
	// pre-loop clampBackoff in dispatch actually bounds: registry config caps
	// initial_backoff_ms at exactly 30s, so a hand-rolled 60s override is the real
	// case. Assert the FIRST retry sleep is clamped below maxBackoff rather than the
	// raw 60s base — this guards the `delay = clampBackoff(delay)` line in dispatch
	// (the test fails if that clamp is removed: jitter(60s) is always >= 30s).
	var sleeps []time.Duration
	origSleep := sleepCtx
	sleepCtx = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil // record the requested delay; never wait real wall-clock time
	}
	defer func() { sleepCtx = origSleep }()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		okResponse(w, "ok")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	// 60s base, double maxBackoff; one retry so exactly one backoff sleep occurs.
	c := New(WithHTTPClient(srv.Client()), WithRetry(0, time.Millisecond, 1.5))
	ctx := WithRetryOverride(context.Background(), 1, 60*time.Second)

	out, err := c.Complete(ctx, Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"})
	require.NoError(t, err)
	assert.Equal(t, "ok", out)
	require.Len(t, sleeps, 1, "exactly one retry sleep before the successful attempt")
	assert.Less(t, sleeps[0], maxBackoff, "first sleep clamped below maxBackoff, not the raw 60s base")
}
