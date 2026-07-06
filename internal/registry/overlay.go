package registry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
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

// registryURLEnv, when set to an http/https URL, makes parseRegistryFile fetch
// the user registry over the network instead of reading the local file. This is
// the "shared team registry" seam (Epic 19.2): a team points every workstation
// at one registry.yaml in a config repo. Only the *user* registry is remote —
// the project overlay (.atcr/registry.yaml) and the trust store stay local.
const registryURLEnv = "ATCR_REGISTRY_URL"

// The following vars are test seams for remote registry fetching. Tests that
// mutate them must not call t.Parallel(), because concurrent mutations would
// race. No production code currently calls these in parallel.

// remoteFetchTimeout bounds a single registry fetch. It is deliberately shorter
// than llmclient's completion budget (that budgets a whole chat completion; this
// is a one-shot YAML GET) and is a var so tests can lower it.
var remoteFetchTimeout = 10 * time.Second

// remoteRegistryBodyLimit caps the fetched body to guard against a hostile or
// misconfigured endpoint streaming an unbounded response. A registry.yaml is a
// few KB; 5 MB is far above any realistic size. A var so tests can lower it.
var remoteRegistryBodyLimit int64 = 5 * 1024 * 1024

// insecureRegistryWarnWriter is the sink for the one-time non-https warning; a
// var so tests can capture it. insecureRegistryWarnSeen tracks URLs that have
// already drawn the warning so each distinct insecure registry URL warns once.
var (
	insecureRegistryWarnWriter io.Writer = os.Stderr
	insecureRegistryWarnSeen   sync.Map
)

// parseRegistryFile reads and strictly parses a user-level registry, stamping
// every entry with the user source tier. Validation is the caller's job — done
// standalone by LoadRegistry, or over the merged view by LoadMergedRegistry.
//
// The bytes come from the remote URL in ATCR_REGISTRY_URL when it is set, or the
// local path otherwise; see loadRegistryBytes.
func parseRegistryFile(path string) (*Registry, error) {
	data, base, err := loadRegistryBytes(path)
	if err != nil {
		return nil, err
	}

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

// loadRegistryBytes returns the raw user-registry bytes and a display label for
// error attribution. When ATCR_REGISTRY_URL is set the registry is fetched over
// HTTP and the local path is ignored; otherwise the local file is read.
//
// There is NO silent fallback to the local file when a set URL is unreachable or
// invalid — a present-but-broken remote source is an unconditional error, so a
// team never diverges onto a stale local copy without noticing. The local read
// is reached only when the env var is unset (matching Epic 19.2's "falling back
// ... when unset").
func loadRegistryBytes(path string) (data []byte, label string, err error) {
	if rawURL := strings.TrimSpace(os.Getenv(registryURLEnv)); rawURL != "" {
		data, err = fetchRemoteRegistry(rawURL)
		return data, remoteRegistryLabel(rawURL), err
	}

	data, err = os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", fmt.Errorf("registry not found at %s: run 'atcr init' and create your provider/agent registry (see docs/registry.md)", path)
	}
	if err != nil {
		return nil, "", fmt.Errorf("reading registry: %w", err)
	}
	return data, filepath.Base(path), nil
}

// fetchRemoteRegistry GETs the user registry from rawURL, returning its body on
// a 2xx response. The scheme must be http or https; a non-https URL draws a
// one-time warning (the fetched registry is fully trusted, unlike a project
// provider). Errors reference the env var name, never the URL value, so a token
// embedded in a query string cannot leak into an error message.
func fetchRemoteRegistry(rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", registryURLEnv, err)
	}
	switch u.Scheme {
	case "https":
	case "http":
		warnInsecureRegistryURLOnce(rawURL)
	default:
		return nil, fmt.Errorf("%s must be an http or https URL (got scheme %q)", registryURLEnv, u.Scheme)
	}

	ctx, cancel := context.WithTimeout(context.Background(), remoteFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building %s request: %w", registryURLEnv, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// A *url.Error stringifies with the full request URL — query string and
		// all — so wrapping it verbatim would leak a token embedded in the URL
		// even over https. Unwrap to the underlying cause so the message names
		// only the env var, honoring the redaction guarantee above.
		cause := err
		var ue *url.Error
		if errors.As(err, &ue) {
			cause = ue.Err
		}
		return nil, fmt.Errorf("fetching registry from %s: %w", registryURLEnv, cause)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetching registry from %s: unexpected status %s", registryURLEnv, resp.Status)
	}

	// Read at most one byte past the limit so an oversized body is detectable.
	body, err := io.ReadAll(io.LimitReader(resp.Body, remoteRegistryBodyLimit+1))
	if err != nil {
		return nil, fmt.Errorf("reading registry from %s: %w", registryURLEnv, err)
	}
	if int64(len(body)) > remoteRegistryBodyLimit {
		return nil, fmt.Errorf("registry from %s exceeds the %d-byte limit", registryURLEnv, remoteRegistryBodyLimit)
	}
	return body, nil
}

// warnInsecureRegistryURLOnce emits a single stderr warning when the registry is
// fetched over plaintext http. The URL is redacted of any embedded credentials
// before it is shown; each distinct redacted URL warns once.
func warnInsecureRegistryURLOnce(rawURL string) {
	redacted := redactRegistryURL(rawURL)
	if _, loaded := insecureRegistryWarnSeen.LoadOrStore(redacted, true); loaded {
		return
	}
	_, _ = fmt.Fprintf(insecureRegistryWarnWriter,
		"warning: %s uses insecure http (%s); prefer https for a shared registry\n",
		registryURLEnv, redacted)
}

// redactRegistryURL returns a display-safe form of rawURL with any embedded
// userinfo (user:password@), query string, and fragment removed, so neither a
// basic-auth credential nor a query-string token can reach a warning or error
// message. Mirrors resolveEndpoint's defensive redaction, extended to the query.
func redactRegistryURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// remoteRegistryLabel derives a short file label from a registry URL for error
// attribution (e.g. "registry.yaml"), falling back to the standard user label
// when the URL has no usable path segment. The query string is excluded, so a
// token there never reaches an error message.
func remoteRegistryLabel(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Path != "" {
		if i := strings.LastIndex(u.Path, "/"); i >= 0 && i+1 < len(u.Path) {
			return u.Path[i+1:]
		}
		if u.Path != "/" {
			return u.Path
		}
	}
	return userRegistryLabel
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
