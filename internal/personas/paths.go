package personas

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// personaNameRe constrains persona names to a safe character class. The dot is
// deliberately excluded, which alone defeats `..` traversal; the segment and
// absolute-path checks below are defense in depth.
var personaNameRe = regexp.MustCompile(`^[a-zA-Z0-9_/-]+$`)

// PersonasDir returns the per-user community personas directory
// (os.UserConfigDir()/atcr/personas). The directory is not created here.
func PersonasDir() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving user config dir: %w", err)
	}
	return filepath.Join(cfg, "atcr", "personas"), nil
}

// validatePersonaName rejects names that are empty, absolute, contain a
// path-traversal segment, or fall outside [a-zA-Z0-9_/-] — before the name is
// ever joined into a filesystem path or a fetch URL.
func validatePersonaName(name string) error {
	if name == "" {
		return fmt.Errorf("invalid persona name %q: must not be empty", name)
	}
	if !personaNameRe.MatchString(name) {
		return fmt.Errorf("invalid persona name %q: only letters, digits, '_', '-', and '/' are allowed", name)
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return fmt.Errorf("invalid persona name %q: must not be an absolute path", name)
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("invalid persona name %q: contains an invalid path segment", name)
		}
	}
	return nil
}

// personaPath validates name and maps it to its destination file under dir,
// confirming the cleaned result stays within dir (defense in depth beyond the
// name guard).
func personaPath(dir, name string) (string, error) {
	if err := validatePersonaName(name); err != nil {
		return "", err
	}
	p := filepath.Join(dir, filepath.FromSlash(name)+".yaml")
	rel, err := filepath.Rel(dir, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid persona name %q: escapes the personas directory", name)
	}
	return p, nil
}
