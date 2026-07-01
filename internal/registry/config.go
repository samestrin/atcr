package registry

import (
	"errors"
	"fmt"
	reclib "github.com/samestrin/atcr/reconcile"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
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
	// RoleExecutor names the optional single fix-generation model (Epic 7.0). It
	// is NOT a reviewer-panel role: an executor lives in the top-level executor:
	// block, not in agents:, so roleValid (which gates agent roles) intentionally
	// excludes it. It is validated only against ExecutorConfig.Role.
	RoleExecutor = "executor"
)

// Executor defaults (Epic 7.0). DefaultExecutorPersona is the fix-focused persona
// applied when the executor block sets none; DefaultFixMinSeverity is the severity
// floor below which a verified finding gets no generated fix (the executor's
// min_severity_for_fix; epic Open-Q4 Option B).
const (
	DefaultExecutorPersona = "fixer"
	DefaultFixMinSeverity  = "MEDIUM"
	// MaxExecutorPersonaLen caps the executor persona length. The persona is
	// interpolated verbatim into the fix-generation prompt, so an over-long value
	// is bounded at load to limit prompt-stuffing by untrusted free text.
	MaxExecutorPersonaLen = 512
	// MaxExecutorSystemPromptLen caps the executor system_prompt override (Epic
	// 7.0.1). The override replaces the default framing verbatim, so it is bounded
	// at load to limit prompt-stuffing. It is larger than the persona cap because it
	// is a full instruction block, not a single token interpolated mid-sentence.
	MaxExecutorSystemPromptLen = 4096
	// MaxExecutorRuleLen caps each executor coding rule (Epic 7.0.1). Each rule is
	// interpolated verbatim into the fix prompt as a constraint line, so it is
	// bounded like the persona to limit prompt-stuffing by untrusted free text.
	MaxExecutorRuleLen = 512
	// MaxExecutorRules caps the number of executor coding rules (Epic 7.0.1). Too
	// many short rules can still stuff the fix prompt, so the count is bounded at
	// load mirroring the per-rule cap.
	MaxExecutorRules = 64
	// DefaultExecutorMaxToolCalls is the agent-mode (Epic 7.4) tool-call budget
	// applied when max_tool_calls is unset or non-positive. It bounds the executor's
	// read/search loop per finding; 10 is conservative — the executor's task (read
	// the cited code, then propose one fix) is narrower than a skeptic's.
	DefaultExecutorMaxToolCalls = 10
	// MaxExecutorToolCalls caps an explicitly-configured max_tool_calls. It is pinned
	// to MaxAgentTurns because invokeExecutor drives the same fanout tool loop as a
	// skeptic (max_tool_calls maps to the agent's MaxTurns budget), whose per-agent
	// turn budget the engine already caps at MaxAgentTurns — so the executor cannot
	// exceed the loop's hard ceiling either.
	MaxExecutorToolCalls = MaxAgentTurns
)

// Verification defaults (Epic 3.0). DefaultVerifyMinSeverity is the floor below
// which findings skip adversarial verification and keep their v1 confidence;
// DefaultVerifyVotes is the number of skeptics consulted per finding (1; the
// --thorough flag forces 3 with majority rule at the orchestration layer).
const (
	DefaultVerifyMinSeverity = "MEDIUM"
	DefaultVerifyVotes       = 1
)

// Debate trigger kinds (Epic 6.0 — Cross-Examination). They mirror the reconcile
// disagreement kinds (reconcile.Kind*) by value so a debate.triggers entry names
// the same tension classes the radar surfaces. They are declared here (not
// imported from reconcile) to keep the registry decoupled from reconcile; a
// drift-guard test in internal/debate asserts the two stay byte-equal.
const (
	DebateTriggerSeveritySplit            = "severity_split"
	DebateTriggerGrayZone                 = "gray_zone"
	DebateTriggerVerificationDisagreement = "verification_disagreement"
)

