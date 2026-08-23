package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	reclib "github.com/samestrin/atcr/reconcile"
)

// Embedded defaults for project-level settings (the lowest precedence tier).
// DefaultFailOn seeds ONLY the config template `atcr init` generates — it
// never participates in gate resolution, which is opt-in (see
// ResolveGateThreshold and the reconcile gate). DefaultConsensus, by contrast,
// aliases the reconcile package's canonical level so the template seed and the
// effective runtime default are one value, not two literals that can drift.
const (
	DefaultPayloadMode = "blocks"
	DefaultFailOn      = "HIGH"
	// DefaultConsensus is the embedded consensus-filter level (epic 35.9.1):
	// "strict" reproduces the hardcoded epic-14.2 behavior, so an unconfigured
	// project sees no change. ResolveConsensus returns "" when nothing is
	// configured and each call site normalizes through
	// reclib.NormalizeConsensus (whose "" maps to reclib.ConsensusStrict),
	// because an unresolvable level must never silently disable the filter.
	// Deriving this const from reclib.ConsensusStrict keeps the template seed
	// and that effective default structurally identical.
	DefaultConsensus = reclib.ConsensusStrict
	// DefaultReviewStrategy is the embedded fan-out strategy (Epic 14.3). "bulk"
	// sends the whole diff in one prompt per persona, keeping API cost strictly
	// bounded; users opt into "chunked" for higher accuracy on large PRs.
	DefaultReviewStrategy = "bulk"
	// DefaultOnOverflow is the embedded F4 degradation policy (plan 19.10) used
	// when a per-agent payload exceeds its effective budget: "chunk" delivers
	// that agent's payload across window-sized chunks, dropping nothing at that
	// step. The guarantee is scoped to that step and no further: the global
	// payload_byte_budget shed (ApplyByteBudgetPreferEscalated, called from
	// buildPayloads in internal/fanout) runs BEFORE any overflow handling and
	// does drop whole files, and on_overflow is never consulted in it. The full
	// ladder is chunk/truncate/fallback/fail; "fallback"/"fail" are recognized as
	// config values but recognized-but-gated per AC4 (dispatch enforcement lives
	// in internal/fanout / Task 04, not here).
	DefaultOnOverflow = "chunk"
	// DefaultPayloadByteBudget is the embedded per-payload byte budget:
	// 512 KiB ≈ 128k tokens at ~4 bytes/token, fitting the dominant
	// 128k-context model tier with prompt headroom. 0 is the documented
	// unlimited escape hatch (AC 06-03).
	// Context-sizing: models with context limits below 128k will fail on this
	// default — reduce payload_byte_budget to ~163840 (160 KiB ≈ 40k tokens)
	// in .atcr/config.yaml for rosters that include smaller-context models.
	DefaultPayloadByteBudget int64 = 524288
	// DefaultCacheMaxBytes is the embedded total-size cap for the diff cache
	// (Epic 5.2): 50 MiB of reviewer outputs under .atcr/cache before
	// least-recently-used eviction kicks in. 0 is the documented unbounded
	// escape hatch (parity with payload_byte_budget / max_parallel).
	DefaultCacheMaxBytes int64 = 50 * 1024 * 1024
	// DefaultMaxSprintPlanBytes is the embedded byte ceiling applied to a
	// --sprint-plan file before it is wrapped in a SCOPE CONSTRAINT block and
	// prepended to every reviewer's payload (Epic 12.2 / plan 19.10 F9). 64 KiB
	// gives operators on larger-context models room for a fuller sprint/epic plan
	// than the original fixed 16 KiB ceiling. Unlike payload_byte_budget /
	// cache_max_bytes, 0 is NOT an "unbounded" sentinel here — there is no
	// unbounded plan-injection use case, so <= 0 is rejected at load.
	DefaultMaxSprintPlanBytes int64 = 65536
)

