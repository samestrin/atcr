package llmclient

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// thinkingKeys are every top-level request member a thinking style can emit.
var thinkingKeys = []string{"enable_thinking", "thinking_budget", "chat_template_kwargs", "reasoning_effort", "thinking"}

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
	{"qwen on high", ThinkingOn, ThinkingLevelHigh, ThinkingStyleQwen, `{"enable_thinking":true,"thinking_budget":16384}`},
	{"qwen level alone medium", "", ThinkingLevelMedium, ThinkingStyleQwen, `{"enable_thinking":true,"thinking_budget":8192}`},
	{"qwen max not clamped", "", ThinkingLevelMax, ThinkingStyleQwen, `{"enable_thinking":true,"thinking_budget":32768}`},
	{"qwen on no level", ThinkingOn, "", ThinkingStyleQwen, `{"enable_thinking":true}`},
	{"qwen off", ThinkingOff, "", ThinkingStyleQwen, `{"enable_thinking":false}`},
	// template_kwargs: on/off only.
	{"template_kwargs off", ThinkingOff, "", ThinkingStyleTemplateKwargs, `{"chat_template_kwargs":{"enable_thinking":false}}`},
	{"template_kwargs on", ThinkingOn, "", ThinkingStyleTemplateKwargs, `{"chat_template_kwargs":{"enable_thinking":true}}`},
	// reasoning_effort: the level itself, max clamped to high.
	{"reasoning_effort low", "", ThinkingLevelLow, ThinkingStyleReasoningEffort, `{"reasoning_effort":"low"}`},
	{"reasoning_effort on medium", ThinkingOn, ThinkingLevelMedium, ThinkingStyleReasoningEffort, `{"reasoning_effort":"medium"}`},
	{"reasoning_effort max clamps", "", ThinkingLevelMax, ThinkingStyleReasoningEffort, `{"reasoning_effort":"high"}`},
	// anthropic: a thinking object; on with no level takes the medium budget.
	{"anthropic on low", ThinkingOn, ThinkingLevelLow, ThinkingStyleAnthropic, `{"thinking":{"type":"enabled","budget_tokens":2048}}`},
	{"anthropic level alone max", "", ThinkingLevelMax, ThinkingStyleAnthropic, `{"thinking":{"type":"enabled","budget_tokens":32768}}`},
	{"anthropic on no level", ThinkingOn, "", ThinkingStyleAnthropic, `{"thinking":{"type":"enabled","budget_tokens":8192}}`},
	{"anthropic off", ThinkingOff, "", ThinkingStyleAnthropic, `{"thinking":{"type":"disabled"}}`},
	// Nothing to send.
	{"unset", "", "", "", `{}`},
	{"style alone", "", "", ThinkingStyleQwen, `{}`},
	{"unknown style not coerced", ThinkingOn, "", "openai", `{}`},
	{"no style", ThinkingOff, "", "", `{}`},
	// Values the registry would reject are not guessed at: nothing is sent.
	{"unknown thinking qwen", "true", "", ThinkingStyleQwen, `{}`},
	{"unknown thinking anthropic", "yes", "", ThinkingStyleAnthropic, `{}`},
	{"unknown level anthropic", "", "extreme", ThinkingStyleAnthropic, `{}`},
	{"unknown level reasoning_effort", "", "extreme", ThinkingStyleReasoningEffort, `{}`},
	{"unknown level qwen", ThinkingOn, "extreme", ThinkingStyleQwen, `{}`},
}

// AC 03-01: one mapper, one populated style per declaration, exact wire JSON.
func TestNewThinkingFields_PerStyle(t *testing.T) {
	for _, tc := range thinkingCases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(newThinkingFields(tc.thinking, tc.level, tc.style))
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(b))
		})
	}
}

// AC 03-01 DoD: a declared false is a non-nil pointer, distinct from unset.
func TestNewThinkingFields_FalseIsNotUnset(t *testing.T) {
	off := newThinkingFields(ThinkingOff, "", ThinkingStyleQwen)
	require.NotNil(t, off.EnableThinking)
	assert.False(t, *off.EnableThinking)
	assert.Nil(t, newThinkingFields("", "", ThinkingStyleQwen).EnableThinking)

	kw := newThinkingFields(ThinkingOff, "", ThinkingStyleTemplateKwargs)
	require.NotNil(t, kw.ChatTemplateKwargs)
	require.NotNil(t, kw.ChatTemplateKwargs.EnableThinking)
	assert.False(t, *kw.ChatTemplateKwargs.EnableThinking)
}