// DefaultDebateMaxItems is the cost cap applied when debate.max_items is unset
// (nil). An explicit 0 means unlimited; an explicit N>0 caps debate to the N
// highest-priority disputed items, with the remainder recorded as overflow.
const DefaultDebateMaxItems = 5

// DefaultDebateTriggers returns the debate trigger kinds enabled when a config
// sets none — the single source of truth for the default-enabled set. Both the
// load-time resolver (applyDefaults) and the stage-time resolver
// (internal/debate.ResolveConfig) reference it, so the two paths can never drift to
// different enabled sets when a trigger is added. A fresh slice is returned per call
// so callers may store it without aliasing shared state.
func DefaultDebateTriggers() []string {
	return []string{
		DebateTriggerSeveritySplit,
		DebateTriggerGrayZone,
		DebateTriggerVerificationDisagreement,
	}
}

// DebateConfig is the optional registry-level cross-examination block (Epic 6.0).
// It is backward-compatible: an absent block, or a present block with unset
// fields, resolves to the defaults (all three triggers on, max_items=5,
// allow_single_model=false) at the debate stage. MaxItems is a pointer so an
// explicit 0 (unlimited) is distinguishable from unset (nil → DefaultDebateMaxItems),
// mirroring MaxFindings/MaxParallel. AllowSingleModel opts in to the same-model
// persona fallback when fewer than three distinct models are available across the
// proposer/challenger/judge roles; the default (false) skips such items and records
// them as unresolved rather than silently loosening the independence requirement.
type DebateConfig struct {
	Triggers         []string `yaml:"triggers,omitempty"`           // default: all three kinds
	MaxItems         *int     `yaml:"max_items,omitempty"`          // nil = default 5; 0 = unlimited; N>0 = cap
	AllowSingleModel bool     `yaml:"allow_single_model,omitempty"` // default false (skip + record unresolved)
	MaxParallel      int      `yaml:"max_parallel,omitempty"`       // bounded worker pool cap (0 = default 4)
}

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

// ExecutorConfig is the optional top-level fix-generation model (Epic 7.0). It is
// backward-compatible: an absent executor: block leaves Registry.Executor nil and
// ATCR behaves exactly as before (no fix generation). The executor is a SINGLE
// model run exclusively in the fix phase — it is not part of the review panel, so
// it lives outside agents: and carries its own role (always "executor"). Provider
// references a key in providers:; Model is the model id; Persona defaults to
// "fixer". MinSeverity (yaml: min_severity_for_fix) is the severity floor a
// verified finding must meet to receive a fix (default MEDIUM, normalized to
// canonical upper-case at load). TimeoutSecs (yaml: fix_timeout) is a pointer so
// an explicit value is distinguishable from unset.
//
// Fix-generation tunables (Epic 7.0.1): Temperature, SystemPrompt, and Rules let
// the user control fix style/determinism without editing ATCR source. Temperature
// is a pointer so an explicit 0.0 survives load; its 0.0 default (deterministic
// fixes) is resolved at call time via EffectiveExecutorTemperature, not mutated at
// load. SystemPrompt, when set, replaces the default fix-prompt framing verbatim
// (persona is superseded for that call). Rules are coding guidelines appended to
// the fix prompt as constraints. All three are optional and backward-compatible.
type ExecutorConfig struct {
	Name         string   `yaml:"name,omitempty"`
	Provider     string   `yaml:"provider"`
	Model        string   `yaml:"model"`
	Persona      string   `yaml:"persona,omitempty"`
	Role         string   `yaml:"role,omitempty"`                 // must be "executor" if set; defaults to executor
	MinSeverity  string   `yaml:"min_severity_for_fix,omitempty"` // fix floor: LOW|MEDIUM|HIGH|CRITICAL (default MEDIUM)
	TimeoutSecs  *int     `yaml:"fix_timeout,omitempty"`          // per-fix timeout (secs); nil = inherit shared timeout
	Temperature  *float64 `yaml:"temperature,omitempty"`          // API temperature [0,2]; nil = deterministic 0.0 default
	SystemPrompt string   `yaml:"system_prompt,omitempty"`        // full fix-prompt framing override; supersedes persona when set
	Rules        []string `yaml:"rules,omitempty"`                // coding guidelines appended to the fix prompt as constraints
	// Agent-mode tool loop (Epic 7.4). AgentMode opts the executor into a read-only
	// tool loop (reusing the skeptics' dispatcher) so it can read files / search the
	// codebase before proposing a fix; default false = the Epic 7.0 snippet path,
	// unchanged. MaxToolCalls bounds that loop per finding (maps to the fanout agent's
	// MaxTurns budget); it is a pointer so an explicit value is distinguishable from
	// unset (nil → DefaultExecutorMaxToolCalls), mirroring AgentConfig.MaxTurns.
	AgentMode    bool `yaml:"agent_mode,omitempty"`     // Epic 7.4: opt-in tool-loop fix generation (default false)
	MaxToolCalls *int `yaml:"max_tool_calls,omitempty"` // Epic 7.4: agent-mode tool-call budget; nil = default 10
}

