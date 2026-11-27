package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/report"
	goaxi "github.com/samestrin/go-axi"
)

// Epic 165 T1: AXI by default on the agent path. Every test here clears
// ATCR_FORMAT and the agent markers first, so the agent environment the suite
// may itself run under (CLAUDECODE=1) cannot flip the mode it asserts.

// clearOutputModeEnv blanks ATCR_FORMAT and every agent marker for one test.
// The resolver treats an empty value as unset.
func clearOutputModeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ATCR_FORMAT", "")
	for _, k := range agentEnvMarkers {
		t.Setenv(k, "")
	}
}

// fakeTTY makes every *os.File stdout count as a terminal for one test, so a
// pipe stands in for an interactive terminal.
func fakeTTY(t *testing.T) {
	t.Helper()
	orig := outputIsTerminal
	outputIsTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { outputIsTerminal = orig })
}

// execPiped runs the tree with stdout on a real pipe (not a terminal, unless
// fakeTTY is in force) and returns the exit code, stdout and stderr.
func execPiped(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	var errBuf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetOut(w)
	root.SetErr(&errBuf)
	execErr := root.ExecuteContext(context.Background())
	require.NoError(t, w.Close())
	out := <-done
	require.NoError(t, r.Close())
	return exitCode(execErr), string(out), errBuf.String()
}

// probeAXI runs path (plus extra args) with stdout on a pipe and the command's
// RunE replaced by a probe, and returns the AXI mode the root PersistentPreRunE
// put in the command's context.
func probeAXI(t *testing.T, path string, extra ...string) (axi bool, code int, stderr string) {
	t.Helper()
	root := NewRootCmd()
	target := root
	var words []string
	if path != "" {
		words = strings.Fields(path)
		found, _, err := root.Find(words)
		require.NoError(t, err)
		target = found
	}
	ran := false
	target.Args = cobra.ArbitraryArgs
	target.RunE = func(cmd *cobra.Command, _ []string) error {
		ran = true
		axi = axiFromContext(cmd.Context())
		return nil
	}
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	var errBuf bytes.Buffer
	root.SetArgs(append(words, extra...))
	root.SetOut(w)
	root.SetErr(&errBuf)
	execErr := root.ExecuteContext(context.Background())
	require.NoError(t, w.Close())
	code = exitCode(execErr)
	stderr = errBuf.String()
	if execErr != nil {
		// SilenceErrors leaves the message to Main; fold it in for assertions.
		stderr += execErr.Error()
	}
	if code == 0 {
		require.True(t, ran, "the probe ran for %q", path)
	}
	return axi, code, stderr
}

// axiModeCommands is every command that has an AXI mode, so the only ones the
// default flips (report decides its format from the same mode).
var axiModeCommands = []string{
	"", // bare atcr: the home view
	"review",
	"reconcile",
	"verify",
	"debate",
	"status",
	"debt clean",
	"debt filter",
	"debt group",
	"debt validate",
	"report",
}

func TestOutputMode_EachAXICommand(t *testing.T) {
	cases := []struct {
		name  string
		tty   bool
		env   map[string]string
		extra []string
		want  bool
	}{
		{name: "pipe", want: true},
		{name: "tty", tty: true, want: false},
		{name: "pipe --human", extra: []string{"--human"}, want: false},
		{name: "pipe ATCR_FORMAT=text", env: map[string]string{"ATCR_FORMAT": "text"}, want: false},
		{name: "tty ATCR_FORMAT=axi", tty: true, env: map[string]string{"ATCR_FORMAT": "axi"}, want: true},
		{name: "tty CLAUDECODE", tty: true, env: map[string]string{"CLAUDECODE": "1"}, want: true},
		{name: "tty CODEX", tty: true, env: map[string]string{"CODEX": "1"}, want: true},
		{name: "tty OPENCODE", tty: true, env: map[string]string{"OPENCODE": "1"}, want: true},
		{name: "agent marker ATCR_FORMAT=text", env: map[string]string{"CLAUDECODE": "1", "ATCR_FORMAT": "text"}, want: false},
		{name: "ATCR_FORMAT=axi --human", env: map[string]string{"ATCR_FORMAT": "axi"}, extra: []string{"--human"}, want: false},
	}
	for _, path := range axiModeCommands {
		for _, tc := range cases {
			t.Run(strings.TrimSpace("atcr "+path)+"/"+tc.name, func(t *testing.T) {
				isolate(t)
				clearOutputModeEnv(t)
				for k, v := range tc.env {
					t.Setenv(k, v)
				}
				if tc.tty {
					fakeTTY(t)
				}
				got, code, stderr := probeAXI(t, path, tc.extra...)
				require.Equal(t, 0, code, "stderr: %s", stderr)
				assert.Equal(t, tc.want, got)
			})
		}
	}
}

