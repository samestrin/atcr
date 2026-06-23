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