// EffectiveMaxToolCalls returns the agent-mode tool-call budget: the executor's own
// max_tool_calls when set positive, otherwise DefaultExecutorMaxToolCalls (10). It
// is the single resolver invokeExecutor uses so the nil/non-positive → 10 fallback
// lives in one place, mirroring EffectiveFixMinSeverity / EffectiveExecutorTimeoutSecs.
func (e ExecutorConfig) EffectiveMaxToolCalls() int {
	if e.MaxToolCalls != nil && *e.MaxToolCalls > 0 {
		return *e.MaxToolCalls
	}
	return DefaultExecutorMaxToolCalls
}

// EffectiveFixMinSeverity returns the executor's own min_severity_for_fix when
// set, otherwise the MEDIUM default — mirroring EffectiveTimeoutSecs. On a
// loaded registry applyDefaults already canonicalizes MinSeverity, so this only
// supplies the fallback for in-memory ExecutorConfig values (e.g. test structs).
// It is the single resolver generateFixes and the verify snapshot pre-check
// share so the empty-check + DefaultFixMinSeverity fallback lives in one place.
func (e ExecutorConfig) EffectiveFixMinSeverity() string {
	if e.MinSeverity == "" {
		return DefaultFixMinSeverity
	}
	return e.MinSeverity
}

// EffectiveExecutorTimeoutSecs returns the per-fix call deadline in seconds: the
// executor's own fix_timeout when set, otherwise the resolved shared verify
// timeout, falling back to DefaultTimeoutSecs (600s) when neither is positive.
// Mirroring AgentConfig.EffectiveTimeoutSecs, it lets callExecutor apply a
// deadline unconditionally so a default executor (nil fix_timeout) against a hung
// provider cannot block the verify run unbounded.
func (e ExecutorConfig) EffectiveExecutorTimeoutSecs(s Settings) int {
	if e.TimeoutSecs != nil && *e.TimeoutSecs > 0 {
		return *e.TimeoutSecs
	}
	if s.TimeoutSecs > 0 {
		return s.TimeoutSecs
	}
	return DefaultTimeoutSecs
}

