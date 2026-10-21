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
// NewCloudSyncRecord had already patched the whitespace half of this at its own
// call site, which is exactly the one-site fix that generalizes here.
//
// Plain strings.ToLower, deliberately: NOT golang.org/x/text/cases folding and NOT
// NFC normalization — even though the module now requires golang.org/x/text
// directly, so availability is not the reason. The persona catalog
// (personas/community/*.yaml) is pure lowercase ASCII, and atcr.dev must reproduce
// this transform in one line of JS to build its persona dictionary. The Go/JS
// equivalence holds for ASCII names (which is the entire published catalog — all
// personas/community/*.yaml name values are lowercase ASCII), and a CASED
// non-ASCII name may be a backend lookup miss. See
// TestHashPersonaID_GoJSDivergenceIsASCIIScoped for the pinned limits.
//
// Invalid UTF-8 is normalized to U+FFFD by strings.ToLower, so byte-distinct
// invalid inputs may collapse to one digest. Hashing remains total over every Go
// string, including the empty string, returns no error, and cannot panic. No
// validation is performed.
//
// PSEUDONYM CONTRACT (epic 35.17). The digest is a pseudonym: it is not anonymous
// and not a secret. SHA-256 is one-way, but Persona IDs are a small, enumerable,
// often publicly-known set (community-registry persona names), so anyone can
// pre-hash the known names and reverse this UNSALTED digest with a dictionary. It
// exists so one persona aggregates under one stable identifier while the raw name
// stays on the user's machine; that is all it promises.
//
// The keyed step runs at INGESTION, not here. Each backend stores
// HMAC-SHA256(pepper, received_digest) and never writes the received digest to a
// table or a log: atcr.dev Epic 2.1 does this for quality signals and
// atcr-enterprise Epic 15.0 for --sync-cloud scorecards, each under its own pepper
// in its own database, so one breach cannot correlate the two populations. What
// remains exposed is the digest in transit, which TLS covers.
//
// A client-side key cannot work, which is why this stays unkeyed. This function
// runs in a public open-source binary: a key compiled into it ships to the attacker
// with the code, so the dictionary is just as rebuildable. Fetching the key at run
// time has no credential to authorize it, because the usage ping is deliberately
// unauthenticated. A per-user key would break determinism, the one property the
// hash exists for. Keying the digest here would also change every digest value
// and split each persona's aggregate at the backend.
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
// Note: PersonaIDHash is pseudonymous, not anonymous; the keyed step that stops a
// stored digest from being reversed runs at ingestion (see HashPersonaID).
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
//
// A whitespace-only persona canonicalizes to the empty string, so it returns the
// zero TelemetryPersonaRecord sentinel rather than sha256("")=e3b0c442..., which
// would look like a real, stable aggregation bucket on the backend.
func NewTelemetryPersonaRecord(r Record) TelemetryPersonaRecord {
	if canonicalPersonaID(r.Reviewer) == "" {
		return TelemetryPersonaRecord{}
	}
	return TelemetryPersonaRecord{
		PersonaIDHash: HashPersonaID(r.Reviewer),
		Model:         r.Model,
	}
}
