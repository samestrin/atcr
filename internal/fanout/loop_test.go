package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
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
	return runWireToolLoopInv(t, llmclient.Invocation{Model: "m", ResponseFormat: responseFormat}, members)
}

// runWireToolLoopInv is runWireToolLoopWith for a given invocation; its
// BaseURL and APIKeyEnv are pointed at the stub server.
func runWireToolLoopInv(t *testing.T, inv llmclient.Invocation, members func(turn int) string) []string {
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
	inv.BaseURL, inv.APIKeyEnv = srv.URL, "ATCR_TEST_KEY"
	a.Invocation = inv

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

// reasoningKeys are the reasoning members an llmclient.Message can carry: every
// json key besides the four plain chat members. Read from the struct so a new
// member is covered by the role-isolation tests without an edit here.
var reasoningKeys = func() []string {
	plain := map[string]bool{"role": true, "content": true, "tool_calls": true, "tool_call_id": true}
	var keys []string
	typ := reflect.TypeOf(llmclient.Message{})
	for i := 0; i < typ.NumField(); i++ {
		if k := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]; !plain[k] {
			keys = append(keys, k)
		}
	}
	return keys
}()

func TestReasoningKeys_ReadFromMessage(t *testing.T) {
	assert.ElementsMatch(t, []string{"reasoning_content", "reasoning", "reasoning_details", "thinking_blocks"}, reasoningKeys)
}

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
			// TD internal/fanout/loop_test.go:207: persistence must hold on EVERY
			// later request, including the forced-final one — turn 1 is the only
			// turn carrying the member, so msgs[1] carries {key: raw} each time.
			for i, body := range bodies[2:] {
				later := wireMessages(t, body)
				require.Greater(t, len(later), 1, "later request %d must carry history", i+2)
				assert.Equal(t, map[string]string{key: raw}, reasoningOn(later[1]),
					"request %d must re-send turn-1 reasoning under its own key", i+2)
			}
		})
	}
}

