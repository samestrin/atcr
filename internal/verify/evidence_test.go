package verify

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/repro"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRedactEvidence covers the stamp-site scrub: a reproduced run's captured
// output reaches findings.json as data (not a log line), so it bypasses the log
// sink's redactor and must be scrubbed here before repro.Stamp persists it.
func TestRedactEvidence(t *testing.T) {
	t.Parallel()
	secret := "sk-live-aaaaaaaaaaaaaaaa"
	r := log.NewRedactor("", secret)

	ev := &reconcile.EvidenceExec{Command: "go test", ExitCode: 1, OutputExcerpt: "echoed " + secret + " in output"}
	redactEvidence(ev, r)
	assert.NotContains(t, ev.OutputExcerpt, secret, "configured secret must be scrubbed from evidence output")
	assert.Contains(t, ev.OutputExcerpt, "[redacted]")

	// A nil redactor (no review secrets configured) is a no-op.
	ev2 := &reconcile.EvidenceExec{OutputExcerpt: "untouched " + secret}
	redactEvidence(ev2, nil)
	assert.Contains(t, ev2.OutputExcerpt, secret)

	// A nil evidence block must not panic.
	redactEvidence(nil, r)
}

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

// TestVerifyFinding_ExecEvidenceRequiresDeterministicRepro is the live T3 gate:
// a skeptic reproduces once (exit 1 → confirmed), but the two-run determinism
// re-run is flaky (exit 1 then exit 0), so no execution evidence may be surfaced
// — a non-deterministic reproduction must not earn a Reproduced badge.
func TestVerifyFinding_ExecEvidenceRequiresDeterministicRepro(t *testing.T) {
	t.Parallel()
	fail := "$ /bin/sh -s <<'EOF'\n./repro.sh\nEOF\nexit code: 1\nassertion failed\n"
	pass := "$ /bin/sh -s <<'EOF'\n./repro.sh\nEOF\nexit code: 0\nok\n"
	cc := &fakeChatCompleter{turns: []chatTurn{
		toolCallTurn("run_script"),
		{content: `{"verdict":"confirmed","reasoning":"reproduced once"}`},
	}}
	// call 1 = skeptic's in-loop repro (fail); calls 2 & 3 = determinism re-runs
	// (fail, then pass) → exit codes disagree → unverifiable → no evidence.
	disp := &sequencedDispatcher{results: []tools.ToolResult{
		{Content: fail, OriginalBytes: len(fail)},
		{Content: fail, OriginalBytes: len(fail)},
		{Content: pass, OriginalBytes: len(pass)},
	}}
	f := reconcile.JSONFinding{File: "calc.go", Line: 10, Problem: "off-by-one"}

	ver, _, ev := verifyFinding(context.Background(), f, []Skeptic{testSkeptic()}, cc, disp, true)
	require.Equal(t, verdictConfirmed, ver.Verdict, "the skeptic's own vote still confirms")
	assert.Nil(t, ev, "a non-deterministic repro must not surface execution evidence")
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
