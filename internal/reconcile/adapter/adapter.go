// Package adapter is ATCR's intended public boundary between internal/stream
// and the extracted reconcile library (github.com/samestrin/atcr/reconcile,
// Epic 8.0). It converts ATCR's stream.Finding to and from the library's
// reconcile.Finding and is the single place ATCR-internal path-validation
// fields are stamped back onto the reconciled wire record — keeping that
// ATCR-specific machinery out of the stdlib-only public library.
//
// Phase 3 transitional note: this package has zero non-test callers today.
// RunReconcile (gate.go) reaches lib.go's Reconcile wrapper, which inlines the
// stream.Finding→Finding field map because adapter imports internal/reconcile,
// creating an import cycle that prevents the reverse import. TD-006 tracks
// collapsing to one conversion once Phase 3 inverts the dependency. The absence
// of live callers does not mean this package is unused or safe to delete — it
// is the intended Phase 3 boundary, not yet reached by the live path.
package adapter

import (
	recon "github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/stream"
	reconcile "github.com/samestrin/atcr/reconcile"
)

// ToFinding converts an ATCR stream.Finding (per-source input) into the library
// reconcile.Finding: the 9 wire fields plus the reviewer columns. Path-validation
// fields are ATCR-internal and are not part of the library type, so they are not
// carried here (they are re-stamped onto the JSONFinding after reconcile).
func ToFinding(f stream.Finding) reconcile.Finding {
	return reconcile.Finding{
		Severity:   f.Severity,
		File:       f.File,
		Line:       f.Line,
		Problem:    f.Problem,
		Fix:        f.Fix,
		Category:   f.Category,
		EstMinutes: f.EstMinutes,
		Evidence:   f.Evidence,
		Reviewer:   f.Reviewer,
		Reviewers:  f.Reviewers,
		Confidence: f.Confidence,
	}
}

// FromFinding converts a reconciled library reconcile.Finding back into an ATCR
// stream.Finding so the ATCR I/O layer can stamp path-validation fields onto it.
// The library finding's Disagreement and Verification ride on the JSONFinding
// record (see ToJSONFinding), not on stream.Finding; path fields come back zeroed.
func FromFinding(f reconcile.Finding) stream.Finding {
	return stream.Finding{
		Severity:   f.Severity,
		File:       f.File,
		Line:       f.Line,
		Problem:    f.Problem,
		Fix:        f.Fix,
		Category:   f.Category,
		EstMinutes: f.EstMinutes,
		Evidence:   f.Evidence,
		Reviewer:   f.Reviewer,
		Reviewers:  f.Reviewers,
		Confidence: f.Confidence,
	}
}

// ToJSONFinding wraps a reconciled library reconcile.Finding back into ATCR's
// internal recon.JSONFinding, the single place ATCR-only path-validation fields
// are stamped onto the reconciled wire record (Phase 2 Clarification Q1). The 9
// wire fields, Reviewers, Confidence, and Disagreement carry over from the
// library finding; PathValid/PathWarning/PathSuggestion are copied from paths
// (the originating ATCR stream.Finding); the *Verification pointer is shared
// (identity preserved) so gate.go and internal/debate read/mutate the same block.
func ToJSONFinding(f reconcile.Finding, paths stream.Finding) recon.JSONFinding {
	return recon.JSONFinding{
		Severity:       f.Severity,
		File:           f.File,
		Line:           f.Line,
		Problem:        f.Problem,
		Fix:            f.Fix,
		Category:       f.Category,
		EstMinutes:     f.EstMinutes,
		Evidence:       f.Evidence,
		Reviewers:      f.Reviewers,
		Confidence:     f.Confidence,
		Disagreement:   f.Disagreement,
		Verification:   f.Verification,
		PathValid:      paths.PathValid,
		PathWarning:    paths.PathWarning,
		PathSuggestion: paths.PathSuggestion,
	}
}
