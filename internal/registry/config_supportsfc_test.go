package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC 04-01: supports_function_calling parses from registry.yaml onto AgentConfig.
func TestRegistry_SupportsFunctionCallingParsed(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  a:
    provider: p
    model: m
    tools: true
    supports_function_calling: true
`))
	require.NoError(t, err)
	assert.True(t, reg.Agents["a"].SupportsFC, "supports_function_calling: true parsed")
}

// AC 04-01 Error Scenario 2: a non-boolean value for supports_function_calling
// or tools must produce a field-named error naming the agent and field, not the
// generic yaml "cannot unmarshal" message.
func TestRegistry_AgentBoolFieldBadType(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantMsg string
	}{
		{
			name: "supports_function_calling non-bool string",
			yaml: `
providers:
  p:
    api_key_env: KEY
agents:
  myagent:
    provider: p
    model: m
    supports_function_calling: "enabled"
`,
			wantMsg: "agent 'myagent': supports_function_calling must be a boolean",
		},
		{
			name: "supports_function_calling integer",
			yaml: `
providers:
  p:
    api_key_env: KEY
agents:
  myagent:
    provider: p
    model: m
    supports_function_calling: 1
`,
			wantMsg: "agent 'myagent': supports_function_calling must be a boolean",
		},
		{
			name: "tools quoted string",
			yaml: `
providers:
  p:
    api_key_env: KEY
agents:
  myagent:
    provider: p
    model: m
    tools: "maybe"
`,
			wantMsg: "agent 'myagent': tools must be a boolean",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadRegistry(writeRegistry(t, tc.yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

// AC 04-01 EC1 / Security default-safe: an absent supports_function_calling
// defaults to false (a model is assumed non-tool-capable unless declared).
func TestRegistry_SupportsFunctionCallingDefault(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  a:
    provider: p
    model: m
    tools: true
`))
	require.NoError(t, err)
	assert.False(t, reg.Agents["a"].SupportsFC, "absent field defaults to false")
}
