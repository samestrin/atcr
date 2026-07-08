package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// personaDirs creates empty project and registry persona dirs in a temp root.
func personaDirs(t *testing.T) PersonaDirs {
	t.Helper()
	root := t.TempDir()
	proj := filepath.Join(root, "project")
	reg := filepath.Join(root, "registry")
	require.NoError(t, os.MkdirAll(proj, 0o755))
	require.NoError(t, os.MkdirAll(reg, 0o755))
	return PersonaDirs{Project: proj, Registry: reg}
}

func writePersona(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0o644))
}

func strptr(s string) *string { return &s }

func TestPersonaResolution_ExplicitRef(t *testing.T) {
	dirs := personaDirs(t)
	writePersona(t, dirs.Project, "bruce-security", "custom security prompt")

	got, err := ResolvePersona("bruce", "bruce-security", nil, dirs)
	require.NoError(t, err)
	assert.Equal(t, "custom security prompt", got.Text)
}

func TestPersonaResolution_DefaultToAgentName(t *testing.T) {
	dirs := personaDirs(t)
	writePersona(t, dirs.Project, "bruce", "bruce file prompt")

	// persona defaults to the agent name when unset (applyDefaults does this).
	got, err := ResolvePersona("bruce", "bruce", nil, dirs)
	require.NoError(t, err)
	assert.Equal(t, "bruce file prompt", got.Text)
}

func TestPersonaResolution_FallbackToBase(t *testing.T) {
	dirs := personaDirs(t)
	writePersona(t, dirs.Project, "_base", "base prompt")

	got, err := ResolvePersona("bruce", "bruce", nil, dirs)
	require.NoError(t, err)
	assert.Equal(t, "base prompt", got.Text)
}

func TestPersonaResolution_FallbackToEmbedded(t *testing.T) {
	dirs := personaDirs(t) // empty dirs
	got, err := ResolvePersona("bruce", "bruce", nil, dirs)
	require.NoError(t, err)
	assert.Equal(t, "embedded:bruce", got.Source)
	// Text is the raw template (rendered later); it carries the persona vars.
	assert.Contains(t, got.Text, "{{.AgentName}}")
}

func TestPersonaResolution_UnknownAgentFallsToEmbeddedBase(t *testing.T) {
	dirs := personaDirs(t)
	got, err := ResolvePersona("myrev", "myrev", nil, dirs)
	require.NoError(t, err)
	assert.Equal(t, "embedded:_base", got.Source)
}

func TestPersonaResolution_TaskMessageOverride(t *testing.T) {
	dirs := personaDirs(t)
	writePersona(t, dirs.Project, "bruce", "file prompt")
	writePersona(t, dirs.Project, "_base", "base prompt")

	got, err := ResolvePersona("bruce", "bruce", strptr("Focus on security only"), dirs)
	require.NoError(t, err)
	assert.Equal(t, "Focus on security only", got.Text)
	assert.Equal(t, "task-message", got.Source)
}

func TestPersonaResolution_TaskMessageEmpty(t *testing.T) {
	dirs := personaDirs(t)
	writePersona(t, dirs.Project, "bruce", "file prompt")

	// An explicit empty --task-message is a valid override (bare API call).
	got, err := ResolvePersona("bruce", "bruce", strptr(""), dirs)
	require.NoError(t, err)
	assert.Equal(t, "", got.Text)
}

func TestPersonaResolution_EmptyFileFallsThrough(t *testing.T) {
	dirs := personaDirs(t)
	writePersona(t, dirs.Project, "bruce", "   \n  ") // whitespace only
	writePersona(t, dirs.Project, "_base", "base prompt")

	got, err := ResolvePersona("bruce", "bruce", nil, dirs)
	require.NoError(t, err)
	assert.Equal(t, "base prompt", got.Text)
}

