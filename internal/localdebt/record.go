package localdebt

import "github.com/samestrin/atcr/internal/history"

// SchemaVersion is the local TD record schema version. It is emitted as an integer
// on every record so the format can evolve independently; a future
// backward-incompatible change increments this and older records stay readable
// while forward-incompatible (newer) records are skipped on read.
const SchemaVersion = 1

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
