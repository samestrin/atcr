package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/verify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	toon "github.com/toon-format/toon-go"
)

// verifyAXIResult is the stubbed run outcome the --axi tests emit: distinct
// non-zero counts so a swapped or dropped field cannot pass by coincidence.
func verifyAXIResult() verify.Result {
	return verify.Result{
		FindingsProcessed: 4,
		VerdictCounts:     verify.VerdictCounts{Confirmed: 2, Refuted: 1, Unverifiable: 1},
	}
}

// verifyAXIStdout is the exact --axi stdout of verifyAXIResult: the run values
// as one TOON object, then the suggestVerify line as help[].
func verifyAXIStdout() string {
	return "findings: 4\nconfirmed: 2\nrefuted: 1\nunverifiable: 1\nreconciled_dir: " +
		filepath.Join(".atcr", "reviews", "r", "reconciled") + "\n" +
		"help[1]: atcr report <id>\n"
}

// verifyAXIFixture stages the hermetic registry + reconciled review and stubs
// the verify stage with res, so the CLI output shape is tested without a model.
func verifyAXIFixture(t *testing.T, res verify.Result) {
	t.Helper()
	isolate(t)
	writeVerifyRegistry(t)
	verifyFixture(t, "r", []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "HIGH",
	}})
	stubVerifyRun(t, res)
}

// TestVerifyCmd_AXI (Epic 104.1 T2): under --axi stdout is exactly the run
// payload followed by the help[] block, with no prose line, and stderr carries
// no Next: lines.
func TestVerifyCmd_AXI(t *testing.T) {
	verifyAXIFixture(t, verifyAXIResult())
	code, stdout, stderr := execCmdSplit(t, "verify", "--axi", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, verifyAXIStdout(), stdout)
	assert.NotContains(t, stdout, "verified 4 finding(s)", "the prose line is gated under --axi")
	assert.NotContains(t, stderr, "Next:", "--axi carries its next steps on stdout, not stderr")

	var got map[string]any
	require.NoError(t, toon.Unmarshal([]byte(stdout), &got), "stdout decodes as one TOON value")
	assert.EqualValues(t, 4, got["findings"])
	assert.EqualValues(t, 2, got["confirmed"])
	assert.EqualValues(t, 1, got["refuted"])
	assert.EqualValues(t, 1, got["unverifiable"])
	assert.Len(t, got["help"], 1)
}

// TestVerifyCmd_AXILogFormatJSON: under --axi --log-format json no Next:
// record reaches the NDJSON stderr either; stdout is the same payload.
func TestVerifyCmd_AXILogFormatJSON(t *testing.T) {
	verifyAXIFixture(t, verifyAXIResult())
	code, stdout, stderr := execCmdSplit(t, "--log-format", "json", "verify", "--axi", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, verifyAXIStdout(), stdout)
	for _, line := range nonEmptyLines(stderr) {
		v := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(line), &v), "stderr stays NDJSON: %q", line)
		msg, _ := v["msg"].(string)
		assert.False(t, strings.HasPrefix(msg, "Next:"), "no Next: record under --axi: %q", msg)
	}
}

// TestVerifyCmd_AXILegacyPipeRejected: verify has no legacy pipe encoder, so
// --legacy-pipe is a usage error, and ATCR_LEGACY_PIPE=1 changes nothing and
// announces no deprecated encoder.
func TestVerifyCmd_AXILegacyPipeRejected(t *testing.T) {
	verifyAXIFixture(t, verifyAXIResult())
	code, stdout, _ := execCmdSplit(t, "verify", "--axi", "--legacy-pipe", "r")
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)

	t.Setenv("ATCR_LEGACY_PIPE", "1")
	code, stdout, stderr := execCmdSplit(t, "verify", "--axi", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, verifyAXIStdout(), stdout)
	assert.NotContains(t, stderr, "deprecated", "no deprecation notice for an encoder this command lacks")
}

