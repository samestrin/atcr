package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template/parse"

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

// allowedPersonaFields is the exact set of PayloadContext fields a fetched
// community persona prompt may reference as a bare `{{.Field}}` action (or as the
// condition of an {{if .ToolsEnabled}} block). Each is a flat read-only
// string/int/bool of the fixed render context — no nested struct, map, method,
// env, or secret is reachable — so a prompt using ONLY these cannot exfiltrate or
// execute anything. Any field chain (`.Payload.X`), pipeline, function call, or
// other construct is rejected by validatePersonaTemplateNode below.
var allowedPersonaFields = map[string]struct{}{
	"AgentName":    {},
	"ScopeRule":    {},
	"FileCount":    {},
	"BaseRef":      {},
	"HeadRef":      {},
	"PayloadMode":  {},
	"Payload":      {},
	"ToolsEnabled": {},
}

// ValidateFetchedPersonaPrompt enforces the C3 untrusted-input guardrails on a
// fetched/pinned community persona prompt: a length cap (MaxPersonaPromptLen) and
// a template allowlist. It parses the prompt with the REAL text/template parser
// (not a regex proxy) so an unbalanced/half-open action is rejected here at
// install/resolve rather than crashing every review at render time, and trim
// markers / interior whitespace are normalized by the parser exactly as the
// renderer sees them. It then walks the parse tree and permits only bare
// references to the known PayloadContext fields and {{if .ToolsEnabled}}…{{end}}
// blocks — the format the authoring contract mandates. Any other construct (a
// field chain, pipeline, function call, {{range}}/{{with}}/{{template}}, or a
// disallowed field) is rejected. Reject-all was wrong: it made every model-tuned
// community persona un-installable and un-resolvable (TD-010), contradicting
// Clarification C1. Rejection is a descriptive error, never a silent truncation
// or transform.
func ValidateFetchedPersonaPrompt(text string) error {
	if len(text) > MaxPersonaPromptLen {
		return fmt.Errorf("persona prompt exceeds maximum length of %d bytes", MaxPersonaPromptLen)
	}
	trees, err := parse.Parse("persona", text, "", "")
	if err != nil {
		return fmt.Errorf("persona prompt is not a valid template: %w", err)
	}
	// {{define}}/{{block}} create additional named trees beyond "persona"; a fetched
	// prompt may not define associated templates.
	if len(trees) != 1 {
		return fmt.Errorf("persona prompt contains a disallowed template definition ({{define}} or {{block}})")
	}
	tree := trees["persona"]
	if tree == nil || tree.Root == nil {
		return nil
	}
	return validatePersonaTemplateNodes(tree.Root)
}

// validatePersonaTemplateNodes validates every node in a parsed template list.
func validatePersonaTemplateNodes(list *parse.ListNode) error {
	if list == nil {
		return nil
	}
	for _, n := range list.Nodes {
		if err := validatePersonaTemplateNode(n); err != nil {
			return err
		}
	}
	return nil
}

// validatePersonaTemplateNode permits literal text, comments, a bare allowed-field
// action, and an {{if <allowed-field>}}…{{end}} block (recursively validated; the
// condition may be any allowlisted field — in practice {{if .ToolsEnabled}} — each
// being a safe scalar with no methods). Everything else — {{range}}, {{with}},
// {{template}}, a pipeline, a function call, or a disallowed/nested field — is
// rejected.
func validatePersonaTemplateNode(n parse.Node) error {
	switch node := n.(type) {
	case *parse.TextNode, *parse.CommentNode:
		return nil
	case *parse.ActionNode:
		return validatePersonaPipe(node.Pipe)
	case *parse.IfNode:
		if err := validatePersonaPipe(node.Pipe); err != nil {
			return err
		}
		if err := validatePersonaTemplateNodes(node.List); err != nil {
			return err
		}
		return validatePersonaTemplateNodes(node.ElseList)
	default:
		_ = node
		return fmt.Errorf("persona prompt contains a disallowed template construct; only the known persona variables and an {{if .ToolsEnabled}} block are permitted")
	}
}

// validatePersonaPipe permits exactly one command consisting of one bare field
// node whose single identifier is in the allowed set — i.e. {{.AgentName}},
// {{.Payload}}, {{if .ToolsEnabled}}, etc. — and rejects declarations, multi-stage
// pipelines, function calls, nested field chains, and non-field expressions.
func validatePersonaPipe(pipe *parse.PipeNode) error {
	if pipe == nil {
		return nil
	}
	if len(pipe.Decl) > 0 {
		return fmt.Errorf("persona prompt contains a disallowed template variable declaration")
	}
	if len(pipe.Cmds) != 1 {
		return fmt.Errorf("persona prompt contains a disallowed template pipeline")
	}
	cmd := pipe.Cmds[0]
	if len(cmd.Args) != 1 {
		return fmt.Errorf("persona prompt contains a disallowed template function or multi-argument command")
	}
	field, ok := cmd.Args[0].(*parse.FieldNode)
	if !ok {
		return fmt.Errorf("persona prompt contains a disallowed template expression; only bare persona fields are permitted")
	}
	if len(field.Ident) != 1 {
		return fmt.Errorf("persona prompt references a disallowed nested template field (.%s)", strings.Join(field.Ident, "."))
	}
	if _, ok := allowedPersonaFields[field.Ident[0]]; !ok {
		return fmt.Errorf("persona prompt references a disallowed template field (.%s)", field.Ident[0])
	}
	return nil
}

// validateCommunityPrompt enforces the C3 guardrails on a pinned-community persona
// prompt resolved from the Registry tier. It delegates to ValidateFetchedPersonaPrompt
// so the resolve-time and install-time (internal/personas) checks share one
// allowlist and can never drift.
func validateCommunityPrompt(text string) error {
	return ValidateFetchedPersonaPrompt(text)
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