// ProjectConfig is the project-level configuration from .atcr/config.yaml:
// the agent roster, payload mode, global timeout, and CI gate threshold.
// TimeoutSecs is a pointer so an explicit zero is caught by validation
// instead of being silently replaced by the default.
type ProjectConfig struct {
	Agents       []string `yaml:"agents"`
	SerialAgents []string `yaml:"serial_agents,omitempty"`
	PayloadMode  string   `yaml:"payload_mode,omitempty"`
	// ReviewStrategy overrides the run-wide fan-out strategy (Epic 14.3): "bulk"
	// or "chunked". Empty inherits the registry tier or the embedded default.
	ReviewStrategy string `yaml:"review_strategy,omitempty"`
	// OnOverflow selects the F4 degradation policy (plan 19.10) when a payload
	// exceeds budget: chunk (default), truncate, fallback, or fail. Empty inherits
	// the registry tier or the embedded default.
	OnOverflow  string `yaml:"on_overflow,omitempty"`
	TimeoutSecs *int   `yaml:"timeout_secs,omitempty"`
	// PayloadByteBudget is a pointer so an explicit 0 (unlimited) survives
	// default application.
	PayloadByteBudget *int64 `yaml:"payload_byte_budget,omitempty"`
	// ChunkByteBudget caps the PER-CHUNK payload independently of
	// PayloadByteBudget. A pointer for the same reason: an explicit 0 (unlimited)
	// must survive, and nil must stay distinguishable from it so "unset" can mean
	// "inherit the payload budget" rather than "unlimited".
	ChunkByteBudget *int64 `yaml:"chunk_byte_budget,omitempty"`
	FailOn          string `yaml:"fail_on,omitempty"`
	// Consensus selects the epic-14.2 consensus filter's corroboration bar
	// (epic 35.9.1): strict (default — today's behavior), lenient (keep
	// MEDIUM-confidence singletons), or off (filter inert). Empty inherits the
	// registry tier or DefaultConsensus. Enum validation lives at the call
	// sites (ResolveConsensus returns the raw string), matching fail_on.
	Consensus string `yaml:"consensus,omitempty"`
	// MaxParallel is a pointer so an explicit 0 (unbounded) survives default
	// application in ResolveSettings.
	MaxParallel *int `yaml:"max_parallel,omitempty"`
	// CacheMaxBytes overrides the diff-cache total-size cap (Epic 5.2). A pointer
	// so an explicit 0 (unbounded) survives default application; unset inherits
	// the registry tier or the embedded DefaultCacheMaxBytes.
	CacheMaxBytes *int64 `yaml:"cache_max_bytes,omitempty"`
	// MaxSprintPlanBytes overrides the sprint-plan byte ceiling (plan 19.10 F9). A
	// pointer so an explicit value survives default application; unset inherits
	// the registry tier or the embedded DefaultMaxSprintPlanBytes.
	MaxSprintPlanBytes *int64 `yaml:"max_sprint_plan_bytes,omitempty"`
	// Sandbox is the optional execution-reproduction backend block (Epic 11.0).
	// nil means execution is unconfigured and `--exec` is refused.
	Sandbox *SandboxConfig `yaml:"sandbox,omitempty"`
	// AutoFix is the optional `--auto-fix` backend block (Sprint 17.0). nil means
	// the config-derived pieces inherit their defaults; it never enables the flow
	// on its own (validateAutoFixBackend gates that).
	AutoFix *AutoFixConfig `yaml:"auto_fix,omitempty"`
	// Telemetry persists the opt-out state for the anonymous usage ping (Sprint
	// 28.0). A pointer so an explicit false survives default application (the
	// Sandbox/AutoFix/MaxParallel idiom); nil means "unset", treated as the
	// default-enabled posture. It is OR'd with the ATCR_TELEMETRY env var: either
	// surface disabling is sufficient and final (see cmd/atcr telemetryGate).
	Telemetry *bool `yaml:"telemetry,omitempty"`
	// QualitySignal persists the opt-IN state for the community prompt quality
	// signal (Sprint 30.0). A pointer so an explicit false survives default
	// application (the Telemetry idiom); nil means "unset", treated as the
	// default-DISABLED posture — the exact inverse of Telemetry's default-enabled
	// opt-out. It is declared here so a persisted quality_signal key passes the
	// strict KnownFields roster load; the opt-in gate reads it via the permissive
	// LoadQualitySignalSetting, never coupling to telemetry (see cmd/atcr
	// qualitySignalGate).
	QualitySignal *bool `yaml:"quality_signal,omitempty"`
	// TelemetryNoticeShown records that the one-time first-run telemetry
	// disclosure has already been printed for this repository (Epic 35.12 TD).
	//
	// It is BOOKKEEPING, not a consent surface: nothing reads it to decide whether
	// to transmit — that is Telemetry/ATCR_TELEMETRY alone — it only stops the
	// notice repeating. Like its siblings it MUST be declared here, because the
	// roster load is strict (KnownFields): an undeclared key makes every review in
	// a repo that has been disclosed to fail with "field ... not found", turning a
	// disclosure into a hard outage. TestProjectConfig_TelemetryNoticeShownIsKnownField
	// pins that.
	TelemetryNoticeShown *bool `yaml:"telemetry_notice_shown,omitempty"`
}

