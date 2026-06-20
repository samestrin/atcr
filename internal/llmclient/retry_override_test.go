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
