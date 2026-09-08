package reconcile

// Verification is the per-finding adversarial-verification block (Epic 3.0): the
// skeptic verdict carried alongside a reconciled finding. It is an omitempty
// pointer on the wire, so a finding without verification serializes to nothing —
// readers and renderers must tolerate both its absence and its presence.
//
// Contract: a writing stage MUST validate Verdict against the allowed enum
// (confirmed, refuted, unverifiable) before persisting; an empty Verdict is a
// contract violation. Readers do not re-validate the enum.
type Verification struct {
	Verdict string `json:"verdict"` // confirmed | refuted | unverifiable
	Skeptic string `json:"skeptic"` // agent that produced the verdict
	// Notes is populated only from the winning verdict during a cluster-merge;
	// minority-verdict reasoning is intentionally not preserved.
	Notes string `json:"notes,omitempty"`
	// ChallengeSurvived marks a finding upheld by the cross-examination stage
	// (Epic 6.0). It rides alongside Verdict and is a display/audit marker, never
	// a separate confidence tier; omitempty keeps every non-debated finding
	// byte-identical.
	ChallengeSurvived bool `json:"challenge_survived,omitempty"`
	// Truncated marks a verdict the skeptic reached from a SHORTENED read: its
	// tool-output ceiling tripped, but because that ceiling was DERIVED from the
	// agent's window rather than declared by an operator, the trip truncated the
	// read instead of overruling the answer (see internal/verify's
	// tripsVoidTheVerdict). The verdict stands — this is a caveat on how it was
	// reached, never a separate tier and never a downgrade.
	//
	// It exists because that fact previously reached exactly ONE artifact,
	// reconciled/verification.json, so report.md rendered a truncated confirmed
	// with no caveat and the durable per-reviewer precision score charged it as a
	// full-confidence read. Like ChallengeSurvived it is an additive omitempty
	// marker, so every finding that is not truncated serializes byte-identically.
	Truncated bool `json:"truncated,omitempty"`
}

// Verdict enum values for Verification.Verdict (Epic 3.0). The verify stage
// validates skeptic output against this set before persisting; the gate reads
// these constants to exclude refuted findings and, under requireVerified, to
// count only confirmed ones.
const (
	VerdictConfirmed    = "confirmed"
	VerdictRefuted      = "refuted"
	VerdictUnverifiable = "unverifiable"
)

// Valid reports whether v carries an in-enum Verdict and a non-empty Skeptic.
// Writing stages must call this before persisting; the contract was prose-only
// before this method existed.
func (v Verification) Valid() bool {
	switch v.Verdict {
	case VerdictConfirmed, VerdictRefuted, VerdictUnverifiable:
		return v.Skeptic != ""
	}
	return false
}
