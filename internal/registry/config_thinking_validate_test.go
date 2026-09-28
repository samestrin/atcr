package registry

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureThinkingWarnings swaps the package-global thinkingWarnWriter
// (config.go) for a buffer for the test. Like every test seam in this
// package (the convention overlay.go states for its own vars), a test that
// mutates it must not call t.Parallel(): concurrent mutations would race,
// and one test's load would write into another test's buffer while the
// exact-byte assertions read whichever landed last. The helper fails fast
// when it does, so the first parallel test in the package surfaces here
// instead of as a nondeterministic assertion failure or a -race report
// later (TD row internal/registry/config_thinking_validate_test.go:13).
func captureThinkingWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	if v := reflect.ValueOf(t).Elem().FieldByName("isParallel"); v.IsValid() && v.Bool() {
		t.Fatalf("captureThinkingWarnings swaps the package-global thinkingWarnWriter; a test calling t.Parallel() would race it (see the test-seam convention in overlay.go)")
	}
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

// AC 03-03 (Sprint 35.16.11.2.2.1): preserve_thinking is legal only as on/off
// under the qwen or glm style with thinking on; glm takes no level. Every other
// combination fails load with an error naming the agent.
func TestValidateAgent_PreserveThinking(t *testing.T) {
	// TD internal/registry/config.go:1304: the expectations below are built from
	// the live preserveThinkingStyles set, so a third style changing the message
	// text fails here, not in production.
	preserveJoined := strings.Join(PreserveThinkingStyles(), ", ")
	preserveOr := strings.Join(PreserveThinkingStyles(), " or ")
	cases := []struct {
		name                             string
		thinking, level, style, preserve string
		wantErr                          string // "" = must load
	}{
		// Valid.
		{"qwen on", ThinkingOn, "", ThinkingStyleQwen, ThinkingOn, ""},
		{"qwen level alone off", "", ThinkingLevelHigh, ThinkingStyleQwen, ThinkingOff, ""},
		// TD internal/registry/config_thinking_validate_test.go:82: a level alone
		// implies thinking on, so preserve_thinking: on must be legal with no
		// thinking key at all (the spec's "or a thinking_level under qwen" arm).
		{"qwen level alone on", "", ThinkingLevelHigh, ThinkingStyleQwen, ThinkingOn, ""},
		{"glm on", ThinkingOn, "", ThinkingStyleGLM, ThinkingOn, ""},
		{"glm on flag off", ThinkingOn, "", ThinkingStyleGLM, ThinkingOff, ""},
		{"glm on no flag", ThinkingOn, "", ThinkingStyleGLM, "", ""},
		{"glm off no flag", ThinkingOff, "", ThinkingStyleGLM, "", ""},

		// Values: the same on/off vocabulary as thinking; a bare YAML bool
		// decodes to "true" and fails the value check.
		{"bare true", ThinkingOn, "", ThinkingStyleQwen, "true", `agent 'myagent': invalid preserve_thinking "true": must be "on" or "off" or unset`},
		{"wrong case", ThinkingOn, "", ThinkingStyleQwen, "ON", `agent 'myagent': invalid preserve_thinking "ON": must be "on" or "off" or unset`},
		// Style: only qwen and glm carry it.
		{"anthropic", ThinkingOn, "", ThinkingStyleAnthropic, ThinkingOn, `agent 'myagent': thinking_style "anthropic" has no preserve_thinking: only ` + preserveJoined + ` send it`},
		{"template_kwargs", ThinkingOn, "", ThinkingStyleTemplateKwargs, ThinkingOn, `agent 'myagent': thinking_style "template_kwargs" has no preserve_thinking: only ` + preserveJoined + ` send it`},
		{"reasoning_effort", "", ThinkingLevelLow, ThinkingStyleReasoningEffort, ThinkingOn, `agent 'myagent': thinking_style "reasoning_effort" has no preserve_thinking: only ` + preserveJoined + ` send it`},
		{"no style", ThinkingOn, "", "", ThinkingOn, "agent 'myagent': preserve_thinking is declared but thinking_style is missing: set thinking_style: " + preserveOr},
		// Thinking must be on (AC 03-01 Edge Cases 4-5, AC 03-03 Edge Case 6).
		{"thinking unset", "", "", ThinkingStyleQwen, ThinkingOn, `agent 'myagent': preserve_thinking is set but thinking is not on: set thinking: on or remove preserve_thinking`},
		{"thinking off", ThinkingOff, "", ThinkingStyleGLM, ThinkingOn, `agent 'myagent': preserve_thinking is set but thinking is not on: set thinking: on or remove preserve_thinking`},
		// glm has no budget, so no level.
		{"glm level", "", ThinkingLevelHigh, ThinkingStyleGLM, "", `agent 'myagent': thinking_style "glm" has no level: remove thinking_level and use thinking: on`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureThinkingWarnings(t)
			agent := thinkingAgent(tc.thinking, tc.level, tc.style)
			if tc.preserve != "" {
				agent += "    preserve_thinking: " + tc.preserve + "\n"
			}
			reg, err := LoadRegistry(writeRegistry(t, thinkingRegistry(agent)))
			if tc.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, tc.preserve, reg.Agents["myagent"].PreserveThinking, "decoded verbatim")
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TD internal/registry/config.go:1304: the expectations are built from the
// live preserveThinkingStyles set via the exported accessor, so a third style
// changing the messages fails here instead of shipping stale text.
func TestValidateAgent_PreserveThinkingMessagesBuiltFromStyleSet(t *testing.T) {
	captureThinkingWarnings(t)
	styles := PreserveThinkingStyles()
	joined := strings.Join(styles, ", ")
	orJoined := strings.Join(styles, " or ")

	_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(thinkingAgent(ThinkingOn, "", ThinkingStyleAnthropic))+"    preserve_thinking: on\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only "+joined+" send it")

	_, err = LoadRegistry(writeRegistry(t, thinkingRegistry(thinkingAgent(ThinkingOn, "", ""))+"    preserve_thinking: on\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "set thinking_style: "+orJoined)
}

// TD internal/registry/config.go:1309: an invalid thinking VALUE with the flag
// set is one fault (the value), not two — the "thinking is not on" case must
// not fire for it, or the operator is sent after preserve_thinking instead of
// the invalid value.
func TestValidateAgent_PreserveThinkingInvalidValueIsOneFault(t *testing.T) {
	captureThinkingWarnings(t)
	_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(thinkingAgent("true", "", ThinkingStyleQwen)+"    preserve_thinking: on\n")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid thinking "true"`)
	assert.NotContains(t, err.Error(), "thinking is not on")
}

// TD-011: an unknown style with the flag is one fault (the style), not two.
func TestValidateAgent_PreserveThinkingUnknownStyleIsOneFault(t *testing.T) {
	captureThinkingWarnings(t)
	_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(thinkingAgent(ThinkingOn, "", "foo")+"    preserve_thinking: on\n")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid thinking_style "foo"`)
	assert.NotContains(t, err.Error(), "has no preserve_thinking")
}

// AC 03-03 Edge Cases 3 and 5: preserve_thinking is never inherited through
// fallback:, in either direction.
func TestAgentConfig_PreserveThinkingNotInheritedByFallback(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, thinkingRegistry(`
  primary:
    provider: p
    model: m
    thinking: on
    thinking_style: qwen
    preserve_thinking: on
    fallback: secondary
  secondary:
    provider: p
    model: m
  own:
    provider: p
    model: m
    thinking: on
    thinking_style: glm
    preserve_thinking: off
  usesown:
    provider: p
    model: m
    fallback: own
`)))
	require.NoError(t, err)
	assert.Empty(t, reg.Agents["secondary"].PreserveThinking, "a fallback must not inherit its primary's flag")
	assert.Empty(t, reg.Agents["usesown"].PreserveThinking, "a primary must not inherit its fallback's flag")
	assert.Equal(t, ThinkingOn, reg.Agents["primary"].PreserveThinking)
	assert.Equal(t, ThinkingOff, reg.Agents["own"].PreserveThinking)
}

