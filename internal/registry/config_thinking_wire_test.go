package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The one level-to-budget table, and which declarations send a budget at all.
func TestThinkingBudgetTokens_Table(t *testing.T) {
	want := map[string]int{ThinkingLevelLow: 2048, ThinkingLevelMedium: 8192, ThinkingLevelHigh: 16384, ThinkingLevelMax: 32768}
	require.Len(t, want, len(ThinkingLevels()))
	for level, budget := range want {
		assert.Equal(t, budget, ThinkingBudgetTokens("", level, ThinkingStyleQwen), "qwen %s", level)
		assert.Equal(t, budget, ThinkingBudgetTokens(ThinkingOn, level, ThinkingStyleAnthropic), "anthropic %s", level)
	}
	assert.Equal(t, 8192, ThinkingBudgetTokens(ThinkingOn, "", ThinkingStyleAnthropic), "anthropic on with no level uses medium")
	for _, tc := range []struct{ thinking, level, style string }{
		{ThinkingOn, "", ThinkingStyleQwen},
		{ThinkingOff, "", ThinkingStyleQwen},
		{ThinkingOff, "", ThinkingStyleAnthropic},
		{"", ThinkingLevelHigh, ThinkingStyleReasoningEffort},
		{ThinkingOn, "", ThinkingStyleTemplateKwargs},
		{"", ThinkingLevelHigh, ""},
		{"", "extreme", ThinkingStyleQwen},
		{"yes", ThinkingLevelHigh, ThinkingStyleAnthropic},
		{"", "", ""},
	} {
		assert.Zero(t, ThinkingBudgetTokens(tc.thinking, tc.level, tc.style), "%+v sends no budget", tc)
	}
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
			"warning: agent 'myagent': thinking budget 16384 (thinking_level \"high\") is not below max_tokens 16384 (--max-tokens can change it); the budget shares the output cap, so raise max_tokens or lower thinking_level\n"},
		{"qwen max over default", "", ThinkingLevelMax, ThinkingStyleQwen, "",
			"warning: agent 'myagent': thinking budget 32768 (thinking_level \"max\") is not below max_tokens 8192 (the default; --max-tokens can change it); the budget shares the output cap, so raise max_tokens or lower thinking_level\n"},
		// anthropic on with no level at the default cap is now a load error
		// (TestValidateAgent_ThinkingBudgetMisfitErrors), so it has no row here:
		// the budget warning covers only the styles whose budget is advisory.
		{"qwen high under cap", "", ThinkingLevelHigh, ThinkingStyleQwen, "32768", ""},
		// Anthropic explicit levels (TD row config_thinking_wire_test.go:38):
		// low fits the default cap and stays silent; a higher explicit level
		// with an adequate declared cap also stays silent. A higher explicit
		// level at the default cap is a load error now, not a warning — that
		// case is pinned by TestValidateAgent_ThinkingBudgetMisfitErrors.
		{"anthropic on level low default cap", ThinkingOn, ThinkingLevelLow, ThinkingStyleAnthropic, "", ""},
		{"anthropic on level high declared cap above", ThinkingOn, ThinkingLevelHigh, ThinkingStyleAnthropic, "32768", ""},
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

// TD internal/registry/config.go:1515: renamed to what it actually proves.
// The len(errs)==0 gate before warnThinkingBudget (config.go:1561) and the
// warning-buffer clear are what suppress warnings on a failed load; this test
// pins THAT observable behavior (no budget warning when validation fails), not
// the gate's internal necessity — warnings only ever emit after a load
// succeeds, so the gate alone is not what the old name claimed to cover.
func TestValidateAgent_ThinkingBudgetWarningsSuppressedWhenValidationFails(t *testing.T) {
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

// The tool loop replays each turn's reasoning (thinking_blocks included), but
// that replay has never run against a live Anthropic model: the proxy serves
// none (sprint 35.16.11.2.2.1, probe result (e)), and Anthropic rejects a
// tool-use turn whose thinking blocks are missing or altered. So the guard
// stays until a live run proves it. Any lane can put an agent in the tool loop
// (the skeptic and debate seats force tools, and a fallback takes its
// primary's tools), but only when the agent's own model declares
// supports_function_calling, so that flag is the guard.
func TestValidateAgent_AnthropicThinkingWithFunctionCalling(t *testing.T) {
	const wantErr = `agent 'myagent': thinking_style "anthropic" with thinking on cannot use supports_function_calling: true: the tool loop's reasoning replay has not been verified against a live Anthropic model, which rejects a tool-use turn whose thinking blocks are missing or altered; set supports_function_calling: false or thinking: off`
	cases := []struct {
		name, thinking, level, style string
		fc, tools, wantErr           bool
	}{
		{"on fc tools", ThinkingOn, "", ThinkingStyleAnthropic, true, true, true},
		{"on fc no tools (skeptic, debate, fallback lanes)", ThinkingOn, "", ThinkingStyleAnthropic, true, false, true},
		{"level alone fc", "", ThinkingLevelLow, ThinkingStyleAnthropic, true, false, true},
		// A Claude agent under reasoning_effort gets no second style-keyed
		// guard: its reasoning rides the same style-agnostic replay
		// (TestToolLoop_ReplayIgnoresThinkingStyle in internal/fanout).
		{"reasoning_effort on fc", ThinkingOn, ThinkingLevelLow, ThinkingStyleReasoningEffort, true, true, false},
		{"on tools without fc degrades to single-shot", ThinkingOn, "", ThinkingStyleAnthropic, false, true, false},
		{"on no fc", ThinkingOn, "", ThinkingStyleAnthropic, false, false, false},
		{"off fc", ThinkingOff, "", ThinkingStyleAnthropic, true, true, false},
		{"qwen on fc", ThinkingOn, "", ThinkingStyleQwen, true, true, false},
		{"style alone fc", "", "", ThinkingStyleAnthropic, true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureThinkingWarnings(t)
			agent := thinkingAgent(tc.thinking, tc.level, tc.style) + "    max_tokens: 65536\n"
			if tc.fc {
				agent += "    supports_function_calling: true\n"
			}
			if tc.tools {
				agent += "    tools: true\n"
			}
			_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(agent)))
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
	// No level was run live, so the guard holds at every level.
	for _, level := range ThinkingLevels() {
		captureThinkingWarnings(t)
		agent := thinkingAgent("", level, ThinkingStyleAnthropic) + "    max_tokens: 65536\n    supports_function_calling: true\n"
		_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(agent)))
		require.Error(t, err, "level %s", level)
		assert.Contains(t, err.Error(), wantErr, "level %s", level)
	}
}

