package registry

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureThinkingWarnings swaps thinkingWarnWriter for a buffer for the test.
func captureThinkingWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := thinkingWarnWriter
	thinkingWarnWriter = &buf
	t.Cleanup(func() { thinkingWarnWriter = orig })
	return &buf
}

// thinkingAgent renders one agent entry named "myagent" with the given
// thinking keys; an empty value omits that key.
func thinkingAgent(thinking, level, style string) string {
	var b strings.Builder
	b.WriteString("  myagent:\n    provider: p\n    model: m\n")
	if thinking != "" {
		b.WriteString("    thinking: " + thinking + "\n")
	}
	if level != "" {
		b.WriteString("    thinking_level: " + level + "\n")
	}
	if style != "" {
		b.WriteString("    thinking_style: " + style + "\n")
	}
	return b.String()
}

// AC 02-01 / 02-02 / 02-03: every invalid combination fails load with an
// error naming the agent; every valid combination loads.
func TestValidateAgent_ThinkingCombinations(t *testing.T) {
	levels := strings.Join(ThinkingLevels(), ", ")
	styles := strings.Join(ThinkingStyles(), ", ")
	cases := []struct {
		name                   string
		thinking, level, style string
		wantErr                string // "" = must load
	}{
		// Valid.
		{"on with style", ThinkingOn, "", ThinkingStyleQwen, ""},
		{"off with qwen", ThinkingOff, "", ThinkingStyleQwen, ""},
		{"off with template_kwargs", ThinkingOff, "", ThinkingStyleTemplateKwargs, ""},
		{"on with template_kwargs", ThinkingOn, "", ThinkingStyleTemplateKwargs, ""},
		{"off with anthropic", ThinkingOff, "", ThinkingStyleAnthropic, ""},
		{"on level style", ThinkingOn, ThinkingLevelMedium, ThinkingStyleAnthropic, ""},
		{"level alone with style", "", ThinkingLevelHigh, ThinkingStyleQwen, ""},
		{"style alone", "", "", ThinkingStyleQwen, ""},
		{"reasoning_effort low", "", ThinkingLevelLow, ThinkingStyleReasoningEffort, ""},
		{"reasoning_effort on low", ThinkingOn, ThinkingLevelLow, ThinkingStyleReasoningEffort, ""},
		{"max under qwen", "", ThinkingLevelMax, ThinkingStyleQwen, ""},
		{"none", "", "", "", ""},

		// 1. Unrecognized thinking value, including bool-shaped literals.
		{"thinking maybe", "maybe", "", ThinkingStyleQwen, `agent 'myagent': invalid thinking "maybe": must be "on" or "off" or unset`},
		{"thinking true", "true", "", ThinkingStyleQwen, `agent 'myagent': invalid thinking "true": must be "on" or "off" or unset`},
		{"thinking false", "false", "", ThinkingStyleQwen, `agent 'myagent': invalid thinking "false": must be "on" or "off" or unset`},
		{"thinking upper", "ON", "", ThinkingStyleQwen, `agent 'myagent': invalid thinking "ON": must be "on" or "off" or unset`},
		// 2. Unknown level, case-sensitive.
		{"level extreme", "", "extreme", ThinkingStyleQwen, `agent 'myagent': invalid thinking_level "extreme": must be one of ` + levels},
		{"level wrong case", "", "Low", ThinkingStyleQwen, `agent 'myagent': invalid thinking_level "Low": must be one of ` + levels},
		// 3. Unknown style, case-sensitive.
		{"style openai", ThinkingOn, "", "openai", `agent 'myagent': invalid thinking_style "openai": must be one of ` + styles},
		{"style wrong case", ThinkingOn, "", "Qwen", `agent 'myagent': invalid thinking_style "Qwen": must be one of ` + styles},
		// 4. off with a level, regardless of style.
		{"off with level", ThinkingOff, ThinkingLevelMedium, ThinkingStyleQwen, `agent 'myagent': thinking is "off" but thinking_level "medium" is set: remove thinking_level or set thinking: on`},
		{"off with level anthropic", ThinkingOff, ThinkingLevelLow, ThinkingStyleAnthropic, `agent 'myagent': thinking is "off" but thinking_level "low" is set: remove thinking_level or set thinking: on`},
		// 5. off with reasoning_effort: the fix names thinking_level: low.
		{"off with reasoning_effort", ThinkingOff, "", ThinkingStyleReasoningEffort, `agent 'myagent': thinking_style "reasoning_effort" has no off value: set thinking_level: low instead of thinking: off`},
		// 6. A thinking key without a style.
		{"on without style", ThinkingOn, "", "", `agent 'myagent': thinking is declared but thinking_style is missing: there is no default style`},
		{"off without style", ThinkingOff, "", "", `agent 'myagent': thinking is declared but thinking_style is missing: there is no default style`},
		{"level without style", "", ThinkingLevelHigh, "", `agent 'myagent': thinking is declared but thinking_style is missing: there is no default style`},
		// 7. on with reasoning_effort and no level (user decision 2026-09-26).
		{"on reasoning_effort no level", ThinkingOn, "", ThinkingStyleReasoningEffort, `agent 'myagent': thinking_style "reasoning_effort" needs a thinking_level: set thinking_level to low, medium, high, or max`},
		// 8. A level under template_kwargs: its wire field carries only on/off
		// (TD-007, user decision 2026-09-27, option A).
		{"level alone template_kwargs", "", ThinkingLevelHigh, ThinkingStyleTemplateKwargs, `agent 'myagent': thinking_style "template_kwargs" has no level: remove thinking_level and use thinking: on`},
		{"on level template_kwargs", ThinkingOn, ThinkingLevelLow, ThinkingStyleTemplateKwargs, `agent 'myagent': thinking_style "template_kwargs" has no level: remove thinking_level and use thinking: on`},
		{"max template_kwargs", "", ThinkingLevelMax, ThinkingStyleTemplateKwargs, `agent 'myagent': thinking_style "template_kwargs" has no level: remove thinking_level and use thinking: on`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureThinkingWarnings(t)
			_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(thinkingAgent(tc.thinking, tc.level, tc.style))))
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// AC 02-03: the rules accumulate rather than short-circuit.
func TestValidateAgent_ThinkingFaultsAccumulate(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(thinkingAgent(ThinkingOff, "extreme", ""))))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid thinking_level "extreme"`)
	assert.Contains(t, err.Error(), `thinking is "off" but thinking_level "extreme" is set`)
	assert.Contains(t, err.Error(), "thinking_style is missing")
}

// AC 02-04: max under reasoning_effort loads, keeps its declared value, and
// writes exactly one warning naming the agent. Other styles get no warning.
func TestValidateAgent_ReasoningEffortMaxWarns(t *testing.T) {
	buf := captureThinkingWarnings(t)
	reg, err := LoadRegistry(writeRegistry(t, thinkingRegistry(thinkingAgent("", ThinkingLevelMax, ThinkingStyleReasoningEffort))))
	require.NoError(t, err)
	assert.Equal(t, ThinkingLevelMax, reg.Agents["myagent"].ThinkingLevel, "validation does not rewrite the level")
	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "\n"), "exactly one warning line: %q", out)
	assert.Contains(t, out, "agent 'myagent'")
	assert.Contains(t, out, `"max" is sent as "high"`)

	for _, style := range []string{ThinkingStyleQwen, ThinkingStyleAnthropic} {
		t.Run(style, func(t *testing.T) {
			buf := captureThinkingWarnings(t)
			// A cap above the max budget keeps the separate budget warning quiet.
			_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(thinkingAgent("", ThinkingLevelMax, style)+"    max_tokens: 65536\n")))
			require.NoError(t, err)
			assert.Empty(t, buf.String(), "max under %s writes no warning", style)
		})
	}

	t.Run("explicit on warns once", func(t *testing.T) {
		buf := captureThinkingWarnings(t)
		_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(thinkingAgent(ThinkingOn, ThinkingLevelMax, ThinkingStyleReasoningEffort))))
		require.NoError(t, err)
		assert.Equal(t, 1, strings.Count(buf.String(), "\n"))
	})
	t.Run("off with max errors without warning", func(t *testing.T) {
		buf := captureThinkingWarnings(t)
		_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(thinkingAgent(ThinkingOff, ThinkingLevelMax, ThinkingStyleReasoningEffort))))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `has no off value: set thinking_level: low`)
		assert.Empty(t, buf.String())
	})
}

// AC 02-05: a community persona may not declare any thinking key; one key is
// enough to reject, and a persona with none still validates.
func TestRejectMachineLocalFields_ThinkingKeysBanned(t *testing.T) {
	const base = "name: sample\nprovider: openrouter\nmodel: anthropic/claude-opus-4.8\n"
	for _, tc := range []struct{ key, body string }{
		{"thinking", "thinking: off\nthinking_style: qwen\n"},
		{"thinking_level", "thinking_level: high\nthinking_style: qwen\n"},
		{"thinking_style", "thinking_style: qwen\n"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			err := ValidateCommunityPersonaYAML("sample", []byte(base+tc.body))
			require.Error(t, err)
			assert.Contains(t, err.Error(), `community persona "sample" must not declare `+tc.key+":")
		})
	}
	require.NoError(t, ValidateCommunityPersonaYAML("sample", []byte(base)))
}
