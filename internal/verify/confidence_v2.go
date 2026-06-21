package verify

import (
	"github.com/samestrin/atcr/internal/reconcile"
)

// ConfidenceVerified is the confidence-v2 tier for a finding a skeptic confirmed.
// It sits above reconcile.ConfHigh in the v2 ordering (VERIFIED > HIGH > MEDIUM >
// LOW). The canonical definition moved to reconcile (the common dependency of the
// verify and debate stages, which both promote a confirmed finding to this tier);
// this alias preserves the verify-package symbol its callers and tests use.
const ConfidenceVerified = reconcile.ConfidenceVerified

// confidenceV2 maps a finding's v1 confidence and a skeptic verdict to its v2
// confidence tier (AC 03-01). It delegates to reconcile.ConfidenceForVerdict, the
// single confidence-from-verdict rule shared with the debate stage: a confirmed
// verdict promotes to VERIFIED, a refuted verdict demotes to LOW, and any other
// verdict passes the v1 confidence through unchanged.
func confidenceV2(v1Confidence, verdict string) string {
	return reconcile.ConfidenceForVerdict(v1Confidence, verdict)
}
