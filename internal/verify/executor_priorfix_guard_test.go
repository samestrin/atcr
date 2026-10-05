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