// AC 02-01 / 02-02 / 02-03: every invalid combination fails load with an
// error naming the agent; every valid combination loads.
func TestValidateAgent_ThinkingCombinations(t *testing.T) {
	levels := strings.Join(ThinkingLevels(), ", ")
	styles := strings.Join(ThinkingStyles(), ", ")
	cases := []struct {
		name                   string
		thinking, level, style string
		maxTokens              string // declared output cap, for anthropic budgets
		wantErr                string // "" = must load
	}{
		// Valid.
		{"on with style", ThinkingOn, "", ThinkingStyleQwen, "", ""},
		{"off with qwen", ThinkingOff, "", ThinkingStyleQwen, "", ""},
		{"off with template_kwargs", ThinkingOff, "", ThinkingStyleTemplateKwargs, "", ""},
		{"on with template_kwargs", ThinkingOn, "", ThinkingStyleTemplateKwargs, "", ""},
		{"off with anthropic", ThinkingOff, "", ThinkingStyleAnthropic, "", ""},
		// Anthropic sends a real budget, so a valid declaration needs a cap
		// above it (the misfit is the load error pinned by
		// TestValidateAgent_ThinkingBudgetMisfitErrors).
		{"on level style", ThinkingOn, ThinkingLevelMedium, ThinkingStyleAnthropic, "16384", ""},
		{"level alone with style", "", ThinkingLevelHigh, ThinkingStyleQwen, "", ""},
		{"style alone", "", "", ThinkingStyleQwen, "", ""},
		{"reasoning_effort low", "", ThinkingLevelLow, ThinkingStyleReasoningEffort, "", ""},
		{"reasoning_effort on low", ThinkingOn, ThinkingLevelLow, ThinkingStyleReasoningEffort, "", ""},
		{"max under qwen", "", ThinkingLevelMax, ThinkingStyleQwen, "", ""},
		{"none", "", "", "", "", ""},

		// 1. Unrecognized thinking value, including bool-shaped literals.
		{"thinking maybe", "maybe", "", ThinkingStyleQwen, "", `agent 'myagent': invalid thinking "maybe": must be "on" or "off" or unset`},
		{"thinking true", "true", "", ThinkingStyleQwen, "", `agent 'myagent': invalid thinking "true": must be "on" or "off" or unset`},
		{"thinking false", "false", "", ThinkingStyleQwen, "", `agent 'myagent': invalid thinking "false": must be "on" or "off" or unset`},
		{"thinking upper", "ON", "", ThinkingStyleQwen, "", `agent 'myagent': invalid thinking "ON": must be "on" or "off" or unset`},
		// 2. Unknown level, case-sensitive.
		{"level extreme", "", "extreme", ThinkingStyleQwen, "", `agent 'myagent': invalid thinking_level "extreme": must be one of ` + levels},
		{"level wrong case", "", "Low", ThinkingStyleQwen, "", `agent 'myagent': invalid thinking_level "Low": must be one of ` + levels},
		// 3. Unknown style, case-sensitive.
		{"style openai", ThinkingOn, "", "openai", "", `agent 'myagent': invalid thinking_style "openai": must be one of ` + styles},
		{"style wrong case", ThinkingOn, "", "Qwen", "", `agent 'myagent': invalid thinking_style "Qwen": must be one of ` + styles},
		// 4. off with a level, regardless of style.
		{"off with level", ThinkingOff, ThinkingLevelMedium, ThinkingStyleQwen, "", `agent 'myagent': thinking is "off" but thinking_level "medium" is set: remove thinking_level or set thinking: on`},
		{"off with level anthropic", ThinkingOff, ThinkingLevelLow, ThinkingStyleAnthropic, "", `agent 'myagent': thinking is "off" but thinking_level "low" is set: remove thinking_level or set thinking: on`},
		// 5. off with reasoning_effort: the fix names thinking_level: low.
		{"off with reasoning_effort", ThinkingOff, "", ThinkingStyleReasoningEffort, "", `agent 'myagent': thinking_style "reasoning_effort" has no off value: set thinking_level: low instead of thinking: off`},
		// 6. A thinking key without a style.
		{"on without style", ThinkingOn, "", "", "", `agent 'myagent': thinking is declared but thinking_style is missing: there is no default style`},
		{"off without style", ThinkingOff, "", "", "", `agent 'myagent': thinking is declared but thinking_style is missing: there is no default style`},
		{"level without style", "", ThinkingLevelHigh, "", "", `agent 'myagent': thinking is declared but thinking_style is missing: there is no default style`},
		// 7. on with reasoning_effort and no level (user decision 2026-09-26).
		{"on reasoning_effort no level", ThinkingOn, "", ThinkingStyleReasoningEffort, "", `agent 'myagent': thinking_style "reasoning_effort" needs a thinking_level: set thinking_level to low, medium, high, or max`},
		// 8. A level under template_kwargs: its wire field carries only on/off
		// (TD-007, user decision 2026-09-27, option A).
		{"level alone template_kwargs", "", ThinkingLevelHigh, ThinkingStyleTemplateKwargs, "", `agent 'myagent': thinking_style "template_kwargs" has no level: remove thinking_level and use thinking: on`},
		{"on level template_kwargs", ThinkingOn, ThinkingLevelLow, ThinkingStyleTemplateKwargs, "", `agent 'myagent': thinking_style "template_kwargs" has no level: remove thinking_level and use thinking: on`},
		{"max template_kwargs", "", ThinkingLevelMax, ThinkingStyleTemplateKwargs, "", `agent 'myagent': thinking_style "template_kwargs" has no level: remove thinking_level and use thinking: on`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureThinkingWarnings(t)
			agent := thinkingAgent(tc.thinking, tc.level, tc.style)
			if tc.maxTokens != "" {
				agent += "    max_tokens: " + tc.maxTokens + "\n"
			}
			_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(agent)))
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
		{"preserve_thinking", "preserve_thinking: on\n"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			err := ValidateCommunityPersonaYAML("sample", []byte(base+tc.body))
			require.Error(t, err)
			assert.Contains(t, err.Error(), `community persona "sample" must not declare `+tc.key+":")
		})
	}
	require.NoError(t, ValidateCommunityPersonaYAML("sample", []byte(base)))
}

