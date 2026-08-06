package scorecard

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// HashPersonaID returns the lowercase hex SHA-256 digest of raw, pseudonymizing a
// raw Persona ID for the separate telemetry / cloud-sync Persona Leaderboard schema.
//
// It is deliberately NOT part of the Epic 10.0 PublicRecord allowlist / scrubField
// export path: it lives here (not in export.go) and never calls, wraps, or
// references PublicRecord, scrubField, AnonymizeRecord, or ScrubPublicRecord.
//
// CANONICALIZATION (epic 35.16.1): raw is reduced to
// strings.ToLower(strings.TrimSpace(raw)) BEFORE hashing, so every casing and
// padding variant of one persona yields one digest. This function previously
// performed no normalization at all; that is no longer true, and the reversal is
// deliberate rather than incidental.
//
// The reason is that the digest is the ONLY persona identity the telemetry backend
// ever holds, and it is irreversible on arrival: atcr.dev re-keys it with
// HMAC-SHA256 under a pepper that is never rotated and discards the pre-pepper
// value. A variant that escapes this function therefore mints a SECOND permanent,
// unmergeable identity for one persona, splitting its aggregate with no backfill
// available afterwards. Normalizing at the hash boundary — rather than at each call
// site — is what makes every producer, present and future, correct by construction;
// cloudsync.go had already patched the whitespace half of this at its own call site,
// which is exactly the one-site fix that generalizes here.
//
// Plain strings.ToLower, deliberately: NOT golang.org/x/text/cases folding and NOT
// NFC normalization. The persona catalog (personas/community/*.yaml) is pure
// lowercase ASCII, x/text is not a direct dependency, and atcr.dev must reproduce
// this transform in one line of JS to build its persona dictionary. A non-ASCII
// persona still hashes fine — ToLower is simply a no-op on scripts without case.
//
// Hashing remains total over every Go string, including the empty string, returns
// no error, and cannot panic. No validation is performed.
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
	sum := sha256.Sum256([]byte(canonicalPersonaID(raw)))
	return hex.EncodeToString(sum[:])
}

// canonicalPersonaID reduces a raw persona name to the single form every producer
// hashes. It is the one definition of "the same persona" on the telemetry path, and
// internal/telemetry inlines an identical copy (that package is a transport leaf and
// may not import this one — see NewQualitySignal). TestQualitySignal_PersonaHashedNotRaw
// locks the two byte-for-byte, so this cannot drift without a test failing.
//
// Trim first, then lower: the order is immaterial for ASCII but fixed so the JS the
// atcr.dev dictionary uses (`name.trim().toLowerCase()`) is a literal translation.
func canonicalPersonaID(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
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
