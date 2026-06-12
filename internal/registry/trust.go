package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// TrustStoreFile is the user-level allow file for project-defined providers,
// co-located with registry.yaml under ~/.config/atcr/.
const TrustStoreFile = "trusted_providers.yaml"

// ErrUntrustedProvider is the sentinel for the project-provider trust gate, so
// callers (and the `atcr trust` command) can recognize the failure class.
var ErrUntrustedProvider = errors.New("untrusted project-defined provider")

// providerTrustHash pins a provider by the sha256 of (base_url, api_key_env).
// The NUL separator prevents any base_url/key-env pair from forging another's
// digest. Changing either field changes the hash, so trust must be re-granted —
// this is what stops a cloned repo from silently redirecting a key to a new
// endpoint.
func providerTrustHash(p Provider) string {
	sum := sha256.Sum256([]byte(p.BaseURL + "\x00" + p.APIKeyEnv))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// trustEntry is one pinned provider. base_url and api_key_env are stored for
// human audit of the file; the gate compares the hash, which is authoritative.
type trustEntry struct {
	Provider  string `yaml:"provider"`
	BaseURL   string `yaml:"base_url,omitempty"`
	APIKeyEnv string `yaml:"api_key_env"`
	Hash      string `yaml:"hash"`
}

type trustFile struct {
	Trusted []trustEntry `yaml:"trusted"`
}

// TrustStore is the parsed allow file: the entries plus an index of their
// hashes for O(1) membership checks.
type TrustStore struct {
	path    string
	entries []trustEntry
	hashes  map[string]bool
}

// DefaultTrustStorePath returns the trust store path inside the registry dir.
func DefaultTrustStorePath(regDir string) string {
	return filepath.Join(regDir, TrustStoreFile)
}

// LoadTrustStore reads and strictly parses the trust store at path. An absent
// or empty file is NOT an error — it yields an empty store (nothing trusted).
func LoadTrustStore(path string) (*TrustStore, error) {
	store := &TrustStore{path: path, hashes: map[string]bool{}}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading trust store: %w", err)
	}

	var tf trustFile
	if err := decodeStrictYAML(data, &tf); err != nil {
		if errors.Is(err, errEmptyDocument) {
			return store, nil
		}
		return nil, fmt.Errorf("failed to parse %s: %w", TrustStoreFile, err)
	}
	for _, e := range tf.Trusted {
		// Recompute the expected hash from the audit columns. If they disagree
		// the entry was hand-edited or restored from a different provider —
		// reject it so the audit columns cannot be used to spoof trust.
		expected := providerTrustHash(Provider{BaseURL: e.BaseURL, APIKeyEnv: e.APIKeyEnv})
		if e.Hash != expected {
			_, _ = fmt.Fprintf(os.Stderr, "atcr: trust store: skipping entry %q — hash mismatch (recorded %s, expected %s)\n", e.Provider, e.Hash, expected)
			continue
		}
		store.entries = append(store.entries, e)
		store.hashes[e.Hash] = true
	}
	return store, nil
}

// IsTrusted reports whether p's pinned hash is present in the store.
func (s *TrustStore) IsTrusted(p Provider) bool {
	return s.hashes[providerTrustHash(p)]
}

// Trust adds a pin for provider p under the given name. It is idempotent: a
// provider whose (base_url, api_key_env) is already pinned is not duplicated.
func (s *TrustStore) Trust(name string, p Provider) {
	h := providerTrustHash(p)
	if s.hashes[h] {
		return
	}
	s.entries = append(s.entries, trustEntry{
		Provider:  name,
		BaseURL:   p.BaseURL,
		APIKeyEnv: p.APIKeyEnv,
		Hash:      h,
	})
	s.hashes[h] = true
}

