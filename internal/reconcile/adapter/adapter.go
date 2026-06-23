// Package adapter is ATCR's boundary between internal/stream and the extracted
// reconcile library (github.com/samestrin/atcr/reconcile, Epic 8.0). It converts
// ATCR's stream.Finding to and from the library's reconcile.Finding and is the
// single place ATCR-internal path-validation fields are stamped back onto the
// reconciled wire record — keeping that ATCR-specific machinery out of the
// stdlib-only public library.
//
// Phase 1 establishes the package boundary with signatures only; the conversion
// bodies (including path-validation stamping and *Verification pointer-identity
// preservation) are implemented in Phase 2, driven by
// TestBoundaryAdapter_FindingConversionRoundTrip.
package adapter

import (
	"github.com/samestrin/atcr/internal/stream"
	reconcile "github.com/samestrin/atcr/reconcile"
)

// ToFinding converts an ATCR stream.Finding (per-source input) into the library
// reconcile.Finding. Path-validation fields are ATCR-internal and are not part
// of the library type, so they are not carried here.
//
// Phase 1 stub — implemented in Phase 2 (Epic 8.0 task 2.2).
func ToFinding(f stream.Finding) reconcile.Finding {
	_ = f
	panic("reconcile/adapter: ToFinding not implemented until Phase 2")
}

// FromFinding converts a reconciled library reconcile.Finding back into an ATCR
// stream.Finding so the ATCR I/O layer can stamp path-validation fields onto it.
//
// Phase 1 stub — implemented in Phase 2 (Epic 8.0 task 2.2).
func FromFinding(f reconcile.Finding) stream.Finding {
	_ = f
	panic("reconcile/adapter: FromFinding not implemented until Phase 2")
}
