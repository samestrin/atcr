package verify

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/repro"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseExecEvidence(t *testing.T) {
	t.Parallel()

	ev := parseExecEvidence("$ go test ./calc\nexit code: 1\nFAIL: want 4 got 5\n")
	require.NotNil(t, ev)
	assert.Equal(t, "go test ./calc", ev.Command)
	assert.Equal(t, 1, ev.ExitCode)
	assert.Equal(t, "FAIL: want 4 got 5\n", ev.OutputExcerpt)

	// A timed-out marker is stripped from the exit code.
	ev = parseExecEvidence("$ sleep 99\nexit code: 124 (timed out)\n")
	require.NotNil(t, ev)
	assert.Equal(t, 124, ev.ExitCode)
	assert.Equal(t, "sleep 99", ev.Command)

	// Content that is not the runInSandbox shape yields no evidence.
	assert.Nil(t, parseExecEvidence("tool error: boom"))
	assert.Nil(t, parseExecEvidence(""))
}

// TestVerifyFinding_ExecEvidenceStampedOnConfirm is the end-to-end check for the
// repro write-back (Epic 11.0 SC-3/SC-4): a skeptic runs run_script, reproduces a
// planted bug (exit 1), and confirms; the reproduced command/exit/output must
// surface as EvidenceExec and stamp onto the finding without downgrading verdict.
func TestVerifyFinding_ExecEvidenceStampedOnConfirm(t *testing.T) {
	t.Parallel()
	reproduced := "$ /bin/sh -s <<'EOF'\n./repro.sh\nEOF\nexit code: 1\nassertion failed: want 4 got 5\n"
	cc := &fakeChatCompleter{turns: []chatTurn{
		toolCallTurn("run_script"),
		{content: `{"verdict":"confirmed","reasoning":"reproduced via run_script"}`},
	}}
	disp := &fakeDispatcher{result: tools.ToolResult{Content: reproduced, OriginalBytes: len(reproduced)}}
	f := reconcile.JSONFinding{File: "calc.go", Line: 10, Problem: "off-by-one"}

	ver, _, ev := verifyFinding(context.Background(), f, []Skeptic{testSkeptic()}, cc, disp, true)
	require.Equal(t, verdictConfirmed, ver.Verdict)
	require.NotNil(t, ev, "a reproduced run on a confirmed verdict must yield execution evidence")
	assert.Equal(t, 1, ev.ExitCode)
	assert.Contains(t, ev.Command, "/bin/sh -s")
	assert.Contains(t, ev.OutputExcerpt, "assertion failed")

	// The write-back stamps it onto the finding and keeps the confirmed verdict.
	repro.Stamp(&f, ver.Verdict, ev)
	require.NotNil(t, f.EvidenceExec)
	assert.Equal(t, 1, f.EvidenceExec.ExitCode)
	assert.Equal(t, verdictConfirmed, f.Verification.Verdict)
}

// TestVerifyFinding_NoEvidenceWhenRefuted verifies evidence is not surfaced for a
// non-confirmed verdict even if a tool ran with a non-zero exit.
func TestVerifyFinding_NoEvidenceWhenRefuted(t *testing.T) {
	t.Parallel()
	reproduced := "$ /bin/sh -s <<'EOF'\n./repro.sh\nEOF\nexit code: 1\nnope\n"
	cc := &fakeChatCompleter{turns: []chatTurn{
		toolCallTurn("run_script"),
		{content: `{"verdict":"refuted","reasoning":"not actually a bug"}`},
	}}
	disp := &fakeDispatcher{result: tools.ToolResult{Content: reproduced, OriginalBytes: len(reproduced)}}
	f := reconcile.JSONFinding{File: "calc.go", Line: 10, Problem: "off-by-one"}

	ver, _, ev := verifyFinding(context.Background(), f, []Skeptic{testSkeptic()}, cc, disp, true)
	require.Equal(t, verdictRefuted, ver.Verdict)
	assert.Nil(t, ev, "a refuted verdict must not surface a Reproduced evidence block")
}
