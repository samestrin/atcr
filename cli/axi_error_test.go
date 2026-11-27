package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	goaxi "github.com/samestrin/go-axi"
)

// Epic 165 T2: structured errors on stdout. Each test clears ATCR_FORMAT and
// the agent markers (clearOutputModeEnv), so the agent environment the suite
// may run under cannot flip the mode, and turns telemetry off, so Main neither
// transmits nor prints the first-run disclosure on stderr.

// mainPiped runs Main with os.Args set to args and stdout on a real pipe (not a
// terminal, unless fakeTTY is in force), and returns the exit code, stdout and
// stderr.
func mainPiped(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Setenv("ATCR_TELEMETRY", "0")
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	os.Args = append([]string{"atcr"}, args...)
	r, w, err := os.Pipe()
	require.NoError(t, err)
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	var errBuf bytes.Buffer
	code = Main(context.Background(), w, &errBuf)
	require.NoError(t, w.Close())
	out := <-done
	require.NoError(t, r.Close())
	return code, string(out), errBuf.String()
}

// errorDoc decodes an AXI error document and checks its fixed fields.
func errorDoc(t *testing.T, stdout string, wantCode int) map[string]any {
	t.Helper()
	v, err := goaxi.Decode(strings.NewReader(stdout))
	require.NoError(t, err, "stdout is one TOON document: %q", stdout)
	doc, ok := v.(map[string]any)
	require.True(t, ok, "stdout decodes to an object: %q", stdout)
	assert.Equal(t, true, doc["error"], "stdout: %q", stdout)
	assert.EqualValues(t, wantCode, doc["code"], "stdout: %q", stdout)
	return doc
}

// helpLines returns the document's help[] as strings.
func helpLines(t *testing.T, doc map[string]any) []string {
	t.Helper()
	raw, ok := doc["help"].([]any)
	require.True(t, ok, "help[] present: %v", doc)
	lines := make([]string, len(raw))
	for i, l := range raw {
		lines[i] = l.(string)
	}
	return lines
}

func TestAXIError_UnknownRootFlag(t *testing.T) {
	clearOutputModeEnv(t)
	isolate(t)
	code, stdout, stderr := mainPiped(t, "--bogus")
	assert.Equal(t, 2, code)
	assert.Empty(t, stderr)
	doc := errorDoc(t, stdout, 2)
	assert.Equal(t, "unknown flag: --bogus", doc["message"])
	help := helpLines(t, doc)
	for _, f := range []string{"--axi", "--human", "--log-format", "-h/--help", "-v/--version"} {
		assert.Contains(t, help, f, "help[] names the root's valid flags")
	}
}

func TestAXIError_UnknownCommand(t *testing.T) {
	clearOutputModeEnv(t)
	isolate(t)
	code, stdout, stderr := mainPiped(t, "nosuch")
	assert.Equal(t, 2, code)
	assert.Empty(t, stderr)
	doc := errorDoc(t, stdout, 2)
	assert.Equal(t, `unknown command "nosuch" for "atcr"`, doc["message"])
	help := helpLines(t, doc)
	for _, c := range []string{"atcr review", "atcr status", "atcr debt", "atcr version"} {
		assert.Contains(t, help, c, "help[] lists the commands")
	}
}

// TestAXIError_UnknownSubcommandOfAGroup: a command group without an AXI mode
// still reports its unknown subcommand as the document, listing its own
// subcommands rather than the root's.
func TestAXIError_UnknownSubcommandOfAGroup(t *testing.T) {
	clearOutputModeEnv(t)
	isolate(t)
	code, stdout, stderr := mainPiped(t, "debt", "nosuch")
	assert.Equal(t, 2, code)
	assert.Empty(t, stderr)
	help := helpLines(t, errorDoc(t, stdout, 2))
	assert.Contains(t, help, "atcr debt list")
	assert.NotContains(t, help, "atcr review")
}

// TestAXIError_UnknownFlagBeforeAXI: the bad flag stops cobra's parse before it
// reaches --axi, and before PersistentPreRunE, so the mode comes from the raw
// args. Under a fake terminal only that --axi makes it AXI.
func TestAXIError_UnknownFlagBeforeAXI(t *testing.T) {
	clearOutputModeEnv(t)
	fakeTTY(t)
	isolate(t)
	code, stdout, stderr := mainPiped(t, "status", "--bogus", "--axi")
	assert.Equal(t, 2, code)
	assert.Empty(t, stderr)
	doc := errorDoc(t, stdout, 2)
	help := helpLines(t, doc)
	assert.Contains(t, help, "--axi", "help[] names status's own flags")
	assert.Contains(t, help, "--human", "and its inherited ones")
	assert.NotContains(t, help, "--legacy-pipe", "not the root's local flags")
}

func TestAXIError_StatusMissingReview(t *testing.T) {
	clearOutputModeEnv(t)
	isolate(t)
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	code, stdout, stderr := mainPiped(t, "status", "--axi", "missing")
	assert.Equal(t, 2, code)
	assert.Empty(t, stderr)
	doc := errorDoc(t, stdout, 2)
	assert.Equal(t, "review not found: missing", doc["message"])
	assert.Equal(t, []string{"atcr status --help"}, helpLines(t, doc))
}

