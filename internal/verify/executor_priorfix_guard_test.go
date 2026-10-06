package verify

import (
	"context"
	"errors"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
)

// TD internal/verify/executor.go:377 (invariant): postCheck's generic `warn != ""`
// branch was the ONE arm that stamped FixWarning with no hasAnyFixAttribution
// guard, so a later tier whose PROVIDER died wrote a warning beside an earlier
// tier's generated Fix — the "a good Fix never carries a FixWarning" invariant
// stated at internal/reconcile/emit.go:158. The salvage, agent-refusal,
// truncation and empty-completion arms all carry the guard; the generic branch
// must too.
//
// The sibling TestGenerateFixes_AgentRefusal_PreservesPriorTierFix pins the same
// property for the refusal arm; this pins it for transport/parse failures, which
// reach postCheck through the generic branch alone.
func TestGenerateFixes_ProviderFailure_PreservesPriorTierFix(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified,
		Fix:      "an earlier tier's good fix",
		Evidence: "Found by bruce; fix by sonnet", // sonnet = a different tier than execConfig's opus
	}}

	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"),
		&recordingExecutor{err: errors.New("provider boom")}, nil, okDispatcher(), 0)

	f := findings[0]
	assert.Equal(t, "an earlier tier's good fix", f.Fix,
		"a later tier's provider failure must not disturb a fix an earlier tier generated")
	assert.Empty(t, f.FixWarning,
		"'a good Fix never carries a FixWarning' (internal/reconcile/emit.go:158) — the generic branch must not stamp one over a generated fix")
}

// The availability half: with nothing to protect, the transport failure MUST
// still be disclosed, exactly as the refusal arm's sibling test requires. A guard
// that silenced this would trade a wrong warning for a silent one.
func TestGenerateFixes_ProviderFailure_WarnsWhenThereIsNoPriorFix(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified,
	}}

	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"),
		&recordingExecutor{err: errors.New("provider boom")}, nil, okDispatcher(), 0)

	f := findings[0]
	assert.Empty(t, f.Fix)
	assert.Contains(t, f.FixWarning, "fix generation failed",
		"with no prior fix to protect, the transport failure is the only record the finding carries")
}

// TD internal/verify/executor.go:748: hasAnyFixAttribution split Evidence on the
// literal "; " and required a segment to START with "fix by ". But
// appendFixAttribution yields a BARE "fix by <name>" when Evidence was empty, and
// internal/reconcile/merge.go's joinEvidence re-joins clustered findings with
// " / " — so a cluster-merged "Found by kai / fix by sonnet" is ONE segment that
// fails the prefix test. The guard then reports "no prior tier fix" over a real
// generated fix, and the refusal/salvage/truncation/empty arms stamp a FixWarning
// beside it — the exact emit.go:158 violation the guard exists to prevent.
func TestHasAnyFixAttribution_RecognisesClusterMergedEvidence(t *testing.T) {
	for _, tc := range []struct {
		evidence string
		want     bool
		why      string
	}{
		{"Found by kai / fix by sonnet", true, "reconcile's joinEvidence delimiter is \" / \", not \"; \""},
		{"fix by sonnet", true, "a bare attribution, the shape appendFixAttribution returns for empty Evidence"},
		{"Found by bruce; fix by sonnet", true, "the original \"; \" delimiter must keep working"},
		{"Found by kai / found by bruce", false, "no attribution anywhere"},
		{"reviewer suggested a fix by hand", false, "prose merely containing the phrase mid-sentence is not an attribution"},
		{"Found by kai / fix by sonnet / found by bruce", true, "an attribution in the middle of a cluster-merged chain"},
	} {
		assert.Equal(t, tc.want, hasAnyFixAttribution(tc.evidence), tc.why)
	}
}

// The consumer trace: the guard must actually protect the crossed-tier case on the
// cluster-merged evidence shape joinEvidence produces.
func TestGenerateFixes_ProviderFailure_PreservesClusterMergedPriorTierFix(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified,
		Fix:      "an earlier tier's good fix",
		Evidence: "Found by kai / fix by sonnet", // joinEvidence's delimiter, sonnet != opus
	}}

	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"),
		&recordingExecutor{err: errors.New("provider boom")}, nil, okDispatcher(), 0)

	f := findings[0]
	assert.Equal(t, "an earlier tier's good fix", f.Fix)
	assert.Empty(t, f.FixWarning,
		"a cluster-merged prior-tier fix must be protected too, not just a \"; \"-joined one")
}

// The over-admission half of the widened predicate (item 16's PREDICATE check): a
// bare "/" must NOT be a segment boundary, or a path like "path/to/fix by hand"
// reads as an attribution and the guard silently suppresses a warning that should
// have been stamped. Multi-character joins only.
func TestHasAnyFixAttribution_DoesNotSplitOnABareSlash(t *testing.T) {
	for _, tc := range []struct{ evidence, why string }{
		{"path/to/fix by hand", "a slash inside a path is not the \" / \" join"},
		{"see docs/fix by design", "nor is a slash inside a directory name"},
		{"a/b/c", "no attribution at all"},
	} {
		assert.False(t, hasAnyFixAttribution(tc.evidence), tc.why)
	}
}

// TD internal/verify/executor.go:409: generateFixes cleared f.FixReview
// UNCONDITIONALLY up front, so every arm that PRESERVES an earlier tier's Fix
// behind hasAnyFixAttribution (the refusal, salvage, truncation and
// empty-completion arms) shipped that preserved fix with its NEEDS_REVIEW
// annotation stripped — a smell-flagged fix rendering unflagged. The clear's own
// stated rationale covers only the WITHHELD-fix case, so its premise is false for
// the four preservation arms: FixReview is documented (internal/reconcile/emit.go)
// as the annotation on a fix that "was ACCEPTED" and is usable, and the earlier
// tier's fix is exactly that.
func TestGenerateFixes_PreservedPriorTierFixKeepsItsFixReviewAnnotation(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified,
		Fix:       "an earlier tier's good fix",
		FixReview: "NEEDS_REVIEW: SOFT over-simplification (stub_body)",
		Evidence:  "Found by bruce; fix by sonnet", // sonnet = a different tier than execConfig's opus
	}}

	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"),
		&recordingExecutor{err: errors.New("provider boom")}, nil, okDispatcher(), 0)

	f := findings[0]
	assert.Equal(t, "an earlier tier's good fix", f.Fix,
		"the earlier tier's fix is preserved")
	assert.Equal(t, "NEEDS_REVIEW: SOFT over-simplification (stub_body)", f.FixReview,
		"and its NEEDS_REVIEW annotation must survive with it — otherwise a smell-flagged fix renders unflagged")
}

// The withheld-fix half must NOT regress: with no prior-tier Fix, a failure arm
// must still clear a stale FixReview, which is the case the up-front clear exists
// to protect.
func TestGenerateFixes_WithheldFixStillClearsAStaleFixReview(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified,
		FixReview: "NEEDS_REVIEW: stale from a prior run",
	}}

	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"),
		&recordingExecutor{err: errors.New("provider boom")}, nil, okDispatcher(), 0)

	f := findings[0]
	assert.Empty(t, f.Fix, "no fix was produced")
	assert.Empty(t, f.FixReview,
		"a stale acceptance annotation must not render beside a withheld patch")
}
