package main

import (
	"bytes"
	"errors"
	"fmt"
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

	for _, sub := range []string{"review", "reconcile", "report", "range", "status", "init", "serve", "doctor", "trust"} {
		assert.Contains(t, out, sub, "help output must list subcommand %q", sub)
	}
}

func TestRootCmd_HasExactlyNineSubcommands(t *testing.T) {
	// The eight prior commands plus `trust` (epic 1.3), the project-provider
	// authorization gate.
	root := newRootCmd()
	names := map[string]bool{}
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		names[c.Name()] = true
	}
	assert.Len(t, names, 9)
	for _, sub := range []string{"review", "reconcile", "report", "range", "status", "init", "serve", "doctor", "trust"} {
		assert.True(t, names[sub], "subcommand %q must be registered", sub)
	}
}

func TestRootCmd_UnknownSubcommandErrors(t *testing.T) {
	_, err := execute(t, "no-such-command")
	assert.Error(t, err)
}

func TestRootCmd_UnknownSubcommandIsUsageError(t *testing.T) {
	// A typo'd subcommand is a usage error (exit 2) — in CI, exit 1 means
	// "findings at/above threshold", so the two must never conflate.
	_, err := execute(t, "no-such-command")
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err))
}

func TestRootCmd_BareInvocationShowsHelp(t *testing.T) {
	out, err := execute(t)
	require.NoError(t, err)
	assert.Contains(t, out, "Usage:")
}

func TestExitCode(t *testing.T) {
	plain := errors.New("boom")
	coded := usageError(plain)

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil error", nil, 0},
		{"plain error", plain, 1},
		{"coded usage error", coded, 2},
		{"wrapped coded error", fmt.Errorf("context: %w", coded), 2},
		{"joined coded error", errors.Join(plain, coded), 2},
		{"explicit zero code", &codedError{code: 0, err: plain}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, exitCode(tt.err))
		})
	}
}

func TestFlagRelationships(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"review head without base", []string{"review", "--head", "def"}},
		{"review base with merge-commit", []string{"review", "--base", "abc", "--head", "def", "--merge-commit", "fff"}},
		{"review head with merge-commit", []string{"review", "--head", "def", "--merge-commit", "fff"}},
		{"range head without base", []string{"range", "--head", "def"}},
		{"range head with merge-commit", []string{"range", "--head", "def", "--merge-commit", "fff"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := execute(t, tt.args...)
			require.Error(t, err)
			assert.Equal(t, 2, exitCode(err), "flag-group violations are usage errors")
		})
	}
}

func TestUsageErrors_ExitCodeTwo(t *testing.T) {
	t.Run("unknown flag", func(t *testing.T) {
		_, err := execute(t, "review", "--no-such-flag")
		require.Error(t, err)
		assert.Equal(t, 2, exitCode(err))
	})
	t.Run("unexpected positional arg", func(t *testing.T) {
		_, err := execute(t, "init", "unexpected")
		require.Error(t, err)
		assert.Equal(t, 2, exitCode(err))
	})
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