// AC 03-01 Edge Case 2: qwen and anthropic read one level-to-budget table, and
// ThinkingBudgetTokens reports exactly what the wire carries.
func TestThinkingBudgetTokens_OneTable(t *testing.T) {
	want := map[string]int{ThinkingLevelLow: 2048, ThinkingLevelMedium: 8192, ThinkingLevelHigh: 16384, ThinkingLevelMax: 32768}
	for level, budget := range want {
		assert.Equal(t, budget, ThinkingBudgetTokens("", level, ThinkingStyleQwen), "qwen %s", level)
		assert.Equal(t, budget, ThinkingBudgetTokens("", level, ThinkingStyleAnthropic), "anthropic %s", level)
		q, a := newThinkingFields("", level, ThinkingStyleQwen), newThinkingFields("", level, ThinkingStyleAnthropic)
		require.NotNil(t, q.ThinkingBudget)
		require.NotNil(t, a.Thinking)
		assert.Equal(t, budget, *q.ThinkingBudget)
		assert.Equal(t, budget, a.Thinking.BudgetTokens)
	}
	assert.Equal(t, 8192, ThinkingBudgetTokens(ThinkingOn, "", ThinkingStyleAnthropic), "anthropic on with no level uses medium")
	for _, tc := range []struct{ thinking, level, style string }{
		{ThinkingOn, "", ThinkingStyleQwen},
		{ThinkingOff, "", ThinkingStyleQwen},
		{ThinkingOff, "", ThinkingStyleAnthropic},
		{"", ThinkingLevelHigh, ThinkingStyleReasoningEffort},
		{ThinkingOn, "", ThinkingStyleTemplateKwargs},
		{"", "", ""},
	} {
		assert.Zero(t, ThinkingBudgetTokens(tc.thinking, tc.level, tc.style), "%+v sends no budget", tc)
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
			enabled := strings.Contains(tc.want, `"type":"enabled"`)
			for path, body := range map[string]string{"complete": captureComplete(t, inv), "chat": captureChat(t, inv), "final": captureChatWith(t, inv, nil)} {
				var got map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(body), &got))
				_, has := got["temperature"]
				assert.Equal(t, !enabled, has, "%s path: temperature sent = %v", path, has)
			}
		})
	}
}

// response_format and a thinking declaration ride the same body together.
func TestThinking_CoexistsWithResponseFormat(t *testing.T) {
	inv := Invocation{Model: "m", ResponseFormat: "json_object", ThinkingLevel: ThinkingLevelLow, ThinkingStyle: ThinkingStyleReasoningEffort}
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
		thinkingFields: newThinkingFields("", "", ""),
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
		thinkingFields: newThinkingFields("", "", ""),
	})
	require.NoError(t, err)
	require.Equal(t, goldenChatToolRequest, string(b))
}

// AC 03-03 Edge Case 1 and AC 03-01 Edge Case 4: an unset or style-only agent
// sends the same bytes as an agent written before the keys existed, on both
// paths, with no thinking key at all (not as {}, not as null).
func TestThinking_UndeclaredSendsNoThinkingKey(t *testing.T) {
	plain := Invocation{Model: "m"}
	styleOnly := Invocation{Model: "m", ThinkingStyle: ThinkingStyleAnthropic}
	for name, capture := range map[string]func(*testing.T, Invocation) string{"complete": captureComplete, "chat": captureChat} {
		t.Run(name, func(t *testing.T) {
			body := capture(t, plain)
			for _, k := range thinkingKeys {
				assert.NotContains(t, body, `"`+k+`"`)
			}
			assert.Equal(t, body, capture(t, styleOnly))
		})
	}
}

// AC 03-03 Scenario 3 and Edge Case 2: a declared false is on the wire, and a
// declared style emits only its own members.
func TestThinking_DeclaredFalsePresentAndOnlyOwnStyle(t *testing.T) {
	assert.Contains(t, captureComplete(t, Invocation{Model: "m", Thinking: ThinkingOff, ThinkingStyle: ThinkingStyleQwen}), `"enable_thinking":false`)
	assert.Equal(t, `{"reasoning_effort":"low"}`,
		thinkingMembers(t, captureComplete(t, Invocation{Model: "m", ThinkingLevel: ThinkingLevelLow, ThinkingStyle: ThinkingStyleReasoningEffort})))
}
