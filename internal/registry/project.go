package registry

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Embedded defaults for project-level settings (the lowest precedence tier).
const (
	DefaultPayloadMode = "blocks"
	DefaultFailOn      = "HIGH"
)

// ProjectConfig is the project-level configuration from .atcr/config.yaml:
// the agent roster, payload mode, global timeout, and CI gate threshold.
type ProjectConfig struct {
	Agents       []string `yaml:"agents"`
	SerialAgents []string `yaml:"serial_agents,omitempty"`
	PayloadMode  string   `yaml:"payload_mode,omitempty"`
	TimeoutSecs  int      `yaml:"timeout_secs,omitempty"`
	FailOn       string   `yaml:"fail_on,omitempty"`
}

// DefaultProjectConfigPath returns .atcr/config.yaml under root.
func DefaultProjectConfigPath(root string) string {
	return filepath.Join(root, ".atcr", "config.yaml")
}

// LoadProjectConfig reads, strictly parses, and validates the project config
// at path, applying embedded defaults to absent optional fields.
func LoadProjectConfig(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no roster found: .atcr/config.yaml not found (looked at %s) — run 'atcr init'", path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading project config: %w", err)
	}

	base := filepath.Base(path)
	var cfg ProjectConfig
	if len(bytes.TrimSpace(data)) > 0 {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("failed to parse %s: %w", base, err)
		}
		if err := dec.Decode(new(ProjectConfig)); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("failed to parse %s: unexpected second YAML document", base)
		}
	}

	if len(cfg.Agents) == 0 {
		return nil, errors.New("no agents selected — add at least one agent to .atcr/config.yaml")
	}
	if cfg.TimeoutSecs < 0 {
		return nil, fmt.Errorf("%s: timeout_secs must not be negative", base)
	}

	cfg.applyDefaults()
	return &cfg, nil
}

// applyDefaults fills absent optional fields from the embedded defaults.
func (c *ProjectConfig) applyDefaults() {
	if c.PayloadMode == "" {
		c.PayloadMode = DefaultPayloadMode
	}
	if c.TimeoutSecs == 0 {
		c.TimeoutSecs = DefaultTimeoutSecs
	}
	if c.FailOn == "" {
		c.FailOn = DefaultFailOn
	}
}

// ValidateAgainst checks that every roster entry (parallel and serial lane)
// exists in the registry, appears only once, and sits in exactly one lane.
func (c *ProjectConfig) ValidateAgainst(reg *Registry) error {
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
