package verify

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
)

// ceilingCtx returns a context whose logger writes to buf at Debug level, so a
// test can assert both the Warn-level class and the Debug-level File:Line detail
// logPipelineWarning emits.
func ceilingCtx() (context.Context, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return log.NewContext(context.Background(), logger), &buf
}

// AC 02-01 Scenario 2 / Edge Case 1: a finding whose EstMinutes exceeds the
// executor's ceiling is skipped BEFORE any provider call — no snippet read, no
// executor call — with a non-empty FixWarning and exactly one
// executor_ceiling_skip warning carrying the finding's File:Line.
func TestGenerateFixes_SkipsAboveComplexityCeiling(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified, EstMinutes: 120},
	}
	ex := execConfig("MEDIUM")
	ex.MaxEstimatedMinutes = intPtr(30)
	rec := &recordingExecutor{out: "a fix"}
	ctx, buf := ceilingCtx()
	generateFixes(ctx, findings, ex, execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 0, rec.calls, "an over-ceiling finding must never reach the provider (no wasted API call)")
	assert.Empty(t, findings[0].Fix, "no fix is generated for a ceiling-skipped finding")
	assert.NotEmpty(t, findings[0].FixWarning, "a ceiling skip must be visible via FixWarning")
	assert.Contains(t, findings[0].FixWarning, "exceeds executor ceiling", "reason text names the ceiling")
	assert.Contains(t, buf.String(), "class=executor_ceiling_skip", "the new warning class is emitted")
	assert.Contains(t, buf.String(), "a.go:1", "the finding File:Line rides the detail")
}

// AC 02-01 Edge Case 1: EstMinutes exactly at the ceiling is within the ceiling
// (inclusive boundary) and is dispatched normally.
func TestGenerateFixes_AtComplexityCeilingDispatches(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified, EstMinutes: 30},
	}
	ex := execConfig("MEDIUM")
	ex.MaxEstimatedMinutes = intPtr(30)
	rec := &recordingExecutor{out: "use a parameterized query"}
	generateFixes(context.Background(), findings, ex, execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 1, rec.calls, "a finding at the ceiling is within it and is dispatched")
	assert.Equal(t, "use a parameterized query", findings[0].Fix)
}

// AC 02-01 Scenario 3: an unset (zero) ceiling never skips on this basis, even
// for a very large EstMinutes — existing single-tier config behavior is preserved.
func TestGenerateFixes_UnsetCeilingDispatchesLargeEstimate(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified, EstMinutes: 100000},
	}
	ex := execConfig("MEDIUM") // MaxEstimatedMinutes nil (unset)
	rec := &recordingExecutor{out: "a fix"}
	ctx, buf := ceilingCtx()
	generateFixes(ctx, findings, ex, execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 1, rec.calls, "no ceiling configured means no ceiling-based skip")
	assert.NotContains(t, buf.String(), "executor_ceiling_skip", "no ceiling skip must be logged")
}

// AC 02-01 Edge Case 2: EstMinutes of zero is "no estimate provided", not a real
// value, and must NOT trigger a ceiling skip on that basis alone.
func TestGenerateFixes_ZeroEstimateNotCeilingSkipped(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified, EstMinutes: 0},
	}
	ex := execConfig("MEDIUM")
	ex.MaxEstimatedMinutes = intPtr(30)
	rec := &recordingExecutor{out: "a fix"}
	generateFixes(context.Background(), findings, ex, execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 1, rec.calls, "a zero (unset) estimate is not skipped by the ceiling")
	assert.Equal(t, "a fix", findings[0].Fix)
}

// AC 02-01 Edge Case 3: a severity ceiling skip uses reason text distinguishable
// from the estimated-minutes case, and a finding above only the severity ceiling
// is skipped via that branch.
func TestGenerateFixes_SkipsAboveSeverityCeiling(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "CRITICAL", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified, EstMinutes: 5},
	}
	ex := execConfig("MEDIUM") // floor MEDIUM
	ex.MaxSeverityForFix = "HIGH"
	rec := &recordingExecutor{out: "a fix"}
	ctx, buf := ceilingCtx()
	generateFixes(ctx, findings, ex, execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 0, rec.calls, "a finding above the severity ceiling must not be dispatched")
	assert.NotEmpty(t, findings[0].FixWarning)
	assert.Contains(t, findings[0].FixWarning, "severity", "severity-ceiling reason is distinguishable from the minutes case")
	assert.NotContains(t, findings[0].FixWarning, "estimated complexity", "must not use the minutes-ceiling reason text")
	assert.Contains(t, buf.String(), "class=executor_ceiling_skip")
}

