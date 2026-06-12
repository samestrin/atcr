package llmclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testKey = "sk-secret-value-123"

// okResponse writes a minimal valid chat-completions response.
func okResponse(w http.ResponseWriter, content string) {
	resp := chatResponse{}
	resp.Choices = append(resp.Choices, struct {
		Message message `json:"message"`
	}{Message: message{Role: "assistant", Content: content}})
	_ = json.NewEncoder(w).Encode(resp)
}

// fastRetry builds a client whose backoff is negligible so tests do not sleep.
func fastRetry(h *http.Client) *Client {
	return New(WithHTTPClient(h), WithRetry(2, time.Millisecond, 1.5))
}

func TestComplete_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer "+testKey, r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), `"model":"m1"`)
		okResponse(w, "findings here")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	c := fastRetry(srv.Client())
	out, err := c.Complete(context.Background(), Invocation{
		BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "review this",
	})
	require.NoError(t, err)
	assert.Equal(t, "findings here", out)
}

func TestComplete_RetryOn503ThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		okResponse(w, "ok after retries")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	out, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.NoError(t, err)
	assert.Equal(t, "ok after retries", out)
	assert.Equal(t, int32(3), calls.Load()) // 1 + 2 retries
}

func TestComplete_RetryOn429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		okResponse(w, "recovered")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	out, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.NoError(t, err)
	assert.Equal(t, "recovered", out)
}

func TestComplete_RetryOnTransportError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			// Drop the connection mid-exchange so the client sees a
			// transport-level error rather than an HTTP status.
			hj, ok := w.(http.Hijacker)
			require.True(t, ok, "test server must support hijacking")
			conn, _, err := hj.Hijack()
			require.NoError(t, err)
			_ = conn.Close()
			return
		}
		okResponse(w, "recovered from transport error")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	out, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.NoError(t, err)
	assert.Equal(t, "recovered from transport error", out)
	assert.Equal(t, int32(2), calls.Load()) // 1 dropped + 1 retry
}

func TestComplete_4xxFailsImmediately(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 400")
	assert.Equal(t, int32(1), calls.Load()) // no retries on 4xx
}

func TestComplete_RetriesExhausted(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exhausted retries")
	assert.Equal(t, int32(3), calls.Load()) // 1 + 2 retries, all 502
}

func TestComplete_CancelDuringBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	// Large backoff so the second attempt waits; cancel during that wait.
	c := New(WithHTTPClient(srv.Client()), WithRetry(2, time.Hour, 1.5))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := c.Complete(ctx, Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"})
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), time.Second, "backoff must abort promptly on cancel")
}

func TestComplete_APIKeyNotSet(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		okResponse(w, "x")
	}))
	defer srv.Close()
	// TEST_KEY deliberately unset.

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "DEFINITELY_UNSET_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not set")
	assert.Equal(t, int32(0), calls.Load()) // no request made without a key
}

func TestComplete_KeyNeverLogged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), testKey)
}

func TestComplete_MalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "{not valid json")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse response")
}

func TestComplete_Temperature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), `"temperature":0.3`)
		okResponse(w, "x")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	temp := 0.3
	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m", Temperature: &temp,
	})
	require.NoError(t, err)
}

func TestComplete_ErrorBodySnippetIncluded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"model not found"}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 400")
	assert.Contains(t, err.Error(), "model not found")
}

func TestComplete_ErrorBodySnippetOnExhaustedRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "{\"error\":\n  {\"message\": \"quota exhausted\"}}")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exhausted retries")
	assert.Contains(t, err.Error(), "HTTP 503")
	// Multi-line body must be collapsed to a single sanitized line.
	assert.Contains(t, err.Error(), `{"error": {"message": "quota exhausted"}}`)
	assert.NotContains(t, err.Error(), "\n")
}

func TestComplete_ErrorBodySnippetNeverEchoesKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"bad token: `+r.Header.Get("Authorization")+`"}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad token")
	assert.NotContains(t, err.Error(), testKey)
}

func TestComplete_ErrorBodySnippetBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, strings.Repeat("x", 8<<10))
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.Less(t, len(err.Error()), (4<<10)+200, "snippet must be bounded")
}

func TestComplete_OversizedResponseRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Leading whitespace is valid JSON padding, so the decoder keeps
		// reading until it crosses the size cap before the real payload.
		_, _ = io.WriteString(w, strings.Repeat(" ", 32<<20+1))
		okResponse(w, "x")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestComplete_UserinfoStrippedFromErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		okResponse(w, "x")
	}))
	// Close immediately so every attempt fails at the transport level with the
	// endpoint URL embedded in the error.
	srv.Close()
	t.Setenv("TEST_KEY", testKey)

	base := strings.Replace(srv.URL, "http://", "http://leakuser:leakpass@", 1)
	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: base, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "leakuser")
	assert.NotContains(t, err.Error(), "leakpass")
}

func TestComplete_ContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		okResponse(w, "too slow")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := fastRetry(srv.Client()).Complete(ctx, Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 80*time.Millisecond, "must return before the 100ms server sleep")
}
