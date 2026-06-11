package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// execute runs the root command with args and returns combined output.
func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestRootCmd_Use(t *testing.T) {
	root := newRootCmd()
	assert.Equal(t, "atcr", root.Use)
}

func TestRootCmd_HelpListsAllSubcommands(t *testing.T) {
	out, err := execute(t, "--help")
	require.NoError(t, err)

	for _, sub := range []string{"review", "reconcile", "report", "range", "init", "serve"} {
		assert.Contains(t, out, sub, "help output must list subcommand %q", sub)
	}
}

func TestRootCmd_HasExactlySixSubcommands(t *testing.T) {
	root := newRootCmd()
	names := map[string]bool{}
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		names[c.Name()] = true
	}
	assert.Len(t, names, 6)
	for _, sub := range []string{"review", "reconcile", "report", "range", "init", "serve"} {
		assert.True(t, names[sub], "subcommand %q must be registered", sub)
	}
}

func TestRootCmd_UnknownSubcommandErrors(t *testing.T) {
	_, err := execute(t, "no-such-command")
	assert.Error(t, err)
}

func TestRootCmd_SubcommandsUseRunE(t *testing.T) {
	// Handlers must return errors (RunE) so exit codes are mapped centrally
	// in main() — no os.Exit inside handlers.
	root := newRootCmd()
	for _, c := range root.Commands() {
		if c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		assert.Nil(t, c.Run, "%s must not use Run", c.Name())
		assert.NotNil(t, c.RunE, "%s must define RunE", c.Name())
	}
}
