// Package audit persists a tamper-evident, append-only record of every
// `atcr review` run and renders a one-page compliance report for a given pull
// request (Epic 19.1).
//
// Every review run appends exactly one JSON record to an append-only JSONL
// ledger at .atcr/audit.log.jsonl, capturing the run timestamp, the resolved
// base/head SHAs, the pull-request number (when known), and a summary of
// findings counted by severity. The `atcr audit-report --pr <n>` command reads
// that ledger back, selects the records for one PR, and renders a markdown
// report suitable for a compliance archive.
package audit

import "time"

// Record is one audit-trail entry for a single review run. It answers "what did
// atcr review, at what commit range, for which PR, and how many findings did it
// surface" — deliberately small and provenance-focused, not a reconstruction of
// the findings themselves (the review directory holds those).
type Record struct {
	// Timestamp is the review run's start time (RFC3339 in JSON).
	Timestamp time.Time `json:"ts"`
	// PR is the pull-request number the run reviewed. It is optional: a local
	// review with no PR context records the run with PR omitted (zero), so the
	// ledger stays complete even off the PR path.
	PR int `json:"pr,omitempty"`
	// Base is the resolved base SHA of the reviewed range.
	Base string `json:"base"`
	// Head is the resolved head SHA of the reviewed range.
	Head string `json:"head"`
	// Findings is the count of distinct findings by severity for this run
	// (e.g. {"HIGH": 2, "LOW": 3}). Empty/omitted when the run surfaced none.
	Findings map[string]int `json:"findings,omitempty"`
}