// Sprint 35.16.11.2.2.1 AC 05-03: replay has no thinking_style gate. LiteLLM
// can turn reasoning_effort into Anthropic extended thinking for a Claude
// model, so a reasoning_effort agent must replay LiteLLM's Claude reply shape
// on every later request exactly as an anthropic-style agent does. This pins
// that parity against a stub; the registry adds no second guard. The replay
// itself is not live-verified against Anthropic (no model served), so a
// reasoning_effort Claude agent is knowingly allowed on it (TD-015).
func TestToolLoop_ReplayIgnoresThinkingStyle(t *testing.T) {
	const (
		content = `"reasoning_content":"step 1"`
		blocks  = `"thinking_blocks":[{"type":"thinking","thinking":"step 1","signature":"EqQBCkgIARABGAIiQL+/zzA0Xq9b=="}]`
	)
	claudeTurn := func(turn int) string {
		if turn == 1 {
			return content + "," + blocks
		}
		return ""
	}
	styles := []struct {
		name, wire string
		inv        llmclient.Invocation
	}{
		{"anthropic", `"thinking":{"type":"enabled"`, llmclient.Invocation{Model: "claude",
			Thinking: registry.ThinkingOn, ThinkingLevel: registry.ThinkingLevelLow, ThinkingStyle: registry.ThinkingStyleAnthropic}},
		{"reasoning_effort", `"reasoning_effort":"low"`, llmclient.Invocation{Model: "claude",
			Thinking: registry.ThinkingOn, ThinkingLevel: registry.ThinkingLevelLow, ThinkingStyle: registry.ThinkingStyleReasoningEffort}},
	}
	replayed := map[string]map[string]string{}
	for _, s := range styles {
		bodies := runWireToolLoopInv(t, s.inv, claudeTurn)
		for i, body := range bodies[1:] {
			require.Contains(t, body, s.wire, "%s request %d: the declared style must reach the wire", s.name, i+2)
			msgs := wireMessages(t, body)
			require.GreaterOrEqual(t, len(msgs), 3, "prompt, assistant tool call, tool result")
			key := fmt.Sprintf("%s request %d", s.name, i+2)
			replayed[key] = reasoningOn(msgs[1])
		}
	}
	want := map[string]string{
		"reasoning_content": `"step 1"`,
		"thinking_blocks":   `[{"type":"thinking","thinking":"step 1","signature":"EqQBCkgIARABGAIiQL+/zzA0Xq9b=="}]`,
	}
	require.Len(t, replayed, 2*3, "requests 2-4 for each style")
	for key, got := range replayed {
		assert.Equal(t, want, got, key)
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
		"wrong types":   `"reasoning_content":42,"reasoning":{"text":"x"},"reasoning_details":"x","thinking_blocks":7`,
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
		if err := json.Unmarshal(b, &req); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
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

// --- T5 (sprint 35.16.11.2.2.4): strip <think> from replayed history ---------

// runWireToolLoopContent drives the real toolLoop against a server whose
// tool-call turns carry CONTENT as well as a tool_call — the shape
// runWireToolLoopInv cannot produce, because its template hardcodes
// "content":null. turnContent supplies the assistant content per turn (1-based);
// the first toolTurns turns request a tool, the next answers with
// finish_reason=stop, so the loop ends there with that turn's content as the
// result. withReasoning adds a reasoning_content member to each tool-call turn.
//
// A real provider sends one shape or the other, never both: one that populates
// reasoning_content has already lifted the block out of content. withReasoning
// true is therefore a deliberate superset, the only way to assert the reasoning
// channel rides through untouched; withReasoning false is the realistic inline
// shape. Both are exercised.
//
// It returns every captured request body and the Result.
func runWireToolLoopContent(t *testing.T, toolTurns int, withReasoning bool, turnContent func(turn int) string) ([]string, Result) {
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
		content, _ := json.Marshal(turnContent(turn))
		if turn <= toolTurns {
			reasoning := ""
			if withReasoning {
				reasoning = fmt.Sprintf(`"reasoning_content":"separate channel %d",`, turn)
			}
			_, _ = fmt.Fprintf(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":%s,%s`+
				`"tool_calls":[{"id":"c%d","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f%d.go\"}"}}]}}]}`,
				content, reasoning, turn, turn)
			return
		}
		_, _ = fmt.Fprintf(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":%s}}]}`, content)
	}))
	defer srv.Close()
	t.Setenv("ATCR_TEST_KEY", "k")

	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}
	a := toolAgent("a", 10, 0)
	a.Invocation = llmclient.Invocation{Model: "m", BaseURL: srv.URL, APIKeyEnv: "ATCR_TEST_KEY"}

	r := toolEngine(llmclient.New(llmclient.WithHTTPClient(srv.Client())), d).invokeAgent(context.Background(), a)
	require.Equal(t, StatusOK, r.Status)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, toolTurns+1, "%d tool turns plus the final answer turn", toolTurns)
	return append([]string(nil), bodies...), r
}

