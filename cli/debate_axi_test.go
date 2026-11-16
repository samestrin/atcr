package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	toon "github.com/toon-format/toon-go"

	"github.com/samestrin/atcr/internal/reconcile"
)

// debateAXIFixture seeds the one-disputed-finding review the debate text tests
// use: no skeptic/judge roles are configured, so the item stays unresolved and
// the run exits 0 without any LLM call (TestDebateCmd_UnresolvedWithoutRoles).
func debateAXIFixture(t *testing.T) {
	t.Helper()
	isolate(t)
	writeVerifyRegistry(t)
	verifyFixture(t, "r", []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "HIGH",
		Reviewers: []string{"bruce"}, Disagreement: "MEDIUM vs HIGH",
	}})
}

// debateAXIStdout is the exact --axi stdout of that fixture: the run values as
// one TOON object, then the suggestDebate line as help[].
func debateAXIStdout() string {
	return "selected: 1\nupheld: 0\noverturned: 0\nsplit: 0\nunresolved: 1\noverflow: 0\nwithheld: 0\n" +
		"reconciled_dir: " + filepath.Join(".atcr", "reviews", "r", "reconciled") + "\n" +
		"help[1]: atcr report <id>\n"
}

// TestDebateCmd_AXI (Epic 104.1 T3): under --axi stdout is exactly the run
// payload followed by the help[] block, with no prose line, and stderr carries
// no Next: lines.
func TestDebateCmd_AXI(t *testing.T) {
	debateAXIFixture(t)
	code, stdout, stderr := execCmdSplit(t, "debate", "--axi", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, debateAXIStdout(), stdout)
	assert.NotContains(t, stdout, "debated 1 item(s)", "the prose line is gated under --axi")
	assert.NotContains(t, stderr, "Next:", "--axi carries its next steps on stdout, not stderr")

	var got map[string]any
	require.NoError(t, toon.Unmarshal([]byte(stdout), &got), "stdout decodes as one TOON value")
	assert.EqualValues(t, 1, got["selected"])
	assert.EqualValues(t, 1, got["unresolved"])
	assert.Len(t, got["help"], 1)
}

// TestDebateCmd_AXILogFormatJSON: under --axi --log-format json no Next:
// record reaches the NDJSON stderr either; stdout is the same payload.
func TestDebateCmd_AXILogFormatJSON(t *testing.T) {
	debateAXIFixture(t)
	code, stdout, stderr := execCmdSplit(t, "--log-format", "json", "debate", "--axi", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, debateAXIStdout(), stdout)
	for _, line := range nonEmptyLines(stderr) {
		v := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(line), &v), "stderr stays NDJSON: %q", line)
		msg, _ := v["msg"].(string)
		assert.False(t, strings.HasPrefix(msg, "Next:"), "no Next: record under --axi: %q", msg)
	}
}

// TestDebateCmd_AXILegacyPipeRejected: debate has no legacy pipe encoder, so
// --legacy-pipe is a usage error naming debate, and ATCR_LEGACY_PIPE=1 changes
// nothing and announces no deprecated encoder.
func TestDebateCmd_AXILegacyPipeRejected(t *testing.T) {
	debateAXIFixture(t)
	code, stdout, stderr := execCmdSplit(t, "debate", "--axi", "--legacy-pipe", "r")
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "--legacy-pipe is not supported by debate")

	t.Setenv("ATCR_LEGACY_PIPE", "1")
	code, stdout, stderr = execCmdSplit(t, "debate", "--axi", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, debateAXIStdout(), stdout)
	assert.NotContains(t, stderr, "deprecated", "no deprecation notice for an encoder this command lacks")
}

// TestDebateCmd_TextModeUnchanged: without --axi stdout and stderr are the
// bytes debate printed before --axi existed.
func TestDebateCmd_TextModeUnchanged(t *testing.T) {
	debateAXIFixture(t)
	code, stdout, stderr := execCmdSplit(t, "debate", "r")
	require.Equal(t, 0, code)
	assert.Equal(t, "debated 1 item(s): 0 upheld, 0 overturned, 0 split, 1 unresolved (0 overflow, 0 withheld) -> "+
		filepath.Join(".atcr", "reviews", "r", "reconciled")+"\n", stdout)
	assert.True(t, strings.HasSuffix(stderr, "Next: atcr report <id>\n"), "stderr: %q", stderr)
}

// TestDebateCmd_LogFormatJSONNextRecords: without --axi under --log-format json
// the Next: lines ride the context logger as NDJSON warn records (the T1
// helper's writeNextStepsCmd path), so stderr stays NDJSON-only; stdout keeps
// the prose line.
func TestDebateCmd_LogFormatJSONNextRecords(t *testing.T) {
	debateAXIFixture(t)
	code, stdout, stderr := execCmdSplit(t, "--log-format", "json", "debate", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "debated 1 item(s): 0 upheld, 0 overturned, 0 split, 1 unresolved (0 overflow, 0 withheld) -> "+
		filepath.Join(".atcr", "reviews", "r", "reconciled")+"\n", stdout)
	nexts := 0
	for _, line := range nonEmptyLines(stderr) {
		v := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(line), &v), "stderr stays NDJSON: %q", line)
		if msg, _ := v["msg"].(string); msg == "Next: atcr report <id>" {
			nexts++
		}
	}
	assert.Equal(t, 1, nexts, "the Next: line rides the logger exactly once")
}

// execDebateBrokenStdout runs args through the real root with a stdout that
// fails every write, and returns the command's error.
func execDebateBrokenStdout(t *testing.T, args ...string) error {
	t.Helper()
	var errBuf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetOut(failWriter{})
	root.SetErr(&errBuf)
	return root.ExecuteContext(context.Background())
}

// TestDebateCmd_AXIRenderFault: a broken stdout under --axi is reported as an
// axi encoder error (exit 1), not swallowed.
func TestDebateCmd_AXIRenderFault(t *testing.T) {
	debateAXIFixture(t)
	err := execDebateBrokenStdout(t, "debate", "--axi", "r")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "axi encoder")
	assert.Equal(t, 1, exitCode(err))
}
