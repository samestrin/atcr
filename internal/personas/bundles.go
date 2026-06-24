package personas

import (
	"embed"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// bundleFS holds the embedded bundle manifests. Only the declared *.yaml files
// under bundles/ are embedded; nothing user-controlled is read at runtime.
//
//go:embed bundles/*.yaml
var bundleFS embed.FS

// ErrUnknownBundle is returned by Resolve and InstallBundle when a bundle name
// does not correspond to an embedded manifest. Callers match it with errors.Is
// rather than string-matching the message.
var ErrUnknownBundle = errors.New("unknown bundle")

// BundleManifest is a parsed bundle YAML: a name, an optional description, and
// the member persona identifiers the bundle installs.
type BundleManifest struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Personas    []string `yaml:"personas"`
}

// parseManifest decodes a bundle manifest and validates required fields. A
// missing name or an empty personas list is a descriptive error (never a panic);
// malformed YAML is wrapped. Extra unknown fields are ignored (yaml.v3 default).
func parseManifest(data []byte) (*BundleManifest, error) {
	var m BundleManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse bundle manifest: %w", err)
	}
	if strings.TrimSpace(m.Name) == "" {
		return nil, fmt.Errorf("bundle manifest missing required field: name")
	}
	if len(m.Personas) == 0 {
		return nil, fmt.Errorf("bundle manifest %q has no personas", m.Name)
	}
	return &m, nil
}

// Resolve expands a bundle name (without the "bundle/" prefix) into its member
// persona identifiers. An unregistered, empty, mixed-case, or traversal name
// returns ErrUnknownBundle with no filesystem access — the embedded-manifest
// lookup is the only gate. Names are case-sensitive; no normalization.
func Resolve(name string) ([]string, error) {
	if !isValidBundleName(name) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownBundle, name)
	}
	data, err := bundleFS.ReadFile("bundles/" + name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownBundle, name)
	}
	m, err := parseManifest(data)
	if err != nil {
		return nil, fmt.Errorf("bundle %q: %w", name, err)
	}
	return m.Personas, nil
}

// isValidBundleName reports whether name is a flat, safe bundle identifier.
func isValidBundleName(name string) bool {
	if name == "" {
		return false
	}
	matched := true
	for _, r := range name {
		isAllowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-'
		if !isAllowed {
			matched = false
			break
		}
	}
	return matched
}

// BundleOutcome reports the result of one bundle member during InstallBundle.
type BundleOutcome struct {
	Name           string
	AlreadyPresent bool  // member was already on disk; not re-fetched
	Err            error // fetch/validation/write failure for this member (others continue)
}

// InstallBundle resolves bundleName and installs each member into destDir,
// skipping members already present (idempotent) and continuing past a member
// that fails so one bad download never aborts the rest. It returns one
// BundleOutcome per member in manifest order. An unknown bundle returns
// (nil, ErrUnknownBundle) before any filesystem access.
func InstallBundle(client HTTPClient, baseURL, bundleName, destDir string) ([]BundleOutcome, error) {
	members, err := Resolve(bundleName)
	if err != nil {
		return nil, err
	}
	outcomes := make([]BundleOutcome, 0, len(members))
	for _, member := range members {
		out := BundleOutcome{Name: member}
		dest, perr := personaPath(destDir, member)
		if perr != nil {
			out.Err = perr
			outcomes = append(outcomes, out)
			continue
		}
		if installed, serr := personaInstalled(dest); serr != nil {
			out.Err = serr
			outcomes = append(outcomes, out)
			continue
		} else if installed {
			out.AlreadyPresent = true
			outcomes = append(outcomes, out)
			continue
		}
		if ierr := Install(client, baseURL, member, destDir); ierr != nil {
			out.Err = ierr
		}
		outcomes = append(outcomes, out)
	}
	return outcomes, nil
}
