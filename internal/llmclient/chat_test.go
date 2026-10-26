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

// Epic 36.0 T1: a message with only Content marshals byte-identically to the
// body TestChat_RoleToolMessageSerialization sends, pinned here in full.
func TestChat_ContentOnlyMessagesByteIdentical(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
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

	want := `{"model":"m1","messages":[` +
		`{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"x\"}"}}]},` +
		`{"role":"tool","content":"   1| package fanout","tool_call_id":"call_1"}]}`
	assert.Equal(t, want, string(gotBody))
}

// Epic 36.0 T1: a message with Parts marshals content as the parts array, a
// text part and an image_url part carrying a data: URL.
func TestMessage_PartsMarshalAsContentArray(t *testing.T) {
	msg := Message{Role: "user", Parts: []ContentPart{
		TextPart("what is on screen?"),
		ImageURLPart("data:image/png;base64,iVBORw0KGgo="),
	}}
	b, err := json.Marshal(msg)
	require.NoError(t, err)
	assert.Equal(t, `{"role":"user","content":[`+
		`{"type":"text","text":"what is on screen?"},`+
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}]}`, string(b))
}

// Epic 36.0 T1: marshaling a message with both Content and Parts set fails
// outside Chat too, rather than silently picking one.
func TestMessage_ContentAndPartsMarshalErrors(t *testing.T) {
	s := "text"
	_, err := json.Marshal(Message{Role: "user", Content: &s, Parts: []ContentPart{TextPart("text")}})
	assert.ErrorIs(t, err, ErrContentAndParts)
}

// Epic 36.0 T1: the parts array reaches the wire through Chat, and the other
// messages in the same request keep their plain-string content. The image
// needs SupportsVision since T3.
func TestChat_PartsReachRequestBody(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	sys := "you verify UIs"
	msgs := []Message{
		{Role: "system", Content: &sys},
		{Role: "user", Parts: []ContentPart{TextPart("look"), ImageURLPart("data:image/png;base64,AAAA")}},
	}
	_, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1", SupportsVision: true,
	}, msgs, nil)
	require.NoError(t, err)
	assert.Equal(t, `{"model":"m1","messages":[`+
		`{"role":"system","content":"you verify UIs"},`+
		`{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`,
		string(gotBody))
}

// Epic 36.0 T1: Content and Parts both set is ambiguous, so Chat refuses it
// before any request is sent.
func TestChat_ContentAndPartsBothSetErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	s := "text"
	resp, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, []Message{{Role: "system", Content: &s}, {Role: "user", Content: &s, Parts: []ContentPart{TextPart("text")}}}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrContentAndParts)
	// Chat's own guard names the offending message; MarshalJSON alone cannot.
	assert.Contains(t, err.Error(), "message 1:")
	assert.Nil(t, resp)
	assert.Zero(t, calls.Load(), "no request may be sent")
}

// Epic 36.0 T3: an image sent to an invocation not declared SupportsVision is
// refused with ErrVisionNotDeclared, and no request reaches the server.
func TestChat_ImageWithoutVisionRefused(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	sys := "you verify UIs"
	resp, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, []Message{
		{Role: "system", Content: &sys},
		{Role: "user", Parts: []ContentPart{TextPart("look"), ImageURLPart("data:image/png;base64,AAAA")}},
	}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrVisionNotDeclared)
	assert.Contains(t, err.Error(), "message 1:")
	assert.Nil(t, resp)
	assert.Zero(t, calls.Load(), "no request may be sent")
}

// Epic 36.0 T3: the vision refusal comes before key resolution, so an unset
// key env var does not mask it.
func TestChat_ImageWithoutVisionRefusedBeforeKey(t *testing.T) {
	_, err := fastRetry(http.DefaultClient).Chat(context.Background(), Invocation{
		BaseURL: "http://127.0.0.1:0", APIKeyEnv: "ATCR_T3_UNSET_KEY", Model: "m1",
	}, []Message{{Role: "user", Parts: []ContentPart{ImageURLPart("https://example.test/a.png")}}}, nil)
	assert.ErrorIs(t, err, ErrVisionNotDeclared)
}

// Epic 36.0 T3: with SupportsVision set, the image is sent in one request.
func TestChat_ImageWithVisionSent(t *testing.T) {
	var calls atomic.Int32
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1", SupportsVision: true,
	}, []Message{{Role: "user", Parts: []ContentPart{TextPart("look"), ImageURLPart("data:image/png;base64,AAAA")}}}, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load())
	assert.Contains(t, string(gotBody), `{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}`)
}

// Epic 36.0 T3: text-only parts carry no image, so an undeclared invocation
// still sends them.
func TestChat_TextPartsWithoutVisionSent(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	_, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, []Message{{Role: "user", Parts: []ContentPart{TextPart("look")}}}, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load())
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

// Sprint 35.16.11.2.2.1 AC 02-03 Edge Case 1: an assistant turn echoed as
// history re-sends its reasoning under the key it arrived in, byte-for-byte,
// and its user and tool neighbours carry none.
func TestChat_EchoedAssistantTurnResendsReasoningUnderArrivalKey(t *testing.T) {
	const blocks = `[{"type":"thinking","thinking":"check f.go","signature":"EqQBCkgIARABGAIiQL+/zzA0Xq9b=="}]`
	var gotBodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBodies = append(gotBodies, string(b))
		if len(gotBodies) == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,`+
				`"reasoning_content":"PRIVATE","thinking_blocks":`+blocks+`,`+
				`"tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{}"}}]}}]}`)
		} else {
			_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]}`)
		}
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	c := fastRetry(srv.Client())
	inv := Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1"}
	u, tr := "review", "package main"
	resp1, err := c.Chat(context.Background(), inv, []Message{{Role: "user", Content: &u}}, nil)
	require.NoError(t, err)
	assert.Equal(t, "PRIVATE", resp1.Reasoning, "the merged reasoning string is unchanged")

	history := []Message{{Role: "user", Content: &u}, resp1.Message, {Role: "tool", Content: &tr, ToolCallID: "c1"}}
	_, err = c.Chat(context.Background(), inv, history, nil)
	require.NoError(t, err)
	require.Len(t, gotBodies, 2)

	var req struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal([]byte(gotBodies[1]), &req))
	require.Len(t, req.Messages, 3)
	assert.Equal(t, `"PRIVATE"`, string(req.Messages[1]["reasoning_content"]))
	assert.Equal(t, blocks, string(req.Messages[1]["thinking_blocks"]))
	assert.NotContains(t, req.Messages[1], "reasoning", "never re-keyed under the alternate name")
	for _, i := range []int{0, 2} {
		for _, k := range []string{"reasoning_content", "reasoning", "reasoning_details", "thinking_blocks"} {
			assert.NotContains(t, req.Messages[i], k, "message %d must carry no reasoning", i)
		}
	}
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

// Epic 35.26 T1: a billed 200 with no choices still reports `usage`, and the
// errored turn returns it on a non-nil *ChatResponse alongside the error, with
// the wire attempt's CallRecords.
func TestChat_NoChoicesReturnsUsageWithError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":7}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	resp, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no choices returned")
	require.NotNil(t, resp)
	assert.Equal(t, 120, resp.Usage.PromptTokens)
	assert.Equal(t, 7, resp.Usage.CompletionTokens)
	assert.Len(t, resp.CallRecords, 1)
	assert.Empty(t, resp.Reasoning, "no message, so no reasoning")
}

// Epic 35.26 T1: a truncated empty turn errors, and returns the provider's
// usage and the salvaged reasoning under either key alongside the error.
func TestChat_TruncatedEmptyTurnReturnsUsageAndReasoningWithError(t *testing.T) {
	for _, tc := range []struct {
		name, finishReason, reasoning string
	}{
		{"length reasoning_content", "length", `"reasoning_content":"thought A"`},
		{"length reasoning", "length", `"reasoning":"thought A"`},
		{"content_filter reasoning_content", "content_filter", `"reasoning_content":"thought A"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"`+tc.finishReason+`","message":{"role":"assistant","content":null,`+tc.reasoning+`}}],`+
					`"usage":{"prompt_tokens":300,"completion_tokens":4096}}`)
			}))
			defer srv.Close()
			t.Setenv("TEST_KEY", testKey)

			resp, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
				BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
			}, nil, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "provider truncated response")
			require.NotNil(t, resp)
			assert.Equal(t, 300, resp.Usage.PromptTokens)
			assert.Equal(t, 4096, resp.Usage.CompletionTokens)
			assert.Equal(t, "thought A", resp.Reasoning)
			assert.Len(t, resp.CallRecords, 1)
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

// TestChat_LengthToolCallTurnClearsStructuredReasoning verifies that a
// "length"-truncated turn carrying tool_calls does not replay its structured
// reasoning members: a cut-off thinking_blocks value (for Anthropic, a block
// with no signature) is neither the blocks the provider signed nor absent, and
// history() would otherwise re-send it on every later turn.
func TestChat_LengthToolCallTurnClearsStructuredReasoning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"grep","arguments":"{}"}}],"thinking_blocks":[{"type":"thinking","thinking":"cut o","signature":""}],"reasoning_details":[{"type":"reasoning.text","text":"cut o"}]}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	resp, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{
		BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m1",
	}, nil, nil)
	require.NoError(t, err)
	require.True(t, resp.Truncated)
	require.Len(t, resp.Message.ToolCalls, 1)
	assert.Nil(t, resp.Message.ThinkingBlocks, "a length-truncated turn must not replay thinking_blocks")
	assert.Nil(t, resp.Message.ReasoningDetails, "a length-truncated turn must not replay reasoning_details")
}

// TestChat_UsageIsPerTurnNotCumulative pins the per-turn-incremental contract
// documented on ChatResponse.Usage: each Chat() returns ONLY its own turn's
// usage and never accumulates across calls. The fanout loop relies on this to
// sum per-turn deltas correctly; if a future change made Chat accumulate, the
// loop would double-count, so this guard locks the decode-site semantics.
func TestChat_UsageIsPerTurnNotCumulative(t *testing.T) {
	var turn int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&turn, 1)
		// Each turn reports a distinct, non-cumulative usage (100/10, then 200/20).
		usageChatResponse(w, "ok", int(n)*100, int(n)*10)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)

	c := fastRetry(srv.Client())
	inv := Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"}

	r1, err := c.Chat(context.Background(), inv, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 100, r1.Usage.PromptTokens)
	assert.Equal(t, 10, r1.Usage.CompletionTokens)

	r2, err := c.Chat(context.Background(), inv, nil, nil)
	require.NoError(t, err)
	// Second call returns turn 2's usage only (200/20), NOT the running total
	// (300/30) — Chat does not accumulate; the caller sums.
	assert.Equal(t, 200, r2.Usage.PromptTokens)
	assert.Equal(t, 20, r2.Usage.CompletionTokens)
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
