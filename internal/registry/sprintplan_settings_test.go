package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// max_sprint_plan_bytes precedence + validation (plan 19.10 F9/AC10). Mirrors
// cache_settings_test.go: registry (global) and project tiers only, no CLI
// override, resolved to a plain Settings field with a post-resolution re-check.
// Unlike cache_max_bytes, 0 is NOT a valid "unbounded" sentinel here.

func TestPrecedence_MaxSprintPlanBytesDefault(t *testing.T) {
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, nil)
	assert.Equal(t, int64(DefaultMaxSprintPlanBytes), s.MaxSprintPlanBytes, "embedded 64 KiB default used when nothing overrides")
}

func TestPrecedence_MaxSprintPlanBytesChain(t *testing.T) {
	reg := loadRegistryWith(t, "max_sprint_plan_bytes: 32768\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nmax_sprint_plan_bytes: 131072\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, int64(131072), s.MaxSprintPlanBytes, "project config wins over registry")
}

func TestPrecedence_MaxSprintPlanBytesRegistryOverridesEmbedded(t *testing.T) {
	reg := loadRegistryWith(t, "max_sprint_plan_bytes: 32768\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, int64(32768), s.MaxSprintPlanBytes, "registry wins over embedded default")
}

func TestProjectConfig_MaxSprintPlanBytesZeroRejected(t *testing.T) {
	// Unlike cache_max_bytes, 0 is not a valid unbounded sentinel — reject it.
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nmax_sprint_plan_bytes: 0\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_sprint_plan_bytes")
}

func TestProjectConfig_MaxSprintPlanBytesNegativeRejected(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nmax_sprint_plan_bytes: -1\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_sprint_plan_bytes")
}

func TestRegistry_MaxSprintPlanBytesInvalidRejected(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m}
max_sprint_plan_bytes: -5
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_sprint_plan_bytes")
}

// A directly-constructed proj that bypasses the file loaders (which reject <= 0)
// must still be caught by the post-resolution sanity check in ResolveSettings.
func TestResolveSettings_MaxSprintPlanBytesDirectlyConstructedInvalidRejected(t *testing.T) {
	bad := int64(-1)
	proj := &ProjectConfig{Agents: []string{"bruce"}, MaxSprintPlanBytes: &bad}
	_, err := ResolveSettings(CLIOverrides{}, proj, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_sprint_plan_bytes")
}
