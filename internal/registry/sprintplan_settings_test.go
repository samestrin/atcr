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

// max_claim_bytes precedence + validation (Epic 35.16.7). Mirrors the
// max_sprint_plan_bytes cases above, with the one deliberate difference: 0 is a
// VALID setting here and means the claim ledger is disabled, so an operator can
// refuse to send commit-message text to a third-party provider.

func TestPrecedence_MaxClaimBytesDefault(t *testing.T) {
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, nil)
	assert.Equal(t, int64(DefaultMaxClaimBytes), s.ResolvedMaxClaimBytes(),
		"embedded 8 KiB default used when nothing overrides")
}

func TestPrecedence_MaxClaimBytesChain(t *testing.T) {
	reg := loadRegistryWith(t, "max_claim_bytes: 4096\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nmax_claim_bytes: 16384\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, int64(16384), s.ResolvedMaxClaimBytes(), "project config wins over registry")
}

func TestPrecedence_MaxClaimBytesRegistryOverridesEmbedded(t *testing.T) {
	reg := loadRegistryWith(t, "max_claim_bytes: 4096\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, int64(4096), s.ResolvedMaxClaimBytes(), "registry wins over embedded default")
}

// 0 is the operator escape hatch, at BOTH tiers, and it must survive default
// application rather than being read as "unset" — that is the whole reason the
// config fields are pointers.
func TestPrecedence_MaxClaimBytesZeroDisablesAtEitherTier(t *testing.T) {
	t.Run("project tier", func(t *testing.T) {
		reg := loadRegistryWith(t, "max_claim_bytes: 4096\n")
		proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nmax_claim_bytes: 0\n"))
		require.NoError(t, err)

		s := resolve(t, CLIOverrides{}, proj, reg)
		assert.Zero(t, s.ResolvedMaxClaimBytes(), "an explicit 0 disables the ledger; it is not 'unset'")
	})
	t.Run("registry tier", func(t *testing.T) {
		reg := loadRegistryWith(t, "max_claim_bytes: 0\n")
		proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
		require.NoError(t, err)

		s := resolve(t, CLIOverrides{}, proj, reg)
		assert.Zero(t, s.ResolvedMaxClaimBytes())
	})
}

func TestProjectConfig_MaxClaimBytesNegativeRejected(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nmax_claim_bytes: -1\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_claim_bytes")
}

// A hand-built Settings{} — an embedder's, or a test roster's — must NOT silently
// ship with the ledger switched off. That is the failure the pointer exists to
// prevent: 0 is both Go's zero value and a meaningful setting, so an unresolved
// field has to be distinguishable from a deliberate disable.
func TestSettings_ZeroValueDoesNotSilentlyDisableTheClaimLedger(t *testing.T) {
	var s Settings
	require.Nil(t, s.MaxClaimBytes, "precondition: an unresolved field is nil, not 0")
	assert.Equal(t, int64(DefaultMaxClaimBytes), s.ResolvedMaxClaimBytes(),
		"an unresolved setting must fall back to the default, never to 'disabled'")
}

// A mis-resolved negative fails SAFE (disabled), never unbounded: the ledger's
// bytes are exempt from every byte budget, so "unbounded" would be prompt text
// nothing downstream could see or shed.
func TestSettings_NegativeMaxClaimBytesResolvesToDisabledNotUnbounded(t *testing.T) {
	neg := int64(-1)
	s := Settings{MaxClaimBytes: &neg}
	assert.Zero(t, s.ResolvedMaxClaimBytes())
}