// TestOutputMode_ExplicitAXIBeatsTheDefault: --axi on a terminal, or under
// ATCR_FORMAT=text, is still AXI; the commands that own --axi keep it.
func TestOutputMode_ExplicitAXIBeatsTheDefault(t *testing.T) {
	for _, path := range axiModeCommands {
		if path == "report" {
			continue // report selects AXI with --format axi, not --axi
		}
		t.Run(strings.TrimSpace("atcr "+path), func(t *testing.T) {
			isolate(t)
			clearOutputModeEnv(t)
			fakeTTY(t)
			t.Setenv("ATCR_FORMAT", "text")
			got, code, stderr := probeAXI(t, path, "--axi")
			require.Equal(t, 0, code, "stderr: %s", stderr)
			assert.True(t, got)

			got, code, _ = probeAXI(t, path, "--axi=false")
			require.Equal(t, 0, code)
			assert.False(t, got, "an explicit --axi=false is human even on the agent path")
		})
	}
}

// TestOutputMode_AXIAndHumanAreExclusive: both flags at once is a usage error.
func TestOutputMode_AXIAndHumanAreExclusive(t *testing.T) {
	isolate(t)
	clearOutputModeEnv(t)
	_, code, stderr := probeAXI(t, "status", "--axi", "--human")
	assert.Equal(t, exitUsage, code)
	assert.Contains(t, stderr, "--axi and --human are mutually exclusive")
	_, code, _ = probeAXI(t, "", "--axi", "--human")
	assert.Equal(t, exitUsage, code)
}

// TestOutputMode_CommandsWithoutAXIModeKeepText: a command with no AXI mode is
// never flipped, on a pipe or under an agent marker, and accepts --human.
func TestOutputMode_CommandsWithoutAXIModeKeepText(t *testing.T) {
	for _, path := range []string{"history", "doctor", "personas list", "debt list", "debt report"} {
		t.Run(path, func(t *testing.T) {
			isolate(t)
			clearOutputModeEnv(t)
			t.Setenv("ATCR_FORMAT", "axi")
			t.Setenv("CLAUDECODE", "1")
			got, code, stderr := probeAXI(t, path)
			require.Equal(t, 0, code, "stderr: %s", stderr)
			assert.False(t, got)
			_, code, stderr = probeAXI(t, path, "--human")
			require.Equal(t, 0, code, "stderr: %s", stderr)
		})
	}
}

// TestOutputMode_UnknownATCRFormatWarnsAndUsesTheDefault: a typo is visible on
// stderr and falls back to the TTY/agent default instead of failing the run.
func TestOutputMode_UnknownATCRFormatWarnsAndUsesTheDefault(t *testing.T) {
	isolate(t)
	clearOutputModeEnv(t)
	t.Setenv("ATCR_FORMAT", "toon")
	got, code, stderr := probeAXI(t, "status")
	require.Equal(t, 0, code)
	assert.True(t, got, "the pipe default still applies")
	assert.Contains(t, stderr, `unrecognized ATCR_FORMAT value "toon"`)

	fakeTTY(t)
	got, _, _ = probeAXI(t, "status")
	assert.False(t, got, "the terminal default still applies")
}

// TestOutputMode_ATCRFormatIsCaseInsensitive: AXI and " Text " are accepted.
func TestOutputMode_ATCRFormatIsCaseInsensitive(t *testing.T) {
	isolate(t)
	clearOutputModeEnv(t)
	fakeTTY(t)
	t.Setenv("ATCR_FORMAT", "AXI")
	got, _, stderr := probeAXI(t, "status")
	assert.True(t, got)
	assert.Empty(t, stderr)
}

