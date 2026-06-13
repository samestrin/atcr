package llmclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readFileToolDef is a representative tool definition for wire-format tests.
func readFileToolDef() ToolDef {
	return ToolDef{
		Name:        "read_file",
		Description: "Read a file from the snapshot.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required":   []string{"path"},
		},
	}
}

// AC 01-01 Scenario 2: Chat request includes tool definitions in wire format.
func TestChat_RequestIncludesToolsArray(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	s := "review this"
	_, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, []Message{{Role: "user", Content: &s}}, []ToolDef{readFileToolDef()})
	require.NoError(t, err)

	assert.Contains(t, gotBody, `"tools"`)
	assert.Contains(t, gotBody, `"type":"function"`)
	assert.Contains(t, gotBody, `"name":"read_file"`)
	assert.Contains(t, gotBody, `"description":"Read a file from the snapshot."`)
	assert.Contains(t, gotBody, `"parameters"`)
	assert.Contains(t, gotBody, `"tool_choice":"auto"`)
}

// AC 01-01 Scenario 3: Chat response carries tool_calls parsed into ChatResponse.
func TestChat_ResponseCarriesToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"engine.go\"}"}}]}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	resp, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, nil, []ToolDef{readFileToolDef()})
	require.NoError(t, err)
	require.Len(t, resp.Message.ToolCalls, 1)
	assert.Equal(t, "call_1", resp.Message.ToolCalls[0].ID)
	assert.Equal(t, "read_file", resp.Message.ToolCalls[0].Function.Name)
	assert.Equal(t, "tool_calls", resp.FinishReason)
	// Arguments decode to the embedded JSON object regardless of string-encoding.
	assert.JSONEq(t, `{"path":"engine.go"}`, string(ToolCallArguments(resp.Message.ToolCalls[0])))
}

// AC 01-01 Scenario 3 (tolerance): some local providers emit arguments as a raw
// JSON object rather than a JSON-encoded string (spike 1.1 #2).
func TestChat_ResponseToleratesRawObjectArguments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"grep","arguments":{"pattern":"foo"}}}]}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	resp, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, resp.Message.ToolCalls, 1)
	assert.JSONEq(t, `{"pattern":"foo"}`, string(ToolCallArguments(resp.Message.ToolCalls[0])))
}

// AC 01-01 Scenario 4: role:tool messages serialize to the expected JSON shape.
func TestChat_RoleToolMessageSerialization(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	content := "   1| package fanout"
	msgs := []Message{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Type: "function", Function: FunctionCall{Name: "read_file", Arguments: json.RawMessage(`"{\"path\":\"x\"}"`)}}}},
		{Role: "tool", ToolCallID: "call_1", Content: &content},
	}
	_, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, msgs, nil)
	require.NoError(t, err)

	assert.Contains(t, gotBody, `"role":"tool"`)
	assert.Contains(t, gotBody, `"tool_call_id":"call_1"`)
	assert.Contains(t, gotBody, `"content":"   1| package fanout"`)
	// The assistant tool-call turn must echo content:null, not "".
	assert.Contains(t, gotBody, `"content":null`)
}

// AC 01-01 Edge Case 1: empty/nil tools slice is omitted from the request.
func TestChat_EmptyToolsOmitted(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	s := "hi"
	_, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, []Message{{Role: "user", Content: &s}}, nil)
	require.NoError(t, err)
	assert.NotContains(t, gotBody, `"tools"`)
	assert.NotContains(t, gotBody, `"tool_choice"`)
}

// AC 01-01 Edge Case 3: finish_reason tool_calls with empty tool_calls array.
func TestChat_ToolCallsFinishReasonEmptyArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":"never mind","tool_calls":[]}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	resp, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, resp.Message.ToolCalls)
	require.NotNil(t, resp.Message.Content)
	assert.Equal(t, "never mind", *resp.Message.Content)
}

// AC 01-01 Error Scenario 1: provider HTTP error surfaces as an error.
func TestChat_HTTPErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

// AC 01-01 Error Scenario 2: malformed response JSON surfaces a decode error.
func TestChat_MalformedResponseJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "parse")
}

// Backward-compat sanity: Complete still works after the shared-send refactor.
func TestChat_CompleteStillWorksAfterRefactor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		okResponse(w, "single shot ok")
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	out, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1", Prompt: "p",
	})
	require.NoError(t, err)
	assert.Equal(t, "single shot ok", out)
}
