package verify

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	reclib "github.com/samestrin/atcr/reconcile"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/repro"
	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/samestrin/atcr/internal/tools"
)

// redactEvidence scrubs configured secrets from a reproduced run's captured
// output before it is stamped onto a finding. The sandbox has no host env and no
// network, but a reproduced test can echo secret-bearing repo content, and the
// evidence_exec block lands in findings.json as data — bypassing the log sink's
// redactor (Epic 4.9). A nil redactor (no review secrets configured) or nil
// evidence is a no-op.
func redactEvidence(ev *reconcile.EvidenceExec, r *log.Redactor) {
	if ev == nil || r == nil {
		return
	}
	ev.OutputExcerpt = r.Redact(ev.OutputExcerpt)
}

// execEvidenceRecorder wraps a Dispatcher and captures the last reproduced
// (non-zero exit) run_tests/run_script result as an EvidenceExec block. A skeptic
// that demonstrates a finding by executing code in the sandbox therefore leaves
// structured evidence the repro write-back can stamp onto the finding, turning an
// asserted verdict into a demonstrated one (Epic 11.0 SC-3/SC-4).
type execEvidenceRecorder struct {
	inner Dispatcher
	last  *reconcile.EvidenceExec
	// lastName/lastArgs remember the skeptic's most recent run_tests/run_script
	// call (regardless of exit code) so reproduceAgain can re-execute it for the
	// two-run determinism pass (Epic 11.0 T3). lastName == "" means the skeptic
	// made no exec call, so there is nothing to reproduce.
	lastName string
	lastArgs json.RawMessage
}

func (r *execEvidenceRecorder) Execute(ctx context.Context, name string, args json.RawMessage) (tools.ToolResult, error) {
	res, err := r.inner.Execute(ctx, name, args)
	if err == nil && (name == "run_tests" || name == "run_script") {
		r.lastName = name
		// Copy: the caller's json.RawMessage may alias a reused decode buffer.
		r.lastArgs = append(json.RawMessage(nil), args...)
		if ev := parseExecEvidence(res.Content); ev != nil && ev.ExitCode != 0 {
			r.last = ev
		}
	}
	return res, err
}

// reproduceAgain re-runs the skeptic's last exec tool call twice more through the
// same dispatcher and applies the two-run determinism rule (repro.Verdict) over
// the pair — the live Epic 11.0 T3 determinism pass. The verify layer holds a
// Dispatcher (not a sandbox.Backend + RunSpec), so the determinism re-runs go
// through the dispatcher the skeptic used rather than repro.Reproduce's direct
// backend.Run; the guarantee is identical (two independent executions, compared
// exit codes). Evidence (built from the confirming second run) is returned ONLY
// for a confirmed verdict; a flaky reproduction (exit codes disagree, a timeout,
// or a deterministic pass), an errored re-run, an unparseable result, or no exec
// call yields (unverifiable, nil) — a non-deterministic repro never earns a badge.
func (r *execEvidenceRecorder) reproduceAgain(ctx context.Context) (string, *reconcile.EvidenceExec) {
	if r.lastName == "" {
		return reclib.VerdictUnverifiable, nil
	}
	// The determinism re-runs go straight to the dispatcher (not through the tool
	// loop that would otherwise grant eligibility), so carry exec eligibility
	// explicitly past the structural gate (Epic 11.1). This path is reached ONLY
	// in an --exec run (callers guard it with `if exec`), and it re-executes the
	// skeptic's own run_tests/run_script call, so the agent was already exec-eligible.
	ctx = tools.WithExecEligibility(ctx, true)
	res1, err1 := r.inner.Execute(ctx, r.lastName, r.lastArgs)
	res2, err2 := r.inner.Execute(ctx, r.lastName, r.lastArgs)
	if err1 != nil || err2 != nil {
		return reclib.VerdictUnverifiable, nil
	}
	rr1, ok1 := parseRunResult(res1.Content)
	rr2, ok2 := parseRunResult(res2.Content)
	if !ok1 || !ok2 {
		return reclib.VerdictUnverifiable, nil
	}
	verdict := repro.Verdict(rr1, rr2)
	if verdict != reclib.VerdictConfirmed {
		return verdict, nil
	}
	return verdict, &reconcile.EvidenceExec{Command: rr2.Command, ExitCode: rr2.ExitCode, OutputExcerpt: rr2.Output}
}

// parseExecEvidence reconstructs an EvidenceExec from the deterministic text that
// (*tools.Dispatcher).runInSandbox renders for a run_tests/run_script result:
//
//	$ <command>
//	exit code: <N>[ (timed out)]
//	<combined output>
//
// It returns nil when the content does not match that shape (e.g. a tool error),
// so a malformed or non-exec result simply yields no evidence.
func parseExecEvidence(content string) *reconcile.EvidenceExec {
	rr, ok := parseRunResult(content)
	if !ok {
		return nil
	}
	return &reconcile.EvidenceExec{
		Command:       rr.Command,
		ExitCode:      rr.ExitCode,
		OutputExcerpt: rr.Output,
	}
}

// parseRunResult reconstructs a sandbox.RunResult (command, exit code, timed-out
// flag, output) from the deterministic text runInSandbox renders. It backs both
// parseExecEvidence (evidence stamping) and reproduceAgain (the T3 determinism
// pass, which needs the timed-out flag repro.Verdict consumes). It returns ok =
// false when content is not the runInSandbox shape (e.g. a tool error).
func parseRunResult(content string) (sandbox.RunResult, bool) {
	if !strings.HasPrefix(content, "$ ") {
		return sandbox.RunResult{}, false
	}
	// The command may span multiple lines: a run_script body renders as the
	// heredoc `/bin/sh -s <<'EOF'\n<script>\nEOF`. Anchor on the first line that is
	// exactly `exit code: <int>[ (timed out)]`; everything before it (minus the
	// leading "$ ") is the command, everything after is the captured output.
	lines := strings.Split(content, "\n")
	exitIdx, code, timedOut := -1, 0, false
	for i := 1; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "exit code: ") {
			continue
		}
		field := strings.TrimPrefix(lines[i], "exit code: ")
		if strings.HasSuffix(field, " (timed out)") {
			timedOut = true
			field = strings.TrimSuffix(field, " (timed out)")
		}
		n, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil {
			continue
		}
		exitIdx, code = i, n
		break
	}
	if exitIdx == -1 {
		return sandbox.RunResult{}, false
	}
	cmdLines := append([]string{strings.TrimPrefix(lines[0], "$ ")}, lines[1:exitIdx]...)
	return sandbox.RunResult{
		Command:  strings.Join(cmdLines, "\n"),
		ExitCode: code,
		TimedOut: timedOut,
		Output:   strings.Join(lines[exitIdx+1:], "\n"),
	}, true
}