// DefaultProjectConfigPath returns .atcr/config.yaml under root.
func DefaultProjectConfigPath(root string) string {
	return filepath.Join(root, ".atcr", "config.yaml")
}

// DefaultProjectConfigYAML renders the config.yaml content `atcr init`
// installs: the given roster plus explicit embedded defaults, so users see
// and can edit every knob.
func DefaultProjectConfigYAML(roster []string) string {
	var b strings.Builder
	b.WriteString("# atcr project configuration — see docs/registry.md\n")
	b.WriteString("# Roster entries must match agent names defined in ~/.config/atcr/registry.yaml,\n")
	b.WriteString("# or, for a self-contained repo, in .atcr/registry.yaml (project overlay).\n")
	b.WriteString("agents:\n")
	for _, name := range roster {
		fmt.Fprintf(&b, "  - %s\n", name)
	}
	b.WriteString("serial_agents: []\n")
	fmt.Fprintf(&b, "payload_mode: %s\n", DefaultPayloadMode)
	fmt.Fprintf(&b, "timeout_secs: %d\n", DefaultTimeoutSecs)
	b.WriteString("# payload_byte_budget: per-payload byte budget. Default 512 KiB ≈ 128k tokens.\n")
	b.WriteString("#   Models with context limits below 128k will fail on the default. For rosters\n")
	b.WriteString("#   that include smaller-context models (e.g. 49k-limit), reduce to 163840 (160 KiB).\n")
	fmt.Fprintf(&b, "payload_byte_budget: %d\n", DefaultPayloadByteBudget)
	b.WriteString("# chunk_byte_budget: byte budget used ONLY to size a per-chunk payload under\n")
	b.WriteString("#   the chunked strategy. Unset (the default) inherits payload_byte_budget, so\n")
	b.WriteString("#   leaving it commented reproduces the behavior from before this key existed.\n")
	b.WriteString("#   Set it when the two budgets want opposite values: lowering payload_byte_budget\n")
	b.WriteString("#   to protect a small-window model also sheds whole FILES from the payload,\n")
	b.WriteString("#   while raising it to keep those files also grows every agent's chunks. Split\n")
	b.WriteString("#   them to hold a large payload and still chunk small. 0 = unlimited.\n")
	b.WriteString("#   ALSO caps the injected --sprint-plan block: the plan is funded at one eighth\n")
	b.WriteString("#   of the budget sizing the call, so on a chunked run the ceiling becomes\n")
	b.WriteString("#   min(max_sprint_plan_bytes, chunk_byte_budget/8, agent budget/8). Uncommenting\n")
	b.WriteString("#   the 65536 below therefore shrinks an injected plan from up to 64 KiB to 8 KiB.\n")
	b.WriteString("#   NOT consulted on a baseline scan: `atcr review --all/--dir` always splits each\n")
	b.WriteString("#   agent's files into per-agent chunks (a separate mechanism from review_strategy,\n")
	b.WriteString("#   which is off for baseline scans), sized by payload_byte_budget, not by this key.\n")
	b.WriteString("# chunk_byte_budget: 65536\n")
	b.WriteString("# max_parallel: cap on concurrent parallel-lane agent calls. Default: 10 (a cap).\n")
	b.WriteString("#   Set to 0 for unbounded — unset is NOT unbounded, it uses the default of 10.\n")
	fmt.Fprintf(&b, "max_parallel: %d\n", DefaultMaxParallel)
	b.WriteString("# cache_max_bytes: total-size cap (bytes) for the diff cache under .atcr/cache.\n")
	b.WriteString("#   Unchanged diffs are served from cache, skipping the LLM call. Default 50 MiB;\n")
	b.WriteString("#   least-recently-used entries are evicted past the cap. Set to 0 for unbounded.\n")
	fmt.Fprintf(&b, "cache_max_bytes: %d\n", DefaultCacheMaxBytes)
	b.WriteString("# max_sprint_plan_bytes: byte ceiling for a --sprint-plan file's SCOPE\n")
	b.WriteString("#   CONSTRAINT injection into every reviewer's payload. Default 64 KiB; raise it\n")
	b.WriteString("#   to give larger-context models more sprint/epic plan detail. Must be > 0.\n")
	fmt.Fprintf(&b, "max_sprint_plan_bytes: %d\n", DefaultMaxSprintPlanBytes)
	b.WriteString("# on_overflow: degradation policy (plan 19.10 F4) when a per-agent payload\n")
	b.WriteString("#   exceeds its per-model budget. One of: chunk (default — deliver that\n")
	b.WriteString("#   agent's payload across window-sized chunks, dropping nothing at THIS\n")
	b.WriteString("#   step), truncate (drop the lowest-priority tail, flagged), fallback, or\n")
	b.WriteString("#   fail. fallback/fail are recognized but their dispatch prerequisites may\n")
	b.WriteString("#   not yet be shipped.\n")
	b.WriteString("#   SCOPE: this governs the per-agent step only. The global\n")
	b.WriteString("#   payload_byte_budget shed runs FIRST and DOES drop whole files; whatever\n")
	b.WriteString("#   it sheds is gone before on_overflow is ever consulted, so no setting\n")
	b.WriteString("#   here can recover it.\n")
	fmt.Fprintf(&b, "on_overflow: %s\n", DefaultOnOverflow)
	fmt.Fprintf(&b, "fail_on: %s\n", DefaultFailOn)
	b.WriteString("# consensus: corroboration bar for the reconcile consensus filter, applied\n")
	b.WriteString("#   only once a panel has 3+ distinct reviewers. One of:\n")
	b.WriteString("#     strict  — (default) sidecar every singleton below HIGH confidence\n")
	b.WriteString("#     lenient — keep MEDIUM-confidence singletons; sidecar only LOW ones\n")
	b.WriteString("#     off     — filter inert; every singleton reaches findings.json\n")
	b.WriteString("#   Security, HIGH/CRITICAL, out-of-scope, and high-trust singletons are\n")
	b.WriteString("#   exempt at every level — only the corroboration bar moves.\n")
	fmt.Fprintf(&b, "consensus: %s\n", DefaultConsensus)
	b.WriteString("# telemetry: anonymous usage ping. Default enabled; set false (or export\n")
	b.WriteString("#   ATCR_TELEMETRY=0) to opt out. Either surface disabling is sufficient.\n")
	b.WriteString("# telemetry: true\n")
	b.WriteString("# quality_signal: content-free reviewer-quality signal (per-persona/model\n")
	b.WriteString("#   confirm/dismiss counts). Default disabled; set true (or export\n")
	b.WriteString("#   ATCR_QUALITY_SIGNAL=1) to opt in.\n")
	b.WriteString("# quality_signal: false\n")
	b.WriteString("# auto_fix: opt-in remediation for `atcr review --auto-fix`. Off unless the flag\n")
	b.WriteString("#   is passed; leave this stanza commented to keep the default review path.\n")
	b.WriteString("#   apply_target: working-tree dir the patch applies to (default: repo root).\n")
	b.WriteString("#   validate_command: argv run after apply; must pass before any GitHub PR is\n")
	b.WriteString("#     opened (default: `go build ./...` for Go projects; required otherwise).\n")
	b.WriteString("#   validate_timeout: max duration for one validation run (default: 2m).\n")
	b.WriteString("# auto_fix:\n")
	b.WriteString("#   apply_target: .\n")
	b.WriteString("#   validate_command: [go, build, ./...]\n")
	b.WriteString("#   validate_timeout: 2m\n")
	return b.String()
}

