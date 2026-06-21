package reconcile

import "strings"

// ConfidenceVerified is the top confidence tier — a finding whose verdict was
// confirmed (by a skeptic in the verify stage, or by surviving a judge ruling in
// the debate stage). The v2 ordering is VERIFIED > HIGH > MEDIUM > LOW. It lives
// here, alongside the other confidence tiers and the JSONFinding it stamps, so
// every stage that recomputes confidence from a verdict shares one definition
// (Epic 6.0 folded the cross-examination stage onto this axis rather than adding a
// DEBATED tier — see Clarifications).
const ConfidenceVerified = "VERIFIED"

// ConfidenceForVerdict maps a finding's prior confidence and a verdict to its
// post-verdict confidence tier: a confirmed verdict promotes to VERIFIED, a
// refuted verdict demotes to LOW, and any other verdict — unverifiable, an empty
// string when no stage ran, or an unrecognized token — passes the prior
// confidence through unchanged. Verdict comparison is case-insensitive so a
// non-canonical casing does not silently fall through to the pass-through branch.
// It is the single confidence-from-verdict rule shared by the verify and debate
// stages.
func ConfidenceForVerdict(prior, verdict string) string {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case VerdictConfirmed:
		return ConfidenceVerified
	case VerdictRefuted:
		return ConfLow
	default:
		return prior
	}
}
