package registry

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// AC 01-01: response_format decodes onto AgentConfig, zero-value-safe, and is
// never copied across a fallback: reference at load.
func TestRegistry_ResponseFormatDecodes(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  declared:
    provider: p
    model: m
    response_format: json_object
  omitted:
    provider: p
    model: m
  empty:
    provider: p
    model: m
    response_format: ""
  falls-back:
    provider: p
    model: m
    fallback: declared
`))
	require.NoError(t, err)
	assert.Equal(t, ResponseFormatJSONObject, reg.Agents["declared"].ResponseFormat)
	assert.Equal(t, "json_object", ResponseFormatJSONObject, "the one legal value is the literal json_object")
	assert.Empty(t, reg.Agents["omitted"].ResponseFormat, "an omitted key stays unset")
	assert.Empty(t, reg.Agents["empty"].ResponseFormat, `an explicit "" is treated as unset`)
	assert.Empty(t, reg.Agents["falls-back"].ResponseFormat,
		"response_format is declared per agent, never inherited from the fallback target")
}

// AC 01-01 Error Scenario 1: a non-scalar value fails load through the
// existing decode path, unchanged from any other string-typed field.
func TestRegistry_ResponseFormatNonScalar(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  a:
    provider: p
    model: m
    response_format:
      type: json_object
`))
	require.Error(t, err)
}

// AC 01-02: validateAgent accepts only "" and "json_object" — strict equality,
// no case-folding, no trimming of an otherwise-invalid value into acceptance.
func TestRegistry_ResponseFormatValidation(t *testing.T) {
	cases := []struct {
		name  string
		value string
		ok    bool
	}{
		{"unset", `""`, true},
		{"json_object", `json_object`, true},
		{"json_schema", `json_schema`, false},
		{"title case", `Json_Object`, false},
		{"upper case", `JSON_OBJECT`, false},
		{"bool-shaped typo", `"true"`, false},
		{"yaml", `yaml`, false},
		{"text", `text`, false},
		{"whitespace padded", `" json_object "`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  myagent:
    provider: p
    model: m
    response_format: `+tc.value+`
`))
			if tc.ok {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			decoded := strings.Trim(tc.value, `"`)
			assert.Contains(t, err.Error(), `invalid response_format `+strconv.Quote(decoded)+`: must be "json_object" or unset`)
		})
	}
}

// TD: the rejection must echo the value it read, so a case/whitespace typo is
// visible in the error itself (the payload check above it already does this).
func TestRegistry_ResponseFormatErrorEchoesValue(t *testing.T) {
	for _, value := range []string{"json_schema", "JSON_OBJECT", " json_object ", "true"} {
		_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  myagent:
    provider: p
    model: m
    response_format: "`+value+`"
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `invalid response_format `+strconv.Quote(value),
			"error must echo the offending value for %q", value)
		assert.Contains(t, err.Error(), `must be "json_object" or unset`)
	}
}

// AC 01-02 Edge Case 3: a response_format fault accumulates alongside another
// fault on the same agent rather than short-circuiting it.
func TestRegistry_ResponseFormatFaultAccumulates(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  myagent:
    model: m
    response_format: json_schema
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid response_format "json_schema": must be "json_object" or unset`)
	assert.Contains(t, err.Error(), "agent 'myagent': required field 'provider' is missing")
}

// AC 01-03 Scenario 1 / Edge Case 1: the package's shared multi-agent fixture
// (no response_format anywhere) loads exactly as TestRegistryLoad_ValidConfig
// expects, and every agent's ResponseFormat is unset.
func TestRegistry_ResponseFormatBackwardCompat(t *testing.T) {
	require.NotContains(t, validRegistry, "response_format", "fixture must predate the field")
	reg, err := LoadRegistry(writeRegistry(t, validRegistry))
	require.NoError(t, err)
	require.Len(t, reg.Agents, 2)
	for name, a := range reg.Agents {
		assert.Empty(t, a.ResponseFormat, "agent %s must not carry a response_format", name)
	}
	greta := reg.Agents["greta"]
	assert.Equal(t, "bruce", greta.Fallback)
	assert.Equal(t, "diff", greta.Payload)
	assert.True(t, greta.RateLimited)
}

// AC 01-03 Scenario 2: a user + project overlay merge with fallback chains and
// zero response_format declarations loads with every agent unset.
func TestRegistry_ResponseFormatBackwardCompatOverlay(t *testing.T) {
	regPath := writeUserRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    tools: true
    supports_function_calling: true
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
	reg, err := LoadMergedRegistry(regPath, root)
	require.NoError(t, err)
	require.Len(t, reg.Agents, 3)
	for name, a := range reg.Agents {
		assert.Empty(t, a.ResponseFormat, "agent %s must not carry a response_format", name)
	}
}

// AC 01-03 Edge Case 2: an unset ResponseFormat is omitted when re-marshaled,
// so a round-tripped registry gains no spurious empty key.
func TestAgentConfig_ResponseFormatYAMLRoundTrip(t *testing.T) {
	unset := AgentConfig{Provider: "p", Model: "m"}
	out, err := yaml.Marshal(&unset)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "response_format",
		"an unset response_format must not appear in the marshalled config (omitempty)")

	declared := AgentConfig{Provider: "p", Model: "m", ResponseFormat: ResponseFormatJSONObject}
	out, err = yaml.Marshal(&declared)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(out), "response_format: json_object"))
}
