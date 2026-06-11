package registry

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Defaults applied to optional agent fields at load time. Payload has no
// agent-level default: an empty payload means "inherit the project default"
// so the precedence chain (CLI > project > registry > embedded) stays intact.
const (
	DefaultTemperature = 0.7
	DefaultTimeoutSecs = 600
)

// Provider is an OpenAI-compatible endpoint definition. API keys are never
// stored; APIKeyEnv names the environment variable resolved at invoke time.
type Provider struct {
	APIKeyEnv string `yaml:"api_key_env"`
	BaseURL   string `yaml:"base_url,omitempty"`
}

// AgentConfig binds a provider+model to a reviewer persona.
type AgentConfig struct {
	Provider    string   `yaml:"provider"`
	Model       string   `yaml:"model"`
	Persona     string   `yaml:"persona,omitempty"`
	Temperature *float64 `yaml:"temperature,omitempty"`
	TimeoutSecs int      `yaml:"timeout_secs,omitempty"`
	RateLimited bool     `yaml:"rate_limited,omitempty"`
	Fallback    string   `yaml:"fallback,omitempty"`
	Payload     string   `yaml:"payload,omitempty"`
}

// Registry is the user-level configuration from ~/.config/atcr/registry.yaml:
// providers and agents. Personas live as .md files next to it, not in YAML.
type Registry struct {
	Providers map[string]Provider    `yaml:"providers"`
	Agents    map[string]AgentConfig `yaml:"agents"`
}

// DefaultRegistryPath returns ~/.config/atcr/registry.yaml.
func DefaultRegistryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".config", "atcr", "registry.yaml"), nil
}

// LoadRegistry reads, strictly parses, and validates the registry at path.
// API key env vars are NOT resolved here; that happens at invoke time.
func LoadRegistry(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("registry not found at %s: run 'atcr init' and create your provider/agent registry (see docs/registry.md)", path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading registry: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("%s is empty: define providers and agents", filepath.Base(path))
	}

	var reg Registry
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&reg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("failed to parse %s: %w", filepath.Base(path), err)
	}

	if err := reg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	reg.applyDefaults()
	return &reg, nil
}

// validate checks required fields and reference integrity.
func (r *Registry) validate() error {
	for name, p := range r.Providers {
		if p.APIKeyEnv == "" {
			return fmt.Errorf("providers.%s: required field 'api_key_env' is missing", name)
		}
		if p.BaseURL != "" {
			u, err := url.Parse(p.BaseURL)
			if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
				return fmt.Errorf("providers.%s: base_url must be a valid http or https URL", name)
			}
		}
	}
	for name, a := range r.Agents {
		if a.Provider == "" {
			return fmt.Errorf("agent '%s': required field 'provider' is missing", name)
		}
		if a.Model == "" {
			return fmt.Errorf("agent '%s': required field 'model' is missing", name)
		}
		if _, ok := r.Providers[a.Provider]; !ok {
			return fmt.Errorf("agent '%s' references unknown provider '%s'", name, a.Provider)
		}
		if a.TimeoutSecs < 0 {
			return fmt.Errorf("agent '%s': timeout_secs must not be negative", name)
		}
		if strings.TrimSpace(name) == "" {
			return errors.New("agents: agent name must not be empty")
		}
	}
	return nil
}

// applyDefaults fills optional agent fields: persona defaults to the agent
// name, temperature to 0.7, timeout_secs to 600. Payload intentionally stays
// empty when unset (inherits the project-level default).
func (r *Registry) applyDefaults() {
	for name, a := range r.Agents {
		if a.Persona == "" {
			a.Persona = name
		}
		if a.Temperature == nil {
			temp := DefaultTemperature
			a.Temperature = &temp
		}
		if a.TimeoutSecs == 0 {
			a.TimeoutSecs = DefaultTimeoutSecs
		}
		r.Agents[name] = a
	}
}
