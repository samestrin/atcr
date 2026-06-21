package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const debateBaseProviders = `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
`

func TestDebateConfig_DefaultsWhenAbsent(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, debateBaseProviders))
	require.NoError(t, err)
	// Triggers default to all three kinds; max_items stays unset (nil) and is
	// resolved at the debate stage; allow_single_model defaults false.
	assert.ElementsMatch(t,
		[]string{DebateTriggerSeveritySplit, DebateTriggerGrayZone, DebateTriggerVerificationDisagreement},
		reg.Debate.Triggers)
	assert.Nil(t, reg.Debate.MaxItems)
	assert.False(t, reg.Debate.AllowSingleModel)
}

func TestDebateConfig_ExplicitValues(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, debateBaseProviders+`
debate:
  triggers:
    - severity_split
  max_items: 0
  allow_single_model: true
`))
	require.NoError(t, err)
	assert.Equal(t, []string{DebateTriggerSeveritySplit}, reg.Debate.Triggers)
	require.NotNil(t, reg.Debate.MaxItems)
	assert.Equal(t, 0, *reg.Debate.MaxItems)
	assert.True(t, reg.Debate.AllowSingleModel)
}

func TestDebateConfig_InvalidTrigger(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, debateBaseProviders+`
debate:
  triggers:
    - solo_finding
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "debate.triggers")
	assert.Contains(t, err.Error(), "severity_split")
}

func TestDebateConfig_NegativeMaxItems(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, debateBaseProviders+`
debate:
  max_items: -1
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "debate.max_items")
}

func TestDebateConfig_ExplicitMaxItemsPreserved(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, debateBaseProviders+`
debate:
  max_items: 3
`))
	require.NoError(t, err)
	require.NotNil(t, reg.Debate.MaxItems)
	assert.Equal(t, 3, *reg.Debate.MaxItems)
}
