package registry

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultTemperature fills an agent's temperature when unset (applied at
// load time — temperature is purely agent-level).
//
// DefaultTimeoutSecs is the embedded-tier floor of the shared-settings
// precedence chain (see ResolveSettings). Agent-level timeout and payload
// deliberately stay unset at load so agents inherit the resolved settings.
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

// Reserved agent roles for the agentic stages (Epics 3.0/4.0). Reserved and
// validated at load in 1.x but acted on by no v1 code path.
const (
	RoleReviewer = "reviewer"
	RoleSkeptic  = "skeptic"
	RoleJudge    = "judge"
)

// AgentConfig binds a provider+model to a reviewer persona. Temperature and
// TimeoutSecs are pointers so an explicit zero survives default application.
//
// Tools, MaxTurns, ToolBudgetBytes, and Role are reserved for the agentic
// stages (Epics 2.0–4.0). They are parsed and validated at load so a config
// targeting a future stage loads cleanly under the strict v1 parser, but no
// v1 code path acts on them and no load-time default is applied — they stay at
// their zero/unset value in 1.x. MaxTurns and ToolBudgetBytes are pointers so
// the activating stage can tell an explicit value from an unset one (the same
// reason TimeoutSecs is a pointer). Tools is intentionally a value bool (not
// *bool) because its planned default is false and no stage needs to distinguish
// "explicitly false" from "unset". Planned defaults (tools=false,
// max_turns=10, role=reviewer) are documented in docs/registry.md.
type AgentConfig struct {
	Provider    string   `yaml:"provider"`
	Model       string   `yaml:"model"`
	Persona     string   `yaml:"persona,omitempty"`
	Temperature *float64 `yaml:"temperature,omitempty"`
	TimeoutSecs *int     `yaml:"timeout_secs,omitempty"`
	RateLimited bool     `yaml:"rate_limited,omitempty"`
	Fallback    string   `yaml:"fallback,omitempty"`
	Payload     string   `yaml:"payload,omitempty"`

	// Reserved for the agentic stages — parsed + validated, inert in 1.x.
	Tools           bool   `yaml:"tools"`             // Stage 2 — enables the tool loop
	MaxTurns        *int   `yaml:"max_turns"`         // Stage 2 — agent-loop turn cap
	ToolBudgetBytes *int64 `yaml:"tool_budget_bytes"` // Stage 2 — cumulative tool-result budget (0 = unlimited, matches PayloadByteBudget)
	Role            string `yaml:"role"`              // Stage 3/4 — reviewer | skeptic | judge
}

// roleValid reports whether r is an allowed reserved role. The empty string is
// allowed in 1.x (the loader provides no default). Epic 3.0/4.0 contract: when
// activating role-based routing, the stage MUST apply the reviewer default for
// agents whose Role is empty. The loader intentionally leaves Role empty rather
// than defaulting it so that activating stages can distinguish "explicitly set"
// from "inherited default" (option-a decision, recorded in epic-3 planning).
func roleValid(r string) bool {
	switch r {
	case "", RoleReviewer, RoleSkeptic, RoleJudge:
		return true
	default:
		return false
	}
}

// Registry is the user-level configuration from ~/.config/atcr/registry.yaml:
// providers, agents, and optional user-level defaults for the shared review
// settings (the tier between project config and embedded defaults in the
// precedence chain). Personas live as .md files next to it, not in YAML.
type Registry struct {
	Providers map[string]Provider    `yaml:"providers"`
	Agents    map[string]AgentConfig `yaml:"agents"`

	PayloadMode       string `yaml:"payload_mode,omitempty"`
	TimeoutSecs       *int   `yaml:"timeout_secs,omitempty"`
	PayloadByteBudget *int64 `yaml:"payload_byte_budget,omitempty"`
	FailOn            string `yaml:"fail_on,omitempty"`
	// MaxParallel is a pointer so an explicit 0 (unbounded) survives default
	// application in ResolveSettings.
	MaxParallel *int `yaml:"max_parallel,omitempty"`

	// ProviderSource and AgentSource record the tier (and defining file) each
	// effective entry came from after the project overlay merge — user or
	// project. Not serialized (yaml:"-"); populated by stampSource (user) and
	// mergeProject (project). An entry absent from the map is treated as user.
	ProviderSource map[string]EntrySource `yaml:"-"`
	AgentSource    map[string]EntrySource `yaml:"-"`
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
	reg, err := parseRegistryFile(path)
	if err != nil {
		return nil, err
	}

	base := filepath.Base(path)
	if err := reg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", base, err)
	}
	if err := reg.ValidateFallbacks(); err != nil {
		return nil, fmt.Errorf("%s: %w", base, err)
	}
	reg.applyDefaults()
	return reg, nil
}