// Sprint 35.16.11.2.2.1 AC 05-04 Edge Case 1: the function-calling guard does
// not mask a sibling guard. One agent that trips it and the temperature,
// response_format, and budget guards reports all four.
func TestValidateAgent_AnthropicFunctionCallingGuardDoesNotMaskSiblings(t *testing.T) {
	captureThinkingWarnings(t)
	agent := thinkingAgent(ThinkingOn, ThinkingLevelMax, ThinkingStyleAnthropic) +
		"    max_tokens: 4096\n    temperature: 0.5\n    response_format: json_object\n    supports_function_calling: true\n"
	_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(agent)))
	require.Error(t, err)
	for _, want := range []string{
		"cannot use supports_function_calling: true",
		"needs temperature 1",
		`cannot use response_format: "json_object"`,
		"is not below max_tokens 4096",
	} {
		assert.Contains(t, err.Error(), want)
	}
}

// TD row config.go:1200: load warnings are collected during validation and
// emitted once, after ALL validation succeeds — a load that fails must not
// print advice for config that never runs. Today the reasoning_effort clamp
// warning is written mid-validation, so a registry with any other fault still
// emits it.
func TestThinkingWarnings_EmittedOnlyOnFullyValidLoad(t *testing.T) {
	// The clamp warning triggers (reasoning_effort + max), but the agent has an
	// unrelated fault, so the load errors and no warning may be written.
	buf := captureThinkingWarnings(t)
	_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(thinkingAgent("", ThinkingLevelMax, ThinkingStyleReasoningEffort)+"    payload: bogus\n")))
	require.Error(t, err)
	assert.Empty(t, buf.String(), "a failed load must not emit thinking warnings, got %q", buf.String())

	// The same agent without the fault loads and emits exactly once.
	buf2 := captureThinkingWarnings(t)
	_, err = LoadRegistry(writeRegistry(t, thinkingRegistry(thinkingAgent("", ThinkingLevelMax, ThinkingStyleReasoningEffort))))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(buf2.String(), "\n"), "exactly one warning on a valid load: %q", buf2.String())
}

// The merged load emits the effective roster's warnings exactly once.
func TestThinkingWarnings_EmittedOnceFromMergedLoad(t *testing.T) {
	// The overlay declares agents only, referencing the user-tier provider p —
	// a project agent on a user provider passes the trust gate freely.
	writeProject := func(t *testing.T, agents string) string {
		dir := t.TempDir()
		path := filepath.Join(dir, ".atcr")
		require.NoError(t, os.MkdirAll(path, 0o755))
		body := "agents:\n" + agents
		require.NoError(t, os.WriteFile(filepath.Join(path, "registry.yaml"), []byte(body), 0o600))
		return dir
	}
	userBody := thinkingRegistry(thinkingAgent("", ThinkingLevelMax, ThinkingStyleReasoningEffort))
	t.Run("user tier alone", func(t *testing.T) {
		buf := captureThinkingWarnings(t)
		_, err := LoadMergedRegistry(writeRegistry(t, userBody), t.TempDir())
		require.NoError(t, err)
		assert.Equal(t, 1, strings.Count(buf.String(), "\n"), "exactly one warning from the merged load: %q", buf.String())
	})
	t.Run("project overlay shadows the warning agent", func(t *testing.T) {
		buf := captureThinkingWarnings(t)
		// The overlay replaces myagent with a low-level declaration that warns
		// about nothing; the user-tier warning must not fire.
		root := writeProject(t, thinkingAgent("", ThinkingLevelLow, ThinkingStyleReasoningEffort))
		_, err := LoadMergedRegistry(writeRegistry(t, userBody), root)
		require.NoError(t, err)
		assert.Empty(t, buf.String(), "the shadowing declaration's state governs, got %q", buf.String())
	})
}