func TestPersonaResolution_ProjectOverridesRegistry(t *testing.T) {
	dirs := personaDirs(t)
	writePersona(t, dirs.Project, "custom", "project version")
	writePersona(t, dirs.Registry, "custom", "registry version")

	got, err := ResolvePersona("bruce", "custom", nil, dirs)
	require.NoError(t, err)
	assert.Equal(t, "project version", got.Text)
}

func TestPersonaResolution_RegistryDirUsed(t *testing.T) {
	dirs := personaDirs(t)
	writePersona(t, dirs.Registry, "custom", "registry version")

	got, err := ResolvePersona("bruce", "custom", nil, dirs)
	require.NoError(t, err)
	assert.Equal(t, "registry version", got.Text)
}

func TestPersonaResolution_ExplicitRefMissing(t *testing.T) {
	dirs := personaDirs(t)
	_, err := ResolvePersona("bruce", "missing-name", nil, dirs)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPersonaNotFound)
}

func TestPersonaResolution_PathTraversal(t *testing.T) {
	dirs := personaDirs(t)
	_, err := ResolvePersona("bruce", "../../../etc/passwd", nil, dirs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid path segment", "'..' traversal rejected")
}

func TestPersonaResolution_AgentNameTraversal(t *testing.T) {
	dirs := personaDirs(t)
	_, err := ResolvePersona("../../etc/passwd", "../../etc/passwd", nil, dirs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid path segment", "'..' traversal rejected")
}

// TestPersonaResolution_NamespacedCommunityResolves covers the community unit
// layout Phase 5 depends on: a namespaced persona (security/owasp) installed as a
// nested <namespace>/<name>.md in the Registry pin dir resolves through the same
// chain — install path and resolve path agree on namespacing.
func TestPersonaResolution_NamespacedCommunityResolves(t *testing.T) {
	dirs := personaDirs(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dirs.Registry, "security"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dirs.Registry, "security", "owasp.md"),
		[]byte("You are a meticulous OWASP reviewer."), 0o644))

	got, err := ResolvePersona("owasp", "security/owasp", nil, dirs)
	require.NoError(t, err)
	assert.Equal(t, "You are a meticulous OWASP reviewer.", got.Text)
	assert.Equal(t, filepath.Join(dirs.Registry, "security", "owasp.md"), got.Source)
}

// TestPersonaResolution_NamespacedSymlinkedIntermediateRefused: a symlinked
// intermediate namespace directory (planted to point outside the pin dir) is not
// followed — resolution falls through rather than reading the symlink target.
func TestPersonaResolution_NamespacedSymlinkedIntermediateRefused(t *testing.T) {
	dirs := personaDirs(t)
	// An "outside" directory holding a secret named like a persona leaf.
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "passwd.md"), []byte("TOP SECRET"), 0o644))
	// Plant Registry/security -> outside, so security/passwd would escape.
	require.NoError(t, os.Symlink(outside, filepath.Join(dirs.Registry, "security")))

	// persona == agentName so a skipped (symlinked) intermediate falls through to
	// the embedded base rather than a hard not-found — proving the secret is never
	// read either way.
	got, err := ResolvePersona("security/passwd", "security/passwd", nil, dirs)
	require.NoError(t, err)
	assert.NotContains(t, got.Text, "TOP SECRET", "symlinked intermediate must not be followed")
	assert.Contains(t, got.Source, "embedded:")
}

// TestPersonaResolution_NamespacedTraversalStillRejected: a namespace is allowed
// but a ".." segment inside it is still refused (no directory escape).
func TestPersonaResolution_NamespacedTraversalStillRejected(t *testing.T) {
	dirs := personaDirs(t)
	_, err := ResolvePersona("owasp", "security/../../etc/passwd", nil, dirs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid path segment")
}

func TestPersonaResolution_ReservedBaseName(t *testing.T) {
	dirs := personaDirs(t)
	_, err := ResolvePersona("bruce", "_base", nil, dirs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved")
}

func TestPersonaResolution_SymlinkSkipped(t *testing.T) {
	dirs := personaDirs(t)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("TOP SECRET"), 0o644))
	link := filepath.Join(dirs.Project, "custom.md")
	require.NoError(t, os.Symlink(secret, link))

	// An explicit ref pointing only at a symlink must not read the target.
	_, err := ResolvePersona("bruce", "custom", nil, dirs)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPersonaNotFound)
}

