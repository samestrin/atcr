package verify

import (
	"encoding/json"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FixReview is the NEEDS_REVIEW channel for a fix accepted despite SOFT
// diff-smells (Epic 35.3). It is a field of its own rather than a reuse of
// FixWarning precisely because a SOFT verdict yields a GOOD fix — and
// generateFixes enforces that a finding never carries both a usable Fix and a
// warning.
func TestJSONFinding_FixReviewSerialization(t *testing.T) {
	// omitempty: a finding with no review annotation must serialize exactly as
	// it did before this field existed.
	b, err := json.Marshal(reconcile.JSONFinding{File: "a.go", Line: 1})
	require.NoError(t, err)
	assert.NotContains(t, string(b), "fix_review")

	b, err = json.Marshal(reconcile.JSONFinding{File: "a.go", Line: 1, FixReview: "NEEDS_REVIEW: suppression"})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"fix_review":"NEEDS_REVIEW: suppression"`)

	// Round-trips.
	var back reconcile.JSONFinding
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, "NEEDS_REVIEW: suppression", back.FixReview)
}

// buildFixReview renders the annotation stamped on a SOFT-verdict fix. It must
// name every smell type so a human reviewer knows which shortcut was taken, and
// stay single-line so it is safe wherever FixWarning already goes.
func TestBuildFixReview(t *testing.T) {
	got := buildFixReview(AnalyzeDiff(dsSuppression))
	assert.Equal(t, "NEEDS_REVIEW: fix accepted with over-simplification smell(s): suppression", got)

	got = buildFixReview(AnalyzeDiff(dsStubBody))
	assert.Contains(t, got, smellStubBody)
	assert.NotContains(t, got, "\n")

	// A clean or nil result yields no annotation.
	assert.Equal(t, "", buildFixReview(AnalyzeDiff(dsImplOnly)))
	assert.Equal(t, "", buildFixReview(nil))
}