// TestAXIError_BadConfig: a bad .atcr/config.yaml value is a usage error, and a
// piped run reports it as the document with no --axi given.
func TestAXIError_BadConfig(t *testing.T) {
	clearOutputModeEnv(t)
	isolate(t)
	fixtureReview(t, "r", trustPanelSources())
	writeProjectConfig(t, "consensus: bogus\n")
	code, stdout, stderr := mainPiped(t, "reconcile", "--no-scorecard", "--no-local-debt", "r")
	assert.Equal(t, 2, code)
	assert.Empty(t, stderr)
	doc := errorDoc(t, stdout, 2)
	assert.Contains(t, doc["message"], "invalid consensus level")
	assert.Equal(t, []string{"atcr reconcile --help"}, helpLines(t, doc))
}

// TestAXIError_BadLogFormat: an error inside PersistentPreRunE, before it
// resolves the mode, still takes the AXI path from the raw args.
func TestAXIError_BadLogFormat(t *testing.T) {
	clearOutputModeEnv(t)
	isolate(t)
	code, stdout, stderr := mainPiped(t, "--log-format", "bogus")
	assert.Equal(t, 2, code)
	assert.Empty(t, stderr)
	errorDoc(t, stdout, 2)
}

// TestAXIError_GateKeepsPayload: a --fail-on gate exit has already written its
// payload, so stdout is that payload alone, with no second document after it.
func TestAXIError_GateKeepsPayload(t *testing.T) {
	clearOutputModeEnv(t)
	isolate(t)
	nextStepsReviewFixture(t)
	code, stdout, _ := mainPiped(t, "reconcile", "--axi", "--fail-on", "low", "--no-scorecard", "--no-local-debt", "r")
	assert.Equal(t, 1, code)
	assert.Equal(t, reconcileAXIStdout(), stdout)
}

// TestAXIError_MessageIsRedacted: the message goes through terminalErrorLine's
// redactor, so a secret-shaped token in an error never reaches stdout.
func TestAXIError_MessageIsRedacted(t *testing.T) {
	var buf bytes.Buffer
	err := errors.New("docker: Authorization: Bearer abcdef0123456789abcdef0123456789")
	require.NoError(t, writeErrorDocument(&buf, err, 1, []string{"atcr verify --help"}))
	assert.NotContains(t, buf.String(), "abcdef0123456789abcdef0123456789")
	doc := errorDoc(t, buf.String(), 1)
	assert.Equal(t, terminalErrorLine(err), doc["message"])
}

// TestAXIError_HumanModeKeepsStderr: on a terminal, and with --human or
// ATCR_FORMAT=text on a pipe, the failures keep today's `atcr: <message>` line
// on stderr and print nothing on stdout.
func TestAXIError_HumanModeKeepsStderr(t *testing.T) {
	cases := []struct {
		name string
		tty  bool
		env  string
		args []string
		want string
	}{
		{name: "tty unknown flag", tty: true, args: []string{"--bogus"}, want: "atcr: unknown flag: --bogus\n"},
		{name: "tty unknown command", tty: true, args: []string{"nosuch"}, want: "atcr: unknown command \"nosuch\" for \"atcr\"\n"},
		{name: "tty missing review", tty: true, args: []string{"status", "missing"}, want: "atcr: review not found: missing\n"},
		{name: "pipe --human", args: []string{"status", "--human", "missing"}, want: "atcr: review not found: missing\n"},
		{name: "pipe --human unknown flag", args: []string{"--human", "--bogus"}, want: "atcr: unknown flag: --bogus\n"},
		{name: "pipe ATCR_FORMAT=text", env: "text", args: []string{"nosuch"}, want: "atcr: unknown command \"nosuch\" for \"atcr\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearOutputModeEnv(t)
			if tc.tty {
				fakeTTY(t)
			}
			if tc.env != "" {
				t.Setenv("ATCR_FORMAT", tc.env)
			}
			isolate(t)
			require.NoError(t, os.MkdirAll(".atcr", 0o755))
			code, stdout, stderr := mainPiped(t, tc.args...)
			assert.Equal(t, 2, code)
			assert.Empty(t, stdout)
			assert.Equal(t, tc.want, stderr)
		})
	}
}

// TestAXIError_CommandWithoutAXIModeKeepsStderr: a command with no AXI mode
// keeps the stderr line even when piped.
func TestAXIError_CommandWithoutAXIModeKeepsStderr(t *testing.T) {
	clearOutputModeEnv(t)
	isolate(t)
	code, stdout, stderr := mainPiped(t, "init", "--bogus")
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)
	assert.Equal(t, "atcr: unknown flag: --bogus\n", stderr)
}

// TestStdoutFile_LooksThroughPayloadWriter: wrapping stdout to track payload
// writes must not hide the terminal from the mode resolver or quickstart.
func TestStdoutFile_LooksThroughPayloadWriter(t *testing.T) {
	f, ok := stdoutFile(&payloadWriter{w: os.Stdout})
	require.True(t, ok)
	assert.Same(t, os.Stdout, f)
	_, ok = stdoutFile(&payloadWriter{w: &bytes.Buffer{}})
	assert.False(t, ok)
}
