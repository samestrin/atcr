package llmclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// thinkingKeys are every top-level request member a thinking style can emit.
var thinkingKeys = []string{"enable_thinking", "thinking_budget", "chat_template_kwargs", "reasoning_effort", "thinking", "preserve_thinking"}

// thinkingMembers decodes a request body and returns only its thinking members,
// re-encoded as one JSON object, so a test can compare them exactly.
func thinkingMembers(t *testing.T, body string) string {
	t.Helper()
	var all map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(body), &all))
	out := map[string]json.RawMessage{}
	for _, k := range thinkingKeys {
		if v, ok := all[k]; ok {
			out[k] = v
		}
	}
	b, err := json.Marshal(out)
	require.NoError(t, err)
	return string(b)
}

// thinkingCases is one declaration per style at two or more levels, plus the
// inputs that must emit nothing. want is the exact JSON of the thinking members.
var thinkingCases = []struct {
	name, thinking, level, style, want string
}{
	// qwen: enable_thinking plus a budget when a level is declared.
	{"qwen on high", registry.ThinkingOn, registry.ThinkingLevelHigh, registry.ThinkingStyleQwen, `{"enable_thinking":true,"thinking_budget":16384}`},
	{"qwen level alone medium", "", registry.ThinkingLevelMedium, registry.ThinkingStyleQwen, `{"enable_thinking":true,"thinking_budget":8192}`},
	{"qwen max not clamped", "", registry.ThinkingLevelMax, registry.ThinkingStyleQwen, `{"enable_thinking":true,"thinking_budget":32768}`},
	{"qwen on no level", registry.ThinkingOn, "", registry.ThinkingStyleQwen, `{"enable_thinking":true}`},
	{"qwen off", registry.ThinkingOff, "", registry.ThinkingStyleQwen, `{"enable_thinking":false}`},
	// template_kwargs: on/off only.
	{"template_kwargs off", registry.ThinkingOff, "", registry.ThinkingStyleTemplateKwargs, `{"chat_template_kwargs":{"enable_thinking":false}}`},
	{"template_kwargs on", registry.ThinkingOn, "", registry.ThinkingStyleTemplateKwargs, `{"chat_template_kwargs":{"enable_thinking":true}}`},
	// reasoning_effort: the level itself, max clamped to high.
	{"reasoning_effort low", "", registry.ThinkingLevelLow, registry.ThinkingStyleReasoningEffort, `{"reasoning_effort":"low"}`},
	{"reasoning_effort on medium", registry.ThinkingOn, registry.ThinkingLevelMedium, registry.ThinkingStyleReasoningEffort, `{"reasoning_effort":"medium"}`},
	{"reasoning_effort max clamps", "", registry.ThinkingLevelMax, registry.ThinkingStyleReasoningEffort, `{"reasoning_effort":"high"}`},
	// anthropic: a thinking object; on with no level takes the medium budget.
	{"anthropic on low", registry.ThinkingOn, registry.ThinkingLevelLow, registry.ThinkingStyleAnthropic, `{"thinking":{"type":"enabled","budget_tokens":2048}}`},
	{"anthropic level alone max", "", registry.ThinkingLevelMax, registry.ThinkingStyleAnthropic, `{"thinking":{"type":"enabled","budget_tokens":32768}}`},
	{"anthropic on no level", registry.ThinkingOn, "", registry.ThinkingStyleAnthropic, `{"thinking":{"type":"enabled","budget_tokens":8192}}`},
	{"anthropic off", registry.ThinkingOff, "", registry.ThinkingStyleAnthropic, `{"thinking":{"type":"disabled"}}`},
	// glm: a thinking object with no budget.
	{"glm on", registry.ThinkingOn, "", registry.ThinkingStyleGLM, `{"thinking":{"type":"enabled"}}`},
	{"glm off", registry.ThinkingOff, "", registry.ThinkingStyleGLM, `{"thinking":{"type":"disabled"}}`},
	// Nothing to send.
	{"unset", "", "", "", `{}`},
	{"style alone", "", "", registry.ThinkingStyleQwen, `{}`},
	{"unknown style not coerced", registry.ThinkingOn, "", "openai", `{}`},
	{"no style", registry.ThinkingOff, "", "", `{}`},
	// Values the registry would reject are not guessed at: nothing is sent.
	{"unknown thinking qwen", "true", "", registry.ThinkingStyleQwen, `{}`},
	{"unknown thinking anthropic", "yes", "", registry.ThinkingStyleAnthropic, `{}`},
	{"unknown level anthropic", "", "extreme", registry.ThinkingStyleAnthropic, `{}`},
	{"unknown level reasoning_effort", "", "extreme", registry.ThinkingStyleReasoningEffort, `{}`},
	{"unknown level qwen", registry.ThinkingOn, "extreme", registry.ThinkingStyleQwen, `{}`},
}

