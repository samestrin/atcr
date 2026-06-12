package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userRegistryWithBruce is a minimal user registry: one provider, one agent.
const userRegistryWithBruce = `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
`

func TestMergedValidation_ProjectAgentUnknownProvider(t *testing.T) {
	regPath := writeUserRegistry(t, userRegistryWithBruce)
	root := t.TempDir()
	writeProjectRegistry(t, root, `
agents:
  team:
    provider: ghost-provider
    model: m
`)
	_, err := LoadMergedRegistry(regPath, root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), projectRegistryLabel, "error names the project file that defined the bad agent")
	assert.Contains(t, err.Error(), "ghost-provider")
}

func TestMergedValidation_ProjectAgentRangeError(t *testing.T) {
	regPath := writeUserRegistry(t, userRegistryWithBruce)
	root := t.TempDir()
	writeProjectRegistry(t, root, `
agents:
  team:
    provider: openai
    model: m
    temperature: 5.0
`)
	_, err := LoadMergedRegistry(regPath, root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), projectRegistryLabel)
	assert.Contains(t, err.Error(), "temperature")
}

func TestMergedValidation_CrossTierDangling(t *testing.T) {
	regPath := writeUserRegistry(t, userRegistryWithBruce)
	root := t.TempDir()
	writeProjectRegistry(t, root, `
agents:
  team:
    provider: openai
    model: m
    fallback: ghost
`)
	_, err := LoadMergedRegistry(regPath, root)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDanglingFallback, "sentinel survives attribution wrapping")
	assert.Contains(t, err.Error(), projectRegistryLabel, "dangling error names the project file")
	assert.Contains(t, err.Error(), "unknown agent 'ghost'")
}

func TestMergedValidation_CrossTierCycle(t *testing.T) {
	// User bruce falls back to project agent team; team falls back to bruce.
	// The cycle only exists in the merged view.
	regPath := writeUserRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
    fallback: team
`)
	root := t.TempDir()
	writeProjectRegistry(t, root, `
agents:
  team:
    provider: openai
    model: m
    fallback: bruce
`)
	_, err := LoadMergedRegistry(regPath, root)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFallbackCycle, "cross-tier cycle fails fast at load")
	assert.Contains(t, err.Error(), "fallback cycle detected")
	// Sorted DFS enters at "bruce" (user tier), so the file named is the user
	// registry; the full cycle path is present regardless.
	assert.Contains(t, err.Error(), "bruce -> team -> bruce")
	assert.Contains(t, err.Error(), userRegistryLabel)
}

func TestMergedValidation_ProjectProviderEmptyName(t *testing.T) {
	// A project registry with an empty provider key must attribute the error
	// to .atcr/registry.yaml (the project file), not to the user registry.
	regPath := writeUserRegistry(t, userRegistryWithBruce)
	root := t.TempDir()
	writeProjectRegistry(t, root, `
providers:
  '':
    api_key_env: FOO
`)
	_, err := LoadMergedRegistry(regPath, root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), projectRegistryLabel,
		"empty-provider error must name the project file, not the user registry")
	assert.Contains(t, err.Error(), "provider name must not be empty")
}

func TestMergedValidation_ProjectAgentEmptyName(t *testing.T) {
	regPath := writeUserRegistry(t, userRegistryWithBruce)
	root := t.TempDir()
	writeProjectRegistry(t, root, `
agents:
  '':
    provider: openai
    model: m
`)
	_, err := LoadMergedRegistry(regPath, root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), projectRegistryLabel,
		"empty-agent error must name the project file, not the user registry")
	assert.Contains(t, err.Error(), "agent name must not be empty")
}

func TestMergedValidation_UserSettingsErrorNamesFile(t *testing.T) {
	// A top-level settings fault in the USER registry must keep the
	// "registry.yaml:" prefix in the merged path (parity with LoadRegistry).
	regPath := writeUserRegistry(t, `
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  bruce:
    provider: openai
    model: gpt-4
payload_mode: nonsense
`)
	_, err := LoadMergedRegistry(regPath, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), userRegistryLabel)
	assert.Contains(t, err.Error(), "payload_mode")
}
