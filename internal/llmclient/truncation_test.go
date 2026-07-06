package llmclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// respondJSON writes a raw chat-completions JSON body so these tests are
// independent of the decode struct's exact Go shape (they assert the wire
// contract: finish_reason=length ⇒ Truncated).
func respondJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func TestCompleteWithMeta_TruncatedOnLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(w, `{"choices":[{"message":{"role":"assistant","content":"partial ramble with no findings"},"finish_reason":"length"}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	c := fastRetry(srv.Client())
	comp, err := c.CompleteWithMeta(context.Background(), Invocation{
		BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "p",
	})
	require.NoError(t, err)
	assert.Equal(t, "partial ramble with no findings", comp.Content)
	assert.True(t, comp.Truncated, "finish_reason=length must set Truncated")
}

func TestCompleteWithMeta_NotTruncatedOnStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(w, `{"choices":[{"message":{"role":"assistant","content":"HIGH|a.go:1|bug|fix|correctness|5|ev|bruce"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	c := fastRetry(srv.Client())
	comp, err := c.CompleteWithMeta(context.Background(), Invocation{
		BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "p",
	})
	require.NoError(t, err)
	assert.False(t, comp.Truncated)
}

// TruncatedWithSalvagedReasoning is the exact runaway scenario: budget burned in
// the <think> block, finish_reason=length, content empty but reasoning_content
// carries the ramble. The salvage must still surface Truncated=true so callers
// cannot mistake the salvaged content for a clean completion.
func TestCompleteWithMeta_TruncatedWithSalvagedReasoning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(w, `{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"thinking... no findings emitted"},"finish_reason":"length"}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	c := fastRetry(srv.Client())
	comp, err := c.CompleteWithMeta(context.Background(), Invocation{
		BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "p",
	})
	require.NoError(t, err)
	assert.Equal(t, "thinking... no findings emitted", comp.Content)
	assert.True(t, comp.Truncated)
}

// CompleteWithUsage must keep its existing four-value contract after being
// refactored to delegate to CompleteWithMeta (regression guard for the mocks and
// callers that still use it).
func TestCompleteWithUsage_StillReturnsContentAndUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	c := fastRetry(srv.Client())
	content, usage, _, err := c.CompleteWithUsage(context.Background(), Invocation{
		BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "p",
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", content)
	assert.Equal(t, 10, usage.PromptTokens)
	assert.Equal(t, 5, usage.CompletionTokens)
}