// AC 03-01: one mapper, one populated style per declaration, exact wire JSON.
func TestNewThinkingFields_PerStyle(t *testing.T) {
	for _, tc := range thinkingCases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(newThinkingFields(tc.thinking, tc.level, tc.style, ""))
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(b))
		})
	}
}

// AC 03-01 DoD: a declared false is a non-nil pointer, distinct from unset.
func TestNewThinkingFields_FalseIsNotUnset(t *testing.T) {
	off := newThinkingFields(registry.ThinkingOff, "", registry.ThinkingStyleQwen, "")
	require.NotNil(t, off.EnableThinking)
	assert.False(t, *off.EnableThinking)
	assert.Nil(t, newThinkingFields("", "", registry.ThinkingStyleQwen, "").EnableThinking)

	kw := newThinkingFields(registry.ThinkingOff, "", registry.ThinkingStyleTemplateKwargs, "")
	require.NotNil(t, kw.ChatTemplateKwargs)
	require.NotNil(t, kw.ChatTemplateKwargs.EnableThinking)
	assert.False(t, *kw.ChatTemplateKwargs.EnableThinking)
}

// AC 03-01 Edge Case 2: qwen and anthropic send the registry's one budget
// table, so the load-time budget warning describes exactly what is sent.
func TestNewThinkingFields_BudgetFromRegistryTable(t *testing.T) {
	for _, level := range registry.ThinkingLevels() {
		want := registry.ThinkingBudgetTokens("", level, registry.ThinkingStyleQwen)
		require.Positive(t, want)
		q, a := newThinkingFields("", level, registry.ThinkingStyleQwen, ""), newThinkingFields("", level, registry.ThinkingStyleAnthropic, "")
		require.NotNil(t, q.ThinkingBudget)
		require.NotNil(t, a.Thinking)
		assert.Equal(t, want, *q.ThinkingBudget, "qwen %s", level)
		assert.Equal(t, registry.ThinkingBudgetTokens("", level, registry.ThinkingStyleAnthropic), a.Thinking.BudgetTokens, "anthropic %s", level)
	}
}

// AC 03-02 Scenarios 1, 2, 4 and Edge Cases 1, 2: both request paths carry the
// declared style's members, byte-identical to each other and to the mapper.
func TestThinking_BothRequestPathsCarryTheMapping(t *testing.T) {
	for _, tc := range thinkingCases {
		t.Run(tc.name, func(t *testing.T) {
			inv := Invocation{Model: "m", Thinking: tc.thinking, ThinkingLevel: tc.level, ThinkingStyle: tc.style}
			fromComplete := thinkingMembers(t, captureComplete(t, inv))
			chatBody := captureChat(t, inv)
			assert.Contains(t, chatBody, `"tools"`)
			assert.Contains(t, chatBody, `"tool_choice":"auto"`)
			assert.Equal(t, tc.want, fromComplete, "single-shot path")
			assert.Equal(t, tc.want, thinkingMembers(t, chatBody), "tool-loop path")
		})
	}
}

// AC 03-02 Scenario 3: the forced-final no-tools turn carries every style's
// members exactly.
func TestChat_ForcedFinalTurnCarriesThinking(t *testing.T) {
	for _, tc := range thinkingCases {
		t.Run(tc.name, func(t *testing.T) {
			body := captureChatWith(t, Invocation{Model: "m", Thinking: tc.thinking, ThinkingLevel: tc.level, ThinkingStyle: tc.style}, nil)
			assert.NotContains(t, body, `"tools"`)
			assert.Equal(t, tc.want, thinkingMembers(t, body))
		})
	}
}

// Anthropic rejects extended thinking with any temperature but 1, so an
// anthropic agent with thinking enabled sends no temperature on either path.
// Every other declaration keeps its temperature.
func TestThinking_AnthropicEnabledSendsNoTemperature(t *testing.T) {
	temp := 0.7
	for _, tc := range thinkingCases {
		t.Run(tc.name, func(t *testing.T) {
			inv := Invocation{Model: "m", Temperature: &temp, Thinking: tc.thinking, ThinkingLevel: tc.level, ThinkingStyle: tc.style}
			enabled := tc.style == registry.ThinkingStyleAnthropic && strings.Contains(tc.want, `"type":"enabled"`)
			for path, body := range map[string]string{"complete": captureComplete(t, inv), "chat": captureChat(t, inv), "final": captureChatWith(t, inv, nil)} {
				var got map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(body), &got))
				_, has := got["temperature"]
				assert.Equal(t, !enabled, has, "%s path: temperature sent = %v", path, has)
			}
		})
	}
}

