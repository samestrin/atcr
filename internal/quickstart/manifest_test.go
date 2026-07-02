package quickstart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/registry"
)

func TestLoadManifest_Embedded(t *testing.T) {
	m, err := LoadManifest()
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "synthetic", m.Provider.Name)
	assert.Equal(t, "LLM_SYNTHETIC_API_KEY", m.Provider.APIKeyEnv)
	assert.True(t, strings.HasPrefix(m.Provider.BaseURL, "https://"), "base_url is https")
	require.NotEmpty(t, m.Models, "manifest ships at least one model")
}

func TestSignupLink(t *testing.T) {
	// No referral → the bare signup URL.
	m := &Manifest{SignupURL: "https://synthetic.new/", Referral: ""}
	assert.Equal(t, "https://synthetic.new/", m.SignupLink())

	// Referral set → appended as a query param.
	m.Referral = "ABC123"
	assert.Equal(t, "https://synthetic.new/?referral=ABC123", m.SignupLink())
}

func TestRegistryYAML_DefinesProviderAndAgents(t *testing.T) {
	m := &Manifest{
		Provider: Provider{Name: "synthetic", BaseURL: "https://api.synthetic.new/openai/v1", APIKeyEnv: "LLM_SYNTHETIC_API_KEY"},
		Models:   []string{"m0", "m1"},
	}
	out := RegistryYAML(m, []string{"bruce", "greta", "kai"})

	assert.Contains(t, out, "base_url: https://api.synthetic.new/openai/v1")
	assert.Contains(t, out, "api_key_env: LLM_SYNTHETIC_API_KEY")
	// Round-robin binding across the two models.
	assert.Contains(t, out, "model: m0")
	assert.Contains(t, out, "model: m1")
	for _, agent := range []string{"bruce", "greta", "kai"} {
		assert.Contains(t, out, agent+":")
		assert.Contains(t, out, "persona: "+agent)
	}
}

// The generated registry.yaml must strict-parse and validate through the real
// registry loader, and the generated roster must resolve against it — otherwise
// quickstart would produce a config that fails at first `atcr review`.
func TestRegistryYAML_LoadsAndRosterResolves(t *testing.T) {
	m, err := LoadManifest()
	require.NoError(t, err)
	roster := []string{"bruce", "greta", "kai"}

	dir := t.TempDir()
	regPath := filepath.Join(dir, "registry.yaml")
	require.NoError(t, os.WriteFile(regPath, []byte(RegistryYAML(m, roster)), 0o644))

	reg, err := registry.LoadRegistry(regPath)
	require.NoError(t, err, "generated registry.yaml must load and validate")
	_, ok := reg.Providers["synthetic"]
	assert.True(t, ok, "synthetic provider defined")
	for _, agent := range roster {
		_, ok := reg.Agents[agent]
		assert.True(t, ok, "agent %q defined in registry", agent)
	}

	// A project roster listing those agents must resolve cleanly.
	cfg := &registry.ProjectConfig{Agents: roster}
	assert.NoError(t, cfg.ValidateAgainst(reg))
}
