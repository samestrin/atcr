package llmclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atcrerrors "github.com/samestrin/atcr/internal/errors"
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

func TestComplete_FallsBackToReasoningWhenContentEmpty(t *testing.T) {
	// A reasoning model that exhausts its output budget mid-thought returns an
	// empty content with the chain-of-thought in reasoning_content. The reviewer
	// must salvage the reasoning (its draft findings are recoverable downstream)
	// rather than contribute an empty review.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := chatResponse{}
		resp.Choices = append(resp.Choices, struct {
			Message message `json:"message"`
		}{Message: message{Role: "assistant", Content: "", ReasoningContent: "HIGH|a.go:1|bug|fix|correctness|5|evidence|greta"}})
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	out, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "review this",
	})
	require.NoError(t, err)
	assert.Contains(t, out, "HIGH|a.go:1|bug")
}

func TestComplete_ContentWinsOverReasoning(t *testing.T) {
	// When both arrive, visible content is authoritative and the reasoning
	// chain-of-thought must not leak into the returned review.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := chatResponse{}
		resp.Choices = append(resp.Choices, struct {
			Message message `json:"message"`
		}{Message: message{Role: "assistant", Content: "CLEAN REVIEW", ReasoningContent: "private thoughts"}})
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	out, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "review this",
	})
	require.NoError(t, err)
	assert.Equal(t, "CLEAN REVIEW", out)
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

func TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens(t *testing.T) {
	// A provider that echoes a DIFFERENT bearer/sk- shaped token (not the
	// configured key) must still have it scrubbed; exact-match alone would leak
	// it into HTTPStatusError.Snippet.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"upstream rejected Bearer sk-OTHER-leaked-99"}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sk-OTHER-leaked-99")
	assert.Contains(t, err.Error(), "[redacted]")
}

