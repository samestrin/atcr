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
const SchemaVersion = 2

// Diagnostic message substrings emitted on the read path, exported so tests assert
// against the same literal the producer emits: a reword updates this one constant
// and every regression test follows it.
const (
	// MsgMalformedSkip is the substring logged when a JSONL line fails to parse.
	MsgMalformedSkip = "skipping malformed record"
)

// Record is one persisted technical-debt finding occurrence (the v1 schema in
// documentation/local-td-store-schema.md). The required block is always present;
// the optional block (omitempty) is present only when the reconciled finding
// carried the corresponding enrichment (Epic 18.3 justification/source_report) or a
// resolution has been recorded.
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