// preserveCases pair a thinking declaration with preserve_thinking. want is
// the exact JSON of the thinking members; the flag renders only under qwen and
// glm with thinking on, and anything the registry would reject sends nothing
// for it.
var preserveCases = []struct {
	name, thinking, level, style, preserve, want string
}{
	{"qwen on", registry.ThinkingOn, "", registry.ThinkingStyleQwen, registry.ThinkingOn, `{"enable_thinking":true,"preserve_thinking":true}`},
	{"qwen level off", "", registry.ThinkingLevelHigh, registry.ThinkingStyleQwen, registry.ThinkingOff, `{"enable_thinking":true,"preserve_thinking":false,"thinking_budget":16384}`},
	{"glm on", registry.ThinkingOn, "", registry.ThinkingStyleGLM, registry.ThinkingOn, `{"thinking":{"type":"enabled","clear_thinking":false}}`},
	{"glm off", registry.ThinkingOn, "", registry.ThinkingStyleGLM, registry.ThinkingOff, `{"thinking":{"type":"enabled","clear_thinking":true}}`},
	// Other styles never carry it.
	{"anthropic ignores it", registry.ThinkingOn, "", registry.ThinkingStyleAnthropic, registry.ThinkingOn, `{"thinking":{"type":"enabled","budget_tokens":8192}}`},
	{"reasoning_effort ignores it", "", registry.ThinkingLevelLow, registry.ThinkingStyleReasoningEffort, registry.ThinkingOn, `{"reasoning_effort":"low"}`},
	{"template_kwargs ignores it", registry.ThinkingOn, "", registry.ThinkingStyleTemplateKwargs, registry.ThinkingOn, `{"chat_template_kwargs":{"enable_thinking":true}}`},
	// Thinking not on: the registry rejects these at load.
	{"flag without thinking", "", "", registry.ThinkingStyleQwen, registry.ThinkingOn, `{}`},
	{"flag with qwen thinking off", registry.ThinkingOff, "", registry.ThinkingStyleQwen, registry.ThinkingOn, `{"enable_thinking":false}`},
	{"flag with glm thinking off", registry.ThinkingOff, "", registry.ThinkingStyleGLM, registry.ThinkingOn, `{"thinking":{"type":"disabled"}}`},
	// A value the registry would reject is not guessed at.
	{"unknown value", registry.ThinkingOn, "", registry.ThinkingStyleQwen, "true", `{}`},
}

// AC 03-01 / 03-02: preserve_thinking renders per style on the mapper, both
// request paths, and the forced-final turn.
func TestPreserveThinking_PerStyleOnEveryPath(t *testing.T) {
	for _, tc := range preserveCases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(newThinkingFields(tc.thinking, tc.level, tc.style, tc.preserve))
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(b), "mapper")
			inv := Invocation{Model: "m", Thinking: tc.thinking, ThinkingLevel: tc.level, ThinkingStyle: tc.style, PreserveThinking: tc.preserve}
			assert.Equal(t, tc.want, thinkingMembers(t, captureComplete(t, inv)), "single-shot path")
			assert.Equal(t, tc.want, thinkingMembers(t, captureChat(t, inv)), "tool-loop path")
			assert.Equal(t, tc.want, thinkingMembers(t, captureChatWith(t, inv, nil)), "forced-final path")
		})
	}
}

// AC 03-01 Edge Case 3: a declared off is a non-nil false, distinct from unset.
func TestPreserveThinking_OffIsNotUnset(t *testing.T) {
	off := newThinkingFields(registry.ThinkingOn, "", registry.ThinkingStyleQwen, registry.ThinkingOff)
	require.NotNil(t, off.PreserveThinking)
	assert.False(t, *off.PreserveThinking)
	assert.Nil(t, newThinkingFields(registry.ThinkingOn, "", registry.ThinkingStyleQwen, "").PreserveThinking)

	glm := newThinkingFields(registry.ThinkingOn, "", registry.ThinkingStyleGLM, "")
	require.NotNil(t, glm.Thinking)
	assert.Nil(t, glm.Thinking.ClearThinking, "glm with no flag sends no clear_thinking")
}

// Decision 4 (2026-09-28): only anthropic drops temperature; glm with thinking
// on keeps the declared temperature.
func TestPreserveThinking_GLMKeepsTemperature(t *testing.T) {
	temp := 0.6
	inv := Invocation{Model: "m", Temperature: &temp, Thinking: registry.ThinkingOn, ThinkingStyle: registry.ThinkingStyleGLM, PreserveThinking: registry.ThinkingOn}
	assert.Contains(t, captureComplete(t, inv), `"temperature":0.6`)
	assert.Contains(t, captureChat(t, inv), `"temperature":0.6`)
	require.NotNil(t, SentTemperature(inv))
	assert.InDelta(t, 0.6, *SentTemperature(inv), 1e-9)
}

// response_format and a thinking declaration ride the same body together.
func TestThinking_CoexistsWithResponseFormat(t *testing.T) {
	inv := Invocation{Model: "m", ResponseFormat: "json_object", ThinkingLevel: registry.ThinkingLevelLow, ThinkingStyle: registry.ThinkingStyleReasoningEffort}
	for path, body := range map[string]string{"complete": captureComplete(t, inv), "chat": captureChat(t, inv)} {
		var got map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(body), &got), path)
		assert.JSONEq(t, `{"type":"json_object"}`, string(got["response_format"]), path)
		assert.JSONEq(t, `"low"`, string(got["reasoning_effort"]), path)
	}
}

