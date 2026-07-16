package scorecard

import (
	"crypto/sha256"
	"encoding/hex"
	"unsafe"
)

// HashPersonaID returns the lowercase hex SHA-256 digest of raw, pseudonymizing a
// raw Persona ID for the separate telemetry / cloud-sync Persona Leaderboard schema.
//
// It is deliberately NOT part of the Epic 10.0 PublicRecord allowlist / scrubField
// export path: it lives here (not in export.go) and never calls, wraps, or
// references PublicRecord, scrubField, AnonymizeRecord, or ScrubPublicRecord. It
// performs no normalization (no case-folding, no trimming) and no validation:
// hashing is total over every Go string, including the empty string, returns no
// error, and cannot panic.
//
// Guarantee and its bound: SHA-256 is a one-way (preimage-resistant) hash, so a
// digest is not directly reversible. But Persona IDs are a small, enumerable,
// often publicly-known set (community-registry persona names), so this UNSALTED
// digest does not defend against a dictionary/rainbow attack that pre-hashes known
// persona names — it pseudonymizes identities for aggregation, it is not a secret.
// Hardening to a keyed HMAC-SHA256 with an application pepper is deferred (see the
// sprint's tech-debt-captured.md TD-007): it needs a provisioned secret and would
// change the AC-pinned digest values, so it is scoped with the real-endpoint decision.
func HashPersonaID(raw string) string {
	// Hash the string's bytes in place without the []byte(raw) copy: a string is
	// an immutable byte sequence, so unsafe.Slice over unsafe.StringData yields the
	// exact same bytes (and thus the exact same digest) as []byte(raw), with no
	// per-call allocation. unsafe.Slice is safe for any pointer when len == 0, so
	// the empty-string case (StringData's result is unspecified there) still hashes
	// to the well-known SHA-256("") constant.
	sum := sha256.Sum256(unsafe.Slice(unsafe.StringData(raw), len(raw)))
	return hex.EncodeToString(sum[:])
}

// TelemetryPersonaRecord is the telemetry / cloud-sync-scoped Persona Leaderboard
// record. It is a distinct type from PublicRecord (no shared embedding or field
// aliasing, so the two are not structurally assignable) and carries a deliberate
// allowlist of its own: the one-way-hashed persona id plus the model (non-PII,
// already public elsewhere in the codebase). It never carries the raw Reviewer,
// RunID, cost, or token fields. Consumed by the Story 4 --sync-cloud payload.
// Note: PersonaIDHash is pseudonymous (not anonymous) and requires HMAC hardening
// before production endpoint activation to prevent dictionary reversing of hashes.
type TelemetryPersonaRecord struct {
	PersonaIDHash string `json:"persona_id_hash"`
	// Model is the bound provider+model slug that answered this review, carried
	// unhashed. Per the project's model-binding contract (see internal/registry
	// and internal/personas), a model identifier is a non-PII, publicly-known
	// catalog slug (e.g. "claude-sonnet-4-6"), never user-supplied free text, so it
	// carries no personal data to protect and is intentionally not hashed.
	Model string `json:"model"`
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
