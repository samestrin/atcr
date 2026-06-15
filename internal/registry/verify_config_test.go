package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const verifyBaseProviders = `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
`

func TestVerifyConfig_DefaultsWhenAbsent(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, verifyBaseProviders))
	require.NoError(t, err)
	assert.Equal(t, DefaultVerifyMinSeverity, reg.Verify.MinSeverity)
	assert.Equal(t, DefaultVerifyVotes, reg.Verify.Votes)
}

func TestVerifyConfig_ExplicitValues(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, verifyBaseProviders+`
verify:
  min_severity: HIGH
  votes: 3
`))
	require.NoError(t, err)
	assert.Equal(t, "HIGH", reg.Verify.MinSeverity)
	assert.Equal(t, 3, reg.Verify.Votes)
}

func TestVerifyConfig_MinSeverityNormalizedToUpper(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, verifyBaseProviders+`
verify:
  min_severity: high
`))
	require.NoError(t, err)
	assert.Equal(t, "HIGH", reg.Verify.MinSeverity)
}

func TestVerifyConfig_EmptyMinSeverityDefaults(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, verifyBaseProviders+`
verify:
  votes: 2
`))
	require.NoError(t, err)
	assert.Equal(t, DefaultVerifyMinSeverity, reg.Verify.MinSeverity)
	assert.Equal(t, 2, reg.Verify.Votes)
}

func TestVerifyConfig_InvalidMinSeverity(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, verifyBaseProviders+`
verify:
  min_severity: BLOCKER
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verify.min_severity")
	assert.Contains(t, err.Error(), "must be LOW, MEDIUM, HIGH, or CRITICAL")
}

func TestVerifyConfig_NegativeVotes(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, verifyBaseProviders+`
verify:
  votes: -1
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verify.votes")
}