// encoding/json silently drops both members when an embedded struct and its
// parent share a tag at the same depth, so the thinking tags must stay
// disjoint from every sibling on both request types.
func TestThinkingFields_TagsDisjointFromRequestTags(t *testing.T) {
	tags := func(typ reflect.Type) map[string]bool {
		out := map[string]bool{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.Anonymous {
				continue
			}
			out[strings.Split(f.Tag.Get("json"), ",")[0]] = true
		}
		return out
	}
	own := tags(reflect.TypeOf(thinkingFields{}))
	require.Len(t, own, len(thinkingKeys))
	for _, k := range thinkingKeys {
		require.True(t, own[k], "thinkingKeys lists %s", k)
	}
	for _, parent := range []reflect.Type{reflect.TypeOf(chatRequest{}), reflect.TypeOf(chatToolRequest{})} {
		for k := range tags(parent) {
			assert.False(t, own[k], "%s.%s collides with a thinking member", parent.Name(), k)
		}
	}
}

// AC 03-03 Scenarios 1, 2: the mapper's unset output leaves both pre-feature
// golden bodies byte-identical.
func TestThinking_UnsetBodyByteIdentical(t *testing.T) {
	temp, maxTok := 0.3, 512
	s := "review this"
	b, err := json.Marshal(chatRequest{
		Model:          "m",
		Messages:       []message{{Role: "user", Content: "review this"}},
		Temperature:    &temp,
		MaxTokens:      &maxTok,
		thinkingFields: newThinkingFields("", "", "", ""),
	})
	require.NoError(t, err)
	require.Equal(t, goldenChatRequest, string(b))

	b, err = json.Marshal(chatToolRequest{
		Model:          "m",
		Messages:       []Message{{Role: "user", Content: &s}},
		Tools:          []ToolDef{{Name: "read_file", Description: "Read a file", Parameters: map[string]any{"type": "object"}}},
		ToolChoice:     "auto",
		Temperature:    &temp,
		MaxTokens:      &maxTok,
		thinkingFields: newThinkingFields("", "", "", ""),
	})
	require.NoError(t, err)
	require.Equal(t, goldenChatToolRequest, string(b))
}

// AC 03-03 Edge Case 1 and AC 03-01 Edge Case 4: an unset or style-only agent
// sends the same bytes as an agent written before the keys existed, on both
// paths, with no thinking key at all (not as {}, not as null).
func TestThinking_UndeclaredSendsNoThinkingKey(t *testing.T) {
	plain := Invocation{Model: "m"}
	styleOnly := Invocation{Model: "m", ThinkingStyle: registry.ThinkingStyleAnthropic}
	flagOnly := Invocation{Model: "m", PreserveThinking: registry.ThinkingOn}
	for name, capture := range map[string]func(*testing.T, Invocation) string{"complete": captureComplete, "chat": captureChat} {
		t.Run(name, func(t *testing.T) {
			body := capture(t, plain)
			for _, k := range thinkingKeys {
				assert.NotContains(t, body, `"`+k+`"`)
			}
			assert.Equal(t, body, capture(t, styleOnly))
			assert.Equal(t, body, capture(t, flagOnly))
		})
	}
}

// AC 03-03 Scenario 3 and Edge Case 2: a declared false is on the wire, and a
// declared style emits only its own members.
func TestThinking_DeclaredFalsePresentAndOnlyOwnStyle(t *testing.T) {
	assert.Contains(t, captureComplete(t, Invocation{Model: "m", Thinking: registry.ThinkingOff, ThinkingStyle: registry.ThinkingStyleQwen}), `"enable_thinking":false`)
	assert.Equal(t, `{"reasoning_effort":"low"}`,
		thinkingMembers(t, captureComplete(t, Invocation{Model: "m", ThinkingLevel: registry.ThinkingLevelLow, ThinkingStyle: registry.ThinkingStyleReasoningEffort})))
}

