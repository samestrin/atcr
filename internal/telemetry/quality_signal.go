package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/samestrin/atcr/internal/modelslug"
)

// canonicalPersonaID is a deliberate BYTE-FOR-BYTE MIRROR of
// scorecard.canonicalPersonaID. It is duplicated rather than imported because this
// package is a transport leaf and must not depend on internal/scorecard (the
// boundaries test enforces that direction).
//
// ⚠️ The drift gate for this duplication lives in the OTHER package's test suite:
// TestQualitySignal_PersonaHashedNotRaw (quality_signal_test.go) compares both
// producers on non-canonical inputs. Editing internal/scorecard and running only
// `go test ./internal/scorecard/...` will NOT catch a divergence — run
// `go test ./internal/telemetry/...` too, or the two silently disagree and split
// one persona into two permanent backend identities.
func canonicalPersonaID(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// QualitySignal is the sole allowlisted outbound payload for the community prompt
// quality signal (Sprint 30.0). Like Event, its fields carry NO omitempty tags,
// and it deliberately does NOT embed or extend Event, any scorecard struct, or
// localdebt.Record — so no source code, file path, or finding text can ever leak
// beyond {persona_id_hash, model, dismissed_count, confirmed_count,
// wrong_merge_count, wrong_split_count, wrong_category_count}. The persona
// identifier is always the one-way HashPersonaID digest, never a raw persona
// name. Adding any field here is a privacy regression that
// TestQualitySignal_PayloadHasExactlyTheAllowlistedKeys guards.
//
// The last three keys (Epic 36.1) are reconciler-error counts: how many of the
// pair's findings a developer flagged as wrongly merged, wrongly split or wrongly
// categorized by reconcile. They are a verdict on the grouping, not on the
// reviewer, so they are kept apart from dismissed_count (the reviewer-error
// count). The atcr.dev backend validates this payload by strict key-set
// equality, so changing the key set is a coordinated change with the ingest.
//
// PersonaIDHash is pseudonymous, not anonymous: HashPersonaID is an UNSALTED
// SHA-256 over a small, enumerable persona-name set, so it does not defend against
// a dictionary attack. It pseudonymizes for aggregation; it is not a secret. The
// keyed step runs at ingestion (atcr.dev peppers the digest on arrival), not here —
// see internal/scorecard.HashPersonaID for the contract.
type QualitySignal struct {
	PersonaIDHash  string `json:"persona_id_hash"`
	Model          string `json:"model"`
	DismissedCount int    `json:"dismissed_count"`
	ConfirmedCount int    `json:"confirmed_count"`

	WrongMergeCount    int `json:"wrong_merge_count"`
	WrongSplitCount    int `json:"wrong_split_count"`
	WrongCategoryCount int `json:"wrong_category_count"`
}

// ReconcilerCounts carries a row's reconciler-error counts into NewQualitySignal,
// one per localdebt reconciler-feedback kind. It exists so the constructor does
// not take five adjacent ints a caller could transpose.
type ReconcilerCounts struct {
	WrongMerge    int
	WrongSplit    int
	WrongCategory int
}

// NewQualitySignal builds a QualitySignal from an aggregated per-(persona, model)
// row's primitive fields, hashing the raw persona name at the payload-construction
// boundary so the raw name never reaches the wire — mirroring
// NewTelemetryPersonaRecord's split between internal aggregation (raw persona) and
// outbound payload (hashed). The counts pass through unchanged. The model is not
// hashed, so it is validated with modelslug.Sanitize, the validator
// NewTelemetryPersonaRecord shares: a slug that is not a plain catalog identifier
// is sent as an empty model (epic 35.17 AC6). The reconciler-error counts pass
// through unchanged too, each on its own key.
//
// The persona is hashed with the SAME unsalted SHA-256 hex scheme as
// internal/scorecard.HashPersonaID, but inlined rather than imported: telemetry is
// a low-level transport leaf and scorecard is a high-level package (imports
// reconcile/fanout/llmclient), so a telemetry->scorecard import would invert the
// dependency direction the boundaries test enforces. TestQualitySignal_PersonaHashedNotRaw
// (a test-only cross-boundary import, which the boundary check permits) locks byte
// equivalence to scorecard.HashPersonaID, so the two schemes cannot silently drift
// — any change to HashPersonaID's scheme fails that test until this is updated in
// lockstep. It takes primitives rather than a localdebt.QualityRow
// so telemetry never imports localdebt either.
//
// CANONICALIZATION (epic 35.16.1): the persona is reduced to
// strings.ToLower(strings.TrimSpace(persona)) before hashing, matching
// scorecard.HashPersonaID exactly. The digest is the only persona identity the
// backend ever holds and atcr.dev peppers it irreversibly on arrival, so a casing
// or padding variant would mint a second permanent identity for one persona with
// no backfill available to merge them.
//
// A non-empty persona is a caller precondition: an empty persona is an upstream
// data-quality bug, and hashing it would produce sha256("")=e3b0c442... — a
// well-known constant that looks like a real, stable aggregation bucket on the
// backend. The constructor instead returns the zero QualitySignal sentinel
// (empty PersonaIDHash), recognizable and droppable by the caller. The sole
// production source (localdebt.AggregateQualitySignal) already excludes empty
// personas structurally, so this guard only fires for future misuse.
//
// The guard is evaluated on the CANONICAL value, not the raw one. A whitespace-only
// persona is empty in every sense that matters here; checking `persona == ""` first
// would let "   " past the sentinel and hash it to exactly the constant the sentinel
// exists to keep off the backend.
func NewQualitySignal(persona, model string, dismissed, confirmed int, reconciler ReconcilerCounts) QualitySignal {
	canonical := canonicalPersonaID(persona)
	if canonical == "" {
		return QualitySignal{}
	}
	sum := sha256.Sum256([]byte(canonical))
	return QualitySignal{
		PersonaIDHash:  hex.EncodeToString(sum[:]),
		Model:          modelslug.Sanitize(model),
		DismissedCount: dismissed,
		ConfirmedCount: confirmed,

		WrongMergeCount:    reconciler.WrongMerge,
		WrongSplitCount:    reconciler.WrongSplit,
		WrongCategoryCount: reconciler.WrongCategory,
	}
}