// EffectiveExecutorTemperature returns the API temperature for the executor's fix
// calls: the executor's own temperature when set, otherwise the deterministic 0.0
// default (Epic 7.0.1). The pointer distinguishes an explicit 0.0 from unset, but
// both resolve to 0.0 here — the executor sends a temperature on every call rather
// than inheriting the provider's own (often non-deterministic) default. It mirrors
// EffectiveFixMinSeverity / EffectiveExecutorTimeoutSecs: the single resolver so the
// nil → 0.0 fallback lives in one place.
func (e ExecutorConfig) EffectiveExecutorTemperature() float64 {
	if e.Temperature != nil {
		return *e.Temperature
	}
	return 0.0
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

	// Language declares the file extensions this agent specializes in, enabling
	// language-aware skeptic routing (Epic 9.0): when a finding's file extension
	// matches one of these, the agent is preferred over an unscoped skeptic in
	// SelectEligibleSkeptics. Optional and backward-compatible — nil/empty means
	// no constraint, so a pre-9.0 registry loads unchanged. Entries are
	// canonicalized at load (trim → strip a single leading dot → lowercase) via
	// NormalizeLanguageToken so "go"/".go"/" .GO " all store as "go" and compare
	// against a finding's normalized extension in one form. validateAgent rejects
	// empty entries and control characters (mirroring the Scope guard); it does
	// NOT enforce a known-language allow-list, so third-party persona authors stay
	// free to declare any extension.
	Language []string `yaml:"language,omitempty"` // file extensions this agent specializes in (routing)

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
//
// Role names are matched case-insensitively (mirroring the severity rubric,
// which normalizes case via reclib.NormalizeSeverity): the value is lower-cased
// and trimmed before comparison, and applyDefaults stores the canonical
// lowercase form so downstream exact-match comparisons stay valid.
func roleValid(r string) bool {
	switch strings.ToLower(strings.TrimSpace(r)) {
	case "", RoleReviewer, RoleSkeptic, RoleJudge:
		return true
	default:
		return false
	}
}

// debateTriggerValid reports whether t names a known debate trigger kind (Epic
// 6.0). The empty string is rejected — a blank triggers entry is a YAML typo, not
// "all" (the all-triggers default applies only to an absent/empty list).
func debateTriggerValid(t string) bool {
	switch t {
	case DebateTriggerSeveritySplit, DebateTriggerGrayZone, DebateTriggerVerificationDisagreement:
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
	// ReviewStrategy is the run-wide fan-out strategy (Epic 14.3): "bulk"
	// (default) or "chunked". A global toggle resolved once per run, mirroring
	// payload_mode; the per-agent max_context_lines governs each bin's size when
	// chunking is on.
	ReviewStrategy string `yaml:"review_strategy,omitempty"`
	// MaxParallel is a pointer so an explicit 0 (unbounded) survives default
	// application in ResolveSettings.
	MaxParallel *int `yaml:"max_parallel,omitempty"`
	// CacheMaxBytes is the user-level (global) tier of the diff-cache size cap
	// (Epic 5.2); a pointer so an explicit 0 (unbounded) survives default
	// application. The project tier overrides it; unset falls through to the
	// embedded DefaultCacheMaxBytes.
	CacheMaxBytes *int64 `yaml:"cache_max_bytes,omitempty"`

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

	// Debate is the optional cross-examination block (Epic 6.0). Triggers are
	// defaulted to all three kinds at load; max_items and allow_single_model are
	// resolved at the debate stage (see internal/debate.ResolveConfig). A registry
	// without a debate block still yields the resolved defaults.
	Debate DebateConfig `yaml:"debate,omitempty"`

	// Executor is the optional fix-generation model (Epic 7.0). A pointer so an
	// absent block (nil) — the backward-compatible default — is distinguishable
	// from a configured one; nil means no fix generation runs.
	Executor *ExecutorConfig `yaml:"executor,omitempty"`

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
	if r.CacheMaxBytes != nil && *r.CacheMaxBytes < 0 {
		errs = append(errs, fmt.Errorf("cache_max_bytes must be >= 0 (0 = unbounded), got %d", *r.CacheMaxBytes))
	}
	// Retry tunables (Epic 4.6): 0 retries is valid (single attempt); the base
	// delay must be positive so the exponential schedule has a starting point.
	for _, m := range validateRetryBounds(r.MaxRetries, r.InitialBackoffMs) {
		errs = append(errs, errors.New(m))
	}
	if !payloadModeValid(r.PayloadMode) {
		errs = append(errs, fmt.Errorf("invalid payload_mode '%s': must be one of diff, blocks, files", r.PayloadMode))
	}
	if !reviewStrategyValid(r.ReviewStrategy) {
		errs = append(errs, fmt.Errorf("invalid review_strategy '%s': must be one of bulk, chunked", r.ReviewStrategy))
	}
	// verify.min_severity (Epic 3.0): an empty value defaults to MEDIUM at load;
	// any non-empty value must be a canonical review severity. Error wording lists
	// the levels low→high so a typo (e.g. "BLOCKER") is corrected quickly.
	if normalized := reclib.NormalizeSeverity(r.Verify.MinSeverity); normalized != "" && !reviewSeverities[normalized] {
		errs = append(errs, fmt.Errorf("invalid verify.min_severity %q: must be LOW, MEDIUM, HIGH, or CRITICAL", r.Verify.MinSeverity))
	}
	if r.Verify.Votes < 0 {
		errs = append(errs, fmt.Errorf("verify.votes must be >= 0 (0 = default), got %d", r.Verify.Votes))
	}
	if r.Verify.MaxParallel < 0 {
		errs = append(errs, fmt.Errorf("verify.max_parallel must be >= 0 (0 = default 4), got %d", r.Verify.MaxParallel))
	}
	// debate.* (Epic 6.0): every trigger must name a known disagreement kind, and
	// max_items must be non-negative (0 = unlimited). Defaults (all three triggers)
	// are applied at load in applyDefaults; an explicit max_items stays as written
	// so 0/unlimited is distinguishable from unset.
	for _, t := range r.Debate.Triggers {
		if !debateTriggerValid(t) {
			errs = append(errs, fmt.Errorf("invalid debate.triggers entry %q: must be one of severity_split, gray_zone, verification_disagreement", t))
		}
	}
	if r.Debate.MaxItems != nil && *r.Debate.MaxItems < 0 {
		errs = append(errs, fmt.Errorf("debate.max_items must be >= 0 (0 = unlimited), got %d", *r.Debate.MaxItems))
	}
	if r.Debate.MaxParallel < 0 {
		errs = append(errs, fmt.Errorf("debate.max_parallel must be >= 0 (0 = default 4), got %d", r.Debate.MaxParallel))
	}

	for _, name := range sortedKeys(r.Providers) {
		errs = append(errs, validateProvider(name, r.Providers[name])...)
	}
	for _, name := range sortedKeys(r.Agents) {
		errs = append(errs, r.validateAgent(name, r.Agents[name])...)
	}
	errs = append(errs, r.validateExecutor()...)

	return errors.Join(errs...)
}

// validateExecutor returns every fault found in the optional executor block (Epic
// 7.0). A nil block (no fix generation) is valid and yields no faults. Like the
// agent checks it accumulates rather than short-circuits (Epic 4.2 / AC6): the
// provider must be present and reference a defined provider, the model is
// required, role (if set) must be "executor", min_severity_for_fix (if set) must
// be a canonical review severity, and fix_timeout must be within bounds.
func (r *Registry) validateExecutor() []error {
	e := r.Executor
	if e == nil {
		return nil
	}
	var errs []error
	if strings.TrimSpace(e.Provider) == "" {
		errs = append(errs, errors.New("executor: required field 'provider' is missing"))
	} else if _, ok := r.Providers[e.Provider]; !ok {
		errs = append(errs, fmt.Errorf("executor references unknown provider '%s'", e.Provider))
	}
	if strings.TrimSpace(e.Model) == "" {
		errs = append(errs, errors.New("executor: required field 'model' is missing"))
	}
	if normalized := strings.ToLower(strings.TrimSpace(e.Role)); normalized != "" && normalized != RoleExecutor {
		errs = append(errs, fmt.Errorf("executor: role must be 'executor', got '%s'", e.Role))
	}
	if normalized := reclib.NormalizeSeverity(e.MinSeverity); normalized != "" && !reviewSeverities[normalized] {
		errs = append(errs, fmt.Errorf("executor: min_severity_for_fix must be one of CRITICAL, HIGH, MEDIUM, LOW, got %q", e.MinSeverity))
	}
	// The persona is interpolated verbatim into the fix-generation prompt
	// (buildFixPrompt), so an untrusted CR/LF or other control character could
	// forge prompt lines / redefine the model's role (prompt injection). Reject
	// control characters and cap the length at load, mirroring the Scope guard.
	if strings.IndexFunc(e.Persona, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }) >= 0 {
		errs = append(errs, errors.New("executor: persona must not contain control characters"))
	}
	if len(e.Persona) > MaxExecutorPersonaLen {
		errs = append(errs, fmt.Errorf("executor: persona must be at most %d characters", MaxExecutorPersonaLen))
	}
	// The name is interpolated into the "fix by <name>" attribution appended to the
	// free-text Evidence column, joined with the "; " separator. Reject control
	// characters (which could forge attribution/prompt lines) and the "; " separator
	// (which would forge phantom attribution segments), mirroring the persona guard.
	if strings.IndexFunc(e.Name, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }) >= 0 {
		errs = append(errs, errors.New("executor: name must not contain control characters"))
	}
	if strings.Contains(e.Name, "; ") {
		errs = append(errs, errors.New("executor: name must not contain the '; ' evidence separator"))
	}
	if e.TimeoutSecs != nil && (*e.TimeoutSecs <= 0 || *e.TimeoutSecs > MaxTimeoutSecs) {
		errs = append(errs, fmt.Errorf("executor: fix_timeout must be within 1..%d", MaxTimeoutSecs))
	}
	// Temperature (Epic 7.0.1): bounded to [0,2] like the agent guard. A pointer so
	// an explicit 0.0 (the deterministic default) is distinguishable from unset.
	if e.Temperature != nil && (*e.Temperature < 0 || *e.Temperature > 2 || math.IsNaN(*e.Temperature) || math.IsInf(*e.Temperature, 0)) {
		errs = append(errs, errors.New("executor: temperature must be within [0, 2]"))
	}
	// SystemPrompt (Epic 7.0.1) replaces the fix-prompt framing verbatim; cap its
	// length to bound prompt-stuffing. Control characters are intentionally NOT
	// rejected — unlike persona (a mid-sentence token), a system prompt is a full
	// instruction block where multi-line content is legitimate.
	if len(e.SystemPrompt) > MaxExecutorSystemPromptLen {
		errs = append(errs, fmt.Errorf("executor: system_prompt must be at most %d characters", MaxExecutorSystemPromptLen))
	}
	// Rules (Epic 7.0.1): each rule is interpolated as a constraint line in the fix
	// prompt. Reject blank entries (a YAML typo, not "no rule"), control characters
	// (CR/LF prompt-line forgery), over-long entries, and an excessive rule count —
	// mirroring the scope and persona guards.
	if len(e.Rules) > MaxExecutorRules {
		errs = append(errs, fmt.Errorf("executor: rules must be at most %d entries", MaxExecutorRules))
	}
	for i, rule := range e.Rules {
		switch {
		case strings.TrimSpace(rule) == "":
			errs = append(errs, fmt.Errorf("executor: rules[%d] must not be empty", i))
		case strings.IndexFunc(rule, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }) >= 0:
			errs = append(errs, fmt.Errorf("executor: rules[%d] must not contain control characters", i))
		case len(rule) > MaxExecutorRuleLen:
			errs = append(errs, fmt.Errorf("executor: rules[%d] must be at most %d characters", i, MaxExecutorRuleLen))
		}
	}
	// Agent-mode tool budget (Epic 7.4): an explicit max_tool_calls is bounded
	// 1..MaxExecutorToolCalls, mirroring max_turns (1..MaxAgentTurns) and max_findings
	// (1..MaxFindingsCap). The executor lives outside agents:, so validateExecutor is
	// its only validation gate — without this an explicit ≤0 or over-cap value would
	// reach the tool loop. A nil pointer (unset) is valid and resolves to the default.
	// agent_mode itself needs no guard: it is a field on this block, so it cannot be
	// set without an executor block (AC8 is satisfied-by-construction via the e == nil
	// early return above).
	if e.MaxToolCalls != nil && (*e.MaxToolCalls <= 0 || *e.MaxToolCalls > MaxExecutorToolCalls) {
		errs = append(errs, fmt.Errorf("executor: max_tool_calls must be within 1..%d", MaxExecutorToolCalls))
	}
	return errs
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
	if a.Temperature != nil && (*a.Temperature < 0 || *a.Temperature > 2 || math.IsNaN(*a.Temperature) || math.IsInf(*a.Temperature, 0)) {
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
	if normalized := reclib.NormalizeSeverity(a.MinSeverity); normalized != "" && !reviewSeverities[normalized] {
		errs = append(errs, agentErrf(name, "agent '%s': min_severity must be one of CRITICAL, HIGH, MEDIUM, LOW", name))
	}
	if a.MaxFindings != nil && (*a.MaxFindings <= 0 || *a.MaxFindings > MaxFindingsCap) {
		errs = append(errs, agentErrf(name, "agent '%s': max_findings must be within 1..%d", name, MaxFindingsCap))
	}
	// Retry tunables (Epic 4.6): 0 retries is valid (single attempt); the base
	// delay must be positive. Same range as the registry tier.
	for _, m := range validateRetryBounds(a.MaxRetries, a.InitialBackoffMs) {
		errs = append(errs, agentErrf(name, "agent '%s': %s", name, m))
	}
	for _, s := range a.Scope {
		if strings.TrimSpace(s) == "" {
			errs = append(errs, agentErrf(name, "agent '%s': scope entries must not be empty", name))
		} else if strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }) >= 0 {
			errs = append(errs, agentErrf(name, "agent '%s': scope entries must not contain control characters", name))
		}
	}
	// Language entries (Epic 9.0) follow the Scope guard: reject blank entries (a
	// YAML typo, not "no constraint" \u2014 that is an absent field) and entries with
	// control characters. The empty check runs on the CANONICAL form
	// (NormalizeLanguageToken), not the raw value, because validate() runs before
	// applyDefaults: an entry like "." or " . " is non-empty raw but canonicalizes
	// to "" (the leading dot is stripped), which would otherwise leak a blank token
	// that routing-matches every extensionless finding. Checking the canonical form
	// rejects "", whitespace-only, ".", and " . " in one guard. The control-char
	// check stays on the raw value (canonicalization never adds/removes control
	// runes). No known-language allow-list \u2014 third-party authors may declare any ext.
	for i, s := range a.Language {
		if NormalizeLanguageToken(s) == "" {
			errs = append(errs, agentErrf(name, "agent '%s': language entry at index %d must not be empty", name, i))
		} else if strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }) >= 0 {
			errs = append(errs, agentErrf(name, "agent '%s': language entry '%s' contains invalid characters", name, s))
		}
	}
	// AgentConfig.Persona names the reviewer prompt template — fanout passes it to
	// ResolvePersona, whose validateName already rejects path traversal. Reject
	// control characters and cap the length here too, mirroring the executor
	// persona guard and the Scope/Language guards, so a malformed community
	// persona fails fast at load rather than at runtime resolution. (Persona is a
	// template selector, not verbatim prompt text, so this is defense-in-depth and
	// a clearer load-time error — it closes the last unguarded prompt/fs-adjacent
	// agent string rather than an active interpolation-injection path.)
	if strings.IndexFunc(a.Persona, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }) >= 0 {
		errs = append(errs, agentErrf(name, "agent '%s': persona must not contain control characters", name))
	}
	if len(a.Persona) > MaxExecutorPersonaLen {
		errs = append(errs, agentErrf(name, "agent '%s': persona must be at most %d characters", name, MaxExecutorPersonaLen))
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

// NormalizeLanguageToken canonicalizes a language/extension token to the form
// used for language-aware skeptic routing (Epic 9.0): surrounding whitespace
// trimmed, all leading dots stripped, and lowercased. It is the single shared
// canonicalizer — applyDefaults runs every AgentConfig.Language entry through
// it at load, and the verify package's normalizeExt delegates to it for a
// finding's file extension — so both sides of a routing match can never drift
// out of the same canonical form. Mirrors the load-time canonicalization of
// MinSeverity and Role.
func NormalizeLanguageToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, ".")
	return strings.ToLower(s)
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
			a.MinSeverity = reclib.NormalizeSeverity(a.MinSeverity)
		}
		// Canonicalize scope entries (Epic 2.2): trim whitespace so downstream
		// comparisons (ScopeFocus rendering, prompt injection) use stable tokens.
		for i, s := range a.Scope {
			a.Scope[i] = strings.TrimSpace(s)
		}
		// Canonicalize language entries (Epic 9.0): trim → strip a single leading
		// dot → lowercase, so a declared "go"/".go"/" .GO " all store as "go" and
		// match a finding's normalized file extension in one form. nil/empty stays
		// unchanged (no constraint).
		for i, s := range a.Language {
			a.Language[i] = NormalizeLanguageToken(s)
		}
		// Canonicalize role to lowercase so downstream exact-match comparisons
		// (AgentsByRole, the Stage 3/4 routing) see a stable token regardless of
		// how it was written. An empty role stays empty — the option-a "explicitly
		// set vs inherited default" distinction is preserved (the loader still does
		// not default it to reviewer).
		a.Role = strings.ToLower(strings.TrimSpace(a.Role))
		r.Agents[name] = a
	}
	// Verification defaults (Epic 3.0): an unset min_severity resolves to MEDIUM,
	// an unset (or zero) votes to 1; a set min_severity is canonicalized so the
	// verify stage compares a stable upper-case token. Validation already rejected
	// any non-canonical value, so reclib.NormalizeSeverity here only fixes casing.
	if r.Verify.MinSeverity == "" {
		r.Verify.MinSeverity = DefaultVerifyMinSeverity
	} else {
		r.Verify.MinSeverity = reclib.NormalizeSeverity(r.Verify.MinSeverity)
	}
	if r.Verify.Votes == 0 {
		r.Verify.Votes = DefaultVerifyVotes
	}
	// Debate triggers (Epic 6.0): an absent or empty list enables the default kinds
	// at load so consumers see a resolved set. The default set comes from the shared
	// DefaultDebateTriggers source, the same one internal/debate.ResolveConfig uses,
	// so load-time and stage-time defaulting cannot drift. max_items and
	// allow_single_model are resolved later (internal/debate.ResolveConfig) —
	// max_items must stay nil here so an explicit 0 (unlimited) remains
	// distinguishable from unset.
	if len(r.Debate.Triggers) == 0 {
		r.Debate.Triggers = DefaultDebateTriggers()
	}
	// Executor defaults (Epic 7.0): an unset persona resolves to "fixer", an unset
	// role to "executor", and an unset min_severity_for_fix to MEDIUM (a set value
	// is canonicalized — validation already rejected any non-canonical value, so
	// NormalizeSeverity here only fixes casing). An unset name falls back to
	// "executor" so attribution ("fix by <name>") always has a token. A nil block
	// (no fix generation) is left untouched.
	if r.Executor != nil {
		if r.Executor.Persona == "" {
			r.Executor.Persona = DefaultExecutorPersona
		}
		r.Executor.Role = strings.ToLower(strings.TrimSpace(r.Executor.Role))
		if r.Executor.Role == "" {
			r.Executor.Role = RoleExecutor
		}
		if r.Executor.Name == "" {
			r.Executor.Name = RoleExecutor
		}
		if r.Executor.MinSeverity == "" {
			r.Executor.MinSeverity = DefaultFixMinSeverity
		} else {
			r.Executor.MinSeverity = reclib.NormalizeSeverity(r.Executor.MinSeverity)
		}
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