// TestOutputMode_NonFileWriterKeepsText: an embedder or test that hands the
// tree an in-memory writer has no stdout to inspect, so its output is unchanged.
func TestOutputMode_NonFileWriterKeepsText(t *testing.T) {
	isolate(t)
	clearOutputModeEnv(t)
	code, stdout, _ := execCmdSplit(t)
	require.Equal(t, 0, code)
	assert.NotContains(t, stdout, "home[1")
}

// TestOutputMode_ExplicitJSONBeatsTheDefault: the debt commands' --json is an
// explicit format choice, so it wins over the pipe default and ATCR_FORMAT=axi.
func TestOutputMode_ExplicitJSONBeatsTheDefault(t *testing.T) {
	for _, path := range []string{"debt clean", "debt filter", "debt group", "debt validate"} {
		t.Run(path, func(t *testing.T) {
			isolate(t)
			clearOutputModeEnv(t)
			t.Setenv("ATCR_FORMAT", "axi")
			got, code, stderr := probeAXI(t, path, "--json")
			require.Equal(t, 0, code, "stderr: %s", stderr)
			assert.False(t, got)
		})
	}
}

// TestOutputMode_AutoFixBeatsTheDefault: review --auto-fix is an interactive
// flow, so on the agent path it runs human instead of hitting the --axi
// mutual-exclusion error; an explicit --axi --auto-fix is still rejected.
func TestOutputMode_AutoFixBeatsTheDefault(t *testing.T) {
	isolate(t)
	clearOutputModeEnv(t)
	got, code, stderr := probeAXI(t, "review", "--auto-fix")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.False(t, got)

	got, code, _ = probeAXI(t, "review", "--auto-fix", "--axi")
	require.Equal(t, 0, code)
	assert.True(t, got, "the explicit flag still reaches runReview, which rejects the pair")
}

// TestOutputMode_LegacyPipeFlagFollowsTheResolvedMode: --legacy-pipe needs AXI
// output, which the pipe default now supplies; on a terminal it still needs --axi.
func TestOutputMode_LegacyPipeFlagFollowsTheResolvedMode(t *testing.T) {
	isolate(t)
	clearOutputModeEnv(t)
	got, code, stderr := probeAXI(t, "", "--legacy-pipe")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.True(t, got)

	fakeTTY(t)
	_, code, stderr = probeAXI(t, "", "--legacy-pipe")
	assert.Equal(t, exitUsage, code)
	assert.Contains(t, stderr, "--legacy-pipe requires --axi")
}

// TestOutputMode_BareAtcrPipedIsTheAXIHomeView: the no-argument call on the
// agent path is byte-for-byte `atcr --axi`, help[] included, exit 0.
func TestOutputMode_BareAtcrPipedIsTheAXIHomeView(t *testing.T) {
	isolate(t)
	clearOutputModeEnv(t)
	code, want, _ := execCmdSplit(t, "--axi")
	require.Equal(t, 0, code)

	code, got, stderr := execPiped(t)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Equal(t, want, got)
	assert.True(t, strings.HasPrefix(got, "home[1"), "the home payload comes first: %q", got)
	assert.Contains(t, got, "help[1]: atcr review\n")

	t.Setenv("ATCR_FORMAT", "axi")
	code, got, _ = execCmdSplit(t)
	require.Equal(t, 0, code)
	assert.Equal(t, want, got, "ATCR_FORMAT=axi gives the same view on any writer")
}

// TestOutputMode_BareAtcrHumanPaths: a terminal, --human and ATCR_FORMAT=text
// each give the unchanged human home view.
func TestOutputMode_BareAtcrHumanPaths(t *testing.T) {
	isolate(t)
	clearOutputModeEnv(t)
	code, want, _ := execCmdSplit(t)
	require.Equal(t, 0, code)
	require.NotContains(t, want, "home[1")

	_, got, _ := execPiped(t, "--human")
	assert.Equal(t, want, got, "--human")

	t.Run("ATCR_FORMAT=text", func(t *testing.T) {
		t.Setenv("ATCR_FORMAT", "text")
		_, got, _ := execPiped(t)
		assert.Equal(t, want, got)
	})
	t.Run("tty", func(t *testing.T) {
		fakeTTY(t)
		_, got, _ := execPiped(t)
		assert.Equal(t, want, got)
	})
}

