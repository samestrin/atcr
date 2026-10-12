package verify

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
)

// TD internal/verify/executor.go:256: the hasFixAttribution skip was a bare
// `continue` with no log record, so a cluster-merged record whose "fix by"
// attribution came from a member other than the one whose Fix survived was
// skipped invisibly. The skip site cannot tell that case from an ordinary
// re-verify of a finding this executor already fixed, so every attribution skip
// leaves one Debug-only executor_attribution_skip record — never a Warn, which
// would repeat for every fixed finding on every run.
func TestGenerateFixes_AttributionSkipLogsDebugRecord(t *testing.T) {
	const evidence = "Found by kai / fix by opus" // opus = execConfig's executor
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 7, Problem: "p", Confidence: ConfidenceVerified,
		Fix:      "opus's earlier fix",
		Evidence: evidence,
	}}
	rec := &recordingExecutor{out: `{"fix":"a second fix"}`}
	ctx, buf := ceilingCtx()

	generateFixes(ctx, findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 0, rec.calls, "the attribution skip must still skip: logging changes nothing about which findings are dispatched")
	assert.Equal(t, evidence, findings[0].Evidence)
	assert.Equal(t, "opus's earlier fix", findings[0].Fix)
	assert.Empty(t, findings[0].FixWarning, "the skip writes no fix warning")

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "class=executor_attribution_skip"), "exactly one record under the stable class")
	assert.Contains(t, out, "level=DEBUG", "the record is Debug level")
	assert.NotContains(t, out, "level=WARN", "an attribution skip must never warn")
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "class=executor_attribution_skip") {
			assert.Contains(t, line, "level=DEBUG")
			assert.Contains(t, line, "a.go:7", "the record names the finding")
			assert.Contains(t, line, "opus", "the record names the executor")
		}
	}
}

// A finding with no attribution is dispatched normally and leaves no
// executor_attribution_skip record.
func TestGenerateFixes_NoAttributionLogsNoSkipRecord(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 7, Problem: "p", Confidence: ConfidenceVerified,
		Evidence: "Found by kai / found by bruce",
	}}
	rec := &recordingExecutor{out: "a fix"}
	ctx, buf := ceilingCtx()

	generateFixes(ctx, findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 1, rec.calls, "an unattributed finding is dispatched")
	assert.NotContains(t, buf.String(), "executor_attribution_skip", "no attribution means no skip record")
}
