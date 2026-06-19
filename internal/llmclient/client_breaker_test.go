package llmclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/circuitbreaker"
)

// breakerCtx attaches a provider so the circuit breaker engages for the call.
func breakerCtx(provider string) context.Context {
	return circuitbreaker.NewContext(context.Background(), provider)
}

// noRetryClient builds a client with no retries so each Complete is exactly one
// HTTP attempt — one breaker outcome and one server hit per call.
func noRetryClient(h *http.Client) *Client {
	return New(WithHTTPClient(h), WithRetry(0, time.Millisecond, 1.5))
}

// statusServer serves a fixed HTTP status and counts hits.
func statusServer(t *testing.T, code int, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func resetBreakers(t *testing.T) {
	t.Helper()
	circuitbreaker.DefaultRegistry.Reset()
	t.Cleanup(circuitbreaker.DefaultRegistry.Reset)
}

// AC9 + AC2: a provider returning 500 trips the breaker after 3 failures; the
// 4th call fails fast with CircuitOpenError and makes NO HTTP request.
func TestBreaker_OpensAfterThree500s(t *testing.T) {
	resetBreakers(t)
	t.Setenv("TEST_KEY", testKey)

	var hits atomic.Int64
	srv := statusServer(t, http.StatusInternalServerError, &hits)
	c := noRetryClient(srv.Client())
	inv := Invocation{BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "x"}
	ctx := breakerCtx("openai")

	for i := 0; i < 3; i++ {
		_, err := c.Complete(ctx, inv)
		require.Error(t, err)
		var coe *CircuitOpenError
		require.False(t, errors.As(err, &coe), "call %d should be a real 500, not circuit-open", i)
	}
	require.Equal(t, int64(3), hits.Load(), "first 3 calls should each hit the server")
	require.Equal(t, circuitbreaker.StateOpen, circuitbreaker.DefaultRegistry.Get("openai").State())

	// 4th call: fail fast, no HTTP request (AC2).
	_, err := c.Complete(ctx, inv)
	var coe *CircuitOpenError
	require.True(t, errors.As(err, &coe), "4th call should be CircuitOpenError, got %v", err)
	assert.Equal(t, "openai", coe.Provider)
	assert.Equal(t, int64(3), hits.Load(), "open circuit must make no HTTP request (AC2)")
}

// AC10: a 429 rate-limit never trips the breaker (owned by Epic 4.6's backoff).
func TestBreaker_429StaysClosed(t *testing.T) {
	resetBreakers(t)
	t.Setenv("TEST_KEY", testKey)

	var hits atomic.Int64
	srv := statusServer(t, http.StatusTooManyRequests, &hits)
	c := noRetryClient(srv.Client())
	inv := Invocation{BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "x"}
	ctx := breakerCtx("openai")

	for i := 0; i < 5; i++ {
		_, err := c.Complete(ctx, inv)
		require.Error(t, err)
	}
	assert.Equal(t, int64(5), hits.Load(), "every 429 call must reach the server (breaker stays closed)")
	assert.Equal(t, circuitbreaker.StateClosed, circuitbreaker.DefaultRegistry.Get("openai").State())
}

// AC10: a 401 auth error never trips the breaker (auth is permanent, not an outage).
func TestBreaker_401StaysClosed(t *testing.T) {
	resetBreakers(t)
	t.Setenv("TEST_KEY", testKey)

	var hits atomic.Int64
	srv := statusServer(t, http.StatusUnauthorized, &hits)
	c := noRetryClient(srv.Client())
	inv := Invocation{BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "x"}
	ctx := breakerCtx("openai")

	for i := 0; i < 5; i++ {
		_, err := c.Complete(ctx, inv)
		require.Error(t, err)
	}
	assert.Equal(t, int64(5), hits.Load(), "every 401 call must reach the server (breaker stays closed)")
	assert.Equal(t, circuitbreaker.StateClosed, circuitbreaker.DefaultRegistry.Get("openai").State())
}

// Extended AC10/AC9 (rubber-duck clarification): a connection-level transport
// failure trips the breaker — a hard-down provider returns fast transport errors,
// not a slow 5xx, so excluding them would defeat outage detection.
func TestBreaker_TransportFailureTrips(t *testing.T) {
	resetBreakers(t)
	t.Setenv("TEST_KEY", testKey)

	// Start then immediately close a server so its URL refuses connections.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := srv.Client()
	baseURL := srv.URL
	srv.Close()

	c := noRetryClient(client)
	inv := Invocation{BaseURL: baseURL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "x"}
	ctx := breakerCtx("openai")

	for i := 0; i < 3; i++ {
		_, err := c.Complete(ctx, inv)
		require.Error(t, err)
	}
	require.Equal(t, circuitbreaker.StateOpen, circuitbreaker.DefaultRegistry.Get("openai").State(),
		"3 transport failures should trip the breaker")

	_, err := c.Complete(ctx, inv)
	var coe *CircuitOpenError
	require.True(t, errors.As(err, &coe), "after transport failures the circuit should be open, got %v", err)
}

// A successful 200 records success and resets the consecutive-failure run, so
// non-consecutive failures never accumulate to the threshold.
func TestBreaker_SuccessResetsFailureRun(t *testing.T) {
	resetBreakers(t)
	t.Setenv("TEST_KEY", testKey)

	var fail atomic.Bool
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		okResponse(w, "ok")
	}))
	t.Cleanup(srv.Close)

	c := noRetryClient(srv.Client())
	inv := Invocation{BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "x"}
	ctx := breakerCtx("openai")

	_, _ = c.Complete(ctx, inv) // fail 1
	_, _ = c.Complete(ctx, inv) // fail 2
	fail.Store(false)
	out, err := c.Complete(ctx, inv) // success → resets the run
	require.NoError(t, err)
	require.Equal(t, "ok", out)
	fail.Store(true)
	_, _ = c.Complete(ctx, inv) // fail 1 (new run)
	_, _ = c.Complete(ctx, inv) // fail 2 (new run)

	// 4 total failures but only 2 consecutive after the success → still closed.
	assert.Equal(t, circuitbreaker.StateClosed, circuitbreaker.DefaultRegistry.Get("openai").State())
}

