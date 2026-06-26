package verify

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
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
}

func (r *execEvidenceRecorder) Execute(ctx context.Context, name string, args json.RawMessage) (tools.ToolResult, error) {
	res, err := r.inner.Execute(ctx, name, args)
	if err == nil && (name == "run_tests" || name == "run_script") {
		if ev := parseExecEvidence(res.Content); ev != nil && ev.ExitCode != 0 {
			r.last = ev
		}
	}
	return res, err
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
	if !strings.HasPrefix(content, "$ ") {
		return nil
	}
	// The command may span multiple lines: a run_script body renders as the
	// heredoc `/bin/sh -s <<'EOF'\n<script>\nEOF`. Anchor on the first line that is
	// exactly `exit code: <int>[ (timed out)]`; everything before it (minus the
	// leading "$ ") is the command, everything after is the captured output.
	lines := strings.Split(content, "\n")
	exitIdx, code := -1, 0
	for i := 1; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "exit code: ") {
			continue
		}
		field := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(lines[i], "exit code: "), " (timed out)"))
		n, err := strconv.Atoi(field)
		if err != nil {
			continue
		}
		exitIdx, code = i, n
		break
	}
	if exitIdx == -1 {
		return nil
	}
	cmdLines := append([]string{strings.TrimPrefix(lines[0], "$ ")}, lines[1:exitIdx]...)
	return &reconcile.EvidenceExec{
		Command:       strings.Join(cmdLines, "\n"),
		ExitCode:      code,
		OutputExcerpt: strings.Join(lines[exitIdx+1:], "\n"),
	}
}
