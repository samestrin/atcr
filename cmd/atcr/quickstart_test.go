package main

import (
	"bytes"
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
