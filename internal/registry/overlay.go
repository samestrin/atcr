package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Source tiers for an effective registry entry after the project overlay
// merge. Definitions come from at most two tiers — the user registry and the
// project overlay — since there are no embedded provider/agent defaults.
const (
	SourceUser    = "user"
	SourceProject = "project"
)

// File labels used for error attribution and the doctor provenance column.
// The user and project registries share the base name "registry.yaml", so the
// project label is qualified with its directory to keep them distinguishable.
const (
	userRegistryLabel    = "registry.yaml"
	projectRegistryLabel = ".atcr/registry.yaml"
)

// sharedSettingsKeys are YAML keys that belong in .atcr/config.yaml, not
// .atcr/registry.yaml. When the strict decoder rejects one, the error is
// amended with a targeted hint so the contributor knows where it belongs.
var sharedSettingsKeys = []string{
	"roster",
	"payload_mode",
	"timeout_secs",
	"max_parallel",
	"fail_on",
}

// amendWithSettingsHint checks whether a YAML strict-parse error mentions a
// known shared-settings key and, if so, returns an error that appends a
// targeted hint. If no match, the original error is returned unchanged.
func amendWithSettingsHint(err error) error {
	msg := err.Error()
	for _, key := range sharedSettingsKeys {
		if strings.Contains(msg, key) {
			return fmt.Errorf("%w — '%s' is a shared setting; shared settings belong in .atcr/config.yaml, not .atcr/registry.yaml", err, key)
		}
	}
	return err
}

// EntrySource records the tier (and defining file) an effective provider or
// agent came from, so validation errors can name the file and `atcr doctor`
// can show provenance.
type EntrySource struct {
	Tier string // SourceUser | SourceProject
	File string // display label of the defining file
}

// ProjectRegistry is the project-level provider/agent overlay from
// .atcr/registry.yaml. It carries definitions only — shared settings stay in
// .atcr/config.yaml — so the two project files keep distinct roles. It reuses
// the Provider and AgentConfig shapes (including the Epic 1.1 reserved fields)
// and is parsed with the same strict (KnownFields) decoder.
type ProjectRegistry struct {
	Providers map[string]Provider    `yaml:"providers,omitempty"`
	Agents    map[string]AgentConfig `yaml:"agents,omitempty"`
}

// DefaultProjectRegistryPath returns .atcr/registry.yaml under root.
func DefaultProjectRegistryPath(root string) string {
	return filepath.Join(root, ".atcr", "registry.yaml")
}

// parseRegistryFile reads and strictly parses a user-level registry, stamping
// every entry with the user source tier. Validation is the caller's job — done
// standalone by LoadRegistry, or over the merged view by LoadMergedRegistry.
func parseRegistryFile(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("registry not found at %s: run 'atcr init' and create your provider/agent registry (see docs/registry.md)", path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading registry: %w", err)
	}

	base := filepath.Base(path)
	var reg Registry
	if err := decodeStrictYAML(data, &reg); err != nil {
		if errors.Is(err, errEmptyDocument) {
			return nil, fmt.Errorf("%s is empty: define providers and agents", base)
		}
		return nil, fmt.Errorf("failed to parse %s: %w", base, amendWithAgentFieldHints(err, data))
	}
	reg.stampSource(SourceUser)
	return &reg, nil
}

// LoadProjectRegistry reads and strictly parses the project registry overlay
// at path. An absent or empty file is NOT an error: the overlay is optional and
// yields a nil *ProjectRegistry so callers fall back to the user registry.
func LoadProjectRegistry(path string) (*ProjectRegistry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading project registry: %w", err)
	}

	var pr ProjectRegistry
	if err := decodeStrictYAML(data, &pr); err != nil {
		if errors.Is(err, errEmptyDocument) {
			return nil, nil // an empty overlay file is treated as no overlay
		}
		return nil, fmt.Errorf("failed to parse %s: %w", projectRegistryLabel, amendWithAgentFieldHints(amendWithSettingsHint(err), data))
	}
	return &pr, nil
}

