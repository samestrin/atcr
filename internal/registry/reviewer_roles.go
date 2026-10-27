package registry

import (
	"path/filepath"
	"strings"
)

// PersonaRole returns the role a persona prompt names in its H1, the text
// after the first em dash in `# {{.AgentName}} — <role>`. Only the first H1
// counts; lines before it (a vendor-guidance comment) are skipped. A prompt
// with no H1, or an H1 with no em dash, has no role and yields "". It is the
// one reader both `atcr personas list` (Epic 83.0 T4) and the report's
// reviewer labels (T5) use.
func PersonaRole(prompt string) string {
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "# ") {
			continue
		}
		_, role, ok := strings.Cut(line, "—")
		if !ok {
			return ""
		}
		return strings.TrimSpace(role)
	}
	return ""
}

// ReviewerRoles maps each agent name to the role its persona names in its H1
// (PersonaRole). The markdown reports label a reviewer `name (role)` from this
// map (Epic 83.0 T5).
//
// The persona resolves through ResolvePersona, the chain a review uses, so a
// project override wins here as it does there. An agent has no entry, and the
// report shows its bare name, when its persona resolves nowhere, resolves only
// to the shared _base template (that H1 names no persona's role), or has an H1
// with no role.
func ReviewerRoles(agents map[string]AgentConfig, dirs PersonaDirs) map[string]string {
	roles := map[string]string{}
	for name, ac := range agents {
		persona := ac.Persona
		if persona == "" {
			persona = name
		}
		p, err := ResolvePersona(name, persona, nil, dirs)
		if err != nil || p.Source == "embedded:_base" || filepath.Base(p.Source) == "_base.md" {
			continue
		}
		if role := PersonaRole(p.Text); role != "" {
			roles[name] = role
		}
	}
	return roles
}
