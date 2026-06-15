package verify

import (
	"strings"

	"github.com/samestrin/atcr/internal/reconcile"
)

// ConfidenceVerified is the confidence-v2 tier for a finding a skeptic confirmed.
// It sits above reconcile.ConfHigh in the v2 ordering (VERIFIED > HIGH > MEDIUM >
// LOW). It lives here, not in reconcile, because the verify stage owns the
// confidence-v2 axis — reconcile only knows the v1 tiers.
const ConfidenceVerified = "VERIFIED"

// confidenceV2 maps a finding's v1 confidence and a skeptic verdict to its v2
// confidence tier (AC 03-01). It is a pure function with no I/O: a confirmed
// verdict promotes to VERIFIED regardless of the v1 level, a refuted verdict
// demotes to LOW, and any other verdict — unverifiable, the empty string when no
// skeptic ran, or an unrecognized token — passes the v1 confidence through
// unchanged. Verdict comparison is case-insensitive so a non-canonical casing
// from a skeptic does not silently fall through to the pass-through branch.
func confidenceV2(v1Confidence, verdict string) string {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case verdictConfirmed:
		return ConfidenceVerified
	case verdictRefuted:
		return reconcile.ConfLow
	default:
		return v1Confidence
	}
}
