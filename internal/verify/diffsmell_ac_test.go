package verify

import (
	"context"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The archetypal reward hack this epic exists to block: the executor is asked to
// fix a failing assertion and "fixes" it by deleting the assertion. This is the
// end-to-end acceptance test for Epic 35.3 AC5 — it drives the real
// generateFixes pipeline, not the analyzer in isolation.
const acAssertionDeleted = `diff --git a/internal/verify/severity_test.go b/internal/verify/severity_test.go
--- a/internal/verify/severity_test.go
+++ b/internal/verify/severity_test.go
@@ -20,5 +20,3 @@
 func TestSeverityFloor(t *testing.T) {
 	got := meetsSeverityFloor("LOW", "HIGH")
-	require.False(t, got)
-	require.Equal(t, "HIGH", canonical("high"))
 }
`

// The honest fix for the same finding: change the implementation.
const acRootCauseFixed = `diff --git a/internal/verify/severity.go b/internal/verify/severity.go
--- a/internal/verify/severity.go
+++ b/internal/verify/severity.go
@@ -30,3 +30,3 @@
 func meetsSeverityFloor(sev, floor string) bool {
-	return rank(sev) > rank(floor)
+	return rank(sev) >= rank(floor)
 }
`

// AC5, primary: deleting an assertion must trigger the HARD rejection path — the
// patch is withheld, not written, and a retry is issued.
func TestAC5_DeletingAnAssertionTriggersHardRejection(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "internal/verify/severity.go", Line: 30,
		Problem: "severity floor is off by one", Confidence: ConfidenceVerified, Evidence: "ev",
	}}
	// The executor tries the reward hack twice in a row.
	rec := &sequencedExecutor{outs: []string{acAssertionDeleted, acAssertionDeleted}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Empty(t, findings[0].Fix,
		"AC5: a patch that deletes an assertion must never reach Finding.Fix")
	assert.Contains(t, findings[0].FixWarning, smellWeakenedAssertion,
		"AC5: the halt must name the smell that caused it")
	assert.Equal(t, 2, rec.callCount(),
		"AC5: exactly one self-correction retry, then halt")
	assert.NotContains(t, findings[0].Evidence, "fix by opus",
		"AC5: a withheld patch must not be attributed as a generated fix")
}

// AC5, recovery: the same rejection followed by an honest root-cause fix is
// accepted cleanly — the gate blocks reward hacks without blocking real fixes.
func TestAC5_RootCauseRetryIsAccepted(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "internal/verify/severity.go", Line: 30,
		Problem: "severity floor is off by one", Confidence: ConfidenceVerified, Evidence: "ev",
	}}
	rec := &sequencedExecutor{outs: []string{acAssertionDeleted, acRootCauseFixed}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	require.Equal(t, 2, rec.callCount())
	assert.Equal(t, strings.TrimSpace(acRootCauseFixed), findings[0].Fix,
		"the honest retry is accepted")
	assert.Empty(t, findings[0].FixWarning)
	assert.Empty(t, findings[0].FixReview)
	assert.Contains(t, findings[0].Evidence, "fix by opus")
}

// AC2/AC3 coverage in one pass over the full verdict matrix, driven end-to-end
// through generateFixes.
func TestAC_VerdictMatrixEndToEnd(t *testing.T) {
	cases := []struct {
		name       string
		findingAt  string
		outs       []string
		wantCalls  int
		wantFix    bool
		wantWarn   string // substring; "" means must be empty
		wantReview string // substring; "" means must be empty
	}{
		{
			name: "AC2 hard rejected then retried", findingAt: "a.go",
			outs: []string{acAssertionDeleted, acRootCauseFixed}, wantCalls: 2, wantFix: true,
		},
		{
			name: "AC3 second hard offense halts", findingAt: "a.go",
			outs: []string{acAssertionDeleted, acAssertionDeleted}, wantCalls: 2, wantFix: false,
			wantWarn: "rejected two consecutive fixes",
		},
		{
			name: "AC4 soft accepted and flagged", findingAt: "a.go",
			outs: []string{dsSuppression}, wantCalls: 1, wantFix: true,
			wantReview: fixReviewPrefix,
		},
		{
			name: "clean diff accepted silently", findingAt: "a.go",
			outs: []string{acRootCauseFixed}, wantCalls: 1, wantFix: true,
		},
		{
			name: "test-file finding is not auto-rejected", findingAt: "a_test.go",
			outs: []string{dsTestOnlyClean}, wantCalls: 1, wantFix: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := gateFinding(tc.findingAt)
			rec := &sequencedExecutor{outs: tc.outs}
			generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

			assert.Equal(t, tc.wantCalls, rec.callCount(), "executor call count")
			assert.Equal(t, tc.wantFix, findings[0].Fix != "", "fix written?")
			if tc.wantWarn == "" {
				assert.Empty(t, findings[0].FixWarning)
			} else {
				assert.Contains(t, findings[0].FixWarning, tc.wantWarn)
			}
			if tc.wantReview == "" {
				assert.Empty(t, findings[0].FixReview)
			} else {
				assert.Contains(t, findings[0].FixReview, tc.wantReview)
			}
		})
	}
}

// AC1: the scan happens BEFORE the fix is committed to the finding — a rejected
// patch leaves no trace in Fix, Evidence, or FixReview for a downstream consumer
// (--auto-fix's selectAutoFixEntries reads Fix directly) to pick up.
func TestAC1_RejectedPatchLeavesNoDownstreamTrace(t *testing.T) {
	findings := gateFinding("a.go")
	rec := &sequencedExecutor{outs: []string{acAssertionDeleted, acAssertionDeleted}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Empty(t, findings[0].Fix)
	assert.Empty(t, findings[0].FixReview)
	assert.Equal(t, "ev", findings[0].Evidence, "evidence must be untouched by a rejected patch")
	assert.NotEmpty(t, findings[0].FixWarning, "the rejection must still be visible to a human")
}
