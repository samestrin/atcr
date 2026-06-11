package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

// loadRegistryWith loads a minimal registry carrying the given user-level
// default lines.
func loadRegistryWith(t *testing.T, globals string) *Registry {
	t.Helper()
	reg, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m}
`+globals))
	require.NoError(t, err)
	return reg
}

func TestPrecedence_CLIOverridesProject(t *testing.T) {
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ntimeout_secs: 600\n"))
	require.NoError(t, err)

	s := ResolveSettings(CLIOverrides{TimeoutSecs: intPtr(300)}, proj, nil)
	assert.Equal(t, 300, s.TimeoutSecs, "CLI flag wins over project config")
}

func TestPrecedence_ProjectOverridesRegistry(t *testing.T) {
	reg := loadRegistryWith(t, "timeout_secs: 1200\npayload_mode: files\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ntimeout_secs: 900\npayload_mode: diff\n"))
	require.NoError(t, err)

	s := ResolveSettings(CLIOverrides{}, proj, reg)
	assert.Equal(t, 900, s.TimeoutSecs, "project config wins over registry")
	assert.Equal(t, "diff", s.PayloadMode, "project config wins over registry")
}

func TestPrecedence_RegistryOverridesEmbedded(t *testing.T) {
	reg := loadRegistryWith(t, "timeout_secs: 1200\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := ResolveSettings(CLIOverrides{}, proj, reg)
	assert.Equal(t, 1200, s.TimeoutSecs, "registry wins over embedded default")
}

func TestPrecedence_FullChain(t *testing.T) {
	// embedded blocks < registry files < project summary-ish < CLI diff.
	// (Enum validation of payload modes is a separate concern; precedence
	// operates on raw values.)
	reg := loadRegistryWith(t, "payload_mode: files\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\npayload_mode: blocks\n"))
	require.NoError(t, err)

	s := ResolveSettings(CLIOverrides{PayloadMode: strPtr("diff")}, proj, reg)
	assert.Equal(t, "diff", s.PayloadMode, "CLI flag wins over the full chain")
}

func TestPrecedence_NoOverride(t *testing.T) {
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := ResolveSettings(CLIOverrides{}, proj, nil)
	assert.Equal(t, DefaultPayloadMode, s.PayloadMode, "embedded default used when nothing overrides")
	assert.Equal(t, DefaultTimeoutSecs, s.TimeoutSecs)
	assert.Equal(t, DefaultFailOn, s.FailOn)
}

func TestPrecedence_EachFieldIndependent(t *testing.T) {
	reg := loadRegistryWith(t, "fail_on: LOW\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ntimeout_secs: 900\n"))
	require.NoError(t, err)

	s := ResolveSettings(CLIOverrides{PayloadMode: strPtr("diff")}, proj, reg)
	assert.Equal(t, "diff", s.PayloadMode, "from CLI")
	assert.Equal(t, 900, s.TimeoutSecs, "from project")
	assert.Equal(t, "LOW", s.FailOn, "from registry")
}

func TestPrecedence_NilTiersFallThrough(t *testing.T) {
	s := ResolveSettings(CLIOverrides{}, nil, nil)
	assert.Equal(t, DefaultPayloadMode, s.PayloadMode)
	assert.Equal(t, DefaultTimeoutSecs, s.TimeoutSecs)
	assert.Equal(t, DefaultFailOn, s.FailOn)
}

func TestRegistryGlobals_Validation(t *testing.T) {
	t.Run("negative registry timeout rejected", func(t *testing.T) {
		_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents: {}
timeout_secs: -1
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout_secs")
	})
}

func TestProjectConfig_AbsentFieldsStayUnset(t *testing.T) {
	// Precedence needs "absent" preserved at load time; defaults are applied
	// only by ResolveSettings.
	cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)
	assert.Empty(t, cfg.PayloadMode)
	assert.Nil(t, cfg.TimeoutSecs)
	assert.Empty(t, cfg.FailOn)
}
