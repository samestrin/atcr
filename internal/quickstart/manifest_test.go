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

func TestRegistryYAML_EmptyModelsDoesNotPanic(t *testing.T) {
	m := &Manifest{
		Provider: Provider{Name: "synthetic", BaseURL: "https://x/y", APIKeyEnv: "K"},
		Models:   nil,
	}
	// Must not panic on the round-robin modulo; emits the provider block only.
	out := RegistryYAML(m, []string{"bruce"})
	assert.Contains(t, out, "synthetic:")
	assert.NotContains(t, out, "model:")
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

func TestValidate_RequiresValidSignupURL(t *testing.T) {
	m := &Manifest{
		SignupURL: "",
		Provider:  Provider{Name: "synthetic", BaseURL: "https://api.synthetic.new/openai/v1", APIKeyEnv: "LLM_SYNTHETIC_API_KEY"},
		Models:    []string{"m0"},
	}
	err := m.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signup_url")

	m.SignupURL = "://not-a-url"
	err = m.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signup_url")
}

func TestValidate_RejectsControlCharInProviderFields(t *testing.T) {
	base := Manifest{
		SignupURL: "https://synthetic.new/",
		Provider:  Provider{Name: "synthetic", BaseURL: "https://api.synthetic.new/openai/v1", APIKeyEnv: "LLM_SYNTHETIC_API_KEY"},
		Models:    []string{"m0"},
	}

	// A newline in provider.name is emitted verbatim into registry.yaml and would
	// forge entirely new YAML keys/agents — reject it at the load boundary, the
	// same defense already applied to model ids.
	m := base
	m.Provider.Name = "synthetic\nagents:\n  evil: {}"
	err := m.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider.name")

	// The other emitted provider fields get the same scan.
	m = base
	m.Provider.APIKeyEnv = "KEY\nINJECT"
	assert.Error(t, m.validate())

	m = base
	m.Provider.BaseURL = "https://x/y\r\nevil: true"
	assert.Error(t, m.validate())
}

func TestValidate_RejectsUnsafeYAMLScalars(t *testing.T) {
	base := Manifest{
		SignupURL: "https://synthetic.new/",
		Provider:  Provider{Name: "synthetic", BaseURL: "https://api.synthetic.new/openai/v1", APIKeyEnv: "LLM_SYNTHETIC_API_KEY"},
		Models:    []string{"m0"},
	}

	// A model id emitted as a bare `model: <id>` scalar can break YAML (`: `),
	// silently truncate (` #`), or change type (`[a,b]` becomes a list). Reject
	// them at the load boundary rather than shipping a broken registry.yaml.
	for _, bad := range []string{"gpt: evil", "gpt # x", "[a,b]", "*alias", "&anchor", "a:"} {
		m := base
		m.Models = []string{bad}
		assert.Errorf(t, m.validate(), "unsafe model id %q must be rejected", bad)
	}

	// The same guard applies to the provider fields, which are also emitted bare.
	m := base
	m.Provider.Name = "synthetic: injected"
	assert.Error(t, m.validate(), "unsafe provider.name must be rejected")

	// A colon NOT followed by a space is a valid plain scalar — real provider ids
	// use them (hf:org/model), and base_url has one (https://…). Do not over-reject.
	m = base
	m.Models = []string{"hf:meta-llama/Llama-3.3-70B"}
	assert.NoError(t, m.validate(), "colon-without-space id must be accepted")
}

func TestSignupLink_HandlesFragment(t *testing.T) {
	m := &Manifest{SignupURL: "https://example.com/#section", Referral: "abc"}
	assert.Equal(t, "https://example.com/?referral=abc#section", m.SignupLink())
}

func TestRegistryYAML_SkipsAgentsHeaderWhenRosterEmpty(t *testing.T) {
	m := &Manifest{
		Provider: Provider{Name: "synthetic", BaseURL: "https://api.synthetic.new/openai/v1", APIKeyEnv: "LLM_SYNTHETIC_API_KEY"},
		Models:   []string{"m0"},
	}
	out := RegistryYAML(m, nil)
	assert.NotContains(t, out, "agents:")
}
