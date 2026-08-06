// Package history persists code-review findings across runs and answers
// time-windowed, per-package queries against that ledger (Epic 19.0).
//
// Every `atcr review` run appends one JSON record per finding to the current
// month's shard, an append-only JSONL file at .atcr/history/YYYY-MM.jsonl (Epic
// 19.4 introduced month sharding; Epic 35.14 moved the shards under .atcr/, so a
// standalone user with no .planning/ directory can record and query history).
// Sharding by month is also the read index: a --since query selects shards by
// filename and never opens one whose filename proves its month falls outside
// the window. Two cases are deliberately always read (see LoadShardsSince's
// doc): a *.jsonl whose stem does not parse as YYYY-MM, and any future-dated
// shard — and the legacy flat ledger is read in full on every query, having no
// month in its name to select on. The `atcr
// history` command reads the selected shards, merged with the legacy pre-19.4
// flat ledger at .atcr/findings-history.jsonl if present, filtered by a duration
// window (--since) and a package path prefix (--package), and renders a markdown
// table of counts by severity per package.
package history

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strconv"
	"time"
)

// Record is one persisted finding occurrence from a single review run. The set
// of fields is intentionally small: the ledger answers "what findings has this
// package accrued over time", not "reconstruct the full finding".
type Record struct {
	// Timestamp is the review run's start time (RFC3339 in JSON).
	Timestamp time.Time `json:"ts"`
	// Package is the finding file's directory (filepath.Dir), the unit the
	// --package filter matches against.
	Package string `json:"package"`
	// Severity is the finding severity at the time of the run. It is stored (for
	// the per-severity table) but deliberately NOT part of ID.
	Severity string `json:"severity"`
	// ID is a stable content hash of file+line+problem, so the same finding
	// shares an id across runs even after its severity is re-settled.
	ID string `json:"id"`
	// File is the cited FILE path of the finding.
	File string `json:"file"`
	// Category is the finding category label.
	Category string `json:"category"`
}

// FindingID derives a stable id from a finding's location and problem text.
// Severity is deliberately excluded: it is mutably re-settled by the debate and
// verify stages (internal/debate/emit.go), so keying on it would mint a new id
// whenever severity changes and defeat cross-run trend tracking. The construction
// mirrors internal/debate.itemID (sha256 over NUL-separated fields, first 8 bytes
// hex-encoded).
func FindingID(file string, line int, problem string) string {
	h := sha256.Sum256([]byte(file + "\x00" + strconv.Itoa(line) + "\x00" + problem))
	return hex.EncodeToString(h[:8])
}

// PackageOf returns the package path for a finding file: its directory
// component, slash-normalized. A bare filename with no directory yields ".".
// ToSlash is applied AFTER filepath.Dir: on Windows filepath.Dir emits
// backslash separators, so normalizing afterward is what actually makes the
// stored package slash-normalized (the invariant Filter's prefix match relies on).
func PackageOf(file string) string {
	return filepath.ToSlash(filepath.Dir(file))
}
