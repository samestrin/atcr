package registry

import (
	"os"
	"path/filepath"
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
	assert.Contains(t, err.Error(), "path separators")
}

func TestPersonaResolution_AgentNameTraversal(t *testing.T) {
	dirs := personaDirs(t)
	_, err := ResolvePersona("../../etc/passwd", "../../etc/passwd", nil, dirs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path separators")
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

func TestPersonaResolution_AllSixEmbeddedResolve(t *testing.T) {
	dirs := personaDirs(t)
	for _, name := range []string{"bruce", "greta", "kai", "mira", "dax", "otto"} {
		got, err := ResolvePersona(name, name, nil, dirs)
		require.NoErrorf(t, err, "agent %s", name)
		assert.Equalf(t, "embedded:"+name, got.Source, "agent %s", name)
		assert.NotEmpty(t, got.Text)
	}
}
