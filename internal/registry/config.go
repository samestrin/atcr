package registry

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Defaults applied to optional agent fields at load time. Payload has no
// agent-level default: an empty payload means "inherit the project default"
// so the precedence chain (CLI > project > registry > embedded) stays intact.
const (
	DefaultTemperature = 0.7
	DefaultTimeoutSecs = 600
)

// envVarName matches valid POSIX environment variable names.
var envVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Provider is an OpenAI-compatible endpoint definition. API keys are never
// stored; APIKeyEnv names the environment variable resolved at invoke time.
type Provider struct {
	APIKeyEnv string `yaml:"api_key_env"`
	BaseURL   string `yaml:"base_url,omitempty"`
}

// AgentConfig binds a provider+model to a reviewer persona. Temperature and
// TimeoutSecs are pointers so an explicit zero survives default application.
type AgentConfig struct {
	Provider    string   `yaml:"provider"`
	Model       string   `yaml:"model"`
	Persona     string   `yaml:"persona,omitempty"`
	Temperature *float64 `yaml:"temperature,omitempty"`
	TimeoutSecs *int     `yaml:"timeout_secs,omitempty"`
	RateLimited bool     `yaml:"rate_limited,omitempty"`
	Fallback    string   `yaml:"fallback,omitempty"`
	Payload     string   `yaml:"payload,omitempty"`
}

// Registry is the user-level configuration from ~/.config/atcr/registry.yaml:
// providers, agents, and optional user-level defaults for the shared review
// settings (the tier between project config and embedded defaults in the
// precedence chain). Personas live as .md files next to it, not in YAML.
type Registry struct {
	Providers map[string]Provider    `yaml:"providers"`
	Agents    map[string]AgentConfig `yaml:"agents"`

	PayloadMode string `yaml:"payload_mode,omitempty"`
	TimeoutSecs *int   `yaml:"timeout_secs,omitempty"`
	FailOn      string `yaml:"fail_on,omitempty"`
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

	base := filepath.Base(path)
	var reg Registry
	if err := decodeStrictYAML(data, &reg); err != nil {
		if errors.Is(err, errEmptyDocument) {
			return nil, fmt.Errorf("%s is empty: define providers and agents", base)
		}
		return nil, fmt.Errorf("failed to parse %s: %w", base, err)
	}

	if err := reg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", base, err)
	}
	reg.applyDefaults()
	return &reg, nil
}

// validate checks required fields and reference integrity.
func (r *Registry) validate() error {
	if r.TimeoutSecs != nil && *r.TimeoutSecs <= 0 {
		return errors.New("timeout_secs must be positive")
	}
	for name, p := range r.Providers {
		if strings.TrimSpace(name) == "" {
			return errors.New("providers: provider name must not be empty")
		}
		if p.APIKeyEnv == "" {
			return fmt.Errorf("providers.%s: required field 'api_key_env' is missing", name)
		}
		if !envVarName.MatchString(p.APIKeyEnv) {
			return fmt.Errorf("providers.%s: api_key_env %q is not a valid environment variable name", name, p.APIKeyEnv)
		}
		if p.BaseURL != "" {
			u, err := url.Parse(p.BaseURL)
			if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
				return fmt.Errorf("providers.%s: base_url must be a valid http or https URL", name)
			}
			if u.User != nil {
				return fmt.Errorf("providers.%s: base_url must not embed credentials (userinfo)", name)
			}
		}
	}
	for name, a := range r.Agents {
		if strings.TrimSpace(name) == "" {
			return errors.New("agents: agent name must not be empty")
		}
		if a.Provider == "" {
			return fmt.Errorf("agent '%s': required field 'provider' is missing", name)
		}
		if a.Model == "" {
			return fmt.Errorf("agent '%s': required field 'model' is missing", name)
		}
		if _, ok := r.Providers[a.Provider]; !ok {
			return fmt.Errorf("agent '%s' references unknown provider '%s'", name, a.Provider)
		}
		if a.TimeoutSecs != nil && *a.TimeoutSecs <= 0 {
			return fmt.Errorf("agent '%s': timeout_secs must be positive", name)
		}
		if a.Temperature != nil && (*a.Temperature < 0 || *a.Temperature > 2) {
			return fmt.Errorf("agent '%s': temperature must be within [0, 2]", name)
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
		if a.TimeoutSecs == nil {
			secs := DefaultTimeoutSecs
			a.TimeoutSecs = &secs
		}
		r.Agents[name] = a
	}
}