// AC 02-01 Edge Case 4: multiple ceiling-exceeding findings each get their own
// independent FixWarning and their own File:Line-bearing log line.
func TestGenerateFixes_MultipleCeilingSkipsAreIndependent(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 10, Problem: "p1", Confidence: ConfidenceVerified, EstMinutes: 90},
		{Severity: "HIGH", File: "b.go", Line: 20, Problem: "p2", Confidence: ConfidenceVerified, EstMinutes: 120},
	}
	ex := execConfig("MEDIUM")
	ex.MaxEstimatedMinutes = intPtr(30)
	rec := &recordingExecutor{out: "a fix"}
	ctx, buf := ceilingCtx()
	generateFixes(ctx, findings, ex, execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 0, rec.calls)
	assert.NotEmpty(t, findings[0].FixWarning)
	assert.NotEmpty(t, findings[1].FixWarning)
	assert.Contains(t, buf.String(), "a.go:10")
	assert.Contains(t, buf.String(), "b.go:20")
}

// AC 02-03 Scenario 2: skip-chain ordering is preserved. A finding below the
// confidence floor AND above the ceiling is skipped at the FIRST gate (confidence,
// a bare silent continue) with NO FixWarning/ceiling log side effect.
func TestGenerateFixes_ConfidenceSkipPrecedesCeiling(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: reclib.ConfMedium, EstMinutes: 120},
	}
	ex := execConfig("MEDIUM")
	ex.MaxEstimatedMinutes = intPtr(30)
	rec := &recordingExecutor{out: "a fix"}
	ctx, buf := ceilingCtx()
	generateFixes(ctx, findings, ex, execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 0, rec.calls)
	assert.Empty(t, findings[0].FixWarning, "a confidence skip precedes the ceiling and stays silent")
	assert.NotContains(t, buf.String(), "executor_ceiling_skip", "the ceiling gate must not fire for a confidence-skipped finding")
}

func TestParseSelfDecline(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		fix         string
		wantDecline bool
	}{
		{"bare marker declines", "ATCR_DECLINE", true},
		{"marker with reason declines", "ATCR_DECLINE: fix exceeds safe complexity", true},
		{"marker with leading/trailing space declines", "  ATCR_DECLINE: too hard  ", true},
		{"marker without colon but suffix does not decline", "ATCR_DECLINED for now", false},
		{"marker embedded mid-text does not decline", "I would say ATCR_DECLINE here", false},
		{"prose mentioning decline does not decline", "declined to change the loop; use a guard", false},
		{"normal code fix does not decline", "func add(a, b int) int { return a + b }", false},
		{"empty does not decline", "", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, declined := parseSelfDecline(tt.fix)
			assert.Equal(t, tt.wantDecline, declined)
		})
	}
}

// AC 02-02 Scenario 2 / Edge Case 1: a snippet-path self-decline lands as a skip —
// non-empty FixWarning, no Fix, no attribution — never as partial Fix content, and
// its log class is executor_ceiling_skip (distinct from executor_fix_failed).
func TestGenerateFixes_SelfGatingDeclineNotPartialFix(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified, Evidence: "Found by bruce"},
	}
	rec := &recordingExecutor{out: "ATCR_DECLINE: fix exceeds safe complexity for this model"}
	ctx, buf := ceilingCtx()
	generateFixes(ctx, findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 1, rec.calls, "the decline is post-dispatch: the executor was called once")
	assert.Empty(t, findings[0].Fix, "a self-declined fix is never presented as Fix content")
	assert.NotContains(t, findings[0].Evidence, "fix by opus", "a declined finding gets no fix attribution")
	assert.NotEmpty(t, findings[0].FixWarning, "the decline must be visible via FixWarning")
	assert.NotContains(t, findings[0].FixWarning, "ATCR_DECLINE", "the raw sentinel must not leak into the warning text")
	assert.Contains(t, buf.String(), "class=executor_ceiling_skip", "a decline logs the ceiling-skip class")
	assert.NotContains(t, buf.String(), "executor_fix_failed", "a decline is not a provider/transport failure")
}

// AC 02-02 Edge Case 3 / Error Scenario 2: an ambiguous, non-marker response is
// NOT misclassified as a decline — it flows through normal fix handling.
func TestGenerateFixes_AmbiguousResponseNotDeclined(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified},
	}
	rec := &recordingExecutor{out: "declined to alter the loop; instead add a nil guard before the deref"}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, "declined to alter the loop; instead add a nil guard before the deref", findings[0].Fix,
		"a prose fix that merely mentions 'declined' is a real fix, not a self-decline")
	assert.Empty(t, findings[0].FixWarning, "a normal fix carries no decline warning")
}

// AC 02-02 Scenario 1: an Agent-Mode self-decline (parseable JSON whose fix field is
// the decline marker) also lands as a skip, not as Fix content.
func TestGenerateFixes_AgentModeSelfGatingDecline(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified},
	}
	cc := &fakeChatCompleter{turns: []chatTurn{{content: `{"fix":"ATCR_DECLINE: too complex to fix safely"}`}}}
	ctx, buf := ceilingCtx()
	generateFixes(ctx, findings, agentExecConfig(), execRegistry("MEDIUM"), &recordingExecutor{}, cc, okDispatcher(), 0)

	assert.Empty(t, findings[0].Fix, "an agent-mode decline is never presented as Fix content")
	assert.NotEmpty(t, findings[0].FixWarning)
	assert.Contains(t, buf.String(), "class=executor_ceiling_skip")
}
