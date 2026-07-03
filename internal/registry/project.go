package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Embedded defaults for project-level settings (the lowest precedence tier).
// DefaultFailOn seeds ONLY the config template `atcr init` generates — it
// never participates in gate resolution, which is opt-in (see
// ResolveGateThreshold and the reconcile gate).
const (
	DefaultPayloadMode = "blocks"
	DefaultFailOn      = "HIGH"
	// DefaultReviewStrategy is the embedded fan-out strategy (Epic 14.3). "bulk"
	// sends the whole diff in one prompt per persona, keeping API cost strictly
	// bounded; users opt into "chunked" for higher accuracy on large PRs.
	DefaultReviewStrategy = "bulk"
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
	TimeoutSecs    *int   `yaml:"timeout_secs,omitempty"`
	// PayloadByteBudget is a pointer so an explicit 0 (unlimited) survives
	// default application.
	PayloadByteBudget *int64 `yaml:"payload_byte_budget,omitempty"`
	FailOn            string `yaml:"fail_on,omitempty"`
	// MaxParallel is a pointer so an explicit 0 (unbounded) survives default
	// application in ResolveSettings.
	MaxParallel *int `yaml:"max_parallel,omitempty"`
	// CacheMaxBytes overrides the diff-cache total-size cap (Epic 5.2). A pointer
	// so an explicit 0 (unbounded) survives default application; unset inherits
	// the registry tier or the embedded DefaultCacheMaxBytes.
	CacheMaxBytes *int64 `yaml:"cache_max_bytes,omitempty"`
	// Sandbox is the optional execution-reproduction backend block (Epic 11.0).
	// nil means execution is unconfigured and `--exec` is refused.
	Sandbox *SandboxConfig `yaml:"sandbox,omitempty"`
	// AutoFix is the optional `--auto-fix` backend block (Sprint 17.0). nil means
	// the config-derived pieces inherit their defaults; it never enables the flow
	// on its own (validateAutoFixBackend gates that).
	AutoFix *AutoFixConfig `yaml:"auto_fix,omitempty"`
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
	b.WriteString("# max_parallel: cap on concurrent parallel-lane agent calls. Default: 10 (a cap).\n")
	b.WriteString("#   Set to 0 for unbounded — unset is NOT unbounded, it uses the default of 10.\n")
	fmt.Fprintf(&b, "max_parallel: %d\n", DefaultMaxParallel)
	b.WriteString("# cache_max_bytes: total-size cap (bytes) for the diff cache under .atcr/cache.\n")
	b.WriteString("#   Unchanged diffs are served from cache, skipping the LLM call. Default 50 MiB;\n")
	b.WriteString("#   least-recently-used entries are evicted past the cap. Set to 0 for unbounded.\n")
	fmt.Fprintf(&b, "cache_max_bytes: %d\n", DefaultCacheMaxBytes)
	fmt.Fprintf(&b, "fail_on: %s\n", DefaultFailOn)
	return b.String()
}

// LoadProjectConfig reads, strictly parses, and validates the project config
// at path. Absent optional fields stay unset; embedded defaults are applied
// by ResolveSettings so precedence can see what this tier actually set.
func LoadProjectConfig(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// Message text mandated by AC 01-01 (Error Scenario 1).
		return nil, fmt.Errorf("no roster found: .atcr/config.yaml not found (looked at %s) — run 'atcr init'", path)
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
	if cfg.MaxParallel != nil && *cfg.MaxParallel < 0 {
		return nil, fmt.Errorf("%s: max_parallel must be >= 0 (0 = unbounded)", base)
	}
	if cfg.CacheMaxBytes != nil && *cfg.CacheMaxBytes < 0 {
		return nil, fmt.Errorf("%s: cache_max_bytes must be >= 0 (0 = unbounded)", base)
	}
	if !payloadModeValid(cfg.PayloadMode) {
		return nil, fmt.Errorf("invalid payload_mode '%s': must be one of diff, blocks, files", strings.TrimSpace(cfg.PayloadMode))
	}
	if !reviewStrategyValid(cfg.ReviewStrategy) {
		return nil, fmt.Errorf("%s: invalid review_strategy '%s': must be one of bulk, chunked", base, strings.TrimSpace(cfg.ReviewStrategy))
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
