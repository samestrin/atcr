package llmclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Original Task 6 / AC 02-01, AC 02-02: the marshaled bodies of a fully
// populated, unset chatRequest and chatToolRequest are pinned byte-for-byte.
// Both literals were captured from the structs BEFORE response_format was
// added, so they are real pre-epic output, not a copy of the new output.
const (
	goldenChatRequest     = `{"model":"m","messages":[{"role":"user","content":"review this"}],"temperature":0.3,"max_tokens":512}`
	goldenChatToolRequest = `{"model":"m","messages":[{"role":"user","content":"review this"}],"tools":[{"function":{"description":"Read a file","name":"read_file","parameters":{"type":"object"}},"type":"function"}],"tool_choice":"auto","temperature":0.3,"max_tokens":512}`
)

func TestChatRequest_UnsetBodyByteIdentical(t *testing.T) {
	temp, maxTok := 0.3, 512
	b, err := json.Marshal(chatRequest{
		Model:       "m",
		Messages:    []message{{Role: "user", Content: "review this"}},
		Temperature: &temp,
		MaxTokens:   &maxTok,
	})
	require.NoError(t, err)
	require.Equal(t, goldenChatRequest, string(b))
}

func TestChatToolRequest_UnsetBodyByteIdentical(t *testing.T) {
	temp, maxTok := 0.3, 512
	s := "review this"
	b, err := json.Marshal(chatToolRequest{
		Model:       "m",
		Messages:    []Message{{Role: "user", Content: &s}},
		Tools:       []ToolDef{{Name: "read_file", Description: "Read a file", Parameters: map[string]any{"type": "object"}}},
		ToolChoice:  "auto",
		Temperature: &temp,
		MaxTokens:   &maxTok,
	})
	require.NoError(t, err)
	require.Equal(t, goldenChatToolRequest, string(b))
}

// wantResponseFormat is the exact nested-object wire shape a declared agent sends.
const wantResponseFormat = `"response_format":{"type":"json_object"}`

// responseFormatRe extracts the response_format member so the two request
// paths' wire shapes can be compared byte-for-byte.
var responseFormatRe = regexp.MustCompile(`"response_format":\{[^}]*\}`)

// captureComplete runs CompleteWithUsage for inv against a capturing server and
// returns the raw request body.
func captureComplete(t *testing.T, inv Invocation) string {
	t.Helper()
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		usageResponse(w, "findings here", 7, 3)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)
	inv.BaseURL, inv.APIKeyEnv = srv.URL, "TEST_KEY"
	content, usage, _, err := fastRetry(srv.Client()).CompleteWithUsage(context.Background(), inv)
	require.NoError(t, err)
	assert.Equal(t, "findings here", content, "the decode path is unchanged")
	assert.Equal(t, UsageData{PromptTokens: 7, CompletionTokens: 3}, usage)
	return gotBody
}

// captureChat runs Client.Chat for inv (with a tool definition) against a
// capturing server and returns the raw request body.
func captureChat(t *testing.T, inv Invocation) string {
	t.Helper()
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_KEY", testKey)
	inv.BaseURL, inv.APIKeyEnv = srv.URL, "TEST_KEY"
	s := "hi"
	tools := []ToolDef{{Name: "read_file", Description: "Read a file", Parameters: map[string]any{"type": "object"}}}
	_, err := fastRetry(srv.Client()).Chat(context.Background(), inv, []Message{{Role: "user", Content: &s}}, tools)
	require.NoError(t, err)
	return gotBody
}

// AC 02-01 Scenario 2 / AC 02-03: modeled on TestComplete_MaxTokensOmittedWhenNil.
func TestComplete_ResponseFormatOmittedWhenUnset(t *testing.T) {
	body := captureComplete(t, Invocation{Model: "m"})
	assert.NotContains(t, body, "response_format")
}

// AC 02-01 Scenario 1 / AC 02-03 Scenario 1.
func TestCompleteWithUsage_ResponseFormatPresentWhenDeclared(t *testing.T) {
	body := captureComplete(t, Invocation{Model: "m", ResponseFormat: "json_object"})
	assert.Contains(t, body, wantResponseFormat)
}

// AC 02-02 / AC 02-03: modeled on TestChat_EmptyToolsOmitted.
func TestChat_ResponseFormatOmittedWhenUnset(t *testing.T) {
	body := captureChat(t, Invocation{Model: "m"})
	assert.Contains(t, body, `"tools"`)
	assert.NotContains(t, body, "response_format")
}

// AC 02-02 / AC 02-03 Scenario 2: tools and response_format coexist.
func TestChat_ResponseFormatPresentWhenDeclared(t *testing.T) {
	body := captureChat(t, Invocation{Model: "m", ResponseFormat: "json_object"})
	assert.Contains(t, body, `"tools"`)
	assert.Contains(t, body, `"tool_choice":"auto"`)
	assert.Contains(t, body, wantResponseFormat)
}

// AC 02-03 Edge Cases 1-2: both request paths carry the same wire shape when
// declared and neither carries it when unset, so the two structs cannot drift.
func TestResponseFormat_RoundTripSymmetry(t *testing.T) {
	declared := Invocation{Model: "m", ResponseFormat: "json_object"}
	fromComplete := responseFormatRe.FindString(captureComplete(t, declared))
	fromChat := responseFormatRe.FindString(captureChat(t, declared))
	require.NotEmpty(t, fromComplete)
	assert.Equal(t, fromComplete, fromChat)

	// The unset half is pinned by the AC-traceable named tests below
	// (TestComplete_ResponseFormatOmittedWhenUnset / TestChat_ResponseFormatOmittedWhenUnset);
	// duplicating it here tripled the HTTP-server round-trips for one property.
}

// AC 02-01 Edge Case 1: the wire layer does not police the value (validation
// is the registry's job); any non-empty string maps to {"type":"<value>"}.
func TestComplete_ResponseFormatValueForwardedVerbatim(t *testing.T) {
	body := captureComplete(t, Invocation{Model: "m", ResponseFormat: "json_schema"})
	assert.Contains(t, body, `"response_format":{"type":"json_schema"}`)
}
