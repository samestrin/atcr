package personas

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/registry"
)

// ReviewerRoles maps each agent to the role in the H1 of the persona it
// resolves to. An agent that resolves only to the shared _base template, or to
// nothing, or to a prompt with no role, has no entry, so the report shows its
// bare name (Epic 83.0 T5).
func TestReviewerRoles(t *testing.T) {
	project, reg := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "special.md"), []byte("# {{.AgentName}} — special reviewer\nbody\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(project, "plain.md"), []byte("# a heading with no dash\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(reg, "_base.md"), []byte("# {{.AgentName}} — code reviewer\n"), 0o644))

	agents := map[string]registry.AgentConfig{
		"greta":   {Persona: "greta"},   // embedded built-in
		"sec":     {Persona: "special"}, // explicit ref to a project persona
		"plain":   {Persona: "plain"},   // H1 with no role
		"custom":  {Persona: "custom"},  // no file anywhere: falls to the _base template
		"broken":  {Persona: "missing"}, // explicit ref that resolves nowhere
		"unnamed": {},                   // empty persona key: the agent name is the persona
	}
	got := ReviewerRoles(agents, registry.PersonaDirs{Project: project, Registry: reg})
	assert.Equal(t, map[string]string{
		"greta": "algorithmic correctness reviewer",
		"sec":   "special reviewer",
	}, got)
}

func TestReviewerRoles_EmbeddedBaseIsNoRole(t *testing.T) {
	got := ReviewerRoles(map[string]registry.AgentConfig{"host": {Persona: "host"}}, registry.PersonaDirs{})
	assert.Empty(t, got, "an agent with no persona of its own resolves to the embedded _base template, which is no role")
}

// ResolveReviewerRoles reads the user registry under HOME, merges the project
// overlay under root, and resolves project personas from root/.atcr/personas.
func TestResolveReviewerRoles(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(
		"providers:\n  p:\n    api_key_env: K\nagents:\n"+
			"  greta:\n    provider: p\n    model: m\n"+
			"  sec:\n    provider: p\n    model: m\n    persona: special\n"), 0o644))
	personasDir := filepath.Join(root, ".atcr", "personas")
	require.NoError(t, os.MkdirAll(personasDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(personasDir, "special.md"), []byte("# {{.AgentName}} — special reviewer\n"), 0o644))

	assert.Equal(t, map[string]string{
		"greta": "algorithmic correctness reviewer",
		"sec":   "special reviewer",
	}, ResolveReviewerRoles(root))
}

// The roles are display metadata: no registry, or one that does not load,
// yields no roles rather than an error, and the report keeps bare names.
func TestResolveReviewerRoles_NoRegistryIsNoRoles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	assert.Nil(t, ResolveReviewerRoles(t.TempDir()))

	home := t.TempDir()
	t.Setenv("HOME", home)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte("not: [valid"), 0o644))
	assert.Nil(t, ResolveReviewerRoles(t.TempDir()))
}
