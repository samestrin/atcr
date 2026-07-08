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
//  1. taskMessage (programmatic override): an internal resolution seam; if
//     non-nil it wins outright, even when empty (an explicit "no system prompt").
//     It is not exposed as a CLI flag, so ordinary CLI and MCP runs pass nothing
//     here and resolution effectively begins at level 2.
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
		// A namespaced persona (security/owasp) traverses intermediate directories
		// under dir. readNonEmpty refuses a symlinked LEAF, but the OS would still
		// follow a symlinked intermediate component, so a planted namespace symlink
		// could read outside dir. Refuse a symlinked intermediate (treated as "not
		// present", so resolution falls through) to keep the whole path within dir.
		if bad, err := hasSymlinkedParent(dir, persona); err != nil {
			return ResolvedPersona{}, err
		} else if bad {
			continue
		}
		path := filepath.Join(dir, persona+".md")
		text, ok, err := readNonEmpty(path)
		if err != nil {
			return ResolvedPersona{}, err
		}
		if ok {
			// The Registry (pinned-community) tier is untrusted fetched content:
			// defensively enforce the C3 guardrails on a resolved <persona>.md so a
			// fetched (or hand-dropped) oversized or {{ }}-bearing custom prompt is
			// rejected even if it bypassed the install-time check. Scope note: this
			// guards the per-persona custom prompt only. The shared _base.md
			// structural template (level 4 below) is intentionally NOT metachar-
			// restricted — a base template must contain {{.Payload}} to function —
			// and cannot be fetched anyway (the name "_base" fails validation on the
			// install/fetch path). The trusted Project tier is likewise exempt (it
			// may use template variables exactly like the embedded built-ins).
			if dir == dirs.Registry {
				if err := validateCommunityPrompt(text); err != nil {
					return ResolvedPersona{}, fmt.Errorf("community persona %q at %s: %w", persona, path, err)
				}
			}
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

// validateCommunityPrompt enforces the C3 untrusted-input guardrails on a
// pinned-community persona prompt resolved from the Registry tier: a length cap
// mirroring MaxExecutorSystemPromptLen, and a reject bar on template
// metacharacters ({{ or }}) so a fetched prompt can never drive template
// expansion. It mirrors the install-time check in internal/personas so a file
// dropped straight into the pin dir is caught too.
func validateCommunityPrompt(text string) error {
	if len(text) > MaxExecutorSystemPromptLen {
		return fmt.Errorf("persona prompt exceeds maximum length of %d bytes", MaxExecutorSystemPromptLen)
	}
	if strings.Contains(text, "{{") || strings.Contains(text, "}}") {
		return fmt.Errorf("persona prompt contains template metacharacters ({{ or }})")
	}
	return nil
}

// validateName rejects names that could escape the persona directory. A single
// forward slash namespace is allowed (e.g. "security/owasp") so a namespaced
// community persona resolves to its nested <namespace>/<name>.md — matching the
// install path (internal/personas.validatePersonaName, which uses the same
// [a-zA-Z0-9_/-] character class). Each "/"-separated segment is validated
// independently: no "" / "." / ".." segment (defeats traversal), no leading dot
// (dotfiles), and no "_base" segment (reserved for the shared base template).
// Backslashes and absolute paths are refused outright. These guarantees keep
// filepath.Join(dir, name+".md") strictly within dir.
func validateName(kind, name string) error {
	if name == "" {
		return fmt.Errorf("%s name must not be empty", kind)
	}
	if strings.ContainsRune(name, '\\') {
		return fmt.Errorf("invalid %s name %q: must not contain a backslash", kind, name)
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return fmt.Errorf("invalid %s name %q: must not be an absolute path", kind, name)
	}
	for _, seg := range strings.Split(name, "/") {
		switch {
		case seg == "" || seg == "." || seg == "..":
			return fmt.Errorf("invalid %s name %q: contains an invalid path segment", kind, name)
		case strings.HasPrefix(seg, "."):
			return fmt.Errorf("invalid %s name %q: a path segment must not start with a dot", kind, name)
		case seg == "_base":
			return fmt.Errorf("invalid %s name %q: \"_base\" is reserved for the shared base template", kind, name)
		}
	}
	return nil
}

// hasSymlinkedParent reports whether any intermediate directory component of a
// namespaced persona (the "/"-separated segments before the leaf) under dir is a
// symlink. The leaf itself is guarded by readNonEmpty; this closes the
// intermediate-component symlink-escape that a multi-segment path would otherwise
// allow. A flat name has no intermediate components, so this is a no-op for it.
// A missing intermediate is not a symlink (nothing to read) and returns false.
func hasSymlinkedParent(dir, name string) (bool, error) {
	segs := strings.Split(name, "/")
	cur := dir
	for _, seg := range segs[:len(segs)-1] { // intermediate dirs only, not the leaf
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("stat persona path %s: %w", cur, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "warning: persona path component %s is a symlink, skipping for safety\n", cur)
			return true, nil
		}
	}
	return false, nil
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
