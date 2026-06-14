package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 2.2: scope / min_severity / max_findings are optional per-agent review
// guardrails. They parse off AgentConfig and are validated at load.
func TestRegistryLoad_ReviewConstraints(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
  bruce-backup:
    provider: openai
    model: gpt-4-mini
    scope: ["performance", "efficiency"]
    min_severity: MEDIUM
    max_findings: 20
`))
	require.NoError(t, err)

	bb := reg.Agents["bruce-backup"]
	assert.Equal(t, []string{"performance", "efficiency"}, bb.Scope)
	assert.Equal(t, "MEDIUM", bb.MinSeverity)
	require.NotNil(t, bb.MaxFindings)
	assert.Equal(t, 20, *bb.MaxFindings)

	// Unset on a plain agent — backward compatible, all zero values.
	bruce := reg.Agents["bruce"]
	assert.Nil(t, bruce.Scope, "scope stays nil when unset")
	assert.Empty(t, bruce.MinSeverity, "min_severity stays empty when unset")
	assert.Nil(t, bruce.MaxFindings, "max_findings stays nil when unset")
}

func TestRegistryLoad_InvalidMinSeverity(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    min_severity: SOMETIMES
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "min_severity")
}

func TestRegistryLoad_InvalidMaxFindings(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    max_findings: 0
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "max_findings")
}

func TestRegistryLoad_EmptyScopeEntry(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    scope: ["performance", ""]
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bruce")
	assert.Contains(t, err.Error(), "scope")
}

// min_severity accepts every rubric level, case-insensitively normalized to the
// canonical upper-case form so enforcement comparisons are stable.
func TestRegistryLoad_MinSeverityCaseInsensitive(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    min_severity: high
`))
	require.NoError(t, err)
	assert.Equal(t, "HIGH", reg.Agents["bruce"].MinSeverity, "min_severity normalized to canonical upper-case")
}
