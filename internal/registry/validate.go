package registry

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ValidateAgentYAML parses a single community-persona document and runs the
// registry's agent validation against it, returning a joined error describing
// every fault (or nil when valid). It is the non-strict registry-level
// validator used when merging an already-installed community persona into the
// user's real registry; the personas install/upgrade path vets untrusted
// fetched YAML with ValidateCommunityPersonaYAML BEFORE writing it to disk.
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

// ValidateCommunityPersonaYAML strictly validates a community-persona file
// (AC 04-06): it decodes with KnownFields(true) over the combined
// community-persona schema (recognized agent fields ∪ the defined catalog-only
// keys) so a key in NEITHER set is rejected as unknown, then runs the same agent
// validation the registry applies at load. This is the strict counterpart of
// ValidateAgentYAML — a fetched community unit is untrusted input, so a smuggled
// unknown field must fail closed rather than be silently ignored.
func ValidateCommunityPersonaYAML(name string, data []byte) error {
	var cf communityPersonaFile
	if err := decodeStrictYAML(data, &cf); err != nil {
		if errors.Is(err, errEmptyDocument) {
			return fmt.Errorf("community persona %q has no content", name)
		}
		return fmt.Errorf("parse community persona %q: %w", name, err)
	}
	r := &Registry{
		Providers: map[string]Provider{cf.Provider: {APIKeyEnv: "PLACEHOLDER"}},
		Agents:    map[string]AgentConfig{name: cf.AgentConfig},
	}
	return errors.Join(r.validateAgent(name, cf.AgentConfig)...)
}

// communityPersonaFile is the combined community-persona schema: the recognized
// agent fields (inlined AgentConfig) plus the defined catalog-only keys. It is
// the strict-decode target for ValidateCommunityPersonaYAML — a key present in
// NEITHER set trips KnownFields(true) as unknown.
type communityPersonaFile struct {
	AgentConfig `yaml:",inline"`
	Name        string   `yaml:"name,omitempty"`
	Version     string   `yaml:"version,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Tasks       []string `yaml:"tasks,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	Fixture     string   `yaml:"fixture,omitempty"`
	Path        string   `yaml:"path,omitempty"`
}
