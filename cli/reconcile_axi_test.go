package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	toon "github.com/toon-format/toon-go"
)

// reconcileAXIStdout is the exact --axi stdout of the one-finding fixture: the
// run values as one TOON object, then the suggestReconcile lines as help[].
func reconcileAXIStdout() string {
	return "findings: 1\nsources: 1\nreconciled_dir: " + filepath.Join(".atcr", "reviews", "r", "reconciled") + "\n" +
		"help[2]: atcr debt list,atcr verify <id>\n"
}

// TestReconcileCmd_AXI (Epic 104.1 T1): under --axi stdout is exactly the run
// payload followed by the help[] block, with no prose line, and stderr carries
// no Next: lines.
func TestReconcileCmd_AXI(t *testing.T) {
	isolate(t)
	nextStepsReviewFixture(t)
	code, stdout, stderr := execCmdSplit(t, "reconcile", "--axi", "--no-scorecard", "--no-local-debt", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, reconcileAXIStdout(), stdout)
	assert.NotContains(t, stdout, "reconciled 1 finding(s)", "the prose line is gated under --axi")
	assert.NotContains(t, stderr, "Next:", "--axi carries its next steps on stdout, not stderr")

	var got map[string]any
	require.NoError(t, toon.Unmarshal([]byte(stdout), &got), "stdout decodes as one TOON value")
	assert.EqualValues(t, 1, got["findings"])
	assert.Len(t, got["help"], 2)
}

// TestReconcileCmd_AXILogFormatJSON: under --axi --log-format json no Next:
// record reaches the NDJSON stderr either; stdout is the same payload.
func TestReconcileCmd_AXILogFormatJSON(t *testing.T) {
	isolate(t)
	nextStepsReviewFixture(t)
	code, stdout, stderr := execCmdSplit(t, "--log-format", "json", "reconcile", "--axi", "--no-scorecard", "--no-local-debt", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, reconcileAXIStdout(), stdout)
	for _, line := range nonEmptyLines(stderr) {
		v := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(line), &v), "stderr stays NDJSON: %q", line)
		msg, _ := v["msg"].(string)
		assert.False(t, strings.HasPrefix(msg, "Next:"), "no Next: record under --axi: %q", msg)
	}
}

// TestReconcileCmd_AXILegacyPipeRejected: reconcile has no legacy pipe encoder,
// so --legacy-pipe is a usage error, and ATCR_LEGACY_PIPE=1 changes nothing and
// announces no deprecated encoder.
func TestReconcileCmd_AXILegacyPipeRejected(t *testing.T) {
	isolate(t)
	nextStepsReviewFixture(t)
	code, stdout, _ := execCmdSplit(t, "reconcile", "--axi", "--legacy-pipe", "--no-scorecard", "--no-local-debt", "r")
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)

	t.Setenv("ATCR_LEGACY_PIPE", "1")
	code, stdout, stderr := execCmdSplit(t, "reconcile", "--axi", "--no-scorecard", "--no-local-debt", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, reconcileAXIStdout(), stdout)
	assert.NotContains(t, stderr, "deprecated", "no deprecation notice for an encoder this command lacks")
}

// TestReconcileCmd_TextModeUnchanged: without --axi stdout and stderr are the
// bytes reconcile printed before --axi existed.
func TestReconcileCmd_TextModeUnchanged(t *testing.T) {
	isolate(t)
	nextStepsReviewFixture(t)
	code, stdout, stderr := execCmdSplit(t, "reconcile", "--no-scorecard", "--no-local-debt", "r")
	require.Equal(t, 0, code)
	assert.Equal(t, "reconciled 1 finding(s) from 1 source(s) -> "+filepath.Join(".atcr", "reviews", "r", "reconciled")+"\n", stdout)
	assert.True(t, strings.HasSuffix(stderr, "Next: atcr debt list\nNext: atcr verify <id>\n"), "stderr: %q", stderr)
}

// TestReconcileCmd_AXIGateFailureStillEmits: a --fail-on gate that exits 1
// still reconciled the findings, so the payload and help[] are still written.
func TestReconcileCmd_AXIGateFailureStillEmits(t *testing.T) {
	isolate(t)
	nextStepsReviewFixture(t)
	code, stdout, stderr := execCmdSplit(t, "reconcile", "--axi", "--fail-on", "low", "--no-scorecard", "--no-local-debt", "r")
	assert.Equal(t, 1, code, stderr)
	assert.Equal(t, reconcileAXIStdout(), stdout)
}

// TestWriteAXIHelp_WriteFault: a broken stdout is reported, not swallowed.
func TestWriteAXIHelp_WriteFault(t *testing.T) {
	payload := toon.NewObject(toon.Field{Key: "findings", Value: 1})
	err := writeAXIHelp(failWriter{}, payload, []string{"atcr debt list"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "axi encoder")
}
