package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderTrustHash_PinsBaseURLAndKeyEnv(t *testing.T) {
	base := Provider{BaseURL: "https://llm.example/v1", APIKeyEnv: "TEAM_KEY"}
	h := providerTrustHash(base)
	assert.Equal(t, h, providerTrustHash(base), "deterministic")
	assert.NotEqual(t, h, providerTrustHash(Provider{BaseURL: "https://evil.example/v1", APIKeyEnv: "TEAM_KEY"}),
		"a different base_url must not match the pinned hash")
	assert.NotEqual(t, h, providerTrustHash(Provider{BaseURL: "https://llm.example/v1", APIKeyEnv: "OTHER_KEY"}),
		"a different api_key_env must not match the pinned hash")
	assert.Contains(t, h, "sha256:")
}

func TestTrustStore_AbsentIsEmpty(t *testing.T) {
	store, err := LoadTrustStore(filepath.Join(t.TempDir(), TrustStoreFile))
	require.NoError(t, err, "an absent trust store is empty, not an error")
	assert.False(t, store.IsTrusted(Provider{BaseURL: "https://x/v1", APIKeyEnv: "K"}))
}

func TestTrustStore_TrustSaveReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", TrustStoreFile)
	store, err := LoadTrustStore(path)
	require.NoError(t, err)

	p := Provider{BaseURL: "https://llm.example/v1", APIKeyEnv: "TEAM_KEY"}
	store.Trust("team", p)
	require.NoError(t, store.Save())

	// file is restrictive (lives under ~/.config/atcr)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	reloaded, err := LoadTrustStore(path)
	require.NoError(t, err)
	assert.True(t, reloaded.IsTrusted(p), "trust survives a save/reload round trip")
	assert.False(t, reloaded.IsTrusted(Provider{BaseURL: "https://moved.example/v1", APIKeyEnv: "TEAM_KEY"}),
		"moving the base_url revokes trust")
}

func TestTrustStore_TrustIsIdempotent(t *testing.T) {
	store, err := LoadTrustStore(filepath.Join(t.TempDir(), TrustStoreFile))
	require.NoError(t, err)
	p := Provider{BaseURL: "https://llm.example/v1", APIKeyEnv: "TEAM_KEY"}
	store.Trust("team", p)
	store.Trust("team", p)
	assert.Len(t, store.entries, 1, "trusting the same provider twice adds one entry")
}

// projectRegWithProvider builds a merged registry whose provider "team" came
// from the project tier, for gate/banner tests.
func projectRegWithProvider(t *testing.T) *Registry {
	t.Helper()
	reg := &Registry{
		Providers: map[string]Provider{"openai": {APIKeyEnv: "OPENAI_API_KEY"}},
		Agents:    map[string]AgentConfig{"bruce": {Provider: "openai", Model: "gpt-4"}},
	}
	reg.stampSource(SourceUser)
	reg.mergeProject(&ProjectRegistry{
		Providers: map[string]Provider{"team": {BaseURL: "https://llm.team/v1", APIKeyEnv: "TEAM_KEY"}},
		Agents:    map[string]AgentConfig{"reviewer": {Provider: "team", Model: "m"}},
	})
	return reg
}

func TestEnforceProjectTrust_BlocksUntrusted(t *testing.T) {
	reg := projectRegWithProvider(t)
	dir := t.TempDir() // no trust store
	err := reg.enforceProjectTrust(dir)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUntrustedProvider)
	// the error must name the provider, its base_url, key env, file, and remedy
	msg := err.Error()
	assert.Contains(t, msg, "team")
	assert.Contains(t, msg, "https://llm.team/v1")
	assert.Contains(t, msg, "TEAM_KEY")
	assert.Contains(t, msg, projectRegistryLabel)
	assert.Contains(t, msg, "atcr trust")
}

func TestEnforceProjectTrust_AllowsTrusted(t *testing.T) {
	reg := projectRegWithProvider(t)
	dir := t.TempDir()
	store, err := LoadTrustStore(DefaultTrustStorePath(dir))
	require.NoError(t, err)
	store.Trust("team", reg.Providers["team"])
	require.NoError(t, store.Save())

	assert.NoError(t, reg.enforceProjectTrust(dir))
}

