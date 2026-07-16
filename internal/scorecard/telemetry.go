package scorecard

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashPersonaID returns the lowercase hex SHA-256 digest of raw.
//
// It is deliberately NOT part of the Epic 10.0 PublicRecord allowlist / scrubField
// export path: it lives here (not in export.go) and never calls, wraps, or
// references PublicRecord, scrubField, AnonymizeRecord, or ScrubPublicRecord. The
// one-way hash — not text scrubbing — is the non-reversibility guarantee for the
// separate telemetry / cloud-sync Persona Leaderboard schema. It performs no
// normalization (no case-folding, no trimming) and no validation: hashing is
// total over every Go string, including the empty string, returns no error, and
// cannot panic.
func HashPersonaID(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// TelemetryPersonaRecord is the telemetry / cloud-sync-scoped Persona Leaderboard
// record. It is a distinct type from PublicRecord (no shared embedding or field
// aliasing, so the two are not structurally assignable) and carries a deliberate
// allowlist of its own: the one-way-hashed persona id plus the model (non-PII,
// already public elsewhere in the codebase). It never carries the raw Reviewer,
// RunID, cost, or token fields. Consumed by the Story 4 --sync-cloud payload.
type TelemetryPersonaRecord struct {
	PersonaIDHash string `json:"persona_id_hash"`
	Model         string `json:"model"`
}

// NewTelemetryPersonaRecord builds a TelemetryPersonaRecord from a scorecard
// Record: it hashes Record.Reviewer (the raw Persona ID — the same field
// AnonymizeRecord reads) via HashPersonaID and copies Model through unhashed. It
// accepts any Record without validation (mirroring AnonymizeRecord's permissive
// style) and never copies the raw Reviewer value in unhashed form; a zero-value
// Record yields the hash of the empty string.
func NewTelemetryPersonaRecord(r Record) TelemetryPersonaRecord {
	return TelemetryPersonaRecord{
		PersonaIDHash: HashPersonaID(r.Reviewer),
		Model:         r.Model,
	}
}