// No provider on the context (e.g. the doctor self-test) disables the breaker:
// every call reaches the server regardless of repeated failures.
func TestBreaker_NoProviderNoOp(t *testing.T) {
	resetBreakers(t)
	t.Setenv("TEST_KEY", testKey)

	var hits atomic.Int64
	srv := statusServer(t, http.StatusInternalServerError, &hits)
	c := noRetryClient(srv.Client())
	inv := Invocation{BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "x"}

	for i := 0; i < 5; i++ {
		_, err := c.Complete(context.Background(), inv) // no provider attached
		require.Error(t, err)
		var coe *CircuitOpenError
		require.False(t, errors.As(err, &coe), "no-provider call must never fail fast")
	}
	assert.Equal(t, int64(5), hits.Load(), "no-provider calls must always reach the server")
}

// A caller-initiated cancellation is not a provider-health signal: it records
// nothing and leaves the circuit closed.
func TestBreaker_CancellationDoesNotTrip(t *testing.T) {
	resetBreakers(t)
	t.Setenv("TEST_KEY", testKey)

	var hits atomic.Int64
	srv := statusServer(t, http.StatusInternalServerError, &hits)
	c := noRetryClient(srv.Client())
	inv := Invocation{BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "x"}

	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(breakerCtx("openai"))
		cancel() // cancel before the call
		_, err := c.Complete(ctx, inv)
		require.Error(t, err)
		require.ErrorIs(t, err, context.Canceled)
	}
	assert.Equal(t, circuitbreaker.StateClosed, circuitbreaker.DefaultRegistry.Get("openai").State(),
		"cancellations must not trip the breaker")
}

