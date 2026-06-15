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

// TestFunctionCall_MarshalJSON_NormalizesRawObjectToString verifies that
// FunctionCall.MarshalJSON always emits arguments as a JSON-encoded string,
// even when the in-memory form is a raw JSON object (as some local providers
// return). This is the wire-canonical form strict OpenAI validators require.
func TestFunctionCall_MarshalJSON_NormalizesRawObjectToString(t *testing.T) {
	fc := FunctionCall{Name: "grep", Arguments: json.RawMessage(`{"pattern":"foo"}`)}
	b, err := json.Marshal(fc)
	require.NoError(t, err)
	// arguments must be a JSON string (first non-whitespace byte is '"')
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &raw))
	args := raw["arguments"]
	require.NotEmpty(t, args)
	assert.Equal(t, byte('"'), args[0], "arguments must be string-encoded in marshaled output")
	// decoding the string gives back the original JSON object
	var s string
	require.NoError(t, json.Unmarshal(args, &s))
	assert.JSONEq(t, `{"pattern":"foo"}`, s)
}

// TestChat_EchoedAssistantToolCallsHaveStringEncodedArguments verifies that
// when an assistant message carrying raw-object arguments is echoed back in a
// subsequent Chat request, the re-serialized arguments are string-encoded so
// strict OpenAI-compatible validators on the upstream endpoint accept the turn.
func TestChat_EchoedAssistantToolCallsHaveStringEncodedArguments(t *testing.T) {
	var gotBodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBodies = append(gotBodies, string(b))
		if len(gotBodies) == 1 {
			// First turn: provider emits arguments as a raw JSON object.
			_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"grep","arguments":{"pattern":"foo"}}}]}}]}`)
		} else {
			_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]}`)
		}
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	c := fastRetry(srv.Client())
	inv := Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1"}

	resp1, err := c.Chat(context.Background(), inv, nil, nil)
	require.NoError(t, err)
	require.Len(t, resp1.Message.ToolCalls, 1)

	// Echo the assistant message (with raw-object arguments) as history.
	_, err = c.Chat(context.Background(), inv, []Message{resp1.Message}, nil)
	require.NoError(t, err)
	require.Len(t, gotBodies, 2)
	// The echoed arguments in the second request must be string-encoded.
	assert.Contains(t, gotBodies[1], `"arguments":"{`, "echoed tool-call arguments must be JSON-string-encoded")
	assert.NotContains(t, gotBodies[1], `"arguments":{"`, "echoed tool-call arguments must not be raw JSON objects")
}

// TestChat_TruncatedFinishReasonWithEmptyContentReturnsError verifies that Chat
// surfaces finish_reason "length" or "content_filter" with empty content as an
// error rather than silently returning a successful empty review (StatusOK).
func TestChat_TruncatedFinishReasonWithEmptyContentReturnsError(t *testing.T) {
	for _, tc := range []struct {
		name         string
		finishReason string
	}{
		{"length", "length"},
		{"content_filter", "content_filter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"`+tc.finishReason+`","message":{"role":"assistant","content":null}}]}`)
			}))
			defer srv.Close()
			t.Setenv("TEST_KEY", testKey)

			_, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
				BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
			}, nil, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.finishReason)
		})
	}
}

// TestChat_StopFinishReasonWithContentSucceeds verifies that a normal "stop"
// response is unaffected by the truncation guard.
func TestChat_StopFinishReasonWithContentSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"review here"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	resp, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, resp.Message.Content)
	assert.Equal(t, "review here", *resp.Message.Content)
}

// TestChat_LengthFinishReasonWithToolCallsSucceeds verifies that a "length"-
// truncated response that still carries tool_calls is not treated as an error
// (the loop can still process the partial calls).
func TestChat_LengthFinishReasonWithToolCallsSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"grep","arguments":"{\"pattern\":\"x\"}"}}]}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	resp, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, nil, nil)
	require.NoError(t, err)
	assert.Len(t, resp.Message.ToolCalls, 1)
}

// TestChat_LengthFinishReasonWithPartialContentSetsTruncated verifies that a
// "length"-truncated response with non-empty content and no tool_calls sets
// ChatResponse.Truncated rather than silently returning as a clean stop.
func TestChat_LengthFinishReasonWithPartialContentSetsTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":"partial review"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	resp, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, resp.Message.Content)
	assert.Equal(t, "partial review", *resp.Message.Content)
	assert.True(t, resp.Truncated, "length finish_reason with partial content must set Truncated")
}

// TestChat_LengthFinishReasonWithToolCallsSetsTruncated verifies that a
// "length"-truncated response carrying tool_calls (possibly with truncated
// arguments) also sets ChatResponse.Truncated.
func TestChat_LengthFinishReasonWithToolCallsSetsTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"grep","arguments":"{\"pattern\":\"x\"}"}}]}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	resp, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, nil, nil)
	require.NoError(t, err)
	assert.Len(t, resp.Message.ToolCalls, 1)
	assert.True(t, resp.Truncated, "length finish_reason with tool_calls must set Truncated")
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
