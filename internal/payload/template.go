package payload

import (
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

// PayloadContext is the typed data passed to a persona prompt template. It is
// the single source of truth for the variables a persona may reference; adding
// a variable means adding a field here. ScopeRule carries the per-payload-mode
// scope instruction injected by the engine (it is data, selected by ScopeRule()
// from PayloadMode, not authored by the persona).
type PayloadContext struct {
	AgentName   string
	BaseRef     string
	HeadRef     string
	FileCount   int
	PayloadMode string
	Payload     string
	ScopeRule   string
	// ToolsEnabled is true for tool-using reviewer agents (Epic 2.0). It gates
	// {{if .ToolsEnabled}} persona sections (tool-exploration guidance and the
	// evidence-citation rule, authored in Story 6); it defaults to false so
	// non-tool personas render exactly as in 1.x.
	ToolsEnabled bool
}

// renderPhase distinguishes the two failure points of RenderPrompt.
type renderPhase int

const (
	phaseParse renderPhase = iota
	phaseExec
)

// RenderError carries which phase failed, the unknown field name (when the
// failure is an undefined template variable), and the underlying text/template
// error (which includes the template line number). Callers in the persona
// resolution layer introspect it to build path-qualified messages.
type RenderError struct {
	phase renderPhase
	Field string
	Err   error
}

func (e *RenderError) Error() string {
	if e.phase == phaseParse {
		return fmt.Sprintf("failed to parse persona prompt template: %v", e.Err)
	}
	if e.Field != "" {
		return fmt.Sprintf("template references unknown variable '%s'", e.Field)
	}
	return fmt.Sprintf("failed to render persona prompt template: %v", e.Err)
}

func (e *RenderError) Unwrap() error { return e.Err }

// IsParse reports whether the failure was at template-parse time.
func (e *RenderError) IsParse() bool { return e.phase == phaseParse }

// unknownFieldRe extracts the offending field name from text/template's
// "can't evaluate field X in type ..." execution error.
var unknownFieldRe = regexp.MustCompile(`can't evaluate field (\w+) in type`)

// RenderPrompt parses text as a persona prompt template and executes it against
// ctx (always a PayloadContext struct). Unknown struct fields fail at execution
// regardless of missingkey; Option("missingkey=error") only guards map keys and
// is kept defensively for any future map-based context. Persona files are
// developer-controlled and trusted, so define/template/block directives are
// honored by design. The payload is injected as data via {{.Payload}} — only
// `text` reaches Parse — so untrusted diff content containing template syntax is
// never re-parsed. On failure it returns a *RenderError.
func RenderPrompt(text string, ctx PayloadContext) (string, error) {
	tmpl, err := template.New("persona").Option("missingkey=error").Parse(text)
	if err != nil {
		return "", &RenderError{phase: phaseParse, Err: err}
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, ctx); err != nil {
		field := ""
		if m := unknownFieldRe.FindStringSubmatch(err.Error()); m != nil {
			field = m[1]
		}
		return "", &RenderError{phase: phaseExec, Field: field, Err: err}
	}
	return b.String(), nil
}