// The anthropic budget misfit is a load error, not a warning (TD row
// config.go:1270): Anthropic rejects budget_tokens >= max_tokens, so the
// misfit is a guaranteed 400 on every live call — the same fail-loud contract
// the temperature and supports_function_calling checks in validateThinking
// already apply. Advisory-budget styles (qwen, reasoning_effort,
// template_kwargs) keep the warnThinkingBudget warning instead.
func TestValidateAgent_ThinkingBudgetMisfitErrors(t *testing.T) {
	cases := []struct {
		name, level, maxTokens string
		wantErr                string // "" = must load without error or warning
	}{
		{"on no level default cap", "", "", "thinking budget 8192"},
		{"on level high default cap", ThinkingLevelHigh, "", "thinking budget 16384"},
		{"on level max default cap", ThinkingLevelMax, "", "thinking budget 32768"},
		{"on level max declared cap equal", ThinkingLevelMax, "32768", "not below max_tokens 32768"},
		{"on level low default cap", ThinkingLevelLow, "", ""},
		{"on level medium declared cap above", ThinkingLevelMedium, "16384", ""},
		{"on level max declared cap above", ThinkingLevelMax, "65536", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureThinkingWarnings(t)
			agent := thinkingAgent(ThinkingOn, tc.level, ThinkingStyleAnthropic)
			if tc.maxTokens != "" {
				agent += "    max_tokens: " + tc.maxTokens + "\n"
			}
			_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(agent)))
			if tc.wantErr == "" {
				require.NoError(t, err)
				assert.Empty(t, buf.String(), "a fitting budget writes no warning: %q", buf.String())
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Contains(t, err.Error(), "declare max_tokens above the budget or lower thinking_level")
			assert.Empty(t, buf.String(), "a load error suppresses the budget warning: %q", buf.String())
		})
	}
}

