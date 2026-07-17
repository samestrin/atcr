package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
)

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
// row's primitive fields, hashing the raw persona name at the payload-construction
// boundary so the raw name never reaches the wire — mirroring
// NewTelemetryPersonaRecord's split between internal aggregation (raw persona) and
// outbound payload (hashed). The model and counts pass through unchanged; a model
// slug is a non-PII catalog identifier.
//
// The persona is hashed with the SAME unsalted SHA-256 hex scheme as
// internal/scorecard.HashPersonaID, but inlined rather than imported: telemetry is
// a low-level transport leaf and scorecard is a high-level package (imports
// reconcile/fanout/llmclient), so a telemetry->scorecard import would invert the
// dependency direction the boundaries test enforces. TestQualitySignal_PersonaHashedNotRaw
// (a test-only cross-boundary import, which the boundary check permits) locks byte
// equivalence to scorecard.HashPersonaID, so the two schemes cannot silently drift
// — e.g. hardening HashPersonaID to a keyed HMAC (TD-007) fails that test until
// this is updated in lockstep. It takes primitives rather than a localdebt.QualityRow
// so telemetry never imports localdebt either.
func NewQualitySignal(persona, model string, dismissed, confirmed int) QualitySignal {
	sum := sha256.Sum256([]byte(persona))
	return QualitySignal{
		PersonaIDHash:  hex.EncodeToString(sum[:]),
		Model:          model,
		DismissedCount: dismissed,
		ConfirmedCount: confirmed,
	}
}
