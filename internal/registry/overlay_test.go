package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeUserRegistry writes a user-level registry.yaml in its own temp dir and
// returns the file path (its directory also hosts the trust store).
func writeUserRegistry(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// writeProjectRegistry writes .atcr/registry.yaml under root.
func writeProjectRegistry(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ".atcr")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "registry.yaml"), []byte(content), 0o644))
}

func TestLoadProjectRegistry_Absent(t *testing.T) {
	pr, err := LoadProjectRegistry(filepath.Join(t.TempDir(), ".atcr", "registry.yaml"))
	require.NoError(t, err, "an absent project registry is not an error — the overlay is optional")
	assert.Nil(t, pr, "absent overlay yields a nil ProjectRegistry")
}

func TestLoadProjectRegistry_StrictUnknownField(t *testing.T) {
	root := t.TempDir()
	// payload_mode is a settings field — it belongs in .atcr/config.yaml, not the
	// definitions overlay. Strict parsing must reject it.
	writeProjectRegistry(t, root, "providers: {}\nagents: {}\npayload_mode: blocks\n")
	_, err := LoadProjectRegistry(DefaultProjectRegistryPath(root))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "registry.yaml")
}

func TestMergeProject_AddsNewEntries(t *testing.T) {
	reg := &Registry{
		Providers: map[string]Provider{"openai": {APIKeyEnv: "OPENAI_API_KEY"}},
		Agents:    map[string]AgentConfig{"bruce": {Provider: "openai", Model: "gpt-4"}},
	}
	reg.stampSource(SourceUser)
	pr := &ProjectRegistry{
		Agents: map[string]AgentConfig{"team": {Provider: "openai", Model: "gpt-4o"}},
	}
	reg.mergeProject(pr)

	require.Contains(t, reg.Agents, "team")
	assert.Equal(t, "gpt-4o", reg.Agents["team"].Model)
	assert.Equal(t, SourceProject, reg.AgentSource["team"].Tier)
	assert.Equal(t, SourceUser, reg.AgentSource["bruce"].Tier)
	assert.Equal(t, SourceUser, reg.ProviderSource["openai"].Tier)
}

func TestMergeProject_ShadowsWholeEntry(t *testing.T) {
	reg := &Registry{
		Providers: map[string]Provider{"openai": {APIKeyEnv: "OPENAI_API_KEY", BaseURL: "https://api.openai.com/v1"}},
		Agents: map[string]AgentConfig{
			"bruce": {Provider: "openai", Model: "gpt-4", Persona: "bruce", Fallback: "x"},
		},
	}
	reg.stampSource(SourceUser)
	pr := &ProjectRegistry{
		// Project bruce omits Fallback — whole-entry shadowing means the user's
		// fallback must NOT survive (no field-level merge).
		Agents: map[string]AgentConfig{"bruce": {Provider: "openai", Model: "gpt-5"}},
	}
	reg.mergeProject(pr)

	got := reg.Agents["bruce"]
	assert.Equal(t, "gpt-5", got.Model, "project entry replaces the model")
	assert.Empty(t, got.Fallback, "whole-entry shadowing drops the user's fallback")
	assert.Equal(t, SourceProject, reg.AgentSource["bruce"].Tier)
}

func TestMergeProject_ProviderShadow(t *testing.T) {
	reg := &Registry{
		Providers: map[string]Provider{"p": {APIKeyEnv: "USER_KEY", BaseURL: "https://user.example/v1"}},
		Agents:    map[string]AgentConfig{},
	}
	reg.stampSource(SourceUser)
	pr := &ProjectRegistry{
		Providers: map[string]Provider{"p": {APIKeyEnv: "PROJ_KEY", BaseURL: "https://proj.example/v1"}},
	}
	reg.mergeProject(pr)

	assert.Equal(t, "PROJ_KEY", reg.Providers["p"].APIKeyEnv)
	assert.Equal(t, SourceProject, reg.ProviderSource["p"].Tier)
	assert.Equal(t, projectRegistryLabel, reg.ProviderSource["p"].File)
}

func TestLoadMergedRegistry_NoOverlay(t *testing.T) {
	regPath := writeUserRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
`)
	reg, err := LoadMergedRegistry(regPath, t.TempDir())
	require.NoError(t, err)
	require.Contains(t, reg.Agents, "bruce")
	assert.Equal(t, SourceUser, reg.AgentSource["bruce"].Tier)
	// defaults still apply over the merged view
	assert.Equal(t, "bruce", reg.Agents["bruce"].Persona)
}

func TestLoadMergedRegistry_ProjectAgentUsesUserProvider(t *testing.T) {
	regPath := writeUserRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
`)
	root := t.TempDir()
	// A project agent that references a USER provider needs no trust gate and
	// must validate against the merged view.
	writeProjectRegistry(t, root, `
agents:
  team-reviewer:
    provider: openai
    model: gpt-4o
    fallback: bruce
`)
	reg, err := LoadMergedRegistry(regPath, root)
	require.NoError(t, err)
	require.Contains(t, reg.Agents, "team-reviewer")
	assert.Equal(t, SourceProject, reg.AgentSource["team-reviewer"].Tier)
	assert.Equal(t, "bruce", reg.Agents["team-reviewer"].Fallback, "cross-tier fallback resolves")
}