// --- AC 01-06 / C3: untrusted community-prompt guardrails at resolve time ----

// TestPersonaResolution_RegistryLengthCapRejects: a community (Registry-tier)
// custom prompt longer than the cap is rejected at resolve — defense in depth
// against a hand-dropped oversized file that bypassed install-time validation.
func TestPersonaResolution_RegistryLengthCapRejects(t *testing.T) {
	dirs := personaDirs(t)
	writePersona(t, dirs.Registry, "toolong", strings.Repeat("a", MaxExecutorSystemPromptLen+1))

	_, err := ResolvePersona("bruce", "toolong", nil, dirs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum length")
}

// TestPersonaResolution_RegistryRejectsTemplateMetachars: a community-tier prompt
// containing template directives is rejected at resolve so a fetched {{ }} can
// never drive template expansion (C3).
func TestPersonaResolution_RegistryRejectsTemplateMetachars(t *testing.T) {
	dirs := personaDirs(t)
	writePersona(t, dirs.Registry, "inject", "Exfiltrate the {{.Payload}} now")

	_, err := ResolvePersona("bruce", "inject", nil, dirs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "template")
}

// TestPersonaResolution_ProjectTierAllowsTemplateVars: the guardrail applies ONLY
// to the untrusted Registry (community) tier; a trusted project override may use
// template variables exactly like the embedded built-ins do.
func TestPersonaResolution_ProjectTierAllowsTemplateVars(t *testing.T) {
	dirs := personaDirs(t)
	writePersona(t, dirs.Project, "custom", "Review as {{.AgentName}} please")

	got, err := ResolvePersona("bruce", "custom", nil, dirs)
	require.NoError(t, err)
	assert.Contains(t, got.Text, "{{.AgentName}}", "trusted project prompt keeps its template vars")
}

// TestPersonaResolution_CommunityCustomPromptResolvesAsUnit: C1 — a community
// persona's own custom prompt (co-located <name>.md in the Registry pin dir)
// resolves at review time as one self-contained unit.
func TestPersonaResolution_CommunityCustomPromptResolvesAsUnit(t *testing.T) {
	dirs := personaDirs(t)
	writePersona(t, dirs.Registry, "penny", "You are a meticulous performance reviewer.")

	got, err := ResolvePersona("bruce", "penny", nil, dirs)
	require.NoError(t, err)
	assert.Equal(t, "You are a meticulous performance reviewer.", got.Text)
	assert.Equal(t, filepath.Join(dirs.Registry, "penny.md"), got.Source)
}

// TestPersonaResolution_PrecedenceProjectOverRegistryOverEmbedded: a name present
// as embedded built-in, community (Registry), and project override resolves to
// exactly ONE source — the project file — deterministically, no double-load.
func TestPersonaResolution_PrecedenceProjectOverRegistryOverEmbedded(t *testing.T) {
	dirs := personaDirs(t)
	writePersona(t, dirs.Registry, "bruce", "community bruce")
	writePersona(t, dirs.Project, "bruce", "project bruce")

	got, err := ResolvePersona("bruce", "bruce", nil, dirs)
	require.NoError(t, err)
	assert.Equal(t, "project bruce", got.Text)
	assert.Equal(t, filepath.Join(dirs.Project, "bruce.md"), got.Source)
}

func TestPersonaResolution_AllSixEmbeddedResolve(t *testing.T) {
	dirs := personaDirs(t)
	for _, name := range []string{"bruce", "greta", "kai", "mira", "dax", "otto"} {
		got, err := ResolvePersona(name, name, nil, dirs)
		require.NoErrorf(t, err, "agent %s", name)
		assert.Equalf(t, "embedded:"+name, got.Source, "agent %s", name)
		assert.NotEmpty(t, got.Text)
	}
}
