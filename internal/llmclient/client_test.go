package llmclient

import (
	"context"
	"encoding/json"
	"errors"
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

	out, usage, err := fastRetry(srv.Client()).CompleteWithUsage(context.Background(), Invocation{
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

	out, usage, err := fastRetry(srv.Client()).CompleteWithUsage(context.Background(), Invocation{
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

func TestClampNonNegative_OverflowReturnsZero(t *testing.T) {
	// 1e20 is a valid finite float64 but exceeds math.MaxInt; int(v) without
	// a cap overflows to implementation-defined garbage (typically MinInt64).
	assert.Equal(t, 0, clampNonNegative(json.Number("1e20")))
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

	_, usage, err := fastRetry(srv.Client()).CompleteWithUsage(context.Background(), Invocation{
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

	out, usage, err := fastRetry(srv.Client()).CompleteWithUsage(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.NoError(t, err)
	assert.Equal(t, "findings", out)
	assert.Equal(t, 14200, usage.PromptTokens)
	assert.Equal(t, 4000, usage.CompletionTokens)
}

func TestCompleteWithUsage_MalformedUsageDegradesToZero(t *testing.T) {
	// A structurally wrong usage block (string instead of number) must not kill
	// the call; usage degrades to zero and the content still returns.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":"oops"}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	out, usage, err := fastRetry(srv.Client()).CompleteWithUsage(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", out)
	assert.Equal(t, 0, usage.PromptTokens)
	assert.Equal(t, 0, usage.CompletionTokens)
}
