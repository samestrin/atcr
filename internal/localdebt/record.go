package localdebt

import (
	"strings"

	"github.com/samestrin/atcr/internal/history"
)

// SchemaVersion is the local TD record schema version. It is emitted as an integer
// on every record so the format can evolve independently; a future
// backward-incompatible change increments this and older records stay readable
// while forward-incompatible (newer) records are skipped on read.
//
// v1 -> v2 (Sprint 30.0): adds the optional Model attribution field. The bump is
// backward-compatible on read — a v1 record has no "model" key and decodes with
// Model == "" — and forward-compatible in the established sense: this reader
// accepts v1 and v2 (r.SchemaVersion <= SchemaVersion), while v3+ records stay
// forward-incompatible and are skipped with a warning (store.go decodeRecord).
//
// Downgrade visibility (accepted loss): a pre-30.0 binary (SchemaVersion == 1)
// reads v2 records as forward-incompatible and skips them, so v2 findings are
// intentionally invisible to older binaries. An old reader would structurally
// decode a v2 record and ignore the unknown model key, but the
// forward-incompat-skip model treats every newer version uniformly rather than
// special-casing additive bumps.
//
// v2 -> v3 (Plan 35.13): adds the optional Origin provenance field and the
// Occurrences / FirstSeen pair, as .atcr/debt/ becomes the single system of record
// for every atcr debt subcommand — a manual `atcr debt add` will write here
// alongside reconcile once T2 rewires it. The bump is backward-compatible on read —
// a v1/v2 record has none of those keys and decodes with the Go zero values, which
// EffectiveOrigin reports as "review" without mutating the record — and
// forward-compatible in the same established sense: this reader accepts v1, v2 and
// v3, while v4+ records stay forward-incompatible and are skipped with a warning.
//
// Downgrade visibility (accepted loss): a pre-35.13 binary (SchemaVersion == 2)
// reads v3 records as forward-incompatible and skips them, so findings written
// after the bump are intentionally invisible to older binaries. This is the same
// accepted loss the v1 -> v2 bump documented; the uniform forward-incompat-skip
// model is preserved rather than special-casing additive bumps.
const SchemaVersion = 3

// Diagnostic message substrings emitted on the read path, exported so tests assert
// against the same literal the producer emits: a reword updates this one constant
// and every regression test follows it.
const (
	// MsgMalformedSkip is the substring logged when a JSONL line fails to parse.
	MsgMalformedSkip = "skipping malformed record"
)

// Origin values are the closed set a v3 record's origin field may carry. They are
// spelled once here so no call site writes a bare literal. An empty Origin on a
// v1/v2 record means OriginReview: reconcile was the only writer before v3 (see
// EffectiveOrigin).
const (
	// OriginReview marks a record produced by an `atcr reconcile` run
	// (cli/reconcile.go's persistence hook).
	OriginReview = "review"
	// OriginManual marks a record filed by hand through `atcr debt add`, which has
	// no reconcile run behind it and so carries a synthetic ManualRunID.
	OriginManual = "manual"
)