// AC3: a model that reasons inline must not have its own discarded draft replayed
// back to it as settled prior output. The history entry carries the stripped
// answer; the reasoning-carrier field it arrived in is untouched.
//
// This ALSO pins the aliasing guard, which is the real bug risk: Message.Content
// is a *string shared by the history entry and resp.Message, so an in-place strip
// would silently also strip l.res.Content — assigned in both the final-answer
// branch and the forced-final branch — and therefore the raw review.md artifact.
// A test that only checked the history entry would pass against that bug; the
// r.Content assertion is what catches it. (Branches named, not cited by line: an
// insert upstream drifts a line number silently.)
func TestToolLoop_ReplayedHistoryCarriesNoThinkBlock(t *testing.T) {
	const finalRaw = "<think>second draft</think>real answer"
	bodies, r := runWireToolLoopContent(t, 1, true, func(turn int) string {
		if turn == 1 {
			return "<think>draft reasoning</think>keep going"
		}
		return finalRaw
	})

	msgs := wireMessages(t, bodies[1])
	require.Len(t, msgs, 3, "prompt, assistant tool call, tool result")
	// Decoded, not matched against the raw body: encoding/json escapes '<' on the
	// wire, so a raw-body search for the literal tag passes even against the bug.
	var replayed string
	require.NoError(t, json.Unmarshal(msgs[1]["content"], &replayed))
	assert.Equal(t, "keep going", replayed,
		"the replayed assistant turn must carry the stripped answer")
	assert.Equal(t, map[string]string{"reasoning_content": `"separate channel 1"`}, reasoningOn(msgs[1]),
		"the separate reasoning channel is a deliberate replay channel and must be untouched")

	assert.Equal(t, finalRaw, r.Content,
		"l.res.Content must still be the RAW reply — review.md writes it unstripped")
}

// The leading-only rule at this lane's call site: a turn whose answer merely
// quotes the tags is replayed byte-identical.
//
// This is a GUARD, not coverage of the strip — it passes with the strip reverted
// too, because the no-op direction is what it protects. What it does catch is a
// future over-greedy strip: swap SplitThink for a position-blind tag delete and
// this test fails. Do not count it toward this change's coverage.
func TestToolLoop_ReplayedHistoryKeepsQuotedTags(t *testing.T) {
	const quoted = "loop.go replays history without stripping <think> or </think>"
	bodies, _ := runWireToolLoopContent(t, 1, true, func(turn int) string {
		if turn == 1 {
			return quoted
		}
		return "done"
	})

	msgs := wireMessages(t, bodies[1])
	require.Len(t, msgs, 3)
	var got string
	require.NoError(t, json.Unmarshal(msgs[1]["content"], &got))
	assert.Equal(t, quoted, got, "an answer that merely names the tags must be replayed unchanged")
}

// ACCEPTED LOSS, pinned so it is a decision on the record rather than an omission.
//
// historyMessage's stated purpose is that a model reasoning inline must not get
// its own discarded draft replayed back as settled prior output. SplitThink is
// LEADING-only, so the most ordinary inline shape defeats it: a one-token
// preamble before the think block leaves the block mid-content, the strip is a
// no-op, and the reply -- draft included -- is replayed byte-identical to the
// provider and re-enters the model's own assistant history as settled output.
//
// The two exits the TD row offers are to gate on llmclient.HasThinkMarkup (which
// IS position-blind) and drop or annotate the turn, or to accept the loss
// explicitly with a pin. This pins it, mirroring how
// response_truncation_test.go pins its own accepted losses: the test asserts the
// CURRENT byte-identical replay, and its name and this comment say that is a
// known gap, not a guarantee. Re-asserting the harmless no-op case (the previous
// test) cannot stand in for this one -- it is the over-greedy direction only.
func TestToolLoop_TrailingThinkBlockIsReplayed(t *testing.T) {
	const trailing = "here is my analysis  thinkingsecond draft, never committed</think>"
	bodies, _ := runWireToolLoopContent(t, 1, true, func(turn int) string {
		if turn == 1 {
			return "ok let me look  thinkingfirst draft, abandoned</think>"
		}
		return trailing
	})

	msgs := wireMessages(t, bodies[1])
	require.Len(t, msgs, 3)
	var replayed string
	require.NoError(t, json.Unmarshal(msgs[1]["content"], &replayed))
	assert.Equal(t, "ok let me look  thinkingfirst draft, abandoned</think>", replayed,
		"KNOWN GAP: a non-leading think block is not stripped, so the abandoned draft replays verbatim")
}

