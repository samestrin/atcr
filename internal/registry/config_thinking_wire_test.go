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

// A failed agent writes no budget warning, whether the fault is a thinking key
// or any other field.
func TestValidateAgent_ThinkingBudgetWarningOnlyOnValidAgent(t *testing.T) {
	for name, agent := range map[string]string{
		"thinking fault":     thinkingAgent(ThinkingOff, ThinkingLevelMax, ThinkingStyleQwen),
		"non-thinking fault": thinkingAgent("", ThinkingLevelMax, ThinkingStyleQwen) + "    max_tokens: 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			buf := captureThinkingWarnings(t)
			_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(agent)))
			require.Error(t, err)
			assert.False(t, strings.Contains(buf.String(), "thinking budget"), "got %q", buf.String())
		})
	}
}

// Anthropic rejects extended thinking with any temperature but 1. A declared
// temperature other than 1 with anthropic thinking on is a load error; an
// undeclared one loads (the wire then sends no temperature).
func TestValidateAgent_AnthropicThinkingTemperature(t *testing.T) {
	const wantErr = `agent 'myagent': thinking_style "anthropic" with thinking on needs temperature 1: remove temperature or set it to 1`
	cases := []struct {
		name, thinking, level, temperature string
		wantErr                            bool
	}{
		{"on with 0.7", ThinkingOn, "", "0.7", true},
		{"level alone with 0", "", ThinkingLevelLow, "0", true},
		{"on with 1", ThinkingOn, "", "1", false},
		{"on with 1.0", ThinkingOn, ThinkingLevelHigh, "1.0", false},
		{"on undeclared", ThinkingOn, "", "", false},
		{"off with 0.7", ThinkingOff, "", "0.7", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureThinkingWarnings(t)
			agent := thinkingAgent(tc.thinking, tc.level, ThinkingStyleAnthropic) + "    max_tokens: 65536\n"
			if tc.temperature != "" {
				agent += "    temperature: " + tc.temperature + "\n"
			}
			reg, err := LoadRegistry(writeRegistry(t, thinkingRegistry(agent)))
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, reg.Agents["myagent"].Temperature, "defaults still apply after validation")
		})
	}
	// Other styles keep any temperature.
	_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(thinkingAgent(ThinkingOn, "", ThinkingStyleQwen)+"    temperature: 0.2\n")))
	require.NoError(t, err)
}
