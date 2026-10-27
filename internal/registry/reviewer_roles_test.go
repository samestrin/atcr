package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PersonaRole is the one H1 role reader: `atcr personas list` (T4) and the
// report labels (T5) both call it, so the list and the report cannot disagree.
func TestPersonaRole(t *testing.T) {
	assert.Equal(t, "threat-model mapper", PersonaRole("# {{.AgentName}} — threat-model mapper\n"))
	assert.Equal(t, "", PersonaRole("# no dash here\n"))
	assert.Equal(t, "", PersonaRole(""))
}

// ReviewerRoles maps each agent to the role in the H1 of the persona it
// resolves to. An agent that resolves to nothing, or to a prompt with no role,
// has no entry, so the report shows its bare name (Epic 83.0 T5).
func TestReviewerRoles(t *testing.T) {
	project, reg := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "special.md"), []byte("# {{.AgentName}} — special reviewer\nbody\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(project, "plain.md"), []byte("# a heading with no dash\n"), 0o644))

	agents := map[string]AgentConfig{
		"greta":   {Persona: "greta"},   // embedded built-in
		"sec":     {Persona: "special"}, // explicit ref to a project persona
		"plain":   {Persona: "plain"},   // H1 with no role
		"broken":  {Persona: "missing"}, // explicit ref that resolves nowhere
		"unnamed": {},                   // empty persona key: the agent name is the persona
	}
	got := ReviewerRoles(agents, PersonaDirs{Project: project, Registry: reg})
	assert.Equal(t, map[string]string{
		"greta": "algorithmic correctness reviewer",
		"sec":   "special reviewer",
	}, got)
}

// A _base.md in a personas dir is the shared template, not a persona, so an
// agent that resolves to it has no role. ResolvePersona tries that file before
// the embedded personas, so it also shadows an embedded built-in such as greta:
// the review runs on _base, and the report must not claim greta's role.
func TestReviewerRoles_BaseFileIsNoRole(t *testing.T) {
	reg := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(reg, "_base.md"), []byte("# {{.AgentName}} — code reviewer\n"), 0o644))
	got := ReviewerRoles(map[string]AgentConfig{
		"custom": {Persona: "custom"},
		"greta":  {Persona: "greta"},
	}, PersonaDirs{Registry: reg})
	assert.Empty(t, got)
}

func TestReviewerRoles_EmbeddedBaseIsNoRole(t *testing.T) {
	got := ReviewerRoles(map[string]AgentConfig{"host": {Persona: "host"}}, PersonaDirs{})
	assert.Empty(t, got, "an agent with no persona of its own resolves to the embedded _base template, which is no role")
}
