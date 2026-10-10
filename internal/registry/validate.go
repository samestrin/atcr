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
//
// It additionally rejects the machine-LOCAL agent fields listed in
// communityForbiddenFields, which are recognized registry keys (so the strict
// decode alone would accept them) but are meaningless — and actively harmful —
// when published to another user's machine.
func ValidateCommunityPersonaYAML(name string, data []byte) error {
	var cf communityPersonaFile
	if err := decodeStrictYAML(data, &cf); err != nil {
		if errors.Is(err, errEmptyDocument) {
			return fmt.Errorf("community persona %q has no content", name)
		}
		return fmt.Errorf("parse community persona %q: %w", name, err)
	}
	if err := rejectMachineLocalFields(name, cf.AgentConfig); err != nil {
		return err
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

// rejectMachineLocalFields fails a community persona that declares an agent
// field whose truth is specific to the AUTHOR's machine and cannot transfer.
//
// The exclusion cannot be expressed in communityPersonaFile's shape: these are
// real AgentConfig keys reached through the `,inline` embed, and re-declaring
// one on the outer struct to shadow it makes yaml.v3 reject the whole type as a
// duplicated key. So the schema admits them and this guard removes them, which
// keeps the strict decode's unknown-key behavior untouched.
//
// context_window_tokens is one member: it describes a proxy-local
// alias's real window (config.go's ContextWindowTokens — "proxy-LOCAL and
// meaningless to any other atcr user"). Installed verbatim onto a consumer whose
// proxy serves that model at 32,768, an author's 128,000 declaration resolves
// AHEAD of the static table and produces guaranteed over-window payloads — the
// one direction the Conservatism NFR forbids.
//
// thinking, thinking_level, and thinking_style follow the same rule as
// response_format below: which request field a model honors is a fact about
// the endpoint the consumer resolves, not about the persona.
//
// replay_reasoning follows it too: whether an endpoint tolerates a replayed
// reasoning member is a fact about the endpoint the consumer resolves.
//
// response_format is another: its own doc (config.go's ResponseFormat) defines
// it as a claim about the endpoint the CONSUMER resolves — "this agent's model
// honors the OpenAI-compatible response_format request field". A published
// json_object declaration imposes that contract on every consumer's review
// calls, and a provider that 400s on it (or silently ignores it, breaking the
// declared JSON mode) fails a lane the consumer never chose, bypassing their own
// doctor verification. Each consumer opts in on their own agents instead.
//
// Rejecting at validation means the
// persona never reaches disk (internal/personas/install.go writes only after
// this returns nil), rather than being silently stripped after the fact.
func rejectMachineLocalFields(name string, cfg AgentConfig) error {
	// Accumulate every rejection (matching validateAgent's posture for the
	// same fields) so a persona author sees the full list in one install
	// attempt instead of one fault per round-trip.
	var errs []error
	if cfg.ContextWindowTokens != nil {
		errs = append(errs, fmt.Errorf("community persona %q must not declare context_window_tokens: "+
			"the window of a proxy-local model alias is specific to the machine that authored it, "+
			"so each consumer declares it in their own registry", name))
	}
	if cfg.ResponseFormat != "" {
		errs = append(errs, fmt.Errorf("community persona %q must not declare response_format: "+
			"whether a model honors the response_format request field is specific to the "+
			"endpoint each consumer resolves, so each consumer declares it on their own agents", name))
	}
	for _, f := range []struct{ key, value string }{
		{"thinking", cfg.Thinking},
		{"thinking_level", cfg.ThinkingLevel},
		{"thinking_style", cfg.ThinkingStyle},
		{"preserve_thinking", cfg.PreserveThinking},
	} {
		if f.value != "" {
			errs = append(errs, fmt.Errorf("community persona %q must not declare %s: "+
				"whether and how a model's thinking behavior is honored is specific to the "+
				"endpoint each consumer resolves, so each consumer declares it on their own agents", name, f.key))
		}
	}
	if cfg.ReplayReasoning != "" {
		errs = append(errs, fmt.Errorf("community persona %q must not declare replay_reasoning: "+
			"whether an endpoint tolerates a replayed reasoning member is specific to the "+
			"endpoint each consumer resolves, so each consumer declares it on their own agents", name))
	}
	return errors.Join(errs...)
}
