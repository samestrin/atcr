package registry

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/stream"
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
	// MaxFindingsCap is the ceiling for per-agent max_findings; consistent with
	// MaxTimeoutSecs/MaxAgentTurns/MaxToolBudgetBytes which each have documented
	// upper bounds. nil = unlimited (unset); any explicit value must be within
	// 1..MaxFindingsCap.
	MaxFindingsCap = 10000
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

// Verification defaults (Epic 3.0). DefaultVerifyMinSeverity is the floor below
// which findings skip adversarial verification and keep their v1 confidence;
// DefaultVerifyVotes is the number of skeptics consulted per finding (1; the
// --thorough flag forces 3 with majority rule at the orchestration layer).
const (
	DefaultVerifyMinSeverity = "MEDIUM"
	DefaultVerifyVotes       = 1
)

// VerifyConfig is the optional registry-level adversarial-verification block
// (Epic 3.0). It is backward-compatible: an absent block, or a present block with
// unset fields, resolves to the defaults (min_severity=MEDIUM, votes=1) at load.
// MinSeverity is normalized to canonical upper-case and validated against the
// review severity rubric at load so the verify stage compares a stable token.
type VerifyConfig struct {
	MinSeverity string `yaml:"min_severity,omitempty"` // floor: LOW|MEDIUM|HIGH|CRITICAL (default MEDIUM)
	Votes       int    `yaml:"votes,omitempty"`        // skeptics per finding (default 1)
	MaxParallel int    `yaml:"max_parallel,omitempty"` // bounded worker pool cap (0 = default 4)
}

// AgentConfig binds a provider+model to a reviewer persona. Temperature and
// TimeoutSecs are pointers so an explicit zero survives default application.
//
// Tools, MaxTurns, ToolBudgetBytes, and SupportsFC are ACTIVE in Epic 2.0: the
// engine drives the multi-turn tool loop from them and applyDefaults sets
// max_turns=10 when tools=true. They were reserved (parsed + validated but
// inert) in 1.1/1.x; a 1.x config that set them keeps loading and the values now
// take effect. Role is still reserved for the agentic stages (Epics 3.0/4.0) —
// parsed and validated, but no code path acts on it yet. MaxTurns and
// ToolBudgetBytes are pointers so an explicit value is distinguishable from unset
// (the same reason TimeoutSecs is a pointer). Tools is a value bool because its
// default is false and nothing needs to distinguish "explicitly false" from
// "unset". Defaults (tools=false, max_turns=10, tool_budget_bytes=0/unlimited,
// supports_function_calling=false, role=reviewer) are documented in
// docs/registry.md.
type AgentConfig struct {
	Provider    string   `yaml:"provider"`
	Model       string   `yaml:"model"`
	Persona     string   `yaml:"persona,omitempty"`
	Temperature *float64 `yaml:"temperature,omitempty"`
	TimeoutSecs *int     `yaml:"timeout_secs,omitempty"`
	RateLimited bool     `yaml:"rate_limited,omitempty"`
	Fallback    string   `yaml:"fallback,omitempty"`
	Payload     string   `yaml:"payload,omitempty"`

	// Active in Epic 2.0 — the engine acts on these (tool loop + budgets).
	Tools           bool   `yaml:"tools"`             // enables the multi-turn tool loop
	MaxTurns        *int   `yaml:"max_turns"`         // agent-loop turn cap (default 10 when tools=true)
	ToolBudgetBytes *int64 `yaml:"tool_budget_bytes"` // cumulative tool-result budget (0 = unlimited, matches PayloadByteBudget)
	// Reserved for the agentic stages — parsed + validated, inert in 2.0.
	Role string `yaml:"role"` // Stage 3/4 — reviewer | skeptic | judge

	// SupportsFC declares whether this agent's model supports OpenAI-style
	// function calling. Active in Epic 2.0 (Phase 4): the engine consults it
	// before starting a tool loop. Default false (a value bool — no stage needs
	// to distinguish "explicitly false" from "unset"), so a model is assumed
	// non-tool-capable unless explicitly declared, and a tools:true agent on an
	// undeclared model degrades safely to single-shot.
	SupportsFC bool `yaml:"supports_function_calling"` // Stage 2 — model function-calling capability

	// Review-constraint guardrails (Epic 2.2). All optional and
	// backward-compatible: an unset field imposes no constraint, so a 1.x/2.0
	// config keeps loading unchanged. Scope is a SOFT prompt-injection focus hint
	// (categories the agent should prioritize) — injected into the persona prompt
	// by the fan-out, it never hard-drops findings. MinSeverity drops findings
	// below the floor and MaxFindings truncates (severity-sorted) the agent's
	// findings to a hard cap; both are enforced deterministically in the fan-out
	// per-source path (internal/fanout), never in the reconciler. MinSeverity is
	// normalized to canonical upper-case at load so enforcement comparisons are
	// stable. MaxFindings is a pointer so an absent cap (nil = unlimited) is
	// distinguishable from any explicit value.
	Scope       []string `yaml:"scope,omitempty"`        // soft focus categories injected into the prompt
	MinSeverity string   `yaml:"min_severity,omitempty"` // drop findings below this floor (CRITICAL|HIGH|MEDIUM|LOW)
	MaxFindings *int     `yaml:"max_findings,omitempty"` // cap on findings (severity-sorted truncate); nil = unlimited

	// Retry/backoff tunables (Epic 4.6) — the per-agent tier, overriding the
	// resolved global settings via EffectiveMaxRetries/EffectiveInitialBackoffMs.
	// Pointers so an explicit 0 max_retries survives and an unset field inherits
	// the resolved setting (same shape as TimeoutSecs).
	MaxRetries       *int `yaml:"max_retries,omitempty"`        // per-agent retry budget (0 = single attempt); nil = inherit
	InitialBackoffMs *int `yaml:"initial_backoff_ms,omitempty"` // per-agent base retry delay (ms); nil = inherit
}

