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
	// fall through to the static model table (or the default) instead of being
	// honored — turning an operator typo into a silent over-window payload.
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
	assert.Contains(t, err.Error(), "must be within",
		"assertion must name the validation message, not just the field — a strict-decode error ('field X not found in type') would otherwise satisfy the check")
	assert.NotContains(t, err.Error(), "not found in type",
		"a strict-decode error must NOT satisfy the validation assertion")
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
	assert.Contains(t, err.Error(), "must be within",
		"assertion must name the validation message, not just the field")
	assert.NotContains(t, err.Error(), "not found in type")
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
	assert.Contains(t, err.Error(), "must be within",
		"assertion must name the validation message, not just the field")
	assert.NotContains(t, err.Error(), "not found in type")
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
	// the payload resolver consumes. Recorded as technical debt by this epic
	// instead, to be fixed registry-wide rather than for one field.
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
		// The strict decoder's TYPE-mismatch error does not name the offending
		// field (only UNKNOWN-FIELD errors do: "field X not found in type"). So
		// we pin the REJECT PATH: it must be a parse error (not a successful
		// load followed by silent ignore) and must mention either the file or
		// the YAML unmarshal mechanism. If a sibling field's decoder were ever
		// to change and this loop passed on a different field's error, that
		// error would still have to come through the registry parse path.
		assert.Contains(t, err.Error(), "parse registry.yaml",
			"the rejection must come from the registry parse path, not a downstream validator that would mask a sibling field change")
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
	assert.Contains(t, err.Error(), "agent 'ottO'",
		"agentErrf must include the agent name so the error is attributable, not just a bare parse error")
	assert.Contains(t, err.Error(), "must be within",
		"assertion must name the validation message, not just the field")
	assert.NotContains(t, err.Error(), "not found in type")
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

func TestValidateCommunityPersonaYAML_RejectsContextWindowTokens(t *testing.T) {
	// context_window_tokens is machine-LOCAL by this field's own stated design:
	// it exists to describe a proxy alias that is meaningless to any other atcr
	// user. A community persona is fetched and installed verbatim onto a consumer
	// whose proxy may serve that model at a fraction of the declared window, so a
	// published declaration produces guaranteed over-window payloads on the
	// consumer's machine. The strict community validator must therefore fail
	// closed on it rather than accept it as an ordinary inlined agent field.
	const y = "name: sample\nprovider: openrouter\nmodel: anthropic/claude-opus-4.8\ncontext_window_tokens: 128000\n"
	err := ValidateCommunityPersonaYAML("sample", []byte(y))
	require.Error(t, err, "a published context_window_tokens declaration must be rejected")
	assert.Contains(t, err.Error(), "context_window_tokens",
		"the error must name the offending key so the persona author can remove it")
	assert.Contains(t, err.Error(), "sample",
		"the error must name the persona, matching the rest of this validator's messages")
}

func TestValidateCommunityPersonaYAML_AcceptsUndeclaredContextWindow(t *testing.T) {
	// The rejection must be scoped to the declaration itself: a persona that
	// simply omits the key stays valid, so the guard cannot degrade into a
	// blanket rejection of every community persona.
	const y = "name: sample\nprovider: openrouter\nmodel: anthropic/claude-opus-4.8\n"
	require.NoError(t, ValidateCommunityPersonaYAML("sample", []byte(y)))
}

func TestAgentConfig_EffectiveContextWindowTokens(t *testing.T) {
	// The single guarded read mirrors EffectiveMaxContextLines's role for the
	// chunked line budget: payload/fanout callers share one source of truth
	// for the declaration, and the loader's range guard need not be duplicated
	// at each call site.

	// unset → 0 sentinel
	var unset AgentConfig
	assert.Equal(t, 0, unset.EffectiveContextWindowTokens(),
		"an unset declaration must return the 'no declaration' sentinel, not a default")

	// declared positive → verbatim (loader has already rejected non-positive)
	declared := 128000
	set := AgentConfig{ContextWindowTokens: &declared}
	assert.Equal(t, 128000, set.EffectiveContextWindowTokens(),
		"a positive declaration must survive verbatim — the accessor performs no clamping")

	// declared = cap boundary
	atCap := ContextWindowTokensCap
	atCapCfg := AgentConfig{ContextWindowTokens: &atCap}
	assert.Equal(t, ContextWindowTokensCap, atCapCfg.EffectiveContextWindowTokens())
}
