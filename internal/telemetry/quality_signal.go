package telemetry

import "github.com/samestrin/atcr/internal/scorecard"

// QualitySignal is the sole allowlisted outbound payload for the community prompt
// quality signal (Sprint 30.0). Like Event, it has exactly four fields with NO
// omitempty tags, and deliberately does NOT embed or extend Event, any scorecard
// struct, or localdebt.Record — so no source code, file path, or finding text can
// ever leak beyond {persona_id_hash, model, dismissed_count, confirmed_count}.
// The persona identifier is always the one-way HashPersonaID digest, never a raw
// persona name. Adding any field here is a privacy regression that
// TestQualitySignal_PayloadHasExactlyFourAllowlistedKeys guards.
//
// PersonaIDHash is pseudonymous, not anonymous: HashPersonaID is an UNSALTED
// SHA-256 over a small, enumerable persona-name set, so it does not defend against
// a dictionary attack (see internal/scorecard/telemetry.go and TD-007). It
// pseudonymizes for aggregation; it is not a secret.
type QualitySignal struct {
	PersonaIDHash  string `json:"persona_id_hash"`
	Model          string `json:"model"`
	DismissedCount int    `json:"dismissed_count"`
	ConfirmedCount int    `json:"confirmed_count"`
}

// NewQualitySignal builds a QualitySignal from an aggregated per-(persona, model)
// row's primitive fields, hashing the raw persona name via HashPersonaID at the
// payload-construction boundary — mirroring NewTelemetryPersonaRecord's split
// between internal aggregation (raw persona) and outbound payload (hashed). The
// model and counts pass through unchanged; a model slug is a non-PII catalog
// identifier. It takes primitives rather than a localdebt.QualityRow so the
// telemetry package never imports localdebt.
func NewQualitySignal(persona, model string, dismissed, confirmed int) QualitySignal {
	return QualitySignal{
		PersonaIDHash:  scorecard.HashPersonaID(persona),
		Model:          model,
		DismissedCount: dismissed,
		ConfirmedCount: confirmed,
	}
}