// reviewSeverities is the canonical finding-severity rubric (personas/_base.md),
// used to validate min_severity. Kept as a set here so the registry validates
// without depending on the fan-out or reconcile packages.
var reviewSeverities = map[string]bool{"CRITICAL": true, "HIGH": true, "MEDIUM": true, "LOW": true}

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

	// Retry/backoff tunables (Epic 4.6) — the user-level (global) tier of the
	// precedence chain, mirroring TimeoutSecs. Pointers so an explicit 0
	// max_retries (single attempt, no retry) survives default application in
	// ResolveSettings. An unset value falls through to the embedded default.
	MaxRetries       *int `yaml:"max_retries,omitempty"`
	InitialBackoffMs *int `yaml:"initial_backoff_ms,omitempty"`

	// Verify is the optional adversarial-verification block (Epic 3.0). Defaults
	// (min_severity=MEDIUM, votes=1) are applied at load, so a registry without a
	// verify block still yields the resolved defaults.
	Verify VerifyConfig `yaml:"verify,omitempty"`

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
	// Staged intentionally: validate() runs before ValidateFallbacks() and an early
	// return short-circuits on structural faults. Epic 4.2 AC6 accumulation is scoped
	// to within each check, not across this boundary — fallback-chain checks assume
	// structurally-valid agents, so running them against a malformed registry would
	// surface misleading errors. The user fixes structural faults first, then re-runs.
	if err := reg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", base, err)
	}
	if err := reg.ValidateFallbacks(); err != nil {
		return nil, fmt.Errorf("%s: %w", base, err)
	}
	reg.applyDefaults()
	return reg, nil
}

