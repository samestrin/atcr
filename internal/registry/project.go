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
	// DefaultPayloadByteBudget is the embedded per-payload byte budget:
	// 512 KiB ≈ 128k tokens at ~4 bytes/token, fitting the dominant
	// 128k-context model tier with prompt headroom. 0 is the documented
	// unlimited escape hatch (AC 06-03).
	DefaultPayloadByteBudget int64 = 524288
)

// ProjectConfig is the project-level configuration from .atcr/config.yaml:
// the agent roster, payload mode, global timeout, and CI gate threshold.
// TimeoutSecs is a pointer so an explicit zero is caught by validation
// instead of being silently replaced by the default.
type ProjectConfig struct {
	Agents       []string `yaml:"agents"`
	SerialAgents []string `yaml:"serial_agents,omitempty"`
	PayloadMode  string   `yaml:"payload_mode,omitempty"`
	TimeoutSecs  *int     `yaml:"timeout_secs,omitempty"`
	// PayloadByteBudget is a pointer so an explicit 0 (unlimited) survives
	// default application.
	PayloadByteBudget *int64 `yaml:"payload_byte_budget,omitempty"`
	FailOn            string `yaml:"fail_on,omitempty"`
	// MaxParallel is a pointer so an explicit 0 (unbounded) survives default
	// application in ResolveSettings.
	MaxParallel *int `yaml:"max_parallel,omitempty"`
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
	fmt.Fprintf(&b, "payload_byte_budget: %d\n", DefaultPayloadByteBudget)
	b.WriteString("# max_parallel: cap on concurrent parallel-lane agent calls; 0 = unbounded.\n")
	fmt.Fprintf(&b, "max_parallel: %d\n", DefaultMaxParallel)
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
	if !payloadModeValid(cfg.PayloadMode) {
		return nil, fmt.Errorf("invalid payload_mode '%s': must be one of diff, blocks, files", strings.TrimSpace(cfg.PayloadMode))
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
