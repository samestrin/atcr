package registry

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ValidateAgentYAML parses a single community-persona document and runs the
// registry's agent validation against it, returning a joined error describing
// every fault (or nil when valid). It is the exported seam the personas CLI
// (internal/personas) uses to vet fetched YAML BEFORE writing it to disk, so
// malformed or malicious community configs never reach the registry.
//
// Unlike LoadRegistry, the unmarshal is NON-strict: a community persona file is
// an AgentConfig superset that also carries persona-file metadata (version,
// description, fixture) the registry schema does not define, and those extra
// keys are intentionally ignored rather than rejected. The agent fields that DO
// matter (provider/model/role/payload/temperature/scope/language/...) are
// validated by the same unexported validateAgent the registry uses at load, so
// the rules can never drift from a hand-maintained duplicate — notably the
// Epic 9.0 Language guard added this sprint stays in one place.
//
// The agent's referenced provider is synthesized into a throwaway single-entry
// registry so validateAgent's provider-reference check passes in isolation;
// whether that provider actually exists is resolved later, when the installed
// file is merged into the user's real registry.
func ValidateAgentYAML(name string, data []byte) error {
	var cfg AgentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse persona %q: %w", name, err)
	}
	r := &Registry{
		Providers: map[string]Provider{cfg.Provider: {APIKeyEnv: "PLACEHOLDER"}},
		Agents:    map[string]AgentConfig{name: cfg},
	}
	return errors.Join(r.validateAgent(name, cfg)...)
}
