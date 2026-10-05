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

// The narrow Complete/CompleteWithUsage paths carry NO Salvaged flag, so returning
// the promoted chain-of-thought as content makes it indistinguishable from a real
// answer — a wrapper that forgets CompleteWithMeta silently re-enables parsing
// abandoned reasoning as findings, and in the executor lane as a patch written to
// disk. The fix is to refuse the salvage on the narrow path: returning an error is
// the only signal those two signatures can carry (TD internal/llmclient/client.go:415).
func TestComplete_RefusesAReasoningSalvageOnTheNarrowPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(w, `{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"draft thought, no answer"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	c := fastRetry(srv.Client())
	inv := Invocation{BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "p"}

	_, err := c.Complete(context.Background(), inv)
	require.Error(t, err, "a stop-reason salvage must not be handed back as content on the narrow path")
	assert.ErrorIs(t, err, ErrSalvagedReply, "the refusal is a distinct, testable error")

	_, usage, records, cuErr := c.CompleteWithUsage(context.Background(), inv)
	require.Error(t, cuErr, "CompleteWithUsage cannot carry Salvaged either, so it must refuse too")
	assert.ErrorIs(t, cuErr, ErrSalvagedReply)
	assert.Equal(t, UsageData{}, usage, "the empty-UsageData-on-error contract still holds")
	assert.NotEmpty(t, records, "the call reached dispatch, so its CallRecords are still surfaced")
}

// The Meta path is where the marker lives, so it must STILL return the salvaged
// Completion — the fix refuses only the two narrow wrappers, not the signal itself.
func TestCompleteWithMeta_StillReturnsASalvageAfterTheNarrowPathRefusesIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(w, `{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"draft thought, no answer"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	c := fastRetry(srv.Client())
	comp, err := c.CompleteWithMeta(context.Background(), Invocation{BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "p"})
	require.NoError(t, err, "the Meta path owns the signal and must not refuse it")
	assert.True(t, comp.Salvaged)
	assert.Equal(t, "draft thought, no answer", comp.Content)
}

// A normal content-bearing reply is untouched by the refusal.
func TestComplete_ReturnsANormalReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(w, `{"choices":[{"message":{"role":"assistant","content":"a real review"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	c := fastRetry(srv.Client())
	content, err := c.Complete(context.Background(), Invocation{BaseURL: srv.URL + "/v1", APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "p"})
	require.NoError(t, err)
	assert.Equal(t, "a real review", content)
}