// TestVerifyCmd_AXITextModeUnchanged: without --axi stdout and stderr are the
// bytes verify printed before --axi existed.
func TestVerifyCmd_AXITextModeUnchanged(t *testing.T) {
	verifyAXIFixture(t, verifyAXIResult())
	code, stdout, stderr := execCmdSplit(t, "verify", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "verified 4 finding(s): 2 confirmed, 1 refuted, 1 unverifiable -> "+
		filepath.Join(".atcr", "reviews", "r", "reconciled")+"\n", stdout)
	assert.True(t, strings.HasSuffix(stderr, "Next: atcr report <id>\n"), "stderr: %q", stderr)
}

// TestVerifyCmd_TextModeLogFormatJSON: routing the text path through the T1
// helper (writeNextStepsCmd) means that under --log-format json without --axi
// the Next: line rides the context logger as an NDJSON warn record, matching
// reconcile, instead of a raw line that would break NDJSON-only stderr.
func TestVerifyCmd_TextModeLogFormatJSON(t *testing.T) {
	verifyAXIFixture(t, verifyAXIResult())
	code, stdout, stderr := execCmdSplit(t, "--log-format", "json", "verify", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "verified 4 finding(s): 2 confirmed, 1 refuted, 1 unverifiable -> "+
		filepath.Join(".atcr", "reviews", "r", "reconciled")+"\n", stdout)
	var next []string
	for _, line := range nonEmptyLines(stderr) {
		v := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(line), &v), "stderr stays NDJSON: %q", line)
		if msg, _ := v["msg"].(string); strings.HasPrefix(msg, "Next:") {
			next = append(next, msg)
		}
	}
	assert.Equal(t, []string{"Next: atcr report <id>"}, next)
}

// TestVerifyCmd_AXIHarnessUnavailableStaysPlainError: the harnessUnavailableError
// return fires before the AXI emission, so a run whose skeptics could not read
// the code stays a plain error (exit 1) with no TOON payload and no help block.
func TestVerifyCmd_AXIHarnessUnavailableStaysPlainError(t *testing.T) {
	verifyAXIFixture(t, verify.Result{
		FindingsProcessed:  2,
		VerdictCounts:      verify.VerdictCounts{Unverifiable: 2},
		HarnessUnavailable: 2,
		HarnessReason:      "the review recorded no head commit to snapshot",
	})
	code, stdout, stderr := execCmdSplit(t, "verify", "--axi", "r")
	assert.Equal(t, 1, code)
	assert.Empty(t, stdout, "no payload and no help[] on the harness-unavailable path")
	assert.NotContains(t, stderr, "Next:")
	assert.NotContains(t, stderr, "help[")

	// Text mode keeps its bytes: the prose line still prints before the error.
	code, stdout, _ = execCmdSplit(t, "verify", "r")
	assert.Equal(t, 1, code)
	assert.Equal(t, "verified 2 finding(s): 0 confirmed, 0 refuted, 2 unverifiable -> "+
		filepath.Join(".atcr", "reviews", "r", "reconciled")+"\n", stdout)
}

// execVerifyBrokenStdout runs args through the real root with a stdout that
// fails every write, and returns the command's error.
func execVerifyBrokenStdout(t *testing.T, args ...string) error {
	t.Helper()
	var errBuf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetOut(failWriter{})
	root.SetErr(&errBuf)
	return root.ExecuteContext(context.Background())
}

// TestVerifyCmd_AXIRenderFaultReported: with no other error pending, a broken
// stdout under --axi is reported as the encoder's fault, not swallowed.
func TestVerifyCmd_AXIRenderFaultReported(t *testing.T) {
	verifyAXIFixture(t, verifyAXIResult())
	err := execVerifyBrokenStdout(t, "verify", "--axi", "r")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "axi encoder")
}

// TestVerifyCmd_AXIRenderFaultKeepsHarnessError: a broken stdout never replaces
// the harness-unavailable error the run already has.
func TestVerifyCmd_AXIRenderFaultKeepsHarnessError(t *testing.T) {
	verifyAXIFixture(t, verify.Result{
		VerdictCounts:      verify.VerdictCounts{Unverifiable: 2},
		HarnessUnavailable: 2,
		HarnessReason:      "the review recorded no head commit to snapshot",
	})
	err := execVerifyBrokenStdout(t, "verify", "--axi", "r")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verification did not run", "the harness error is kept")
	assert.NotContains(t, err.Error(), "axi encoder")
	assert.Equal(t, 1, exitCode(err))
}
