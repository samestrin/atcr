package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 165 T3 (axi.md Principle 10): -v, -V and --version each print the bare
// version on stdout, exit 0, in both output modes. The `version` subcommand
// keeps its conventional `atcr version <ver>` line
// (TestVersion_FlagAndSubcommandAgree). Every test clears ATCR_FORMAT and the
// agent markers first, so the environment the suite runs under cannot flip the
// mode it asserts.

// TestVersionFlags_BareVersionInBothModes runs each spelling on the agent path
// (pipe, ATCR_FORMAT=axi) and the human paths (TTY, --human, ATCR_FORMAT=text)
// and pins stdout to exactly the bare version plus a newline, exit 0, empty
// stderr.
func TestVersionFlags_BareVersionInBothModes(t *testing.T) {
	modes := []struct {
		name  string
		tty   bool
		env   map[string]string
		extra []string
	}{
		{name: "pipe"},
		{name: "tty", tty: true},
		{name: "pipe --human", extra: []string{"--human"}},
		{name: "pipe ATCR_FORMAT=text", env: map[string]string{"ATCR_FORMAT": "text"}},
		{name: "tty ATCR_FORMAT=axi", tty: true, env: map[string]string{"ATCR_FORMAT": "axi"}},
	}
	for _, spelling := range []string{"-v", "-V", "--version"} {
		for _, m := range modes {
			t.Run(spelling+"/"+m.name, func(t *testing.T) {
				isolate(t)
				clearOutputModeEnv(t)
				for k, v := range m.env {
					t.Setenv(k, v)
				}
				if m.tty {
					fakeTTY(t)
				}
				code, stdout, stderr := execPiped(t, append(append([]string{}, m.extra...), spelling)...)
				require.Equal(t, 0, code, "stderr: %s", stderr)
				assert.Equal(t, atcrVersion()+"\n", stdout)
				assert.Empty(t, stderr)
			})
		}
	}
}

// TestVersionFlags_MainExitZero is the end-to-end form through Main (os.Args,
// real pipe stdout): each spelling exits 0 with the bare version on stdout and
// nothing on stderr.
func TestVersionFlags_MainExitZero(t *testing.T) {
	for _, spelling := range []string{"-v", "-V", "--version"} {
		t.Run(spelling, func(t *testing.T) {
			isolate(t)
			clearOutputModeEnv(t)
			code, stdout, stderr := mainPiped(t, spelling)
			assert.Equal(t, 0, code)
			assert.Equal(t, atcrVersion()+"\n", stdout)
			assert.Empty(t, stderr)
		})
	}
}

// TestVersionFlags_ShortCircuitBeforePreRun pins that all three spellings
// short-circuit before the root PersistentPreRunE, like cobra's --version
// always has: an invalid --log-format is a usage error (exit 2) when the hook
// runs, but a version flag must print the bare version and exit 0 regardless.
func TestVersionFlags_ShortCircuitBeforePreRun(t *testing.T) {
	isolate(t)
	clearOutputModeEnv(t)
	code, _, _ := execPiped(t, "--log-format", "bogus")
	require.Equal(t, exitUsage, code, "control: the hook rejects a bad --log-format")

	for _, spelling := range []string{"-v", "-V", "--version"} {
		t.Run(spelling, func(t *testing.T) {
			code, stdout, stderr := execPiped(t, spelling, "--log-format", "bogus")
			require.Equal(t, 0, code, "stderr: %s", stderr)
			assert.Equal(t, atcrVersion()+"\n", stdout)
			assert.Empty(t, stderr)
		})
	}
}

// TestInitVersionFlags_NoVersionFlag pins the nil guard in initVersionFlags:
// a command with no Version set has no --version flag to alias, so the
// function must return without registering either flag (and without
// panicking on the nil lookup). Production always sets root.Version
// (newRootCmd), so this exercises the guard directly.
func TestInitVersionFlags_NoVersionFlag(t *testing.T) {
	root := &cobra.Command{Use: "atcr"}
	initVersionFlags(root)
	assert.Nil(t, root.Flags().Lookup("version"), "no Version set: cobra registers no --version")
	assert.Nil(t, root.Flags().Lookup("version-upper"), "no version flag: no -V alias")
}

// TestVersionFlags_NotInheritedBySubcommands: the version flags are root-local
// (cobra registers --version on the root only), so -V must not leak onto a
// subcommand any more than --version does.
func TestVersionFlags_NotInheritedBySubcommands(t *testing.T) {
	isolate(t)
	clearOutputModeEnv(t)
	for _, spelling := range []string{"-V", "--version"} {
		code, _, _ := execPiped(t, "status", spelling)
		assert.Equal(t, exitUsage, code, "status %s is an unknown flag", spelling)
	}
}
