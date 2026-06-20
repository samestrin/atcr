package registry

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 4.6: max_retries / initial_backoff_ms are configurable at the registry
// (global) and agent tiers, resolving through the shared-settings precedence
// chain exactly like timeout_secs.

func TestResolveSettings_RetryDefaults(t *testing.T) {
	s := resolve(t, CLIOverrides{}, nil, nil)
	assert.Equal(t, DefaultMaxRetries, s.MaxRetries, "embedded default max_retries")
	assert.Equal(t, 5, s.MaxRetries, "epic 4.6 AC: default max_retries is 5")
	assert.Equal(t, DefaultInitialBackoffMs, s.InitialBackoffMs, "embedded default initial_backoff_ms")
}

func TestResolveSettings_RegistryRetryOverride(t *testing.T) {
	reg := loadRegistryWith(t, "max_retries: 7\ninitial_backoff_ms: 250\n")
	s := resolve(t, CLIOverrides{}, nil, reg)
	assert.Equal(t, 7, s.MaxRetries, "registry tier overrides the embedded default")
	assert.Equal(t, 250, s.InitialBackoffMs, "registry tier overrides the embedded default")
}

func TestResolveSettings_RegistryRetryZeroRetriesSurvives(t *testing.T) {
	// 0 retries is valid (single attempt, no retry) and must survive default
	// application — the pointer field distinguishes "explicit 0" from "unset".
	reg := loadRegistryWith(t, "max_retries: 0\n")
	s := resolve(t, CLIOverrides{}, nil, reg)
	assert.Equal(t, 0, s.MaxRetries, "explicit 0 retries is honored, not replaced by the default")
}

func TestEffectiveMaxRetries(t *testing.T) {
	s := Settings{MaxRetries: 5, InitialBackoffMs: 500}
	withOwn := AgentConfig{MaxRetries: intPtr(9)}
	without := AgentConfig{}
	zero := AgentConfig{MaxRetries: intPtr(0)}
	assert.Equal(t, 9, withOwn.EffectiveMaxRetries(s), "agent's own max_retries wins")
	assert.Equal(t, 5, without.EffectiveMaxRetries(s), "unset agent max_retries inherits resolved settings")
	assert.Equal(t, 0, zero.EffectiveMaxRetries(s), "explicit 0 override wins over the inherited default")
}

func TestEffectiveInitialBackoffMs(t *testing.T) {
	s := Settings{MaxRetries: 5, InitialBackoffMs: 500}
	withOwn := AgentConfig{InitialBackoffMs: intPtr(125)}
	without := AgentConfig{}
	assert.Equal(t, 125, withOwn.EffectiveInitialBackoffMs(s), "agent's own initial_backoff_ms wins")
	assert.Equal(t, 500, without.EffectiveInitialBackoffMs(s), "unset agent initial_backoff_ms inherits resolved settings")
}

func TestValidate_AgentRetryOutOfRange(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m, max_retries: -1}
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_retries must be within 0..")

	_, err = LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m, initial_backoff_ms: 0}
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "initial_backoff_ms must be within 1..")
}

func TestValidate_RegistryRetryOutOfRange(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m}
max_retries: 999999
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_retries must be within 0..")
}

func TestResolveSettings_DirectRegistryRetryOutOfRangeRejected(t *testing.T) {
	// A directly-constructed Registry (bypassing the file loader) can carry an
	// out-of-range value; ResolveSettings must catch it so the engine never sees it.
	_, err := ResolveSettings(CLIOverrides{}, nil, &Registry{MaxRetries: intPtr(MaxRetriesCap + 1)})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "max_retries"), "post-resolution validation rejects out-of-range max_retries")
}