// Record is one persisted technical-debt finding occurrence (the schema documented
// in docs/technical-debt.md; v3 as of Plan 35.13, which added the optional Origin,
// Occurrences and FirstSeen fields on top of v2's Model). The
// required block is always present; the optional block
// (omitempty) is present only when the reconciled finding carried the
// corresponding enrichment (Epic 18.3 justification/source_report), a resolved
// model attribution, or a recorded resolution.
type Record struct {
	SchemaVersion int      `json:"schema_version"`
	ID            string   `json:"id"`
	RunID         string   `json:"run_id"`
	Timestamp     string   `json:"ts"`
	Severity      string   `json:"severity"`
	File          string   `json:"file"`
	Line          int      `json:"line"`
	Problem       string   `json:"problem"`
	Fix           string   `json:"fix"`
	Category      string   `json:"category"`
	EstMinutes    int      `json:"est_minutes"`
	Evidence      string   `json:"evidence"`
	Reviewers     []string `json:"reviewers"`
	Confidence    string   `json:"confidence"`

	// Model is the provider+model slug (e.g. "claude-sonnet-4-6") that produced
	// this finding, added in schema v2 for per-(persona, model) quality-signal
	// aggregation (Sprint 30.0). It is populated at write time by persistLocalDebt
	// from the fan-out pool summary's AgentStatus.Model. omitempty keeps it absent
	// from v1 records and from v2 records whose model attribution could not be
	// resolved; such attribution-incomplete records are excluded from per-model
	// aggregation rows rather than bucketed under an empty model. A model slug is a
	// non-PII, publicly-known catalog identifier (see internal/scorecard/telemetry.go),
	// never code/path/finding content.
	Model string `json:"model,omitempty"`

	// Origin is the provenance of this record — OriginReview for a finding produced
	// by an `atcr reconcile` run, OriginManual for one filed by `atcr debt add` —
	// added in schema v3 (Plan 35.13) when .atcr/debt/ became the single store for
	// every debt subcommand and a manual entry stopped being distinguishable from a
	// review finding. omitempty keeps it absent whenever no origin was recorded —
	// on every v1/v2 record, and on any v3 record whose writer left it unset — and
	// an absent origin means OriginReview on ANY schema version, because reconcile
	// was the only writer before v3 and remains the only one until T2 rewires
	// `atcr debt add`. Read it through EffectiveOrigin rather than defaulting at
	// each call site.
	Origin string `json:"origin,omitempty"`

	// Occurrences is how many times this id has been observed across the store's
	// history, added in schema v3 (Plan 35.13). It is reserved for compaction: T5
	// will fold an id's records to the latest one and carry this count forward, so
	// the regression signal that re-openable resolution creates survives at O(1)
	// size instead of O(history). Until that lands, nothing populates it and
	// FoldRecords preserves only the selected record's value. omitempty keeps it
	// absent from uncompacted records; readers treat an absent/zero value as one
	// occurrence ("not yet aggregated").
	Occurrences int `json:"occurrences,omitempty"`

	// FirstSeen is the RFC3339 timestamp of the earliest record observed for this
	// id, added in schema v3 (Plan 35.13). Like Occurrences it is reserved for
	// compaction: T5 will carry it forward so the original sighting is not lost
	// when superseded records are dropped; today FoldRecords preserves only the
	// selected record's value. omitempty keeps it absent from uncompacted records,
	// where the record's own Timestamp is the earliest known sighting.
	FirstSeen string `json:"first_seen,omitempty"`

	Justification string        `json:"justification,omitempty"`
	SourceReport  *SourceReport `json:"source_report,omitempty"`
	Status        string        `json:"status,omitempty"`
	ResolvedAt    string        `json:"resolved_at,omitempty"`
}

// SourceReport is a back-reference to the review.md section a justification was
// extracted from (Epic 18.3). Path is always present when the object is; Line and
// Section are best-effort.
type SourceReport struct {
	Path    string `json:"path"`
	Line    int    `json:"line,omitempty"`
	Section string `json:"section,omitempty"`
}

// StampID sets ID to the stable content hash of the finding's location and problem
// text, reusing history.FindingID verbatim (SHA-256 over file\x00line\x00problem,
// first 8 bytes hex-encoded). Severity is deliberately excluded so a re-settled
// severity keeps the same ID across runs. StampID is a pure setter with no error
// path: FindingID always yields a deterministic digest and never panics, even on an
// empty problem string.
func (r *Record) StampID() {
	r.ID = history.FindingID(r.File, r.Line, r.Problem)
}

// EffectiveOrigin reports the record's provenance, resolving a v1/v2 record's
// absent origin to OriginReview — reconcile was the only writer before schema v3.
// The value is trimmed and lowercased, and a whitespace-only origin is treated as
// absent rather than as a distinct value.
//
// The default is applied here, at the call site, and deliberately NOT on the read
// path: Compact re-marshals exactly what ReadAll returned, so backfilling Origin
// during decode would rewrite a v1/v2 record with an origin key while its
// schema_version still reads 1 or 2 — an on-disk record matching no schema
// version. EffectiveOrigin takes a value receiver and never mutates r.
func (r Record) EffectiveOrigin() string {
	if o := strings.ToLower(strings.TrimSpace(r.Origin)); o != "" {
		return o
	}
	return OriginReview
}

// IsClosedStatus reports whether a record's status takes an item out of the open
// backlog. Both resolved and wontfix/deferred terminal statuses are closed.
func IsClosedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "resolved", "deferred", "wontfix":
		return true
	default:
		return false
	}
}

// ClosedStatusRank orders terminal statuses so a deterministic effective status can
// be chosen when divergent terminal records exist for one id.
func ClosedStatusRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "wontfix":
		return 3
	case "resolved":
		return 2
	case "deferred":
		return 1
	default:
		return 0
	}
}

// HigherClosedStatus returns whichever of the two terminal statuses ranks higher.
func HigherClosedStatus(current, candidate string) string {
	if ClosedStatusRank(candidate) > ClosedStatusRank(current) {
		return candidate
	}
	return current
}