// TestOutputMode_StatusEndToEnd: status on a pipe is its --axi payload, and on
// a terminal its unchanged JSON document.
func TestOutputMode_StatusEndToEnd(t *testing.T) {
	isolate(t)
	clearOutputModeEnv(t)
	writeStatusFixture(t, ".", "r")

	code, got, stderr := execPiped(t, "status")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Equal(t, statusAXIStdout(), got)

	fakeTTY(t)
	code, got, _ = execPiped(t, "status")
	require.Equal(t, 0, code)
	assert.Equal(t, statusFixtureJSON, got)
}

// TestOutputMode_ReportEndToEnd: report with no --format renders AXI on the
// agent path when it writes to stdout, markdown on a terminal or with --output.
func TestOutputMode_ReportEndToEnd(t *testing.T) {
	isolate(t)
	clearOutputModeEnv(t)
	js := manyFindingsJSON(t, 3)
	fixtureReconciled(t, "r", js)

	var fs []reconcile.JSONFinding
	require.NoError(t, json.Unmarshal([]byte(js), &fs))
	var axi bytes.Buffer
	require.NoError(t, report.RenderAXIPaginated(&axi, fs, report.AXIMaxLinesDefault))
	require.NoError(t, goaxi.WriteHelp(&axi, []string{"atcr debt list", "atcr verify <id>"}))
	_, md, _ := execCmdSplit(t, "report", "--format", "md", "r")

	code, got, stderr := execPiped(t, "report", "r")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Equal(t, axi.String(), got, "piped report defaults to AXI")

	_, got, _ = execPiped(t, "report", "--human", "r")
	assert.Equal(t, md, got, "--human")

	_, got, _ = execPiped(t, "report", "--format", "md", "r")
	assert.Equal(t, md, got, "an explicit --format wins")

	_, got, _ = execPiped(t, "report", "--json", "r")
	assert.True(t, strings.HasPrefix(strings.TrimSpace(got), "["), "an explicit --json wins: %q", got)

	out := filepath.Join(t.TempDir(), "report.md")
	code, _, _ = execPiped(t, "report", "--output", out, "r")
	require.Equal(t, 0, code)
	written, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, md, string(written), "--output keeps the markdown default")

	t.Run("tty", func(t *testing.T) {
		fakeTTY(t)
		_, got, _ := execPiped(t, "report", "r")
		assert.Equal(t, md, got)
	})
	t.Run("ATCR_FORMAT=text", func(t *testing.T) {
		t.Setenv("ATCR_FORMAT", "text")
		_, got, _ := execPiped(t, "report", "r")
		assert.Equal(t, md, got)
	})
}

// TestReportCmd_FormatTextIsMarkdown: --format text is report's alias of md,
// on any writer.
func TestReportCmd_FormatTextIsMarkdown(t *testing.T) {
	isolate(t)
	clearOutputModeEnv(t)
	fixtureReconciled(t, "r", manyFindingsJSON(t, 3))
	_, md, _ := execCmdSplit(t, "report", "--format", "md", "r")

	code, got, stderr := execCmdSplit(t, "report", "--format", "text", "r")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Equal(t, md, got)

	code, got, _ = execPiped(t, "report", "--format", "text", "r")
	require.Equal(t, 0, code)
	assert.Equal(t, md, got, "an explicit text beats the agent-path default")

	code, _, stderr = execCmdSplit(t, "report", "--disagreements", "--format", "text", "r")
	assert.Equal(t, 0, code, "text is md for the radar too; stderr: %s", stderr)
}

// TestOutputMode_ReportDisagreementsKeepsMarkdown: the radar has no AXI view,
// so on the agent path it keeps its markdown default instead of failing.
func TestOutputMode_ReportDisagreementsKeepsMarkdown(t *testing.T) {
	isolate(t)
	clearOutputModeEnv(t)
	fixtureReconciled(t, "r", manyFindingsJSON(t, 3))
	_, want, _ := execCmdSplit(t, "report", "--disagreements", "r")

	code, got, stderr := execPiped(t, "report", "--disagreements", "r")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Equal(t, want, got)
}
