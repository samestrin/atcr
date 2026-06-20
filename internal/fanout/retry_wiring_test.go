package fanout

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
)

// Epic 4.6: buildAgent resolves each agent's effective retry budget from the
// per-agent override layered over the resolved global Settings, and invokeAgent
// threads it onto the call context so the shared client's dispatch honors it.

func retryCfg() *ReviewConfig {
	return &ReviewConfig{
		Registry: &registry.Registry{
			Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: "http://x"}},
			Agents: map[string]registry.AgentConfig{
				// inherits the resolved global budget
				"greta": {Provider: "p", Model: "m", Persona: "greta", Temperature: ptrF(0.7)},
				// overrides max_retries; inherits initial_backoff_ms
				"kai": {Provider: "p", Model: "m2", Persona: "kai", Temperature: ptrF(0.7), MaxRetries: ptrInt(9)},
			},
		},
		Project:  &registry.ProjectConfig{Agents: []string{"greta"}},
		Settings: registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600, MaxRetries: 5, InitialBackoffMs: 500},
	}
}

func TestBuildAgent_PropagatesRetryFields(t *testing.T) {
	cfg := retryCfg()
	payloads := map[string]modePayload{"blocks": {Text: "x", FileCount: 1}}

	greta, _, err := buildAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"})
	require.NoError(t, err)
	assert.Equal(t, 5, greta.MaxRetries, "unset agent inherits resolved global max_retries")
	assert.Equal(t, 500, greta.InitialBackoffMs, "unset agent inherits resolved global initial_backoff_ms")

	kai, _, err := buildAgent(cfg, "kai", payloads, ReviewRange{Base: "a", Head: "b"})
	require.NoError(t, err)
	assert.Equal(t, 9, kai.MaxRetries, "per-agent max_retries override wins")
	assert.Equal(t, 500, kai.InitialBackoffMs, "inherits global initial_backoff_ms when not overridden")
}

// retrying503Server returns 503 for the first failUntil calls, then a valid
// completion. It counts calls so a test can assert the retry budget consumed.
func retrying503Server(t *testing.T, failUntil int, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= int32(failUntil) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
}

func TestInvokeAgent_AppliesRetryOverride(t *testing.T) {
	var calls atomic.Int32
	srv := retrying503Server(t, 2, &calls)
	defer srv.Close()
	t.Setenv("ATCR_TEST_KEY", "sk-test")

	// Shared client built with ZERO retry budget: only the per-agent override on
	// the context can carry the call past the two 503s.
	client := llmclient.New(llmclient.WithHTTPClient(srv.Client()), llmclient.WithRetry(0, time.Millisecond, 1.5))
	e := NewEngine(client)

	a := Agent{
		Name:             "kai",
		Invocation:       llmclient.Invocation{BaseURL: srv.URL, APIKeyEnv: "ATCR_TEST_KEY", Model: "m"},
		MaxRetries:       3,
		InitialBackoffMs: 1,
	}
	r := e.invokeAgent(context.Background(), a)

	require.Equal(t, StatusOK, r.Status, "per-agent override budget should drive retries past the 503s")
	assert.Equal(t, int32(3), calls.Load(), "1 + 2 retries from the override budget")
}

func TestInvokeAgent_NoOverrideUsesClientDefault(t *testing.T) {
	var calls atomic.Int32
	srv := retrying503Server(t, 2, &calls)
	defer srv.Close()
	t.Setenv("ATCR_TEST_KEY", "sk-test")

	// Bare Agent (no retry fields) → no context override → the client's own zero
	// budget governs, so the first 503 fails fast.
	client := llmclient.New(llmclient.WithHTTPClient(srv.Client()), llmclient.WithRetry(0, time.Millisecond, 1.5))
	e := NewEngine(client)

	a := Agent{Name: "bare", Invocation: llmclient.Invocation{BaseURL: srv.URL, APIKeyEnv: "ATCR_TEST_KEY", Model: "m"}}
	r := e.invokeAgent(context.Background(), a)

	require.Equal(t, StatusFailed, r.Status, "without an override the zero-budget client fails on the first 503")
	assert.Equal(t, int32(1), calls.Load(), "exactly one attempt")
}