// AC 03-04: completion_tokens_details.reasoning_tokens decodes through the same
// tolerant path as the sibling counts, with a presence signal that tells a
// reported zero from a provider that never reports the field.
func TestUsageData_ReasoningTokensDecode(t *testing.T) {
	cases := []struct {
		name, usage string
		want        UsageData
	}{
		{"reported", `{"prompt_tokens":7,"completion_tokens":3,"completion_tokens_details":{"reasoning_tokens":120}}`,
			UsageData{PromptTokens: 7, CompletionTokens: 3, ReasoningTokens: 120, ReasoningTokensReported: true}},
		{"reported zero", `{"prompt_tokens":7,"completion_tokens":3,"completion_tokens_details":{"reasoning_tokens":0}}`,
			UsageData{PromptTokens: 7, CompletionTokens: 3, ReasoningTokensReported: true}},
		{"float", `{"completion_tokens_details":{"reasoning_tokens":45.0}}`,
			UsageData{ReasoningTokens: 45, ReasoningTokensReported: true}},
		{"block absent", `{"prompt_tokens":7,"completion_tokens":3}`,
			UsageData{PromptTokens: 7, CompletionTokens: 3}},
		{"field absent", `{"prompt_tokens":7,"completion_tokens_details":{"accepted_prediction_tokens":1}}`,
			UsageData{PromptTokens: 7}},
		{"block not an object", `{"prompt_tokens":7,"completion_tokens":3,"completion_tokens_details":"not-an-object"}`,
			UsageData{PromptTokens: 7, CompletionTokens: 3}},
		{"count not a number", `{"prompt_tokens":7,"completion_tokens":3,"completion_tokens_details":{"reasoning_tokens":"lots"}}`,
			UsageData{PromptTokens: 7, CompletionTokens: 3}},
		{"count null", `{"completion_tokens_details":{"reasoning_tokens":null}}`, UsageData{}},
		{"negative count", `{"completion_tokens_details":{"reasoning_tokens":-4}}`, UsageData{}},
		{"usage not an object", `"oops"`, UsageData{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var u UsageData
			require.NoError(t, json.Unmarshal([]byte(tc.usage), &u))
			assert.Equal(t, tc.want, u)
		})
	}
}