// validate checks required fields and reference integrity. It accumulates every
// fault and reports them together via errors.Join (Epic 4.2 / AC6) rather than
// short-circuiting on the first, so a user fixes all config mistakes in one
// edit. Providers and agents are walked in sorted-name order so the joined
// message is deterministic despite randomized map iteration. errors.Join
// returns nil when no faults were collected, preserving the valid-config path.
func (r *Registry) validate() error {
	var errs []error

	// Settings-level checks, in fixed source order.
	if r.TimeoutSecs != nil && (*r.TimeoutSecs <= 0 || *r.TimeoutSecs > MaxTimeoutSecs) {
		errs = append(errs, fmt.Errorf("timeout_secs must be within 1..%d", MaxTimeoutSecs))
	}
	if r.PayloadByteBudget != nil && *r.PayloadByteBudget < 0 {
		errs = append(errs, fmt.Errorf("payload_byte_budget must be >= 0 (0 = unlimited), got %d", *r.PayloadByteBudget))
	}
	if r.MaxParallel != nil && *r.MaxParallel < 0 {
		errs = append(errs, fmt.Errorf("max_parallel must be >= 0 (0 = unbounded), got %d", *r.MaxParallel))
	}
	// Retry tunables (Epic 4.6): 0 retries is valid (single attempt); the base
	// delay must be positive so the exponential schedule has a starting point.
	if r.MaxRetries != nil && (*r.MaxRetries < 0 || *r.MaxRetries > MaxRetriesCap) {
		errs = append(errs, fmt.Errorf("max_retries must be within 0..%d", MaxRetriesCap))
	}
	if r.InitialBackoffMs != nil && (*r.InitialBackoffMs <= 0 || *r.InitialBackoffMs > MaxInitialBackoffMs) {
		errs = append(errs, fmt.Errorf("initial_backoff_ms must be within 1..%d", MaxInitialBackoffMs))
	}
	if !payloadModeValid(r.PayloadMode) {
		errs = append(errs, fmt.Errorf("invalid payload_mode '%s': must be one of diff, blocks, files", r.PayloadMode))
	}
	// verify.min_severity (Epic 3.0): an empty value defaults to MEDIUM at load;
	// any non-empty value must be a canonical review severity. Error wording lists
	// the levels low→high so a typo (e.g. "BLOCKER") is corrected quickly.
	if normalized := stream.NormalizeSeverity(r.Verify.MinSeverity); normalized != "" && !reviewSeverities[normalized] {
		errs = append(errs, fmt.Errorf("invalid verify.min_severity %q: must be LOW, MEDIUM, HIGH, or CRITICAL", r.Verify.MinSeverity))
	}
	if r.Verify.Votes < 0 {
		errs = append(errs, fmt.Errorf("verify.votes must be >= 0 (0 = default), got %d", r.Verify.Votes))
	}
	if r.Verify.MaxParallel < 0 {
		errs = append(errs, fmt.Errorf("verify.max_parallel must be >= 0 (0 = default 4), got %d", r.Verify.MaxParallel))
	}

	for _, name := range sortedKeys(r.Providers) {
		errs = append(errs, validateProvider(name, r.Providers[name])...)
	}
	for _, name := range sortedKeys(r.Agents) {
		errs = append(errs, r.validateAgent(name, r.Agents[name])...)
	}

	return errors.Join(errs...)
}

// validateProvider returns every fault found in a single provider entry (Epic
// 4.2 / AC6 — accumulate rather than short-circuit).
func validateProvider(name string, p Provider) []error {
	var errs []error
	if strings.TrimSpace(name) == "" {
		errs = append(errs, providerErrf(name, "providers.%s: provider name must not be empty", name))
	}
	if p.APIKeyEnv == "" {
		errs = append(errs, providerErrf(name, "providers.%s: required field 'api_key_env' is missing", name))
	} else if !envVarName.MatchString(p.APIKeyEnv) {
		errs = append(errs, providerErrf(name, "providers.%s: api_key_env %q is not a valid environment variable name", name, p.APIKeyEnv))
	}
	if p.BaseURL != "" {
		u, err := url.Parse(p.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, providerErrf(name, "providers.%s: base_url must be a valid http or https URL", name))
		} else if u.User != nil {
			errs = append(errs, providerErrf(name, "providers.%s: base_url must not embed credentials (userinfo)", name))
		}
	}
	return errs
}

