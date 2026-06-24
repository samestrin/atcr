package personas

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/samestrin/atcr/internal/registry"
)

// Install fetches the named persona from baseURL, validates it with the registry
// agent validator, and writes it to destDir/<name>.yaml. Validation runs BEFORE
// any disk write, so malformed or malicious community YAML never reaches disk.
// Re-installing an existing persona overwrites it.
func Install(client HTTPClient, baseURL, name, destDir string) error {
	dest, err := personaPath(destDir, name)
	if err != nil {
		return err
	}
	data, err := FetchPersonaYAML(client, baseURL, name)
	if err != nil {
		return err
	}
	if err := registry.ValidateAgentYAML(name, data); err != nil {
		return fmt.Errorf("persona %q failed validation: %w", name, err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("failed to create personas directory: %w", err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return fmt.Errorf("failed to write persona to %s: %w", dest, err)
	}
	return nil
}