// reasoningServer answers every request with one choice carrying the given
// message JSON and a usage block that reports 120 reasoning tokens.
func reasoningServer(t *testing.T, messageJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(w, `{"choices":[{"finish_reason":"stop","message":`+messageJSON+`}],"usage":{"prompt_tokens":7,"completion_tokens":3,"completion_tokens_details":{"reasoning_tokens":120}}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TEST_KEY", testKey)
	return srv
}

// AC 03-04 Scenarios 1, 2 and AC 03-05: both paths report reasoning tokens and
// reasoning content on their own fields, alongside a non-empty answer.
func TestReasoningSignal_BothPaths(t *testing.T) {
	const msg = `{"role":"assistant","content":"the review","reasoning_content":"thinking it over"}`
	wantUsage := UsageData{PromptTokens: 7, CompletionTokens: 3, ReasoningTokens: 120, ReasoningTokensReported: true}

	srv := reasoningServer(t, msg)
	inv := Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"}
	comp, err := fastRetry(srv.Client()).CompleteWithMeta(context.Background(), inv)
	require.NoError(t, err)
	assert.Equal(t, "the review", comp.Content)
	assert.Equal(t, "thinking it over", comp.Reasoning)
	assert.Equal(t, wantUsage, comp.Usage)

	s := "hi"
	resp, err := fastRetry(srv.Client()).Chat(context.Background(), inv, []Message{{Role: "user", Content: &s}}, nil)
	require.NoError(t, err)
	require.NotNil(t, resp.Message.Content)
	assert.Equal(t, "the review", *resp.Message.Content)
	assert.Equal(t, "thinking it over", resp.Reasoning)
	assert.Equal(t, wantUsage, resp.Usage)
}

// AC 03-05 Scenario 3: the empty-Content salvage is unchanged and Reasoning
// carries the same text beside it.
func TestReasoningSignal_SalvageUnchanged(t *testing.T) {
	srv := reasoningServer(t, `{"role":"assistant","content":"","reasoning_content":"HIGH|a.go:1|bug"}`)
	comp, err := fastRetry(srv.Client()).CompleteWithMeta(context.Background(), Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"})
	require.NoError(t, err)
	assert.Equal(t, "HIGH|a.go:1|bug", comp.Content)
	assert.Equal(t, "HIGH|a.go:1|bug", comp.Reasoning)
}

// AC 03-05 Edge Case 1: no reasoning_content leaves the field empty on both paths.
func TestReasoningSignal_AbsentIsEmpty(t *testing.T) {
	srv := reasoningServer(t, `{"role":"assistant","content":"the review"}`)
	inv := Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"}
	comp, err := fastRetry(srv.Client()).CompleteWithMeta(context.Background(), inv)
	require.NoError(t, err)
	assert.Empty(t, comp.Reasoning)
	s := "hi"
	resp, err := fastRetry(srv.Client()).Chat(context.Background(), inv, []Message{{Role: "user", Content: &s}}, nil)
	require.NoError(t, err)
	assert.Empty(t, resp.Reasoning)
}

// carrierKeys are the reasoning members an assistant Message carries back into
// tool-loop history (sprint 35.16.11.2.2.1).
var carrierKeys = []string{"reasoning_content", "reasoning", "reasoning_details", "thinking_blocks"}

// carrierOf returns the reasoning members m would send, as the exact bytes it
// marshals them to.
func carrierOf(t *testing.T, m Message) map[string]string {
	t.Helper()
	b, err := json.Marshal(m)
	require.NoError(t, err)
	var all map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &all))
	out := map[string]string{}
	for _, k := range carrierKeys {
		if v, ok := all[k]; ok {
			out[k] = string(v)
		}
	}
	return out
}

// chatReply runs one Chat turn against a server that replies with messageJSON.
func chatReply(t *testing.T, messageJSON string) *ChatResponse {
	t.Helper()
	srv := reasoningServer(t, messageJSON)
	s := "hi"
	resp, err := fastRetry(srv.Client()).Chat(context.Background(), Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"}, []Message{{Role: "user", Content: &s}}, nil)
	require.NoError(t, err)
	return resp
}

// toolCallTurn is an assistant tool-call reply with extra members appended.
func toolCallTurn(members string) string {
	msg := `{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{}"}}]`
	if members != "" {
		msg += "," + members
	}
	return msg + "}"
}

// Sprint 35.16.11.2.2.1 AC 02-01: each provider's reasoning member decodes onto
// the assistant Message as the bytes received, under the key it arrived in.
// reasoning_content and reasoning stay separate, never merged.
func TestReasoningCarrier_DecodesEachShapeOntoMessage(t *testing.T) {
	const blocks = `[{"type":"thinking","thinking":"step 1","signature":"EqQBCkgIARABGAIiQL+/zzA0Xq9b=="}]`
	const details = `[{"type":"reasoning.text","text":"step 1"}]`
	cases := map[string]struct {
		members string
		want    map[string]string
	}{
		"reasoning_content": {`"reasoning_content":"because X"`, map[string]string{"reasoning_content": `"because X"`}},
		"reasoning":         {`"reasoning":"chain of thought"`, map[string]string{"reasoning": `"chain of thought"`}},
		"both string keys": {`"reasoning_content":"primary","reasoning":"alt"`,
			map[string]string{"reasoning_content": `"primary"`, "reasoning": `"alt"`}},
		"reasoning_details": {`"reasoning_details":` + details, map[string]string{"reasoning_details": details}},
		"thinking_blocks":   {`"thinking_blocks":` + blocks, map[string]string{"thinking_blocks": blocks}},
		"content and details": {`"reasoning_content":"because X","reasoning_details":` + details,
			map[string]string{"reasoning_content": `"because X"`, "reasoning_details": details}},
		"empty array and object": {`"thinking_blocks":[],"reasoning_details":{}`,
			map[string]string{"thinking_blocks": `[]`, "reasoning_details": `{}`}},
		"none": {"", map[string]string{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resp := chatReply(t, toolCallTurn(tc.members))
			assert.Equal(t, tc.want, carrierOf(t, resp.Message))
			require.Len(t, resp.Message.ToolCalls, 1, "the tool call survives beside the carrier")
		})
	}
}

// AC 02-01 Edge Cases 4-5 and Error Scenario 1: a null, an empty string, or a
// value of the wrong type is absent. It never fails the turn and never
// re-marshals as a member: null would change the request body, and "" is the
// presence marker the user decided never to send (Phase 2 clarification).
// Strings must be strings; reasoning_details and thinking_blocks must be an
// array or an object.
func TestReasoningCarrier_AbsentValuesLeaveNoMember(t *testing.T) {
	cases := map[string]string{
		"null reasoning_content":   `"reasoning_content":null`,
		"null reasoning":           `"reasoning":null`,
		"null reasoning_details":   `"reasoning_details":null`,
		"null thinking_blocks":     `"thinking_blocks":null`,
		"empty reasoning_content":  `"reasoning_content":""`,
		"empty reasoning":          `"reasoning":""`,
		"number reasoning_content": `"reasoning_content":42`,
		"object reasoning":         `"reasoning":{"text":"x"}`,
		"string thinking_blocks":   `"thinking_blocks":"x"`,
		"number reasoning_details": `"reasoning_details":7`,
	}
	for name, members := range cases {
		t.Run(name, func(t *testing.T) {
			resp := chatReply(t, toolCallTurn(members))
			assert.Empty(t, carrierOf(t, resp.Message))
			assert.Empty(t, resp.Reasoning)
			require.Len(t, resp.Message.ToolCalls, 1)
		})
	}
}

// AC 02-03 Scenarios 2-4 and Edge Cases 2, 4: each shape re-marshals as the
// exact bytes received. Escapes, unicode, and the Anthropic signature are
// unchanged, and array/object members stay JSON, never a quoted string.
func TestReasoningCarrier_RoundTripByteForByte(t *testing.T) {
	cases := map[string]string{
		"reasoning_content": `"line one\nline \"two\" caf\u00e9 café"`,
		"reasoning":         `"tab\there / slash"`,
		"reasoning_details": `[{"type":"reasoning.text","text":"a\tb","index":0}]`,
		"thinking_blocks":   `[{"type":"thinking","thinking":"weigh \"x\"","signature":"EqQBCkgIARABGAIiQL+/zzA0Xq9b+/9w=="},{"type":"redacted_thinking","data":"c2VjcmV0"}]`,
	}
	for key, raw := range cases {
		t.Run(key, func(t *testing.T) {
			resp := chatReply(t, toolCallTurn(`"`+key+`":`+raw))
			got := carrierOf(t, resp.Message)
			assert.Equal(t, raw, got[key], "%s must round-trip byte-for-byte", key)
			if key == "reasoning_details" || key == "thinking_blocks" {
				require.NotEmpty(t, got[key])
				assert.Contains(t, "[{", got[key][:1], "%s re-marshals as JSON, not a string", key)
			}
		})
	}
}

// encoding/json HTML-escapes <, >, and & in every marshaled string, the carrier
// included, so reasoning holding them re-sends as the same JSON value with
// different bytes. The decoded text is unchanged, which is what a provider
// reads.
func TestReasoningCarrier_HTMLCharsReencodeEquivalently(t *testing.T) {
	resp := chatReply(t, toolCallTurn(`"reasoning_content":"if a < b && c > d"`))
	got := carrierOf(t, resp.Message)["reasoning_content"]
	assert.Equal(t, `"if a \u003c b \u0026\u0026 c \u003e d"`, got)
	var s string
	require.NoError(t, json.Unmarshal([]byte(got), &s))
	assert.Equal(t, "if a < b && c > d", s)
}

// The same holds for a structured member sent with whitespace: it re-sends
// compacted and escaped, as the same JSON value, and the signature string is
// unchanged byte-for-byte.
func TestReasoningCarrier_StructuredMemberReencodesAsSameValue(t *testing.T) {
	const sent = `[ {"type": "thinking", "thinking": "a<b & c", "signature": "EqQBCkgIARABGAIiQL+/zzA0Xq9b=="} ]`
	resp := chatReply(t, toolCallTurn(`"thinking_blocks":`+sent))
	got := carrierOf(t, resp.Message)["thinking_blocks"]
	assert.JSONEq(t, sent, got)
	assert.Contains(t, got, `"signature":"EqQBCkgIARABGAIiQL+/zzA0Xq9b=="`)
	assert.Contains(t, got, `"thinking":"a\u003cb \u0026 c"`)
}

// goldenToolHistoryRequest is a tool-loop turn-2 body (user, assistant tool
// call, tool result) captured from pre-plan main at e5c9754d, before Message
// had any reasoning member.
const goldenToolHistoryRequest = `{"model":"m","messages":[{"role":"user","content":"review f.go"},{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f.go\"}"}}]},{"role":"tool","content":"package main","tool_call_id":"c1"}],"tools":[{"function":{"description":"Read a file","name":"read_file","parameters":{"type":"object"}},"type":"function"}],"tool_choice":"auto","temperature":0.3,"max_tokens":512}`

// AC 02-03 Scenario 1: a history with no reasoning set, tool-call turn
// included, marshals byte-identical to the pre-plan body.
func TestMessage_ReasoningCarrierUnsetIsByteIdentical(t *testing.T) {
	u, tr := "review f.go", "package main"
	temp, maxTok := 0.3, 512
	b, err := json.Marshal(chatToolRequest{
		Model: "m",
		Messages: []Message{
			{Role: "user", Content: &u},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "read_file", Arguments: json.RawMessage(`"{\"path\":\"f.go\"}"`)}}}},
			{Role: "tool", Content: &tr, ToolCallID: "c1"},
		},
		Tools:          []ToolDef{{Name: "read_file", Description: "Read a file", Parameters: map[string]any{"type": "object"}}},
		ToolChoice:     "auto",
		Temperature:    &temp,
		MaxTokens:      &maxTok,
		thinkingFields: newThinkingFields("", "", "", ""),
	})
	require.NoError(t, err)
	require.Equal(t, goldenToolHistoryRequest, string(b))
}

