package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolSig_CanonicalizesKeyOrder(t *testing.T) {
	a := llmclient.ToolCall{Function: llmclient.FunctionCall{Name: "read", Arguments: json.RawMessage(`{"b":2,"a":1}`)}}
	b := llmclient.ToolCall{Function: llmclient.FunctionCall{Name: "read", Arguments: json.RawMessage(`{"a":1,"b":2}`)}}
	assert.Equal(t, toolSig(a), toolSig(b), "semantically identical object arguments with reordered keys must produce the same signature")
}

func TestToolSig_CanonicalizesWhitespace(t *testing.T) {
	a := llmclient.ToolCall{Function: llmclient.FunctionCall{Name: "read", Arguments: json.RawMessage(`{"a":1,"b":2}`)}}
	b := llmclient.ToolCall{Function: llmclient.FunctionCall{Name: "read", Arguments: json.RawMessage("{\"a\": 1,\n \"b\": 2}")}}
	assert.Equal(t, toolSig(a), toolSig(b), "semantically identical arguments with differing whitespace must produce the same signature")
}

func TestToolSig_FallsBackForInvalidJSON(t *testing.T) {
	raw := json.RawMessage(`not json`)
	a := llmclient.ToolCall{Function: llmclient.FunctionCall{Name: "read", Arguments: raw}}
	assert.Equal(t, "read\x00not json", toolSig(a), "invalid JSON args must fall back to raw bytes")
}

// runWireToolLoop drives the real toolLoop through a real llmclient.Client
// against a server that answers three tool-call turns, so MaxTurns=3 trips and
// the loop sends a fourth, forced-final no-tools request. It returns every
// captured request body in turn order.
func runWireToolLoop(t *testing.T, responseFormat string) []string {
	t.Helper()
	var (
		mu     sync.Mutex
		bodies []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		turn := len(bodies)
		mu.Unlock()
		if turn <= 3 {
			_, _ = fmt.Fprintf(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,`+
				`"tool_calls":[{"id":"c%d","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f%d.go\"}"}}]}}]}`, turn, turn)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"NO FINDINGS"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("ATCR_TEST_KEY", "k")

	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}
	a := toolAgent("a", 3, 0)
	a.Invocation = llmclient.Invocation{BaseURL: srv.URL, APIKeyEnv: "ATCR_TEST_KEY", Model: "m", ResponseFormat: responseFormat}

	r := toolEngine(llmclient.New(llmclient.WithHTTPClient(srv.Client())), d).invokeAgent(context.Background(), a)
	require.Equal(t, StatusOK, r.Status)
	require.Equal(t, []string{budgetMaxTurns}, r.TrippedBudgets, "the loop must reach the forced-final turn")
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 4, "three tool turns plus one forced-final turn")
	assert.NotContains(t, bodies[3], `"tools"`, "the forced-final turn carries no tools")
	return append([]string(nil), bodies...)
}

// AC 02-02 Scenarios 1-2 / Edge Case 1: a declared agent sends response_format
// on the first, middle, and forced-final tool-loop turn.
func TestToolLoop_ResponseFormatOnEveryTurn(t *testing.T) {
	for i, body := range runWireToolLoop(t, "json_object") {
		assert.Contains(t, body, `"response_format":{"type":"json_object"}`, "turn %d", i+1)
	}
}

// AC 02-02 Edge Case 2: an unset agent's tool loop never sends response_format.
func TestToolLoop_ResponseFormatAbsentWhenUnset(t *testing.T) {
	for i, body := range runWireToolLoop(t, "") {
		assert.NotContains(t, body, "response_format", "turn %d", i+1)
	}
}

// Sprint 35.16.11.2.2 (AC 04 story, TD-013): a declared thinking setting rides
// every tool-loop turn, and a turn-1 reply's reasoning_content is never re-sent
// in the turn-2 history — reasoning rides the response only, never a Message.
func TestToolLoop_ThinkingOnEveryTurnAndReasoningNeverResent(t *testing.T) {
	var (
		mu     sync.Mutex
		bodies []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		turn := len(bodies)
		mu.Unlock()
		if turn == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,`+
				`"reasoning_content":"PRIVATE-CHAIN-OF-THOUGHT",`+
				`"tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f.go\"}"}}]}}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"NO FINDINGS"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("ATCR_TEST_KEY", "k")

	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}
	a := toolAgent("a", 3, 0)
	a.Invocation = llmclient.Invocation{BaseURL: srv.URL, APIKeyEnv: "ATCR_TEST_KEY", Model: "m",
		Thinking: "off", ThinkingStyle: "qwen"}

	r := toolEngine(llmclient.New(llmclient.WithHTTPClient(srv.Client())), d).invokeAgent(context.Background(), a)
	require.Equal(t, StatusOK, r.Status)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 2, "one tool turn, then the final answer")
	for i, body := range bodies {
		assert.Contains(t, body, `"enable_thinking":false`, "turn %d", i+1)
	}
	assert.NotContains(t, bodies[1], "PRIVATE-CHAIN-OF-THOUGHT", "turn-1 reasoning must not be re-sent")
	assert.NotContains(t, bodies[1], "reasoning_content")
}