// A tool-call turn whose whole Content was reasoning strips to blank, and blank
// must replay as content:null, NOT "". llmclient.Message reserves the pointer for
// exactly that distinction ("which OpenAI requires", chat.go), and
// TestChat_RoleToolMessageSerialization (internal/llmclient/chat_test.go) pins it
// on the request side. TD loop.go:70 narrowed the guard so ONLY a stripped turn
// (the strip removed something) takes the null shape — a genuinely-empty content
// keeps the pre-sprint empty-string replay, since changing the wire shape of a
// turn the sprint did not strip is out of scope and the validator-risk rationale
// for it was speculative, never verified against a real proxy.
func TestToolLoop_ThinkOnlyTurnReplaysAsNullContent(t *testing.T) {
	cases := map[string]string{
		"closed pair, nothing after":       "<think>I should read f1.go</think>",
		"whitespace remainder":             "<think>I should read f1.go</think>\n\n  ",
		"unclosed opener, cut mid-thought": "<think>I should read f1.go",
		// Not a think block at all. TD loop.go:70 narrowed the guard: only a strip
		// that actually removed something can nil the content, so a genuinely-empty
		// content replays as the empty string — the pre-sprint wire shape. This
		// subtest asserts the empty string REPLAYS (the wire body carries "" not
		// null); the null-shape assertion below cannot hold for it, so it is
		// excluded from the null assertion and checked separately in
		// TestToolLoop_GenuinelyEmptyContent_ReplaysAsEmptyString.
		"already empty, no think markup": "",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			bodies, _ := runWireToolLoopContent(t, 1, false, func(turn int) string {
				if turn == 1 {
					return content
				}
				return "final answer"
			})
			msgs := wireMessages(t, bodies[1])
			require.Len(t, msgs, 3, "prompt, assistant tool call, tool result")
			if name == "already empty, no think markup" {
				assert.Equal(t, `""`, string(msgs[1]["content"]),
					"no think markup: the pre-sprint wire shape (empty string) is restored, not null")
				return
			}
			assert.Equal(t, "null", string(msgs[1]["content"]),
				"a turn that was entirely reasoning must take the canonical content:null shape")
		})
	}
}

// The realistic inline shape: a provider that leaves the block in content sends no
// reasoning member at all. The combined fixture elsewhere is a deliberate superset
// used to assert the reasoning channel is untouched; this is the shape that occurs.
func TestToolLoop_InlineThinkWithNoReasoningMemberIsStripped(t *testing.T) {
	bodies, _ := runWireToolLoopContent(t, 1, false, func(turn int) string {
		if turn == 1 {
			return "<think>let me look</think>reading f1.go now"
		}
		return "final answer"
	})
	msgs := wireMessages(t, bodies[1])
	require.Len(t, msgs, 3)
	var replayed string
	require.NoError(t, json.Unmarshal(msgs[1]["content"], &replayed))
	assert.Equal(t, "reading f1.go now", replayed)
	assert.Empty(t, reasoningOn(msgs[1]), "no reasoning member was sent, so none may be replayed")
}

// The strip must persist on EVERY later request, for EVERY earlier assistant turn
// — the same property TestToolLoop_ReplaysEachReasoningShapeUnderItsKey asserts
// for the reasoning members. One tool turn cannot show this.
func TestToolLoop_EveryEarlierAssistantTurnStaysStripped(t *testing.T) {
	bodies, _ := runWireToolLoopContent(t, 2, false, func(turn int) string {
		return fmt.Sprintf("<think>draft %d</think>answer %d", turn, turn)
	})
	// bodies[2] is the third request: it carries BOTH earlier assistant turns.
	msgs := wireMessages(t, bodies[2])
	require.Len(t, msgs, 5, "prompt, assistant 1, tool 1, assistant 2, tool 2")
	for i, idx := range []int{1, 3} {
		var replayed string
		require.NoError(t, json.Unmarshal(msgs[idx]["content"], &replayed))
		assert.Equal(t, fmt.Sprintf("answer %d", i+1), replayed,
			"assistant turn %d must still be stripped on the third request", i+1)
	}
}