// Replaces TestMessage_HasNoReasoningField (sprint 35.16.11.2.2), which pinned
// the opposite: Message now carries reasoning back into tool-loop history.
// Every carrier member is raw JSON, so no shape is reshaped, and omitempty, so
// an unset carrier adds nothing to the body.
func TestMessage_ReasoningCarrierIsRawAndOmitempty(t *testing.T) {
	typ := reflect.TypeOf(Message{})
	var found []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")
		for _, k := range carrierKeys {
			if tag[0] != k {
				continue
			}
			found = append(found, k)
			assert.Equal(t, reflect.TypeOf(json.RawMessage(nil)), f.Type, "%s is raw JSON", k)
			assert.Contains(t, tag[1:], "omitempty", "%s is omitempty", k)
		}
	}
	assert.ElementsMatch(t, carrierKeys, found)
	content := "the review"
	assert.Empty(t, carrierOf(t, Message{Role: "assistant", Content: &content}))
}

// Sprint 35.16.11.2.2.1 AC 04-03: the Message shapes the tool loop builds for
// user and tool turns (Role and Content, plus ToolCallID) marshal with no
// reasoning member, while an assistant turn with a carrier set sends it. This
// pins the wire shape only; that the loop's own helpers never set a carrier is
// proved in internal/fanout (TestToolLoop_EachAssistantTurnReplaysOnlyItsOwnReasoning).
func TestMessage_ReasoningRidesOnlyTheAssistantTurn(t *testing.T) {
	content, result := "the review", "package main"
	assistant := Message{Role: "assistant", Content: &content, ReasoningContent: json.RawMessage(`"because X"`)}
	assert.Equal(t, map[string]string{"reasoning_content": `"because X"`}, carrierOf(t, assistant))
	assert.Empty(t, carrierOf(t, Message{Role: "user", Content: &content}))
	assert.Empty(t, carrierOf(t, Message{Role: "tool", ToolCallID: "c1", Content: &result}))
}

