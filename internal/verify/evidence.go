package verify

import (
	"context"
	"encoding/json"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/tools"
)

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
	// Stubbed: real parsing lands in the GREEN step.
	return nil
}