// validateAgent returns every fault found in a single agent entry (Epic 4.2 /
// AC6 — accumulate rather than short-circuit). The unknown-provider reference
// check is suppressed when provider is empty so a missing-provider agent reports
// only the "required field" fault, not a spurious "references unknown provider "".
func (r *Registry) validateAgent(name string, a AgentConfig) []error {
	var errs []error
	if strings.TrimSpace(name) == "" {
		errs = append(errs, agentErrf(name, "agent '%s': agent name must not be empty", name))
	}
	if a.Provider == "" {
		errs = append(errs, agentErrf(name, "agent '%s': required field 'provider' is missing", name))
	} else if _, ok := r.Providers[a.Provider]; !ok {
		errs = append(errs, agentErrf(name, "agent '%s' references unknown provider '%s'", name, a.Provider))
	}
	if a.Model == "" {
		errs = append(errs, agentErrf(name, "agent '%s': required field 'model' is missing", name))
	}
	if a.TimeoutSecs != nil && (*a.TimeoutSecs <= 0 || *a.TimeoutSecs > MaxTimeoutSecs) {
		errs = append(errs, agentErrf(name, "agent '%s': timeout_secs must be within 1..%d", name, MaxTimeoutSecs))
	}
	if a.Temperature != nil && (*a.Temperature < 0 || *a.Temperature > 2) {
		errs = append(errs, agentErrf(name, "agent '%s': temperature must be within [0, 2]", name))
	}
	if !payloadModeValid(a.Payload) {
		errs = append(errs, agentErrf(name, "agent '%s': invalid payload '%s': must be one of diff, blocks, files", name, a.Payload))
	}
	// role is still reserved (Stage 3/4) but validated at load; max_turns and
	// tool_budget_bytes are active in 2.0 and bound the tool loop.
	if !roleValid(a.Role) {
		errs = append(errs, agentErrf(name, "agent '%s': role must be one of reviewer, skeptic, judge", name))
	}
	if a.MaxTurns != nil && (*a.MaxTurns <= 0 || *a.MaxTurns > MaxAgentTurns) {
		errs = append(errs, agentErrf(name, "agent '%s': max_turns must be within 1..%d", name, MaxAgentTurns))
	}
	if a.ToolBudgetBytes != nil && (*a.ToolBudgetBytes < 0 || *a.ToolBudgetBytes > MaxToolBudgetBytes) {
		errs = append(errs, agentErrf(name, "agent '%s': tool_budget_bytes must be within 0..%d (0 = unlimited)", name, MaxToolBudgetBytes))
	}
	// Review-constraint guardrails (Epic 2.2). All optional; an unset field is
	// not validated. min_severity is checked case-insensitively against the
	// rubric, max_findings must be a positive cap, and every scope entry must
	// be a non-empty category (a blank entry is a YAML typo, not "all").
	if normalized := stream.NormalizeSeverity(a.MinSeverity); normalized != "" && !reviewSeverities[normalized] {
		errs = append(errs, agentErrf(name, "agent '%s': min_severity must be one of CRITICAL, HIGH, MEDIUM, LOW", name))
	}
	if a.MaxFindings != nil && (*a.MaxFindings <= 0 || *a.MaxFindings > MaxFindingsCap) {
		errs = append(errs, agentErrf(name, "agent '%s': max_findings must be within 1..%d", name, MaxFindingsCap))
	}
	// Retry tunables (Epic 4.6): 0 retries is valid (single attempt); the base
	// delay must be positive. Same range as the registry tier.
	if a.MaxRetries != nil && (*a.MaxRetries < 0 || *a.MaxRetries > MaxRetriesCap) {
		errs = append(errs, agentErrf(name, "agent '%s': max_retries must be within 0..%d", name, MaxRetriesCap))
	}
	if a.InitialBackoffMs != nil && (*a.InitialBackoffMs <= 0 || *a.InitialBackoffMs > MaxInitialBackoffMs) {
		errs = append(errs, agentErrf(name, "agent '%s': initial_backoff_ms must be within 1..%d", name, MaxInitialBackoffMs))
	}
	for _, s := range a.Scope {
		if strings.TrimSpace(s) == "" {
			errs = append(errs, agentErrf(name, "agent '%s': scope entries must not be empty", name))
		} else if strings.IndexFunc(s, func(r rune) bool { return r < 32 }) >= 0 {
			errs = append(errs, agentErrf(name, "agent '%s': scope entries must not contain control characters", name))
		}
	}
	return errs
}