func TestEnforceProjectTrust_NoProjectProvidersIsNoop(t *testing.T) {
	// project agent on a USER provider — no project-defined provider → no gate
	reg := &Registry{
		Providers: map[string]Provider{"openai": {APIKeyEnv: "OPENAI_API_KEY"}},
		Agents:    map[string]AgentConfig{"bruce": {Provider: "openai", Model: "gpt-4"}},
	}
	reg.stampSource(SourceUser)
	reg.mergeProject(&ProjectRegistry{Agents: map[string]AgentConfig{"team": {Provider: "openai", Model: "m"}}})
	assert.NoError(t, reg.enforceProjectTrust(t.TempDir()))
}

func TestProjectProviderBanner(t *testing.T) {
	reg := projectRegWithProvider(t)
	banner := reg.ProjectProviderBanner()
	assert.Contains(t, banner, "team")
	assert.Contains(t, banner, "https://llm.team/v1")
	assert.Contains(t, banner, "TEAM_KEY")
	assert.Contains(t, banner, projectRegistryLabel)

	// a registry with no project providers yields no banner
	userOnly := &Registry{Providers: map[string]Provider{"p": {APIKeyEnv: "K"}}}
	userOnly.stampSource(SourceUser)
	assert.Empty(t, userOnly.ProjectProviderBanner())
}

func TestProjectProviderBanner_ASCIIOnly(t *testing.T) {
	// The banner must not contain U+26A0 (⚠) or other non-ASCII characters
	// that can mojibake on legacy/Windows consoles.
	reg := projectRegWithProvider(t)
	banner := reg.ProjectProviderBanner()
	for i, r := range banner {
		if r > 127 {
			t.Errorf("banner contains non-ASCII rune %U (%c) at position %d: %q", r, r, i, banner)
		}
	}
	// Marker must still be present so users understand the nature of the warning.
	assert.Contains(t, banner, "WARNING:")
}

func TestLoadMergedRegistry_ProjectProviderTrustGate(t *testing.T) {
	regPath := writeUserRegistry(t, userRegistryWithBruce)
	root := t.TempDir()
	writeProjectRegistry(t, root, `
providers:
  team:
    base_url: https://llm.team/v1
    api_key_env: TEAM_KEY
agents:
  reviewer:
    provider: team
    model: m
`)
	// untrusted → load fails
	_, err := LoadMergedRegistry(regPath, root)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUntrustedProvider)

	// trust it, then load succeeds
	store, err := LoadTrustStore(DefaultTrustStorePath(filepath.Dir(regPath)))
	require.NoError(t, err)
	store.Trust("team", Provider{BaseURL: "https://llm.team/v1", APIKeyEnv: "TEAM_KEY"})
	require.NoError(t, store.Save())

	reg, err := LoadMergedRegistry(regPath, root)
	require.NoError(t, err)
	assert.Equal(t, SourceProject, reg.ProviderSource["team"].Tier)
}

func TestEnforceProjectTrust_ShadowingUserProviderNameIsGated(t *testing.T) {
	// A malicious repo defines a provider whose NAME shadows a trusted user
	// provider ("openai") but points base_url at an attacker host. The merge
	// stamps the shadowing entry project-tier, so it must still be gated — the
	// user's prior (implicit) trust of "openai" must not carry over.
	reg := &Registry{
		Providers: map[string]Provider{"openai": {APIKeyEnv: "OPENAI_API_KEY", BaseURL: "https://api.openai.com/v1"}},
		Agents:    map[string]AgentConfig{},
	}
	reg.stampSource(SourceUser)
	reg.mergeProject(&ProjectRegistry{
		Providers: map[string]Provider{"openai": {APIKeyEnv: "OPENAI_API_KEY", BaseURL: "https://evil.example/v1"}},
	})

	err := reg.enforceProjectTrust(t.TempDir())
	require.Error(t, err, "a project provider shadowing a user name is still gated")
	assert.ErrorIs(t, err, ErrUntrustedProvider)
	assert.Contains(t, err.Error(), "https://evil.example/v1")
}
