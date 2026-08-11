package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.5.1 (T1/AC5): context_window_tokens is a per-agent declaration of
// the model's real context window, resolved AHEAD of the static table in
// internal/payload. It exists because a litellm-backed roster names models by
// bare proxy alias, which no static-table key matches, so every such agent
// silently resolves the conservative 32,768-token default.
//
// Shape mirrors max_context_lines (AgentConfig.MaxContextLines): a *int so an
// unset field (nil) is distinguishable from any explicit value, validated at
// load within 1..ContextWindowTokensCap.

func TestContextWindowTokens_NilWhenUnset(t *testing.T) {
	// AC2 backward compatibility at the config tier: a roster that declares
	// nothing must leave the field nil, so the payload resolver falls through to
	// the static table exactly as it did pre-epic.
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce:
    provider: p
    model: m
`))
	require.NoError(t, err)
	assert.Nil(t, reg.Agents["bruce"].ContextWindowTokens,
		"an undeclared window must stay nil, not default to a number at the config tier")
}

func TestContextWindowTokens_ParsedFromYAML(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce:
    provider: p
    model: m
    context_window_tokens: 262144
`))
	require.NoError(t, err)
	require.NotNil(t, reg.Agents["bruce"].ContextWindowTokens)
	assert.Equal(t, 262144, *reg.Agents["bruce"].ContextWindowTokens,
		"the probed max_model_len of the local vLLM deployment must survive load verbatim")
}

func TestContextWindowTokens_RejectsZeroAtLoad(t *testing.T) {
	// 0 is rejected rather than treated as "unset": a declared 0 would otherwise
	// drive EffectiveByteBudget to 0, which the bulk path reads as the honest
	// "window too small to reserve output headroom" degradation — a real
	// overflow state produced by a typo.
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce:
    provider: p
    model: m
    context_window_tokens: 0
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context_window_tokens")
}

func TestContextWindowTokens_RejectsNegativeAtLoad(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce:
    provider: p
    model: m
    context_window_tokens: -1
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context_window_tokens")
}

func TestContextWindowTokens_RejectsOverCapAtLoad(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce:
    provider: p
    model: m
    context_window_tokens: 10000001
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context_window_tokens")
}

func TestContextWindowTokens_AcceptsCapBoundary(t *testing.T) {
	// The range is inclusive at both ends (1..cap), matching max_context_lines.
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce:
    provider: p
    model: m
    context_window_tokens: 10000000
  greta:
    provider: p
    model: m2
    context_window_tokens: 1
`))
	require.NoError(t, err)
	require.NotNil(t, reg.Agents["bruce"].ContextWindowTokens)
	assert.Equal(t, ContextWindowTokensCap, *reg.Agents["bruce"].ContextWindowTokens)
	require.NotNil(t, reg.Agents["greta"].ContextWindowTokens)
	assert.Equal(t, 1, *reg.Agents["greta"].ContextWindowTokens)
}

func TestContextWindowTokens_RejectsNonIntegerAtLoad(t *testing.T) {
	// AC5's "non-integer declarations are rejected at load" — a quoted string, a
	// bool, or a collection must fail the strict decode rather than coercing.
	//
	// A YAML FLOAT (128000.5) is deliberately NOT asserted here: gopkg.in/yaml.v3
	// silently truncates a float into any *int field, so max_context_lines: 1200.5
	// and max_findings: 7.9 load as 1200 and 7 today. That is registry-WIDE
	// decoder behavior, not a property of this field, and AC5 scopes itself to
	// "matching existing registry validation behavior". Rejecting it for this one
	// field would require a bespoke unmarshaler that breaks the plain *int shape
	// the payload resolver consumes. Captured as technical debt instead.
	for _, bad := range []string{`"512k"`, `true`, `[1, 2]`, `{a: 1}`} {
		_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce:
    provider: p
    model: m
    context_window_tokens: `+bad+`
`))
		require.Error(t, err, "context_window_tokens: %s must not load", bad)
	}
}

func TestContextWindowTokens_ErrorNamesDefiningFile(t *testing.T) {
	// AC5: the error must be actionable, which means naming WHICH registry
	// defined the offending agent — the project tier here, not the user tier.
	regPath := writeUserRegistry(t, userRegistryWithBruce)
	root := t.TempDir()
	writeProjectRegistry(t, root, `
agents:
  ottO:
    provider: openai
    model: gpt-4
    context_window_tokens: 0
`)
	_, err := LoadMergedRegistry(regPath, root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context_window_tokens")
	assert.Contains(t, err.Error(), projectRegistryLabel,
		"an out-of-range declaration must name the file that defined the agent")
}

func TestContextWindowTokensCap_ExceedsLargestStaticTableWindow(t *testing.T) {
	// The cap deliberately does NOT reuse MaxContextLinesCap (1,000,000). The
	// static table in internal/payload already resolves exactly 1,000,000 for the
	// Gemini entries, so a 1,000,000 ceiling would leave the declaration tier
	// unable to exceed the very table tier it exists to override.
	assert.Greater(t, ContextWindowTokensCap, 1000000,
		"the declaration tier must be able to exceed the largest static-table window")
	assert.Equal(t, 10000000, ContextWindowTokensCap)
}
