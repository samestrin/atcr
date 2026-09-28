package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	return runWireToolLoopWith(t, responseFormat, func(int) string { return "" })
}

// runWireToolLoopWith is runWireToolLoop with extra members on each tool-call
// reply: members(turn) is appended inside turn's assistant message.
func runWireToolLoopWith(t *testing.T, responseFormat string, members func(turn int) string) []string {
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
			_, _ = fmt.Fprintf(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,%s`+
				`"tool_calls":[{"id":"c%d","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f%d.go\"}"}}]}}]}`,
				withComma(members(turn)), turn, turn)
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

// Sprint 35.16.11.2.2 (AC 04 story): a declared thinking setting rides every
// tool-loop turn. Sprint 35.16.11.2.2.1 inverted the second half: a turn-1
// reply's reasoning_content is re-sent on its assistant turn in the turn-2
// history, since providers expect their own reasoning back.
func TestToolLoop_ThinkingOnEveryTurnAndReasoningResent(t *testing.T) {
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
	assert.Contains(t, bodies[1], `"reasoning_content":"PRIVATE-CHAIN-OF-THOUGHT"`, "turn-1 reasoning must be re-sent")
	assert.Equal(t, 1, strings.Count(bodies[1], "reasoning_content"), "only the assistant turn carries it")
}

// reasoningKeys are the reasoning members an llmclient.Message can carry.
var reasoningKeys = []string{"reasoning_content", "reasoning", "reasoning_details", "thinking_blocks"}

func withComma(members string) string {
	if members == "" {
		return ""
	}
	return members + ","
}

// wireMessages decodes a captured request body's messages, each as its raw
// members.
func wireMessages(t *testing.T, body string) []map[string]json.RawMessage {
	t.Helper()
	var req struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &req))
	return req.Messages
}

// reasoningOn returns the reasoning members one wire message carries, as sent.
func reasoningOn(m map[string]json.RawMessage) map[string]string {
	out := map[string]string{}
	for _, k := range reasoningKeys {
		if v, ok := m[k]; ok {
			out[k] = string(v)
		}
	}
	return out
}

// Sprint 35.16.11.2.2.1 AC 04-01 Scenario 3: every reasoning shape a provider
// returns rides its assistant turn in the next request, under the key it
// arrived in, as the bytes received.
func TestToolLoop_ReplaysEachReasoningShapeUnderItsKey(t *testing.T) {
	cases := map[string]string{
		"reasoning_content": `"because X"`,
		"reasoning":         `"chain of thought"`,
		"reasoning_details": `[{"type":"reasoning.text","text":"step 1","index":0}]`,
		"thinking_blocks":   `[{"type":"thinking","thinking":"step 1","signature":"EqQBCkgIARABGAIiQL+/zzA0Xq9b=="}]`,
	}
	for key, raw := range cases {
		t.Run(key, func(t *testing.T) {
			bodies := runWireToolLoopWith(t, "", func(turn int) string {
				if turn == 1 {
					return `"` + key + `":` + raw
				}
				return ""
			})
			msgs := wireMessages(t, bodies[1])
			require.Len(t, msgs, 3, "prompt, assistant tool call, tool result")
			assert.Empty(t, reasoningOn(msgs[0]))
			assert.Equal(t, map[string]string{key: raw}, reasoningOn(msgs[1]))
			assert.Empty(t, reasoningOn(msgs[2]))
		})
	}
}

// AC 04-01 Scenario 2 and AC 04-02: each assistant turn carries its own
// reasoning on every later request, so request N carries exactly the N-1
// earlier turns' reasoning. The loop's own user and tool messages (the prompt,
// the tool results, the skipped-call answers at the max_turns trip, and the
// forced-final request) carry none.
func TestToolLoop_EachAssistantTurnReplaysOnlyItsOwnReasoning(t *testing.T) {
	bodies := runWireToolLoopWith(t, "", func(turn int) string {
		return fmt.Sprintf(`"reasoning_content":"R-%d"`, turn)
	})
	for i, body := range bodies {
		assistant := 0
		for j, m := range wireMessages(t, body) {
			var role string
			require.NoError(t, json.Unmarshal(m["role"], &role))
			if role != "assistant" {
				assert.Empty(t, reasoningOn(m), "request %d message %d (%s)", i+1, j, role)
				continue
			}
			assistant++
			assert.Equal(t, map[string]string{"reasoning_content": fmt.Sprintf(`"R-%d"`, assistant)}, reasoningOn(m),
				"request %d assistant turn %d", i+1, assistant)
		}
		assert.Equal(t, i, assistant, "request %d replays every earlier assistant turn", i+1)
	}
	final := wireMessages(t, bodies[3])
	require.Len(t, final, 8, "prompt, three tool turns with results, and the final-answer request")
	assert.JSONEq(t, `"user"`, string(final[7]["role"]))
}

// goldenForcedFinalBody is runWireToolLoop's forced-final request body, captured
// from pre-plan main at e5c9754d, before Message had any reasoning member. It
// holds the whole history: every assistant turn, tool result, skipped-call
// answer, and the final-answer request.
const goldenForcedFinalBody = `{"model":"m","messages":[{"role":"user","content":""},{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f1.go\"}"}}]},{"role":"tool","content":"x","tool_call_id":"c1"},{"role":"assistant","content":null,"tool_calls":[{"id":"c2","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f2.go\"}"}}]},{"role":"tool","content":"x","tool_call_id":"c2"},{"role":"assistant","content":null,"tool_calls":[{"id":"c3","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f3.go\"}"}}]},{"role":"tool","content":"skipped: turn budget reached; provide your final answer now","tool_call_id":"c3"},{"role":"user","content":"You have reached your exploration budget. Stop calling tools and write your final review now, based only on the evidence you have already gathered."}]}`

// AC 04-01 Edge Case 2 (TD-003): replies with no usable reasoning (absent,
// null, or "") leave every request body byte-identical to the pre-plan loop.
func TestToolLoop_NoReasoningBodiesUnchanged(t *testing.T) {
	absent := runWireToolLoop(t, "")
	require.Equal(t, goldenForcedFinalBody, absent[3])
	cases := map[string]string{
		"null":          `"reasoning_content":null,"reasoning":null,"reasoning_details":null,"thinking_blocks":null`,
		"empty strings": `"reasoning_content":"","reasoning":""`,
	}
	for name, members := range cases {
		t.Run(name, func(t *testing.T) {
			got := runWireToolLoopWith(t, "", func(int) string { return members })
			assert.Equal(t, absent, got)
		})
	}
}

// AC 04-01 Edge Case 3: a fallback starts a fresh history, so its first
// request carries none of the failed primary's reasoning, although the primary
// itself re-sent it.
func TestToolLoop_FallbackCarriesNoPrimaryReasoning(t *testing.T) {
	var (
		mu              sync.Mutex
		primary, backup []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(b, &req)
		mu.Lock()
		defer mu.Unlock()
		if req.Model == "backup" {
			backup = append(backup, string(b))
			_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"NO FINDINGS"}}]}`)
			return
		}
		primary = append(primary, string(b))
		if len(primary) == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,`+
				`"reasoning_content":"PRIMARY-REASONING",`+
				`"tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f.go\"}"}}]}}]}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"primary fails its second turn"}}`)
	}))
	defer srv.Close()
	t.Setenv("ATCR_TEST_KEY", "k")

	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}
	p, fb := toolAgent("p", 3, 0), toolAgent("b", 3, 0)
	p.Invocation = llmclient.Invocation{BaseURL: srv.URL, APIKeyEnv: "ATCR_TEST_KEY", Model: "primary"}
	fb.Invocation = llmclient.Invocation{BaseURL: srv.URL, APIKeyEnv: "ATCR_TEST_KEY", Model: "backup"}

	r := toolEngine(llmclient.New(llmclient.WithHTTPClient(srv.Client())), d).
		invokeSlot(context.Background(), Slot{Primary: p, Fallbacks: []Agent{fb}})
	require.Equal(t, StatusOK, r.Status)
	require.True(t, r.FallbackUsed)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, primary, 2)
	require.Contains(t, primary[1], "PRIMARY-REASONING", "the primary re-sent its own reasoning")
	require.NotEmpty(t, backup)
	assert.NotContains(t, backup[0], "PRIMARY-REASONING")
	for j, m := range wireMessages(t, backup[0]) {
		assert.Empty(t, reasoningOn(m), "fallback request message %d", j)
	}
}