// Save writes the trust store to disk with restrictive (0600) permissions,
// creating the parent directory if needed. The write is atomic (temp file +
// rename in the same dir) so a crash can never truncate the file into a
// corrupt, review-blocking state. Concurrency note: the rename prevents
// partial/corrupt files but does NOT serialize Load→Trust→Save sequences —
// two concurrent `atcr trust` runs may each load the old store, add their
// own entry, and the second rename clobbers the first's grant (lost update).
// Given CLI one-shot usage, this is acceptable; a file lock would be needed
// if concurrent trust grants become a real scenario.
func (s *TrustStore) Save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating trust store dir: %w", err)
	}
	body, err := yaml.Marshal(trustFile{Trusted: s.entries})
	if err != nil {
		return fmt.Errorf("encoding trust store: %w", err)
	}
	header := "# atcr trusted project-defined providers — see docs/registry.md\n" +
		"# Each entry pins a .atcr/registry.yaml provider by the sha256 of its\n" +
		"# (base_url, api_key_env). Remove an entry to revoke trust.\n"
	content := append([]byte(header), body...)

	tmp, err := os.CreateTemp(dir, ".trusted_providers-*.tmp")
	if err != nil {
		return fmt.Errorf("creating trust store temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("securing trust store temp: %w", err)
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing trust store temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing trust store temp: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replacing trust store: %w", err)
	}
	return nil
}

// ProviderRef is a project provider's identifying fields for the trust gate
// error and the first-use banner.
type ProviderRef struct {
	Name      string
	BaseURL   string
	APIKeyEnv string
}

// Line returns the provider identification line used across the gate error,
// the first-use banner, and the `atcr trust` command. A single shared
// formatter prevents the -> vs → drift and ensures any change to how a
// provider is identified is edited in one place. Uses ASCII -> to keep the
// banner (printed to stderr) safe for legacy/Windows consoles.
func (r ProviderRef) Line() string {
	return fmt.Sprintf("%s -> base_url=%s  api_key_env=%s", r.Name, r.BaseURL, r.APIKeyEnv)
}

// projectProviders returns the project-tier providers in the merged registry,
// sorted by name for deterministic output.
func (r *Registry) projectProviders() []ProviderRef {
	var refs []ProviderRef
	for name, src := range r.ProviderSource {
		if src.Tier != SourceProject {
			continue
		}
		p := r.Providers[name]
		refs = append(refs, ProviderRef{Name: name, BaseURL: p.BaseURL, APIKeyEnv: p.APIKeyEnv})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs
}

// untrustedProviderError reports project-defined providers missing from the
// trust store. It names each provider's base_url + key env and the remedy, and
// wraps ErrUntrustedProvider for errors.Is.
type untrustedProviderError struct {
	refs []ProviderRef
}

func (e *untrustedProviderError) Error() string {
	var b strings.Builder
	b.WriteString("untrusted project-defined provider(s) in " + projectRegistryLabel + ":\n")
	for _, r := range e.refs {
		fmt.Fprintf(&b, "  - %s\n", r.Line())
	}
	b.WriteString("a cloned repo cannot direct your key to these endpoints until you authorize them — run 'atcr trust' to review and approve")
	return b.String()
}

func (e *untrustedProviderError) Unwrap() error { return ErrUntrustedProvider }

// enforceProjectTrust blocks any project-defined provider not present in the
// trust store under userRegDir. It is the security gate: a cloned repo cannot
// direct a key to an arbitrary endpoint until the user runs `atcr trust`. It is
// a no-op when no project providers are present (project agents that reference
// user providers need no trust).
//
// userRegDir is the directory containing the user-level registry.yaml. The
// trust store MUST live beside it (never under the project root) so that the
// (registry path, trust store path) pairing cannot drift — see cmd/atcr/trust.go
// which resolves the same pair independently.
func (r *Registry) enforceProjectTrust(userRegDir string) error {
	refs := r.projectProviders()
	if len(refs) == 0 {
		return nil
	}
	store, err := LoadTrustStore(DefaultTrustStorePath(userRegDir))
	if err != nil {
		return err
	}
	var untrusted []ProviderRef
	for _, ref := range refs {
		if !store.IsTrusted(r.Providers[ref.Name]) {
			untrusted = append(untrusted, ref)
		}
	}
	if len(untrusted) == 0 {
		return nil
	}
	return &untrustedProviderError{refs: untrusted}
}

// ProjectProviderBanner returns a loud first-use banner naming each active
// project-defined provider's base_url and key env, or "" when there are none.
// Callers print it to stderr after a successful load (UX confirmation that the
// gate passed — not the security control itself).
func (r *Registry) ProjectProviderBanner() string {
	refs := r.projectProviders()
	if len(refs) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "WARNING: Using project-defined provider(s) from %s:\n", projectRegistryLabel)
	for _, ref := range refs {
		fmt.Fprintf(&b, "  - %s\n", ref.Line())
	}
	b.WriteString("  These endpoints are authorized to receive the named API keys (trusted via 'atcr trust').")
	return b.String()
}