// LoadMergedRegistry loads the user registry at regPath, overlays the optional
// project registry at <root>/.atcr/registry.yaml (project entries shadow
// same-named user entries whole, by name; new names are added), enforces the
// trust gate for project-defined providers, and validates the merged view.
//
// Validation, fallback-chain, and range checks all run over the merged
// registry, so a project agent may fall back to a user agent and vice versa;
// errors name the file that defined the offending entry.
func LoadMergedRegistry(regPath, root string) (*Registry, error) {
	reg, err := parseRegistryFile(regPath)
	if err != nil {
		return nil, err
	}

	pr, err := LoadProjectRegistry(DefaultProjectRegistryPath(root))
	if err != nil {
		return nil, err
	}
	if pr != nil {
		reg.mergeProject(pr)
	}

	if err := reg.validateMerged(); err != nil {
		return nil, err
	}

	// Security gate: a project-defined provider must be explicitly trusted
	// before it can ever receive a key. No-op when no project providers exist
	// (project agents on user providers pass freely).
	if err := reg.enforceProjectTrust(filepath.Dir(regPath)); err != nil {
		return nil, err
	}

	reg.applyDefaults()
	return reg, nil
}

// stampSource tags every current provider and agent with the given source
// tier and its file label, initializing the source maps if needed.
func (r *Registry) stampSource(tier string) {
	if r.ProviderSource == nil {
		r.ProviderSource = make(map[string]EntrySource, len(r.Providers))
	}
	if r.AgentSource == nil {
		r.AgentSource = make(map[string]EntrySource, len(r.Agents))
	}
	src := EntrySource{Tier: tier, File: registryLabelFor(tier)}
	for name := range r.Providers {
		r.ProviderSource[name] = src
	}
	for name := range r.Agents {
		r.AgentSource[name] = src
	}
}

// AgentTier returns the source tier (user | project) recorded for an agent,
// defaulting to user when no overlay merge stamped it (e.g. a registry built
// directly without LoadMergedRegistry).
func (r *Registry) AgentTier(name string) string {
	if src, ok := r.AgentSource[name]; ok && src.Tier != "" {
		return src.Tier
	}
	return SourceUser
}

// registryLabelFor maps a source tier to the file label used in messages.
func registryLabelFor(tier string) string {
	if tier == SourceProject {
		return projectRegistryLabel
	}
	return userRegistryLabel
}

// mergeProject overlays a project registry onto r: project providers and agents
// replace same-named user entries whole (no field-level merge) and new names
// are added. Source tiers are restamped so provenance is visible downstream.
func (r *Registry) mergeProject(pr *ProjectRegistry) {
	if r.Providers == nil {
		r.Providers = map[string]Provider{}
	}
	if r.Agents == nil {
		r.Agents = map[string]AgentConfig{}
	}
	if r.ProviderSource == nil {
		r.ProviderSource = map[string]EntrySource{}
	}
	if r.AgentSource == nil {
		r.AgentSource = map[string]EntrySource{}
	}
	src := EntrySource{Tier: SourceProject, File: projectRegistryLabel}
	for name, p := range pr.Providers {
		r.Providers[name] = p
		r.ProviderSource[name] = src
	}
	for name, a := range pr.Agents {
		r.Agents[name] = a
		r.AgentSource[name] = src
	}
}

// validateMerged runs the standard validation and fallback-chain checks over
// the merged registry, attributing any entry-specific failure to the file that
// defined the offending entry (project vs user).
func (r *Registry) validateMerged() error {
	// Staged intentionally (see LoadRegistry): validate() precedes ValidateFallbacks()
	// with an early return. Epic 4.2 AC6 accumulation is within-check, not across this
	// boundary — fallback-chain checks assume structurally-valid agents, so running
	// them on a malformed registry would surface misleading errors.
	if err := r.validate(); err != nil {
		return r.attribute(err)
	}
	if err := r.ValidateFallbacks(); err != nil {
		return r.attribute(err)
	}
	return nil
}