// isBreakerFailure classification is unit-tested directly so every branch —
// including the defensive nil guard the call site never reaches — is covered.
func TestIsBreakerFailureClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"canceled", context.Canceled, false},
		{"wrapped canceled", fmt.Errorf("x: %w", context.Canceled), false},
		{"500", &HTTPStatusError{Status: 500}, true},
		{"503", &HTTPStatusError{Status: 503}, true},
		{"wrapped 500", fmt.Errorf("exhausted: %w", &HTTPStatusError{Status: 500}), true},
		{"429", &HTTPStatusError{Status: 429}, false},
		{"401", &HTTPStatusError{Status: 401}, false},
		{"404", &HTTPStatusError{Status: 404}, false},
		{"transport", errors.New("connection refused"), true},
		{"deadline", context.DeadlineExceeded, true},
	}
	for _, tc := range cases {
		if got := isBreakerFailure(tc.err); got != tc.want {
			t.Errorf("isBreakerFailure(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCircuitOpenErrorMessage(t *testing.T) {
	err := &CircuitOpenError{Provider: "openai"}
	msg := err.Error()
	if !strings.Contains(msg, "openai") || !strings.Contains(msg, "circuit breaker open") {
		t.Fatalf("CircuitOpenError.Error() = %q, want it to mention the provider and 'circuit breaker open'", msg)
	}
}

// A 4xx (here 429) means the provider replied — it is reachable — so the breaker
// treats it as a healthy round-trip that resets the consecutive-failure run.
// (Independent-review fix: this also closes a half-open probe instead of wedging
// it.) Four 500s interrupted by a 429 never reach 3 consecutive failures.
func TestBreaker_4xxResetsFailureRun(t *testing.T) {
	resetBreakers(t)
	t.Setenv("TEST_KEY", testKey)

	var code atomic.Int64
	code.Store(http.StatusInternalServerError)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(code.Load()))
		_, _ = w.Write([]byte(`{"error":"x"}`))
	}))
	t.Cleanup(srv.Close)

	c := noRetryClient(srv.Client())
	inv := Invocation{BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "x"}
	ctx := breakerCtx("openai")

	_, _ = c.Complete(ctx, inv) // 500 → fail 1
	_, _ = c.Complete(ctx, inv) // 500 → fail 2
	code.Store(http.StatusTooManyRequests)
	_, _ = c.Complete(ctx, inv) // 429 → reachable → resets the run
	code.Store(http.StatusInternalServerError)
	_, _ = c.Complete(ctx, inv) // 500 → fail 1 (new run)
	_, _ = c.Complete(ctx, inv) // 500 → fail 2 (new run)

	assert.Equal(t, circuitbreaker.StateClosed, circuitbreaker.DefaultRegistry.Get("openai").State(),
		"a 429 between failures resets the run, so 4 total 500s never reach 3 consecutive")
}

// AC1 granularity (independent review): the breaker records one outcome per agent
// invocation (after that invocation's own retry budget), not per HTTP attempt.
// With retries enabled, three invocations against a 500 — nine HTTP attempts —
// trip the breaker after the third invocation.
func TestBreaker_RetriesThenTripsPerInvocation(t *testing.T) {
	resetBreakers(t)
	t.Setenv("TEST_KEY", testKey)

	var hits atomic.Int64
	srv := statusServer(t, http.StatusInternalServerError, &hits)
	c := New(WithHTTPClient(srv.Client()), WithRetry(2, time.Millisecond, 1.5)) // 3 attempts/invocation
	inv := Invocation{BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "x"}
	ctx := breakerCtx("openai")

	for i := 0; i < 3; i++ {
		_, err := c.Complete(ctx, inv)
		require.Error(t, err)
	}
	assert.Equal(t, int64(9), hits.Load(), "each invocation exhausts its 3-attempt retry budget (3×3)")
	require.Equal(t, circuitbreaker.StateOpen, circuitbreaker.DefaultRegistry.Get("openai").State(),
		"breaker trips after 3 invocations, not 3 HTTP attempts")

	_, err := c.Complete(ctx, inv)
	var coe *CircuitOpenError
	require.True(t, errors.As(err, &coe))
	assert.Equal(t, int64(9), hits.Load(), "open circuit makes no further HTTP attempts")
}
