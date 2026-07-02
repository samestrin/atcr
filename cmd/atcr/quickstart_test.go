package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/registry"
)

// quickstartInput is the canned interactive input for a non-interactive test
// run: an empty key line (skip key entry) followed by an empty shell-profile
// choice (skip profile append). Extended per task as the flow grows.
const quickstartInput = "\n\n"

func TestQuickstart_CommandWiring(t *testing.T) {
	cmd := newQuickstartCmd()
	assert.Equal(t, "quickstart", cmd.Name(), "command is named quickstart")

	// It must be registered on the root command tree.
	root := newRootCmd()
	var found bool
	for _, c := range root.Commands() {
		if c.Name() == "quickstart" {
			found = true
			break
		}
	}
	assert.True(t, found, "quickstart is registered on the root command")
}

func TestQuickstart_ReusesInitWriters(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir()) // registry writes land in a throwaway home
	out := &bytes.Buffer{}
	err := runQuickstart(quickstartOpts{
		dir:    dir,
		in:     strings.NewReader(quickstartInput),
		out:    out,
		errOut: &bytes.Buffer{},
	})
	require.NoError(t, err)

	// The .atcr side is produced by reusing init's writers: config + personas.
	cfg, err := registry.LoadProjectConfig(filepath.Join(dir, ".atcr", "config.yaml"))
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.Agents, "roster is populated by the reused init writer")
	assert.FileExists(t, filepath.Join(dir, ".atcr", "personas", "bruce.md"))
	assert.FileExists(t, filepath.Join(dir, ".atcr", "personas", "_base.md"))
}

func TestQuickstart_WritesSyntheticRegistry(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, runQuickstart(quickstartOpts{
		dir:    dir,
		in:     strings.NewReader(quickstartInput),
		out:    &bytes.Buffer{},
		errOut: &bytes.Buffer{},
	}))

	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	reg, err := registry.LoadRegistry(regPath)
	require.NoError(t, err, "quickstart writes a valid registry.yaml")
	_, ok := reg.Providers["synthetic"]
	assert.True(t, ok, "synthetic provider is defined")

	// Every roster entry in the generated project config must resolve against
	// the generated registry — otherwise the first review would fail.
	cfg, err := registry.LoadProjectConfig(filepath.Join(dir, ".atcr", "config.yaml"))
	require.NoError(t, err)
	assert.NoError(t, cfg.ValidateAgainst(reg), "roster resolves against synthetic registry")
}

func TestQuickstart_RegistryGuard_SkipsExistingWithoutForce(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(regPath), 0o755))
	require.NoError(t, os.WriteFile(regPath, []byte("# user's own registry\n"), 0o644))

	out := &bytes.Buffer{}
	require.NoError(t, runQuickstart(quickstartOpts{
		dir:    dir,
		in:     strings.NewReader(quickstartInput),
		out:    out,
		errOut: &bytes.Buffer{},
	}))

	// The existing registry must be untouched (no clobber).
	data, err := os.ReadFile(regPath)
	require.NoError(t, err)
	assert.Equal(t, "# user's own registry\n", string(data), "existing registry left intact")
	assert.Contains(t, out.String(), "registry.yaml", "user is told the snippet was not applied")
}

func TestQuickstart_RegistryGuard_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(regPath), 0o755))
	require.NoError(t, os.WriteFile(regPath, []byte("# user's own registry\n"), 0o644))

	require.NoError(t, runQuickstart(quickstartOpts{
		dir:    dir,
		force:  true,
		in:     strings.NewReader(quickstartInput),
		out:    &bytes.Buffer{},
		errOut: &bytes.Buffer{},
	}))

	reg, err := registry.LoadRegistry(regPath)
	require.NoError(t, err, "force overwrites with a valid synthetic registry")
	_, ok := reg.Providers["synthetic"]
	assert.True(t, ok, "synthetic provider present after force overwrite")
}