// validate checks required fields and reference integrity.
func (r *Registry) validate() error {
	if r.TimeoutSecs != nil && (*r.TimeoutSecs <= 0 || *r.TimeoutSecs > MaxTimeoutSecs) {
		return fmt.Errorf("timeout_secs must be within 1..%d", MaxTimeoutSecs)
	}
	if r.PayloadByteBudget != nil && *r.PayloadByteBudget < 0 {
		return fmt.Errorf("payload_byte_budget must be >= 0 (0 = unlimited), got %d", *r.PayloadByteBudget)
	}
	if r.MaxParallel != nil && *r.MaxParallel < 0 {
		return fmt.Errorf("max_parallel must be >= 0 (0 = unbounded), got %d", *r.MaxParallel)
	}
	if !payloadModeValid(r.PayloadMode) {
		return fmt.Errorf("invalid payload_mode '%s': must be one of diff, blocks, files", strings.TrimSpace(r.PayloadMode))
	}
	for name, p := range r.Providers {
		if strings.TrimSpace(name) == "" {
			return providerErrf(name, "providers.%s: provider name must not be empty", name)
		}
		if p.APIKeyEnv == "" {
			return providerErrf(name, "providers.%s: required field 'api_key_env' is missing", name)
		}
		if !envVarName.MatchString(p.APIKeyEnv) {
			return providerErrf(name, "providers.%s: api_key_env %q is not a valid environment variable name", name, p.APIKeyEnv)
		}
		if p.BaseURL != "" {
			u, err := url.Parse(p.BaseURL)
			if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
				return providerErrf(name, "providers.%s: base_url must be a valid http or https URL", name)
			}
			if u.User != nil {
				return providerErrf(name, "providers.%s: base_url must not embed credentials (userinfo)", name)
			}
		}
	}
	for name, a := range r.Agents {
		if strings.TrimSpace(name) == "" {
			return agentErrf(name, "agent '%s': agent name must not be empty", name)
		}
		if a.Provider == "" {
			return agentErrf(name, "agent '%s': required field 'provider' is missing", name)
		}
		if a.Model == "" {
			return agentErrf(name, "agent '%s': required field 'model' is missing", name)
		}
		if _, ok := r.Providers[a.Provider]; !ok {
			return agentErrf(name, "agent '%s' references unknown provider '%s'", name, a.Provider)
		}
		if a.TimeoutSecs != nil && (*a.TimeoutSecs <= 0 || *a.TimeoutSecs > MaxTimeoutSecs) {
			return agentErrf(name, "agent '%s': timeout_secs must be within 1..%d", name, MaxTimeoutSecs)
		}
		if a.Temperature != nil && (*a.Temperature < 0 || *a.Temperature > 2) {
			return agentErrf(name, "agent '%s': temperature must be within [0, 2]", name)
		}
		if !payloadModeValid(a.Payload) {
			return agentErrf(name, "agent '%s': invalid payload '%s': must be one of diff, blocks, files", name, strings.TrimSpace(a.Payload))
		}
		// Reserved agentic-stage fields: validated at load (inert in 1.x).
		if !roleValid(a.Role) {
			return agentErrf(name, "agent '%s': role must be one of reviewer, skeptic, judge", name)
		}
		if a.MaxTurns != nil && (*a.MaxTurns <= 0 || *a.MaxTurns > MaxAgentTurns) {
			return agentErrf(name, "agent '%s': max_turns must be within 1..%d", name, MaxAgentTurns)
		}
		if a.ToolBudgetBytes != nil && *a.ToolBudgetBytes < 0 {
			return agentErrf(name, "agent '%s': tool_budget_bytes must be >= 0 (0 = unlimited)", name)
		}
	}
	return nil
}

// applyDefaults fills optional agent fields: persona defaults to the agent
// name and temperature to 0.7. TimeoutSecs and Payload intentionally stay
// unset (nil/empty) so agents inherit the resolved shared settings — see
// EffectiveTimeoutSecs and the precedence chain in ResolveSettings.
func (r *Registry) applyDefaults() {
	for name, a := range r.Agents {
		if a.Persona == "" {
			a.Persona = name
		}
		if a.Temperature == nil {
			temp := DefaultTemperature
			a.Temperature = &temp
		}
		r.Agents[name] = a
	}
}

// EffectiveTimeoutSecs returns the agent's own timeout when set, otherwise
// the resolved shared timeout.
func (a AgentConfig) EffectiveTimeoutSecs(s Settings) int {
	if a.TimeoutSecs != nil {
		return *a.TimeoutSecs
	}
	return s.TimeoutSecs
}

// EffectivePayloadMode returns the agent's own payload override when set,
// otherwise the resolved shared payload mode. (Enum validation of payload
// values is the payload-configuration stage's concern.)
func (a AgentConfig) EffectivePayloadMode(s Settings) string {
	if v := strings.TrimSpace(a.Payload); v != "" {
		return v
	}
	return s.PayloadMode
}
