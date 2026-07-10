package personas

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/samestrin/atcr/internal/registry"
)

// personaNameRe constrains persona names to a safe character class. The dot is
// deliberately excluded, which alone defeats `..` traversal. Requiring an
// alphanumeric first character prevents separator-only names like "-", "_", or
// "//". The segment and absolute-path checks below are defense in depth.
var personaNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_/-]*$`)

// PersonasDir returns the per-user community personas directory. It MUST equal
// the resolver's Registry dir (filepath.Dir(DefaultRegistryPath())/personas) so a
// fetched persona lands on internal/registry.ResolvePersona's chain. It is derived
// from DefaultRegistryPath() — the same source the resolver uses — rather than
// os.UserConfigDir(), which on darwin resolves to ~/Library/Application Support and
// would strand installs in a directory the resolver never searches. The directory
// is not created here.
//
// Back-compat (TD-001): redefining this from os.UserConfigDir() moves the effective
// darwin dir from ~/Library/Application Support/atcr/personas to ~/.config/atcr/personas.
// No pre-public-launch back-compat migration is owed — the live install flow is not
// exercised until samestrin/atcr is public, so no real user has personas at the old
// path yet. A one-time move/symlink migration is deferred to a bounded fast-follow.
func PersonasDir() (string, error) {
	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return "", fmt.Errorf("resolving registry path: %w", err)
	}
	return filepath.Join(filepath.Dir(regPath), "personas"), nil
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

// ValidName reports whether name passes the community persona-name guard
// (validatePersonaName). It is the exported wrapper cmd/atcr uses to pre-filter
// fetched community-index entries, so one malformed/empty index name is skipped with
// a warning rather than reaching InstallUnit->validatePersonaName and tripping the
// all-or-nothing rollback that would abort init/quickstart for every user.
func ValidName(name string) bool {
	return validatePersonaName(name) == nil
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
