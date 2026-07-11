package adapter

import (
	"testing"

	recon "github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/stream"
	reconcile "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBoundaryAdapter_FindingConversionRoundTrip is the new-behavior RED test for
// the Epic 8.0 boundary adapter (sprint 8.0 task 2.1 / AC 01-02). It locks down:
//   - stream.Finding -> reconcile.Finding preserves all 9 wire fields plus
//     Reviewer/Reviewers/Confidence with zero data loss;
//   - the round-trip back (FromFinding) preserves the same wire fields;
//   - path-validation fields (PathValid/PathWarning/PathSuggestion) live ONLY on
//     the ATCR JSONFinding, re-stamped at the boundary from the source finding —
//     never on the stdlib-only library reconcile.Finding;
//   - the *Verification pointer is shared across the boundary (identity, not just
//     value, so gate.go and internal/debate mutate the same block).
//
// It is RED until the adapter bodies are implemented in task 2.2 (the stubs panic).
func TestBoundaryAdapter_FindingConversionRoundTrip(t *testing.T) {
	src := stream.Finding{
		Severity:       "HIGH",
		File:           "internal/auth/login.go",
		Line:           42,
		Problem:        "token never expires",
		Fix:            "set a TTL",
		Category:       "security",
		EstMinutes:     15,
		Evidence:       "saw it in the handler",
		Reviewer:       "greta",
		Reviewers:      []string{"greta", "host"},
		Confidence:     "HIGH",
		PathValid:      false,
		PathWarning:    "file not found",
		PathSuggestion: "internal/auth/validate.go",
	}

	// stream.Finding -> reconcile.Finding: the 9 wire fields + reviewer columns
	// round-trip; path-validation fields are intentionally dropped (the library
	// type has no home for them).
	rf := ToFinding(src)
	assert.Equal(t, src.Severity, rf.Severity)
	assert.Equal(t, src.File, rf.File)
	assert.Equal(t, src.Line, rf.Line)
	assert.Equal(t, src.Problem, rf.Problem)
	assert.Equal(t, src.Fix, rf.Fix)
	assert.Equal(t, src.Category, rf.Category)
	assert.Equal(t, src.EstMinutes, rf.EstMinutes)
	assert.Equal(t, src.Evidence, rf.Evidence)
	assert.Equal(t, src.Reviewer, rf.Reviewer)
	assert.Equal(t, src.Reviewers, rf.Reviewers)
	assert.Equal(t, src.Confidence, rf.Confidence)

	// reconcile.Finding -> stream.Finding: the same wire fields survive the
	// return trip (path fields come back zeroed — they are re-stamped later).
	back := FromFinding(rf)
	assert.Equal(t, src.Severity, back.Severity)
	assert.Equal(t, src.File, back.File)
	assert.Equal(t, src.Line, back.Line)
	assert.Equal(t, src.Problem, back.Problem)
	assert.Equal(t, src.Fix, back.Fix)
	assert.Equal(t, src.Category, back.Category)
	assert.Equal(t, src.EstMinutes, back.EstMinutes)
	assert.Equal(t, src.Evidence, back.Evidence)
	assert.Equal(t, src.Reviewer, back.Reviewer)
	assert.Equal(t, src.Reviewers, back.Reviewers)
	assert.Equal(t, src.Confidence, back.Confidence)

	// reconcile.Finding -> JSONFinding: wire fields carry over, the *Verification
	// pointer is shared, and path-validation fields are stamped from the source.
	verif := &reconcile.Verification{Verdict: reconcile.VerdictConfirmed, Skeptic: "skeptic-a"}
	rf.Verification = verif
	jf := ToJSONFinding(rf, src)

	assert.Equal(t, rf.Severity, jf.Severity)
	assert.Equal(t, rf.File, jf.File)
	assert.Equal(t, rf.Line, jf.Line)
	assert.Equal(t, rf.Problem, jf.Problem)
	assert.Equal(t, rf.Fix, jf.Fix)
	assert.Equal(t, rf.Category, jf.Category)
	assert.Equal(t, rf.EstMinutes, jf.EstMinutes)
	assert.Equal(t, rf.Evidence, jf.Evidence)
	assert.Equal(t, rf.Reviewers, jf.Reviewers)
	assert.Equal(t, rf.Confidence, jf.Confidence)

	// Path-validation fields stamped onto JSONFinding from the source finding.
	assert.Equal(t, src.PathValid, jf.PathValid)
	assert.Equal(t, src.PathWarning, jf.PathWarning)
	assert.Equal(t, src.PathSuggestion, jf.PathSuggestion)

	// *Verification pointer identity preserved across the boundary.
	require.Same(t, verif, jf.Verification)
}

// TestToJSONFinding_StampsFallbackReviewers verifies the Epic 19.10 F5 fallback
// provenance is stamped onto JSONFinding from the side-channel stream.Finding,
// mirroring the PathValid/PathWarning stamping — and left empty when the source
// slot ran on its own configured model (no fallback).
func TestToJSONFinding_StampsFallbackReviewers(t *testing.T) {
	rf := reconcile.Finding{Severity: "HIGH", File: "a.go", Line: 1, Reviewers: []string{"greta"}}

	// Fallback-served source → FallbackReviewers maps reviewer → served model.
	served := stream.Finding{Reviewer: "greta", FallbackModel: "net-model"}
	jf := ToJSONFinding(rf, served)
	assert.Equal(t, map[string]string{"greta": "net-model"}, jf.FallbackReviewers)

	// No fallback → the map stays nil (omitempty keeps findings.json byte-identical).
	own := stream.Finding{Reviewer: "greta"}
	jf2 := ToJSONFinding(rf, own)
	assert.Nil(t, jf2.FallbackReviewers)
}

// TestJSONFindings_LivePathPreservesVerificationIdentity covers the live path
// debate/gate depend on: Merged.Verification -> Result.JSONFindings() keeps the
// same *Verification pointer, not just an equal value. A deep-copy refactor of
// JSONFindings() would otherwise pass the adapter boundary test but silently
// break gate.go's IsFailing checks.
func TestJSONFindings_LivePathPreservesVerificationIdentity(t *testing.T) {
	verif := &reconcile.Verification{Verdict: reconcile.VerdictConfirmed, Skeptic: "skeptic-a"}
	res := recon.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{
				Severity:     "HIGH",
				File:         "auth.go",
				Line:         42,
				Problem:      "token never expires",
				Fix:          "set a TTL",
				Category:     "security",
				EstMinutes:   15,
				Evidence:     "saw it",
				Reviewers:    []string{"greta"},
				Confidence:   "HIGH",
				Verification: verif,
			}},
		},
	}

	jsonFindings := res.JSONFindings()
	require.Len(t, jsonFindings, 1)
	require.Same(t, verif, jsonFindings[0].Verification)
}