// LoadProjectConfig reads, strictly parses, and validates the project config
// at path. Absent optional fields stay unset; embedded defaults are applied
// by ResolveSettings so precedence can see what this tier actually set.
func LoadProjectConfig(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// Message text mandated by AC 01-01 (Error Scenario 1). os.ErrNotExist is
		// WRAPPED rather than replaced so a tiered resolver can skip this tier on
		// absence alone (errors.Is) instead of pre-checking with os.Stat — a
		// pre-check cannot tell "absent" from "unreachable", and races the read.
		return nil, fmt.Errorf("no roster found: .atcr/config.yaml not found (looked at %s) — run 'atcr init': %w", path, os.ErrNotExist)
	}
	if err != nil {
		return nil, fmt.Errorf("reading project config: %w", err)
	}

	base := filepath.Base(path)
	var cfg ProjectConfig
	if err := decodeStrictYAML(data, &cfg); err != nil && !errors.Is(err, errEmptyDocument) {
		return nil, fmt.Errorf("failed to parse %s: %w", base, err)
	}

	// The roster is the union of both lanes (matching fanout's ErrEmptyRoster
	// contract): a serial-only config is legitimate when every provider is
	// rate-limited, so reject only when BOTH lanes are empty.
	if len(cfg.Agents) == 0 && len(cfg.SerialAgents) == 0 {
		return nil, errors.New("no agents selected — add at least one agent to .atcr/config.yaml")
	}
	for _, lane := range [][]string{cfg.Agents, cfg.SerialAgents} {
		for _, name := range lane {
			if strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("%s: roster entries must not be empty", base)
			}
		}
	}
	if cfg.TimeoutSecs != nil && (*cfg.TimeoutSecs <= 0 || *cfg.TimeoutSecs > MaxTimeoutSecs) {
		return nil, fmt.Errorf("%s: timeout_secs must be positive (max %d)", base, MaxTimeoutSecs)
	}
	if cfg.PayloadByteBudget != nil && *cfg.PayloadByteBudget < 0 {
		return nil, fmt.Errorf("%s: payload_byte_budget must be >= 0 (0 = unlimited)", base)
	}
	if cfg.ChunkByteBudget != nil && *cfg.ChunkByteBudget < 0 {
		return nil, fmt.Errorf("%s: chunk_byte_budget must be >= 0 (0 = unlimited)", base)
	}
	if cfg.MaxParallel != nil && *cfg.MaxParallel < 0 {
		return nil, fmt.Errorf("%s: max_parallel must be >= 0 (0 = unbounded)", base)
	}
	if cfg.CacheMaxBytes != nil && *cfg.CacheMaxBytes < 0 {
		return nil, fmt.Errorf("%s: cache_max_bytes must be >= 0 (0 = unbounded)", base)
	}
	if cfg.MaxSprintPlanBytes != nil && *cfg.MaxSprintPlanBytes <= 0 {
		return nil, fmt.Errorf("%s: max_sprint_plan_bytes must be > 0, got %d", base, *cfg.MaxSprintPlanBytes)
	}
	if !payloadModeValid(cfg.PayloadMode) {
		return nil, fmt.Errorf("invalid payload_mode '%s': must be one of diff, blocks, files", strings.TrimSpace(cfg.PayloadMode))
	}
	if !reviewStrategyValid(cfg.ReviewStrategy) {
		return nil, fmt.Errorf("%s: invalid review_strategy '%s': must be one of bulk, chunked", base, strings.TrimSpace(cfg.ReviewStrategy))
	}
	if !onOverflowValid(cfg.OnOverflow) {
		return nil, fmt.Errorf("%s: invalid on_overflow '%s': must be one of chunk, truncate, fallback, fail", base, strings.TrimSpace(cfg.OnOverflow))
	}
	if err := cfg.Sandbox.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", base, err)
	}
	if err := cfg.AutoFix.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", base, err)
	}

	// Absent optional fields stay unset here; embedded defaults are applied
	// by ResolveSettings so the precedence chain can see what each tier
	// actually configured.
	return &cfg, nil
}

// ValidateAgainst checks that every roster entry (parallel and serial lane)
// exists in the registry, appears only once, and sits in exactly one lane.
func (c *ProjectConfig) ValidateAgainst(reg *Registry) error {
	if reg == nil {
		return errors.New("cannot validate roster: registry is nil")
	}
	seen := map[string]string{} // agent -> lane
	check := func(lane string, names []string) error {
		for _, name := range names {
			if _, ok := reg.Agents[name]; !ok {
				return fmt.Errorf("agent '%s' in project config not found in registry", name)
			}
			if prev, dup := seen[name]; dup {
				if prev != lane {
					return fmt.Errorf("agent '%s' appears in both agents and serial_agents", name)
				}
				return fmt.Errorf("agent '%s' listed more than once in %s", name, lane)
			}
			seen[name] = lane
		}
		return nil
	}
	if err := check("agents", c.Agents); err != nil {
		return err
	}
	return check("serial_agents", c.SerialAgents)
}