func TestComplete_ErrorBodyRedactsURLEncodedKey(t *testing.T) {
	// A key echoed URL-encoded will not match the literal-substring scrub; the
	// encoded form must be scrubbed too.
	const specialKey = "tok-secret/with+special=chars"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"echo `+url.QueryEscape(specialKey)+`"}`)
	}))
	defer srv.Close()
	t.Setenv("SPECIAL_KEY", specialKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "SPECIAL_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), url.QueryEscape(specialKey))
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

// --- Epic 1.2 Task 2: structured HTTPStatusError + MaxTokens ---

func TestComplete_HTTPStatusErrorSurfacedForClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"message":"model not found"}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)

	var se *HTTPStatusError
	require.True(t, errors.As(err, &se), "callers must be able to classify by status via errors.As")
	assert.Equal(t, http.StatusNotFound, se.Status)
	assert.Contains(t, se.Snippet, "model not found")
	// Error() text contract preserved for existing callers.
	assert.Contains(t, err.Error(), "HTTP 404")
	assert.Contains(t, err.Error(), "model not found")
}

func TestComplete_HTTPStatusErrorSurfacedThroughExhaustedRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"upstream down"}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)

	var se *HTTPStatusError
	require.True(t, errors.As(err, &se), "errors.As must unwrap through the exhausted-retries wrapper")
	assert.Equal(t, http.StatusServiceUnavailable, se.Status)
}

// --- Epic 4.0 Phase 4.3: ClassifiedError taxonomy (AC11, AC12) ---

// TestComplete_TransientError_IsRetryable verifies a 503 exhausted through the
// retry budget is classified Transient so IsRetryable returns true (AC11, AC12).
func TestComplete_TransientError_IsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"upstream down"}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.True(t, atcrerrors.IsRetryable(err), "exhausted 503 must classify as retryable transient")
}

// TestComplete_PermanentError_NotRetryable verifies a 404 is classified
// Permanent so IsRetryable returns false (AC11, AC12).
func TestComplete_PermanentError_NotRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"message":"model not found"}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.False(t, atcrerrors.IsRetryable(err), "404 permanent must not be retryable")
}

// TestComplete_TransientError_ErrorsAsHTTPStatusError verifies the ClassifiedError
// transient wrapper does not break errors.As reachability to *HTTPStatusError
// through the exhausted-retries wrapper (AC11).
func TestComplete_TransientError_ErrorsAsHTTPStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"upstream down"}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)

	var se *HTTPStatusError
	require.True(t, errors.As(err, &se), "errors.As must reach *HTTPStatusError through the ClassifiedError wrapper")
	assert.Equal(t, http.StatusServiceUnavailable, se.Status)
}

// TestComplete_PermanentError_ErrorsAsHTTPStatusError verifies the ClassifiedError
// permanent wrapper keeps *HTTPStatusError errors.As-reachable (AC11, AC12).
func TestComplete_PermanentError_ErrorsAsHTTPStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"message":"model not found"}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)

	var se *HTTPStatusError
	require.True(t, errors.As(err, &se), "errors.As must reach *HTTPStatusError through the ClassifiedError wrapper")
	assert.Equal(t, http.StatusNotFound, se.Status)
}

// TestComplete_TransportError_IsRetryable verifies a transport-level failure
// (connection dropped on every attempt) exhausts the budget and classifies as
// transient (AC11, AC12).
func TestComplete_TransportError_IsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Drop the connection mid-exchange on every attempt so the client only
		// ever sees a transport-level error, never an HTTP status.
		hj, ok := w.(http.Hijacker)
		require.True(t, ok, "test server must support hijacking")
		conn, _, err := hj.Hijack()
		require.NoError(t, err)
		_ = conn.Close()
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exhausted retries")
	assert.True(t, atcrerrors.IsRetryable(err), "exhausted transport failure must classify as retryable transient")
}

// TestComplete_ContextDeadline_NotWrapped verifies a context deadline is its own
// sentinel: it is NOT wrapped in ClassifiedError (so errors.Is still reaches
// context.DeadlineExceeded and IsRetryable does not misclassify it as transient).
func TestComplete_ContextDeadline_NotWrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		okResponse(w, "too slow")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := fastRetry(srv.Client()).Complete(ctx, Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "deadline sentinel must remain reachable")

	var ce *atcrerrors.ClassifiedError
	assert.False(t, errors.As(err, &ce), "context deadline must not be wrapped in ClassifiedError")
	assert.False(t, atcrerrors.IsRetryable(err), "an unwrapped deadline is not a transient classification")
}

func TestComplete_MaxTokensIncludedWhenSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), `"max_tokens":2048`)
		okResponse(w, "x")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	mt := 2048
	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m", MaxTokens: &mt,
	})
	require.NoError(t, err)
}

func TestComplete_MaxTokensOmittedWhenNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.NotContains(t, string(body), "max_tokens")
		okResponse(w, "x")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.NoError(t, err)
}

// --- Epic 3.3 Phase 1: provider usage decoding + cost computation ---

// usageResponse writes a chat-completions response that includes a provider
// `usage` block, as a raw JSON map so the test does not depend on the internal
// response struct carrying a Usage field.
func usageResponse(w http.ResponseWriter, content string, promptTokens, completionTokens int) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{
			map[string]any{"message": map[string]any{"role": "assistant", "content": content}},
		},
		"usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
		},
	})
}

// usageChatResponse writes a tool-capable chat response including a usage block.
func usageChatResponse(w http.ResponseWriter, content string, promptTokens, completionTokens int) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{
			map[string]any{
				"finish_reason": "stop",
				"message":       map[string]any{"role": "assistant", "content": content},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
		},
	})
}

func TestCompleteWithUsage_TokensFromUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		usageResponse(w, "findings here", 14200, 4000)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	out, usage, _, err := fastRetry(srv.Client()).CompleteWithUsage(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "review this",
	})
	require.NoError(t, err)
	assert.Equal(t, "findings here", out)
	assert.Equal(t, 14200, usage.PromptTokens)
	assert.Equal(t, 4000, usage.CompletionTokens)
}

func TestCompleteWithUsage_AbsentUsageIsZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// No usage block at all — provider omitted it entirely.
		okResponse(w, "no usage here")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	out, usage, _, err := fastRetry(srv.Client()).CompleteWithUsage(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.NoError(t, err)
	assert.Equal(t, "no usage here", out)
	assert.Equal(t, 0, usage.PromptTokens)
	assert.Equal(t, 0, usage.CompletionTokens)
}

func TestChat_TokensFromUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		usageChatResponse(w, "reviewed", 8000, 1500)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	content := "review"
	resp, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	}, []Message{{Role: "user", Content: &content}}, nil)
	require.NoError(t, err)
	assert.Equal(t, 8000, resp.Usage.PromptTokens)
	assert.Equal(t, 1500, resp.Usage.CompletionTokens)
}

func TestClampNonNegative_OverflowClampsToMaxInt(t *testing.T) {
	// 1e20 is a valid finite float64 but exceeds math.MaxInt; int(v) without
	// a cap overflows to implementation-defined garbage (typically MinInt64).
	// A genuinely large but valid count must clamp to the ceiling, not collapse
	// to 0 (which would mask a real request as free).
	assert.Equal(t, math.MaxInt, clampNonNegative(json.Number("1e20")))
}

func TestClampNonNegative_LargeValidCountPreserved(t *testing.T) {
	// 1e16 is above the old >1e15 guard yet well within int64 range; it must pass
	// through, not be discarded. The previous guard returned 0 for in-range counts,
	// reporting a real token count as zero (a free request) instead of preserving it.
	assert.Equal(t, 10_000_000_000_000_000, clampNonNegative(json.Number("1e16")))
}

func TestClampNonNegative_InfReturnsZero(t *testing.T) {
	// strconv.ParseFloat("Inf") returns (+Inf, nil) so the v<0 guard alone
	// does not protect int(v) from implementation-defined behaviour.
	assert.Equal(t, 0, clampNonNegative(json.Number("Inf")))
}

func TestClampNonNegative_NaNReturnsZero(t *testing.T) {
	// strconv.ParseFloat("NaN") returns (NaN, nil); all NaN comparisons are
	// false so v<0 does not catch it.
	assert.Equal(t, 0, clampNonNegative(json.Number("NaN")))
}

func TestComputeCostUSD_KnownModel(t *testing.T) {
	// claude-sonnet-4-6 is a known model in the rate table ($3/M in, $15/M out).
	// 1M input + 1M output tokens => 3.00 + 15.00 = 18.00 USD.
	cost := ComputeCostUSD("claude-sonnet-4-6", 1_000_000, 1_000_000)
	assert.InDelta(t, 18.0, cost, 1e-9)
}

func TestComputeCostUSD_NormalizesVariantSuffix(t *testing.T) {
	// A trailing [...] variant marker (e.g. the 1M-context tag) decorates the
	// same priced model; it must normalize to the bare key, not miss the table.
	got := ComputeCostUSD("claude-opus-4-8[1m]", 1_000_000, 1_000_000)
	want := ComputeCostUSD("claude-opus-4-8", 1_000_000, 1_000_000)
	assert.InDelta(t, want, got, 1e-9)
	assert.Greater(t, got, 0.0)
}

func TestComputeCostUSD_NormalizesProviderPrefix(t *testing.T) {
	// OpenRouter-style provider prefix: anthropic/claude-sonnet-4-6 prices as
	// claude-sonnet-4-6 ($3/M in).
	got := ComputeCostUSD("anthropic/claude-sonnet-4-6", 1_000_000, 0)
	assert.InDelta(t, 3.0, got, 1e-9)
}

func TestComputeCostUSD_NormalizesBedrockPrefix(t *testing.T) {
	// Bedrock-style region.provider prefix: us.anthropic.claude-sonnet-4-6.
	got := ComputeCostUSD("us.anthropic.claude-sonnet-4-6", 1_000_000, 0)
	assert.InDelta(t, 3.0, got, 1e-9)
}

func TestComputeCostUSD_NormalizationDoesNotInventPrices(t *testing.T) {
	// Normalization must not turn a genuinely unknown model into a priced one.
	assert.Equal(t, 0.0, ComputeCostUSD("anthropic/totally-unknown-xyz", 1000, 1000))
	assert.Equal(t, 0.0, ComputeCostUSD("claude-not-real-9-9[1m]", 1000, 1000))
}

func TestComputeCostUSD_UnknownModel(t *testing.T) {
	// Unknown model must yield zero cost, never panic.
	assert.Equal(t, 0.0, ComputeCostUSD("totally-unknown-model-xyz", 1000, 1000))
}

func TestComputeCostUSD_EmptyModel(t *testing.T) {
	// Empty model string must behave as unknown: zero, no panic.
	assert.Equal(t, 0.0, ComputeCostUSD("", 1000, 1000))
}

func TestComputeCostUSD_NegativeTokensClamped(t *testing.T) {
	// Negative token counts (malformed provider response) clamp to zero, so cost
	// is never negative.
	assert.Equal(t, 0.0, ComputeCostUSD("claude-sonnet-4-6", -100, -200))
}

func TestUsageData_CostUSD(t *testing.T) {
	// The convenience method maps prompt/completion to in/out exactly once, so
	// callers cannot transpose the two arguments.
	u := UsageData{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}
	assert.InDelta(t, 18.0, u.CostUSD("claude-sonnet-4-6"), 1e-9)
	assert.Equal(t, 0.0, UsageData{}.CostUSD("claude-sonnet-4-6"))
}

func TestUsageData_NegativeCountsClampedAtDecode(t *testing.T) {
	// A negative provider count is clamped to zero at the data boundary, so every
	// consumer of UsageData (not just ComputeCostUSD) sees non-negative counts.
	var u UsageData
	err := json.Unmarshal([]byte(`{"prompt_tokens":-5,"completion_tokens":10}`), &u)
	require.NoError(t, err)
	assert.Equal(t, 0, u.PromptTokens)
	assert.Equal(t, 10, u.CompletionTokens)
}

func TestCompleteWithUsage_PartialUsage(t *testing.T) {
	// Provider sends only prompt_tokens; completion_tokens defaults to zero.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": "x"}},
			},
			"usage": map[string]any{"prompt_tokens": 500},
		})
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, usage, _, err := fastRetry(srv.Client()).CompleteWithUsage(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.NoError(t, err)
	assert.Equal(t, 500, usage.PromptTokens)
	assert.Equal(t, 0, usage.CompletionTokens)
}

func TestCompleteWithUsage_FloatUsageDoesNotFailDecode(t *testing.T) {
	// Some gateways emit token counts as JSON floats (e.g. 14200.0). The usage
	// block is non-load-bearing metadata; a float must NOT fail the whole
	// response decode and discard the assistant content.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"findings"}}],"usage":{"prompt_tokens":14200.0,"completion_tokens":4000.0}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	out, usage, _, err := fastRetry(srv.Client()).CompleteWithUsage(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.NoError(t, err)
	assert.Equal(t, "findings", out)
	assert.Equal(t, 14200, usage.PromptTokens)
	assert.Equal(t, 4000, usage.CompletionTokens)
}

func TestUsageData_PartialMalformedIndependentDecode(t *testing.T) {
	// When prompt_tokens is valid but completion_tokens is malformed, the valid
	// field should survive — only the bad one degrades to zero. The current
	// atomic struct unmarshal drops BOTH; independent field decoding fixes this.
	var u UsageData
	err := json.Unmarshal([]byte(`{"prompt_tokens":500,"completion_tokens":"oops"}`), &u)
	require.NoError(t, err)
	assert.Equal(t, 500, u.PromptTokens, "valid prompt_tokens should survive malformed completion_tokens")
	assert.Equal(t, 0, u.CompletionTokens, "malformed completion_tokens degrades to zero")
}

func TestCompleteWithUsage_MalformedUsageDegradesToZero(t *testing.T) {
	// A structurally wrong usage block (string instead of number) must not kill
	// the call; usage degrades to zero and the content still returns.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":"oops"}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	out, usage, _, err := fastRetry(srv.Client()).CompleteWithUsage(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", out)
	assert.Equal(t, 0, usage.PromptTokens)
	assert.Equal(t, 0, usage.CompletionTokens)
}

// unboundedReader yields up to `remaining` bytes and counts how many were read.
type unboundedReader struct {
	remaining int
	read      int
}

func (u *unboundedReader) Read(p []byte) (int, error) {
	if u.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > u.remaining {
		n = u.remaining
	}
	u.remaining -= n
	u.read += n
	return n, nil
}

func TestReadErrorSnippet_DrainIsBounded(t *testing.T) {
	// A hostile endpoint streaming a huge error body must not be read in full on
	// the error path: the snippet read plus the connection-reuse drain are both
	// bounded.
	r := &unboundedReader{remaining: 64 << 20}
	_ = readErrorSnippet(r)
	assert.LessOrEqual(t, r.read, maxErrorBodyBytes*2, "error-body read (snippet + drain) must be bounded")
}

func TestWithHTTPClient_PreservesNoRedirectGuard(t *testing.T) {
	// A client injected via WithHTTPClient must still refuse to follow redirects,
	// so the Authorization: Bearer header is never forwarded to a redirect target.
	var gotAuthAtTarget atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/target", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			gotAuthAtTarget.Store(true)
		}
		okResponse(w, "leaked")
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/target", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	// srv.Client() follows redirects by default; WithHTTPClient must re-apply the
	// no-redirect guard onto it.
	c := New(WithHTTPClient(srv.Client()), WithRetry(0, time.Millisecond, 1.5))
	_, err := c.Complete(context.Background(), Invocation{
		BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err, "a 302 must be a hard failure, not a followed redirect")
	assert.False(t, gotAuthAtTarget.Load(), "Authorization must not be forwarded to redirect target")
}

func TestCompleteWithUsage_EmptyCompletionReturnsError(t *testing.T) {
	// When both content and reasoning_content are empty, the call must fail
	// loudly so callers do not propagate an empty review as success.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := chatResponse{}
		resp.Choices = append(resp.Choices, struct {
			Message message `json:"message"`
		}{Message: message{Role: "assistant", Content: "", ReasoningContent: ""}})
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	out, _, _, err := fastRetry(srv.Client()).CompleteWithUsage(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.Error(t, err)
	assert.Empty(t, out)
	assert.Contains(t, err.Error(), "empty completion")
	assert.False(t, atcrerrors.IsRetryable(err), "an empty completion must not be retryable")
}

func TestWithRetry_NegativeMaxRetriesClampedToSingleAttempt(t *testing.T) {
	// A negative WithRetry budget must not produce a zero-attempt loop that
	// falls through to "exhausted retries" wrapping a nil cause; it clamps to a
	// single attempt.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		okResponse(w, "ok")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	c := New(WithHTTPClient(srv.Client()), WithRetry(-1, time.Millisecond, 1.5))
	out, err := c.Complete(context.Background(), Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"})
	require.NoError(t, err)
	assert.Equal(t, "ok", out)
	assert.Equal(t, int32(1), calls.Load(), "negative maxRetries must clamp to a single attempt")
}

func TestClampBackoff_BoundsGrowth(t *testing.T) {
	assert.Equal(t, maxBackoff, clampBackoff(maxBackoff+time.Hour))
	assert.Equal(t, 5*time.Second, clampBackoff(5*time.Second))
}

func TestJitter_BoundedBelowFull(t *testing.T) {
	d := 100 * time.Millisecond
	for i := 0; i < 200; i++ {
		j := jitter(d)
		assert.GreaterOrEqual(t, j, d/2)
		assert.Less(t, j, d)
	}
}

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"delta-seconds", "5", 5 * time.Second},
		{"zero", "0", 0},
		{"negative", "-3", 0},
		{"empty", "", 0},
		{"garbage", "soon", 0},
		{"whitespace-padded", "  2 ", 2 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, parseRetryAfter(c.value))
		})
	}
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	// An HTTP-date in the future yields a positive delay; a past date yields 0.
	future := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(future)
	assert.Greater(t, got, time.Duration(0))
	assert.LessOrEqual(t, got, 30*time.Second)

	past := time.Now().Add(-30 * time.Second).UTC().Format(http.TimeFormat)
	assert.Equal(t, time.Duration(0), parseRetryAfter(past))
}

func TestParseRetryAfter_CapsExcessiveAndOverflowValues(t *testing.T) {
	const ceiling = 5 * time.Minute

	// A large but well-formed delta-seconds (1 hour) must be clamped so a hostile
	// or misconfigured endpoint cannot stall a worker far beyond a sane cooldown.
	got := parseRetryAfter("3600")
	assert.Greater(t, got, time.Duration(0))
	assert.LessOrEqual(t, got, ceiling, "an hour-long Retry-After must be capped")

	// A delta-seconds value that would overflow time.Duration when multiplied by
	// time.Second must not wrap to a non-positive/garbage delay.
	got = parseRetryAfter("999999999999999999")
	assert.Greater(t, got, time.Duration(0), "overflowing Retry-After must not produce a non-positive delay")
	assert.LessOrEqual(t, got, ceiling, "overflowing Retry-After must be capped")

	// A far-future HTTP-date is likewise capped.
	future := time.Now().Add(1 * time.Hour).UTC().Format(http.TimeFormat)
	got = parseRetryAfter(future)
	assert.Greater(t, got, time.Duration(0))
	assert.LessOrEqual(t, got, ceiling, "a far-future Retry-After date must be capped")
}

func TestComplete_HonorsRetryAfterHeader(t *testing.T) {
	// A 429 advertising Retry-After must override the (tiny) fixed backoff: the
	// client must wait at least the advertised cooldown before retrying.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		okResponse(w, "recovered")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	// initialBackoff is 1ms; without honoring Retry-After the retry fires almost
	// immediately, so an elapsed >= ~1s proves the header was honored.
	c := New(WithHTTPClient(srv.Client()), WithRetry(2, time.Millisecond, 1.5))
	start := time.Now()
	out, err := c.Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.Equal(t, "recovered", out)
	assert.GreaterOrEqual(t, elapsed, 900*time.Millisecond, "Retry-After cooldown not honored")
}

func TestRedactErrorSnippet_SKKeyCaseInsensitive(t *testing.T) {
	// skKeyPattern must match upper/mixed-case sk- tokens, mirroring the
	// case-insensitive scrub in internal/log/redact.go. A SK-/Sk- shaped foreign
	// token echoed in a provider error body must not bypass the scrub.
	got := redactErrorSnippet("upstream rejected SK-ABC123 and Sk-Def456", "")
	assert.NotContains(t, got, "SK-ABC123")
	assert.NotContains(t, got, "Sk-Def456")
	assert.Contains(t, got, "[redacted]")
}

func TestRedactErrorSnippet_ForeignFleetKeyPrefixes(t *testing.T) {
	// Other OpenAI-compatible fleet providers use distinct key prefixes (Google
	// AIza..., Groq gsk_..., xAI xai-...). A provider echoing one of these into its
	// JSON error body must have it scrubbed, just like sk-/Bearer tokens, so it
	// never reaches HTTPStatusError.Snippet.
	got := redactErrorSnippet("rejected AIzaSyD-FAKEkey_123 gsk_FAKEgroqKEY456 xai-FAKExaiKEY789", "")
	assert.NotContains(t, got, "AIzaSyD-FAKEkey_123")
	assert.NotContains(t, got, "gsk_FAKEgroqKEY456")
	assert.NotContains(t, got, "xai-FAKExaiKEY789")
	assert.Contains(t, got, "[redacted]")
}