// TD row config.go:1260 (user decision 2026-09-27, path b): Anthropic rejects
// extended thinking alongside a forced tool_choice, and providers map
// response_format onto exactly that — so anthropic thinking-on plus
// response_format: json_object is rejected at load, next to the temperature
// and supports_function_calling rules. The live-proxy probe the row asked for
// could not be constructed: the flat-rate proxy served no anthropic model, so
// the guard rests on the documented provider constraint, like its siblings.
func TestValidateAgent_AnthropicThinkingWithResponseFormat(t *testing.T) {
	const wantErr = `thinking_style "anthropic" with thinking on cannot use response_format: "json_object"`
	cases := []struct {
		name, thinking, level, style, responseFormat string
		wantErr                                      string
	}{
		{"on with json_object", ThinkingOn, "", ThinkingStyleAnthropic, ResponseFormatJSONObject, wantErr},
		{"level alone with json_object", "", ThinkingLevelLow, ThinkingStyleAnthropic, ResponseFormatJSONObject, wantErr},
		{"off with json_object", ThinkingOff, "", ThinkingStyleAnthropic, ResponseFormatJSONObject, ""},
		{"on without response_format", ThinkingOn, "", ThinkingStyleAnthropic, "", ""},
		{"qwen on with json_object", ThinkingOn, "", ThinkingStyleQwen, ResponseFormatJSONObject, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureThinkingWarnings(t)
			agent := thinkingAgent(tc.thinking, tc.level, tc.style) + "    max_tokens: 65536\n"
			if tc.responseFormat != "" {
				agent += "    response_format: " + tc.responseFormat + "\n"
			}
			_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(agent)))
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
