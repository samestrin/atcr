package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 14.3 (AC2): max_context_lines is a per-agent cap on a single chunk's
// diff line count, consulted only in the chunked strategy. Unset falls back to
// a package default so a pre-14.3 registry keeps loading unchanged.

func TestMaxContextLines_DefaultWhenUnset(t *testing.T) {
	ac := AgentConfig{}
	assert.Equal(t, DefaultMaxContextLines, ac.EffectiveMaxContextLines())
	assert.Equal(t, 1500, ac.EffectiveMaxContextLines(), "the epic's confirmed default")
}

func TestMaxContextLines_ExplicitValue(t *testing.T) {
	n := 800
	ac := AgentConfig{MaxContextLines: &n}
	assert.Equal(t, 800, ac.EffectiveMaxContextLines())
}

func TestMaxContextLines_ClampsNonPositiveDirectlyConstructed(t *testing.T) {
	zero := 0
	ac := AgentConfig{MaxContextLines: &zero}
	assert.Equal(t, DefaultMaxContextLines, ac.EffectiveMaxContextLines(), "directly-constructed zero must fall back to default")

	neg := -10
	ac = AgentConfig{MaxContextLines: &neg}
	assert.Equal(t, DefaultMaxContextLines, ac.EffectiveMaxContextLines(), "directly-constructed negative must fall back to default")
}

func TestMaxContextLines_ParsedFromYAML(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce:
    provider: p
    model: m
    max_context_lines: 1200
`))
	require.NoError(t, err)
	require.NotNil(t, reg.Agents["bruce"].MaxContextLines)
	assert.Equal(t, 1200, *reg.Agents["bruce"].MaxContextLines)
}

func TestMaxContextLines_RejectsNonPositiveAtLoad(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce:
    provider: p
    model: m
    max_context_lines: 0
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_context_lines")
}

func TestMaxContextLines_RejectsOverCapAtLoad(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce:
    provider: p
    model: m
    max_context_lines: 99999999
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_context_lines")
}