// A malformed reasoning signal never fails the turn: a non-string
// reasoning_content is treated as absent on both paths, and the answer and
// tool calls survive. A tool-call turn with content:null still decodes through
// the response wrapper.
func TestReasoningSignal_NonStringIsAbsent(t *testing.T) {
	for name, rc := range map[string]string{"object": `{"text":"x"}`, "array": `["x"]`, "number": `7`, "null": `null`} {
		t.Run(name, func(t *testing.T) {
			srv := reasoningServer(t, `{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{}"}}],"reasoning_content":`+rc+`}`)
			inv := Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"}
			s := "hi"
			resp, err := fastRetry(srv.Client()).Chat(context.Background(), inv, []Message{{Role: "user", Content: &s}}, nil)
			require.NoError(t, err)
			assert.Nil(t, resp.Message.Content)
			require.Len(t, resp.Message.ToolCalls, 1)
			assert.Equal(t, "read_file", resp.Message.ToolCalls[0].Function.Name)
			assert.Empty(t, resp.Reasoning)

			srv2 := reasoningServer(t, `{"role":"assistant","content":"the review","reasoning_content":`+rc+`}`)
			inv.BaseURL = srv2.URL
			comp, err := fastRetry(srv2.Client()).CompleteWithMeta(context.Background(), inv)
			require.NoError(t, err)
			assert.Equal(t, "the review", comp.Content)
			assert.Empty(t, comp.Reasoning)
		})
	}
}

// The reasoning fields are decode-only: UsageData has no wire key for them, so
// a later serializer must choose their names on purpose.
func TestUsageData_ReasoningFieldsNotSerialized(t *testing.T) {
	b, err := json.Marshal(UsageData{PromptTokens: 1, ReasoningTokens: 5, ReasoningTokensReported: true})
	require.NoError(t, err)
	assert.JSONEq(t, `{"prompt_tokens":1,"completion_tokens":0}`, string(b))
}

// SentTemperature reports what the body carries: the declared temperature,
// except under enabled anthropic thinking, where none is sent.
func TestSentTemperature(t *testing.T) {
	temp := 0.7
	inv := Invocation{Temperature: &temp}
	assert.Same(t, &temp, SentTemperature(inv), "undeclared: sent as declared")
	inv.Thinking, inv.ThinkingStyle = "off", "anthropic"
	assert.Same(t, &temp, SentTemperature(inv), "anthropic off: sent as declared")
	inv.Thinking = "on"
	assert.Nil(t, SentTemperature(inv), "anthropic on: none sent")
	inv.ThinkingStyle = "qwen"
	assert.Same(t, &temp, SentTemperature(inv), "other styles: sent as declared")
}

// TD-012: OpenRouter and newer vLLM put reasoning under message.reasoning.
// It is read when reasoning_content is absent, and reasoning_content wins when
// both are sent. The empty-Content salvage reads both keys too, so a cut-off
// thinking reply salvages the same way regardless of which key the provider
// uses.
func TestReasoningSignal_ReasoningKeyFallback(t *testing.T) {
	cases := map[string]struct{ msg, want string }{
		"reasoning only":   {`{"role":"assistant","content":"the review","reasoning":"alt channel"}`, "alt channel"},
		"both keys":        {`{"role":"assistant","content":"the review","reasoning_content":"primary","reasoning":"alt channel"}`, "primary"},
		"reasoning object": {`{"role":"assistant","content":"the review","reasoning":{"text":"x"}}`, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := reasoningServer(t, tc.msg)
			inv := Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"}
			comp, err := fastRetry(srv.Client()).CompleteWithMeta(context.Background(), inv)
			require.NoError(t, err)
			assert.Equal(t, "the review", comp.Content)
			assert.Equal(t, tc.want, comp.Reasoning)
			s := "hi"
			resp, err := fastRetry(srv.Client()).Chat(context.Background(), inv, []Message{{Role: "user", Content: &s}}, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.want, resp.Reasoning)
		})
	}

	srv := reasoningServer(t, `{"role":"assistant","content":"","reasoning":"HIGH|a.go:1|bug"}`)
	comp, err := fastRetry(srv.Client()).CompleteWithMeta(context.Background(), Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"})
	require.NoError(t, err, "the reasoning key alone must be salvaged into Content like reasoning_content")
	assert.Equal(t, "HIGH|a.go:1|bug", comp.Content)
}
