package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/personas"
)

// PersonaDirs names the two on-disk persona search roots. Project takes
// precedence over Registry. Callers supply the concrete paths (typically
// <root>/.atcr/personas and <config>/atcr/personas).
type PersonaDirs struct {
	Project  string
	Registry string
}

// ResolvedPersona is a resolved persona prompt with the origin it came from
// (a file path, "task-message", or "embedded:<name>") for error attribution.
type ResolvedPersona struct {
	Text   string
	Source string
}

// ErrPersonaNotFound is returned when an explicit persona ref resolves nowhere.
var ErrPersonaNotFound = errors.New("persona not found")

// ResolvePersona walks the six-level resolution chain:
//
//  1. taskMessage (the --task-message flag): if non-nil it wins outright, even
//     when empty (an explicit "no system prompt").
//  2. <persona>.md in the project personas dir.
//  3. <persona>.md in the registry personas dir.
//  4. _base.md in the project dir, then the registry dir.
//  5. embedded <agentName>.md, then embedded _base.md.
//
// When persona names a value other than agentName (an explicit ref) and no file
// exists at level 2 or 3, resolution fails with ErrPersonaNotFound — an explicit
// ref never silently falls through. Empty files are treated as missing (with a
// stderr warning). Persona/agent names are sanitized against path traversal.
func ResolvePersona(agentName, persona string, taskMessage *string, dirs PersonaDirs) (ResolvedPersona, error) {
	// taskMessage wins outright, so the persona/agent names are intentionally
	// not validated on this path — they are never used to touch the filesystem.
	if taskMessage != nil {
		return ResolvedPersona{Text: *taskMessage, Source: "task-message"}, nil
	}
	if err := validateName("agent", agentName); err != nil {
		return ResolvedPersona{}, err
	}
	if err := validateName("persona", persona); err != nil {
		return ResolvedPersona{}, err
	}

	// Levels 2-3: <persona>.md in project then registry dir.
	for _, dir := range []string{dirs.Project, dirs.Registry} {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, persona+".md")
		text, ok, err := readNonEmpty(path)
		if err != nil {
			return ResolvedPersona{}, err
		}
		if ok {
			return ResolvedPersona{Text: text, Source: path}, nil
		}
	}

	// An explicit persona ref (distinct from the agent name) must resolve to a
	// file; it never falls through to _base or embedded defaults.
	if persona != agentName {
		return ResolvedPersona{}, fmt.Errorf("persona %q not found in %s or %s: %w",
			persona, dirs.Project, dirs.Registry, ErrPersonaNotFound)
	}

	// Level 4: _base.md in project then registry dir.
	for _, dir := range []string{dirs.Project, dirs.Registry} {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, "_base.md")
		text, ok, err := readNonEmpty(path)
		if err != nil {
			return ResolvedPersona{}, err
		}
		if ok {
			return ResolvedPersona{Text: text, Source: path}, nil
		}
	}

	// Level 5: embedded <agentName>.md, then embedded _base.md.
	if text, err := personas.Get(agentName); err == nil && strings.TrimSpace(text) != "" {
		return ResolvedPersona{Text: text, Source: "embedded:" + agentName}, nil
	}
	base, err := personas.Base()
	if err != nil {
		return ResolvedPersona{}, fmt.Errorf("internal error: no persona found for agent '%s' — embedded default missing: %w", agentName, err)
	}
	return ResolvedPersona{Text: base, Source: "embedded:_base"}, nil
}

// validateName rejects names that could escape the persona directory via path
// separators or "..", a leading dot (dotfiles / "."/".."), and the reserved
// "_base" (which names the shared base template, not a persona). Persona files
// are looked up by simple base name only.
func validateName(kind, name string) error {
	if name == "" {
		return fmt.Errorf("%s name must not be empty", kind)
	}
	if name != filepath.Base(name) || strings.Contains(name, "..") ||
		strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\\') {
		return fmt.Errorf("invalid %s name %q: must not contain path separators", kind, name)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("invalid %s name %q: must not start with a dot", kind, name)
	}
	if name == "_base" {
		return fmt.Errorf("invalid %s name %q: \"_base\" is reserved for the shared base template", kind, name)
	}
	return nil
}

// readNonEmpty reads path, treating a missing file, a symlink, or an
// empty/whitespace-only file as "not present" (the latter two with a stderr
// warning so the fall-through is visible). Symlinks are refused because persona
// text is fed verbatim into LLM prompts; following one could exfiltrate an
// arbitrary file. Non-NotExist errors are surfaced.
func readNonEmpty(path string) (string, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("stat persona file %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintf(os.Stderr, "warning: persona file %s is a symlink, skipping for safety\n", path)
		return "", false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("reading persona file %s: %w", path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		fmt.Fprintf(os.Stderr, "warning: persona file %s is empty, using fallback\n", path)
		return "", false, nil
	}
	return string(data), true, nil
}