// sortedKeys returns a map's string keys in ascending order, so validation walks
// providers and agents deterministically regardless of Go's randomized map
// iteration.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
		// Tool-loop default (Epic 2.0): a tool-enabled agent with no explicit
		// max_turns gets DefaultMaxTurns so the engine loop is always bounded.
		// Non-tool agents keep MaxTurns unset (nil) — the field is inert for them.
		if a.Tools && a.MaxTurns == nil {
			mt := DefaultMaxTurns
			a.MaxTurns = &mt
		}
		// Canonicalize min_severity (Epic 2.2) so downstream enforcement compares
		// against a stable upper-case token regardless of how it was written.
		if a.MinSeverity != "" {
			a.MinSeverity = stream.NormalizeSeverity(a.MinSeverity)
		}
		// Canonicalize scope entries (Epic 2.2): trim whitespace so downstream
		// comparisons (ScopeFocus rendering, prompt injection) use stable tokens.
		for i, s := range a.Scope {
			a.Scope[i] = strings.TrimSpace(s)
		}
		r.Agents[name] = a
	}
	// Verification defaults (Epic 3.0): an unset min_severity resolves to MEDIUM,
	// an unset (or zero) votes to 1; a set min_severity is canonicalized so the
	// verify stage compares a stable upper-case token. Validation already rejected
	// any non-canonical value, so stream.NormalizeSeverity here only fixes casing.
	if r.Verify.MinSeverity == "" {
		r.Verify.MinSeverity = DefaultVerifyMinSeverity
	} else {
		r.Verify.MinSeverity = stream.NormalizeSeverity(r.Verify.MinSeverity)
	}
	if r.Verify.Votes == 0 {
		r.Verify.Votes = DefaultVerifyVotes
	}
}

// AgentsByRole returns the agents whose effective role matches role, keyed by
// agent name. An empty Role is normalized to RoleReviewer for the comparison
// only (backward compatibility for 1.x configs); the underlying AgentConfig is
// never mutated, preserving the loader's "explicitly set vs inherited default"
// distinction (option-a decision, see roleValid). The result is always a
// non-nil map — empty when nothing matches, the registry is empty, or the
// receiver is nil. An unknown role simply matches nothing.
//
// Read-only contract: callers must not mutate the returned AgentConfig values.
// Reference fields (Scope, pointer fields) alias the registry's backing memory;
// mutating them corrupts the shared registry for the lifetime of the process.
func (r *Registry) AgentsByRole(role string) map[string]AgentConfig {
	out := make(map[string]AgentConfig)
	if r == nil {
		return out
	}
	for name, a := range r.Agents {
		effective := a.Role
		if effective == "" {
			effective = RoleReviewer
		}
		if effective == role {
			out[name] = a
		}
	}
	return out
}

// EffectiveTimeoutSecs returns the agent's own timeout when set, otherwise
// the resolved shared timeout.
func (a AgentConfig) EffectiveTimeoutSecs(s Settings) int {
	if a.TimeoutSecs != nil {
		return *a.TimeoutSecs
	}
	return s.TimeoutSecs
}

// EffectiveMaxRetries returns the agent's own retry budget when set, otherwise
// the resolved shared budget (Epic 4.6). An explicit 0 (single attempt, no
// retry) is honored — the pointer distinguishes it from "unset".
func (a AgentConfig) EffectiveMaxRetries(s Settings) int {
	if a.MaxRetries != nil {
		return *a.MaxRetries
	}
	return s.MaxRetries
}

// EffectiveInitialBackoffMs returns the agent's own base retry delay (ms) when
// set, otherwise the resolved shared delay (Epic 4.6).
func (a AgentConfig) EffectiveInitialBackoffMs(s Settings) int {
	if a.InitialBackoffMs != nil {
		return *a.InitialBackoffMs
	}
	return s.InitialBackoffMs
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
