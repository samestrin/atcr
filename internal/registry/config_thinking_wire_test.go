package registry

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TD-006: internal/llmclient stays a leaf and duplicates the thinking values
// it forwards, so a renamed value on one side must fail here, not at the wire.
func TestThinkingValues_MatchLLMClient(t *testing.T) {
	assert.Equal(t, []string{llmclient.ThinkingOn, llmclient.ThinkingOff}, ThinkingValues())
	assert.Equal(t, []string{llmclient.ThinkingLevelLow, llmclient.ThinkingLevelMedium, llmclient.ThinkingLevelHigh, llmclient.ThinkingLevelMax}, ThinkingLevels())
	assert.Equal(t, []string{llmclient.ThinkingStyleQwen, llmclient.ThinkingStyleTemplateKwargs, llmclient.ThinkingStyleReasoningEffort, llmclient.ThinkingStyleAnthropic}, ThinkingStyles())
}

// Phase 1 clarification: a budget that is not below the agent's output cap
// warns at load (never an error). The cap is the declared max_tokens, else the
// 8192 default.
func TestValidateAgent_ThinkingBudgetWarning(t *testing.T) {
	cases := []struct {
		name, thinking, level, style, maxTokens string
		want                                    string // "" = no warning
	}{
		{"qwen high equals cap", "", ThinkingLevelHigh, ThinkingStyleQwen, "16384",
			"warning: agent 'myagent': thinking budget 16384 (thinking_level \"high\") is not below max_tokens 16384; the budget shares the output cap, so raise max_tokens or lower thinking_level\n"},
		{"qwen max over default", "", ThinkingLevelMax, ThinkingStyleQwen, "",
			"warning: agent 'myagent': thinking budget 32768 (thinking_level \"max\") is not below max_tokens 8192 (the default; --max-tokens can change it); the budget shares the output cap, so raise max_tokens or lower thinking_level\n"},
		{"anthropic on uses medium at default", ThinkingOn, "", ThinkingStyleAnthropic, "",
			"warning: agent 'myagent': thinking budget 8192 (thinking_level \"medium\") is not below max_tokens 8192 (the default; --max-tokens can change it); the budget shares the output cap, so raise max_tokens or lower thinking_level\n"},
		{"qwen high under cap", "", ThinkingLevelHigh, ThinkingStyleQwen, "32768", ""},
		{"qwen low under default", "", ThinkingLevelLow, ThinkingStyleQwen, "", ""},
		{"qwen on no level", ThinkingOn, "", ThinkingStyleQwen, "", ""},
		{"qwen off", ThinkingOff, "", ThinkingStyleQwen, "100", ""},
		{"anthropic off", ThinkingOff, "", ThinkingStyleAnthropic, "100", ""},
		{"reasoning_effort sends no budget", "", ThinkingLevelHigh, ThinkingStyleReasoningEffort, "100", ""},
		{"template_kwargs sends no budget", ThinkingOn, "", ThinkingStyleTemplateKwargs, "100", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureThinkingWarnings(t)
			agent := thinkingAgent(tc.thinking, tc.level, tc.style)
			if tc.maxTokens != "" {
				agent += "    max_tokens: " + tc.maxTokens + "\n"
			}
			_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(agent)))
			require.NoError(t, err, "the budget check never fails a load")
			assert.Equal(t, tc.want, buf.String())
		})
	}
}

// The budget warning and the reasoning_effort clamp warning are independent;
// a failed load writes no budget warning.
func TestValidateAgent_ThinkingBudgetWarningOnlyOnValidAgent(t *testing.T) {
	buf := captureThinkingWarnings(t)
	_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(thinkingAgent(ThinkingOff, ThinkingLevelMax, ThinkingStyleQwen))))
	require.Error(t, err)
	assert.False(t, strings.Contains(buf.String(), "thinking budget"), "got %q", buf.String())
}
