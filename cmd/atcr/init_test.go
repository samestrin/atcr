package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/personas"
)

var personaNames = []string{"bruce", "greta", "kai", "mira", "dax", "otto"}

// initDir runs a fresh init into dir, failing the test on error.
func initDir(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, runInit(dir, false, &bytes.Buffer{}, &bytes.Buffer{}))
}

func TestInit_FreshDirectory(t *testing.T) {
	dir := t.TempDir()
	out := &bytes.Buffer{}
	require.NoError(t, runInit(dir, false, out, &bytes.Buffer{}))

	// Config exists, parses strictly, and carries the documented defaults.
	cfg, err := registry.LoadProjectConfig(filepath.Join(dir, ".atcr", "config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, personaNames, cfg.Agents, "default roster lists all six personas")
	assert.Equal(t, "blocks", cfg.PayloadMode)
	require.NotNil(t, cfg.TimeoutSecs)
	assert.Equal(t, 600, *cfg.TimeoutSecs)
	assert.Equal(t, "HIGH", cfg.FailOn)
	require.NotNil(t, cfg.MaxParallel, "template must carry max_parallel so the knob is visible")
	assert.Equal(t, registry.DefaultMaxParallel, *cfg.MaxParallel)

	// Six persona files plus the base template.
	for _, name := range append([]string{"_base"}, personaNames...) {
		path := filepath.Join(dir, ".atcr", "personas", name+".md")
		assert.FileExists(t, path)
	}

	// Success message lists created files.
	for _, want := range []string{"config.yaml", "bruce.md", "_base.md"} {
		assert.Contains(t, out.String(), want)
	}
}

// TestInit_WritesGitignore: `atcr init` drops a .atcr/.gitignore so the runtime
// outputs atcr writes under .atcr/ (the diff cache, up to cache_max_bytes, and
// reviewer outputs) are ignored by git out of the box — even for end users who
// never manually ignore .atcr/. The editable config.yaml and personas/ alongside
// it stay tracked.
func TestInit_WritesGitignore(t *testing.T) {
	dir := t.TempDir()
	initDir(t, dir)

	data, err := os.ReadFile(filepath.Join(dir, ".atcr", ".gitignore"))
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "cache/", "the diff cache dir must be ignored")
	assert.Contains(t, content, "reviews/", "the reviews dir must be ignored")
}

func TestInit_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	dir := t.TempDir()
	initDir(t, dir)

	dirInfo, err := os.Stat(filepath.Join(dir, ".atcr", "personas"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), dirInfo.Mode().Perm(), "directories use 0755")

	fileInfo, err := os.Stat(filepath.Join(dir, ".atcr", "config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), fileInfo.Mode().Perm(), "files use 0644")
}

func TestInit_PersonaContent(t *testing.T) {
	dir := t.TempDir()
	initDir(t, dir)

	for _, name := range append([]string{"_base"}, personaNames...) {
		data, err := os.ReadFile(filepath.Join(dir, ".atcr", "personas", name+".md"))
		require.NoError(t, err)
		content := string(data)
		for _, header := range []string{"## Role", "## Focus", "## Severity Rubric", "## Output Format"} {
			assert.Contains(t, content, header, "%s.md must contain %s", name, header)
		}
		assert.Contains(t, content, "{{.Payload}}", "%s.md must reference the payload placeholder", name)
		assert.Contains(t, content, "{{.AgentName}}", "%s.md must reference the agent-name placeholder", name)
	}
}

func TestInit_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	initDir(t, dir)

	// Tamper with a persona so we can prove nothing is modified.
	brucePath := filepath.Join(dir, ".atcr", "personas", "bruce.md")
	require.NoError(t, os.WriteFile(brucePath, []byte("EDITED"), 0o644))

	err := runInit(dir, false, &bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config already exists at .atcr/config.yaml")
	assert.Contains(t, err.Error(), "--force")

	data, err := os.ReadFile(brucePath)
	require.NoError(t, err)
	assert.Equal(t, "EDITED", string(data), "existing files must not be modified without --force")
}

func TestInit_GuardCoversPersonasWithoutConfig(t *testing.T) {
	// Customized personas must survive even when config.yaml is missing
	// (e.g. deleted, or a previous init failed midway).
	dir := t.TempDir()
	initDir(t, dir)
	require.NoError(t, os.Remove(filepath.Join(dir, ".atcr", "config.yaml")))

	brucePath := filepath.Join(dir, ".atcr", "personas", "bruce.md")
	require.NoError(t, os.WriteFile(brucePath, []byte("CUSTOMIZED"), 0o644))

	err := runInit(dir, false, &bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err, "existing persona files must trigger the overwrite guard")

	data, err := os.ReadFile(brucePath)
	require.NoError(t, err)
	assert.Equal(t, "CUSTOMIZED", string(data))
}

func TestInit_Force(t *testing.T) {
	dir := t.TempDir()
	initDir(t, dir)

	brucePath := filepath.Join(dir, ".atcr", "personas", "bruce.md")
	require.NoError(t, os.WriteFile(brucePath, []byte("EDITED"), 0o644))

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	require.NoError(t, runInit(dir, true, out, errOut))
	assert.Contains(t, errOut.String(), "Overwriting existing configuration and persona files",
		"warning goes to stderr, not stdout")
	assert.NotContains(t, out.String(), "Overwriting")

	data, err := os.ReadFile(brucePath)
	require.NoError(t, err)
	assert.NotEqual(t, "EDITED", string(data), "--force must restore defaults")
}

func TestInit_ForceDoesNotWriteThroughSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	dir := t.TempDir()
	initDir(t, dir)

	// Replace a persona with a symlink to an external file.
	external := filepath.Join(t.TempDir(), "external.md")
	require.NoError(t, os.WriteFile(external, []byte("EXTERNAL"), 0o644))
	brucePath := filepath.Join(dir, ".atcr", "personas", "bruce.md")
	require.NoError(t, os.Remove(brucePath))
	require.NoError(t, os.Symlink(external, brucePath))

	require.NoError(t, runInit(dir, true, &bytes.Buffer{}, &bytes.Buffer{}))

	data, err := os.ReadFile(external)
	require.NoError(t, err)
	assert.Equal(t, "EXTERNAL", string(data), "symlink target outside the workspace must not be written through")

	info, err := os.Lstat(brucePath)
	require.NoError(t, err)
	assert.Zero(t, info.Mode()&os.ModeSymlink, "persona path must be a regular file after --force")
}

func TestInit_ReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("requires POSIX permissions and non-root user")
	}
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := runInit(dir, false, &bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create .atcr/")
}

func TestInit_CommandWiring(t *testing.T) {
	// `atcr init` must run against the working directory; the test only
	// verifies the flag plumbing by running in a temp cwd.
	dir := t.TempDir()
	t.Chdir(dir)

	_, err := execute(t, "init")
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, ".atcr", "config.yaml"))

	_, err = execute(t, "init")
	require.Error(t, err, "second init without --force must fail")

	_, err = execute(t, "init", "--force")
	require.NoError(t, err)
}

func TestPersonas_EmbeddedSetComplete(t *testing.T) {
	assert.ElementsMatch(t, personaNames, personas.Names())
	for _, name := range personaNames {
		content, err := personas.Get(name)
		require.NoError(t, err)
		assert.NotEmpty(t, content)
	}
	base, err := personas.Base()
	require.NoError(t, err)
	assert.NotEmpty(t, base)
}

func TestPersonas_UnknownName(t *testing.T) {
	_, err := personas.Get("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}
