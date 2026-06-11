package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Embedded defaults for project-level settings (the lowest precedence tier).
const (
	DefaultPayloadMode = "blocks"
	DefaultFailOn      = "HIGH"
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
	FailOn       string   `yaml:"fail_on,omitempty"`
}

// DefaultProjectConfigPath returns .atcr/config.yaml under root.
func DefaultProjectConfigPath(root string) string {
	return filepath.Join(root, ".atcr", "config.yaml")
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

	if len(cfg.Agents) == 0 {
		return nil, errors.New("no agents selected — add at least one agent to .atcr/config.yaml")
	}
	for _, lane := range [][]string{cfg.Agents, cfg.SerialAgents} {
		for _, name := range lane {
			if strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("%s: roster entries must not be empty", base)
			}
		}
	}
	if cfg.TimeoutSecs != nil && *cfg.TimeoutSecs <= 0 {
		return nil, fmt.Errorf("%s: timeout_secs must be positive", base)
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
