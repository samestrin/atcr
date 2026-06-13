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
