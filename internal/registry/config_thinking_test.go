package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// thinkingRegistry wraps agent entries in a one-provider registry document.
func thinkingRegistry(agents string) string {
	return `
providers:
  p:
    api_key_env: KEY
agents:
` + agents
}

// AC 01-01 / 01-02: each thinking key decodes onto AgentConfig verbatim, any
// subset parses, and an omitted or empty key stays unset.
func TestAgentConfig_ThinkingFieldsDecode(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, thinkingRegistry(`
  on:
    provider: p
    model: m
    thinking: on
    thinking_style: qwen
  off:
    provider: p
    model: m
    thinking: off
    thinking_style: qwen
  all:
    provider: p
    model: m
    thinking: on
    thinking_level: max
    thinking_style: anthropic
    max_tokens: 65536
  style-only:
    provider: p
    model: m
    thinking_style: qwen
  omitted:
    provider: p
    model: m
  empty:
    provider: p
    model: m
    thinking: ""
    thinking_level: ""
    thinking_style: ""
  mixed:
    provider: p
    model: m
    tools: true
    supports_function_calling: true
    response_format: json_object
    thinking: on
    thinking_style: qwen
`)))
	require.NoError(t, err)

	assert.Equal(t, ThinkingOn, reg.Agents["on"].Thinking)
	assert.Equal(t, ThinkingOff, reg.Agents["off"].Thinking)

	all := reg.Agents["all"]
	assert.Equal(t, ThinkingOn, all.Thinking)
	assert.Equal(t, ThinkingLevelMax, all.ThinkingLevel)
	assert.Equal(t, ThinkingStyleAnthropic, all.ThinkingStyle)

	styleOnly := reg.Agents["style-only"]
	assert.Equal(t, ThinkingStyleQwen, styleOnly.ThinkingStyle)
	assert.Empty(t, styleOnly.Thinking)
	assert.Empty(t, styleOnly.ThinkingLevel)

	for _, name := range []string{"omitted", "empty"} {
		a := reg.Agents[name]
		assert.Empty(t, a.Thinking, "%s: thinking stays unset", name)
		assert.Empty(t, a.ThinkingLevel, "%s: thinking_level stays unset", name)
		assert.Empty(t, a.ThinkingStyle, "%s: thinking_style stays unset", name)
	}

	mixed := reg.Agents["mixed"]
	assert.Equal(t, ThinkingOn, mixed.Thinking)
	assert.Equal(t, ThinkingStyleQwen, mixed.ThinkingStyle)
	assert.Equal(t, ResponseFormatJSONObject, mixed.ResponseFormat)
	assert.True(t, mixed.SupportsFC)
	assert.True(t, mixed.Tools)
}

// AC 01-02 Scenario 1: a level alone decodes with thinking unset; the implied
// "on" is a semantic rule for the wire layer, not a synthesized field.
func TestAgentConfig_ThinkingLevelAloneImpliesOn(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, thinkingRegistry(`
  a:
    provider: p
    model: m
    thinking_level: high
    thinking_style: qwen
`)))
	require.NoError(t, err)
	a := reg.Agents["a"]
	assert.Equal(t, ThinkingLevelHigh, a.ThinkingLevel)
	assert.Empty(t, a.Thinking, "no thinking value is synthesized from a level")
}

// AC 01-02 Scenarios 4-5: every legal level and style round-trips through load.
func TestAgentConfig_ThinkingEveryLegalValueDecodes(t *testing.T) {
	for _, level := range ThinkingLevels() {
		t.Run("level_"+level, func(t *testing.T) {
			reg, err := LoadRegistry(writeRegistry(t, thinkingRegistry(`
  a:
    provider: p
    model: m
    thinking_level: `+level+`
    thinking_style: anthropic
    max_tokens: 65536
`)))
			require.NoError(t, err)
			assert.Equal(t, level, reg.Agents["a"].ThinkingLevel)
		})
	}
	for _, style := range ThinkingStyles() {
		t.Run("style_"+style, func(t *testing.T) {
			reg, err := LoadRegistry(writeRegistry(t, thinkingRegistry(`
  a:
    provider: p
    model: m
    thinking_style: `+style+`
`)))
			require.NoError(t, err)
			assert.Equal(t, style, reg.Agents["a"].ThinkingStyle)
		})
	}
}

// AC 01-02 Edge Cases 1 and 3: decode alone neither validates nor case-folds.
// These combinations fail LoadRegistry once validation lands, so they decode
// through yaml.Unmarshal directly.
func TestAgentConfig_ThinkingDecodeIsVerbatim(t *testing.T) {
	var a AgentConfig
	require.NoError(t, yaml.Unmarshal([]byte("thinking: off\nthinking_level: low\n"), &a))
	assert.Equal(t, ThinkingOff, a.Thinking)
	assert.Equal(t, ThinkingLevelLow, a.ThinkingLevel)

	var b AgentConfig
	require.NoError(t, yaml.Unmarshal([]byte("thinking: on\nthinking_level: HIGH\n"), &b))
	assert.Equal(t, ThinkingOn, b.Thinking)
	assert.Equal(t, "HIGH", b.ThinkingLevel, "decode preserves case")
}

