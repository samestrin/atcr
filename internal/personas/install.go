package personas

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/registry"
)

// Install fetches the named persona from baseURL, validates it with the registry
// agent validator, and writes it to destDir/<name>.yaml. Validation runs BEFORE
// any disk write, so malformed or malicious community YAML never reaches disk.
// Re-installing an existing persona overwrites it.
//
// A "bundle/"-prefixed name is rejected here (defense in depth): a bundle must
// be expanded via InstallBundle so it never round-trips through the
// single-persona fetch path.
func Install(client HTTPClient, baseURL, name, destDir string) error {
	if strings.HasPrefix(name, "bundle/") {
		return fmt.Errorf("%q is a bundle; install it via the bundle path, not as a single persona", name)
	}
	dest, err := personaPath(destDir, name)
	if err != nil {
		return err
	}
	data, err := FetchPersonaYAML(client, baseURL, name)
	if err != nil {
		return err
	}
	if err := registry.ValidateCommunityPersonaYAML(name, data); err != nil {
		return fmt.Errorf("persona %q failed validation: %w", name, err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return fmt.Errorf("failed to create personas directory: %w", err)
	}
	// writeFileAtomic provides the same symlink guard, temp-file staging, and
	// rename-into-place behavior that used to be inlined here — keep one copy
	// of the security-sensitive write path so hardening stays uniform.
	return writeFileAtomic(dest, data)
}

// personaInstalled reports whether a persona file already exists at dest. A
// stat error other than "not exists" is surfaced so a bundle install fails
// loudly on a permission problem rather than silently re-fetching.
func personaInstalled(dest string) (bool, error) {
	if _, err := os.Stat(dest); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("cannot access personas directory: %w", err)
	}
	return true, nil
}
