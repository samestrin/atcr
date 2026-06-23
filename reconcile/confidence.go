package reconcile

import "strings"

// ConfidenceVerified is the top confidence tier — a finding whose verdict was
// confirmed (by a skeptic in the verify stage, or by surviving a judge ruling in
// the debate stage). The ordering is VERIFIED > HIGH > MEDIUM > LOW. It lives
// here, alongside the other confidence tiers, so every stage that recomputes
// confidence from a verdict shares one definition.
const ConfidenceVerified = "VERIFIED"

// ConfidenceForVerdict maps a finding's prior confidence and a verdict to its
// post-verdict confidence tier: a confirmed verdict promotes to VERIFIED, a
// refuted verdict demotes to LOW, and any other verdict — unverifiable, an empty
// string when no stage ran, or an unrecognized token — passes the prior
// confidence through unchanged. Verdict comparison is case-insensitive so a
// non-canonical casing does not silently fall through to the pass-through branch.
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

// confidenceRank is the confidence ordinal rubric (VERIFIED > HIGH > MEDIUM >
// LOW), the single source consumers gate on. An unknown tier maps to 0 so
// ConfidenceAtOrAbove fails closed for it.
var confidenceRank = map[string]int{
	ConfLow:            1,
	ConfMedium:         2,
	ConfHigh:           3,
	ConfidenceVerified: 4,
}

// ConfidenceAtOrAbove reports whether confidence c is at or above floor in the
// ordering (VERIFIED > HIGH > MEDIUM > LOW). Both arguments are normalized to
// canonical upper-case so the comparison is case- and whitespace-insensitive. It
// fails closed (returns false) when either value is empty or an unrecognized
// tier — a gate must never fire on a token it does not understand.
func ConfidenceAtOrAbove(c, floor string) bool {
	cr := confidenceRank[strings.ToUpper(strings.TrimSpace(c))]
	fr := confidenceRank[strings.ToUpper(strings.TrimSpace(floor))]
	if cr == 0 || fr == 0 {
		return false
	}
	return cr >= fr
}