// AC 01-01 / 01-02 Error Scenarios: a non-scalar value fails load through the
// existing decode path.
func TestAgentConfig_ThinkingNonScalarRejected(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"thinking map", "    thinking:\n      mode: on\n"},
		{"thinking_level seq", "    thinking_level:\n      - high\n"},
		{"thinking_style map", "    thinking_style:\n      name: qwen\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(`
  a:
    provider: p
    model: m
`+tc.body)))
			require.Error(t, err)
		})
	}
}

// AC 01-03: thinking keys are declared per agent and never inherited through
// fallback:, across a direct reference, a multi-hop chain, and an entry that
// declares its own distinct values.
func TestAgentConfig_ThinkingNotInheritedByFallback(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, thinkingRegistry(`
  primary:
    provider: p
    model: m
    thinking: on
    thinking_level: max
    thinking_style: anthropic
    max_tokens: 65536
  secondary:
    provider: p
    model: m
    fallback: primary
  own:
    provider: p
    model: m
    fallback: primary
    thinking: off
    thinking_style: qwen
  hop2:
    provider: p
    model: m
    fallback: secondary
`)))
	require.NoError(t, err)
	for _, name := range []string{"secondary", "hop2"} {
		a := reg.Agents[name]
		assert.Empty(t, a.Thinking, "%s must not inherit thinking", name)
		assert.Empty(t, a.ThinkingLevel, "%s must not inherit thinking_level", name)
		assert.Empty(t, a.ThinkingStyle, "%s must not inherit thinking_style", name)
	}
	own := reg.Agents["own"]
	assert.Equal(t, ThinkingOff, own.Thinking)
	assert.Empty(t, own.ThinkingLevel, "own declaration is not blended with the primary's level")
	assert.Equal(t, ThinkingStyleQwen, own.ThinkingStyle)
}

// AC 01-03 Edge Cases 2-3: the shared fixture and a user+project overlay merge
// load with every thinking field unset.
func TestAgentConfig_ThinkingBackwardCompat(t *testing.T) {
	require.NotContains(t, validRegistry, "thinking", "fixture must predate the fields")
	reg, err := LoadRegistry(writeRegistry(t, validRegistry))
	require.NoError(t, err)
	for name, a := range reg.Agents {
		assert.Empty(t, a.Thinking+a.ThinkingLevel+a.ThinkingStyle, "agent %s must carry no thinking keys", name)
	}

	regPath := writeUserRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
  greta:
    provider: openai
    model: gpt-4o
    fallback: bruce
`)
	root := t.TempDir()
	writeProjectRegistry(t, root, `
agents:
  team-reviewer:
    provider: openai
    model: gpt-4o
    fallback: greta
`)
	merged, err := LoadMergedRegistry(regPath, root)
	require.NoError(t, err)
	require.Len(t, merged.Agents, 3)
	for name, a := range merged.Agents {
		assert.Empty(t, a.Thinking+a.ThinkingLevel+a.ThinkingStyle, "agent %s must carry no thinking keys", name)
	}
}

// Unset thinking keys are omitted when re-marshaled, so a round-tripped
// registry gains no spurious empty keys.
func TestAgentConfig_ThinkingYAMLRoundTrip(t *testing.T) {
	out, err := yaml.Marshal(&AgentConfig{Provider: "p", Model: "m"})
	require.NoError(t, err)
	assert.NotContains(t, string(out), "thinking")

	out, err = yaml.Marshal(&AgentConfig{Provider: "p", Model: "m",
		Thinking: ThinkingOff, ThinkingStyle: ThinkingStyleQwen})
	require.NoError(t, err)
	assert.Contains(t, string(out), "thinking: \"off\"")
	assert.Contains(t, string(out), "thinking_style: qwen")
}

// TD-002: the legal-value accessors return copies, so a caller cannot change
// the legal set for the rest of the process.
func TestThinkingLegalValues_ReturnCopies(t *testing.T) {
	assert.Equal(t, []string{ThinkingOn, ThinkingOff}, ThinkingValues())
	assert.Equal(t, []string{"low", "medium", "high", "max"}, ThinkingLevels())
	assert.Equal(t, []string{"qwen", "template_kwargs", "reasoning_effort", "anthropic", "glm"}, ThinkingStyles())

	ThinkingValues()[0] = "x"
	ThinkingLevels()[0] = "x"
	ThinkingStyles()[0] = "x"
	assert.Equal(t, ThinkingOn, ThinkingValues()[0])
	assert.Equal(t, ThinkingLevelLow, ThinkingLevels()[0])
	assert.Equal(t, ThinkingStyleQwen, ThinkingStyles()[0])
}
