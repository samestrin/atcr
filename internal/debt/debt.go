// Package debt provides read/query tooling over the Epic-12.1 sharded
// technical-debt store under .planning/technical-debt/items/. It is a thin,
// deterministic layer on top of internal/tdmigrate: it reuses that package's
// Item/Shard types, shard loader, and README<->shard migration rather than
// re-implementing a parser. The atcr debt CLI (cmd/atcr/debt.go) is the only
// consumer today; the logic lives here so the whole surface is unit-testable
// without spawning a process.
package debt

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/tdmigrate"
)

// Record is a single technical-debt item flattened out of its shard, carrying
// the shard-level provenance (Date, Label) that an item needs for age-based
// sorting and source grouping but that lives on the Shard, not the Item.
type Record struct {
	tdmigrate.Item
	Date  string // shard date, YYYY-MM-DD
	Label string // shard label (the source section the item came from)
}

// Load reads every YAML shard under itemsDir (strict-loaded and schema-checked
// by tdmigrate.LoadShards) and flattens them into a deterministic slice ordered
// by shard filename then in-shard item order.
func Load(itemsDir string) ([]Record, error) {
	shards, err := tdmigrate.LoadShards(itemsDir)
	if err != nil {
		return nil, err
	}
	return Flatten(shards), nil
}

// Flatten expands shards into their constituent Records, preserving order.
func Flatten(shards []tdmigrate.Shard) []Record {
	var recs []Record
	for _, s := range shards {
		for _, it := range s.Items {
			recs = append(recs, Record{Item: it, Date: s.Date, Label: s.Label})
		}
	}
	return recs
}

// Filter selects Records by exact/substring field matches. A zero-value field
// (empty string) matches anything, so a zero Filter is a pass-through.
type Filter struct {
	Severity  string // exact, case-insensitive (CRITICAL|HIGH|MEDIUM|LOW)
	Status    string // exact (open|deferred|resolved)
	Category  string // substring, case-insensitive
	Component string // path-prefix match against the item's File
	Group     string // exact group label
}

// filePath strips a trailing :line (or :line-range) suffix from a File value so
// component prefix matching compares directory paths, not line numbers. It only
// trims a suffix that is purely digits/`-` after the last colon, leaving
// free-text File values (no colon, or a non-numeric tail) untouched.
func filePath(file string) string {
	i := strings.LastIndex(file, ":")
	if i < 0 {
		return file
	}
	tail := file[i+1:]
	if tail == "" {
		return file
	}
	for _, r := range tail {
		if (r < '0' || r > '9') && r != '-' {
			return file // not a line/range suffix; keep verbatim
		}
	}
	return file[:i]
}

// Match reports whether r satisfies every non-empty field of f.
func (f Filter) Match(r Record) bool {
	if f.Severity != "" && !strings.EqualFold(r.Severity, f.Severity) {
		return false
	}
	if f.Status != "" && !strings.EqualFold(r.Status, f.Status) {
		return false
	}
	if f.Category != "" && !strings.Contains(strings.ToLower(r.Category), strings.ToLower(f.Category)) {
		return false
	}
	if f.Group != "" && r.Group != f.Group {
		return false
	}
	if f.Component != "" && !strings.HasPrefix(filePath(r.File), f.Component) {
		return false
	}
	return true
}

// Apply returns the subset of recs matching f, preserving order. It always
// returns a non-nil slice so callers can range/marshal without nil checks.
func Apply(recs []Record, f Filter) []Record {
	out := make([]Record, 0, len(recs))
	for _, r := range recs {
		if f.Match(r) {
			out = append(out, r)
		}
	}
	return out
}

// sanitizeCell makes a value safe to embed in a Markdown table cell: newlines
// collapse to spaces and literal pipes become "/", mirroring the canonical
// TD-table contract used by tdmigrate.GenerateTable so a row round-trips. Shared
// by the README-append (add) and dashboard renderers.
func sanitizeCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "/")
	return strings.TrimSpace(s)
}

// Sort keys accepted by Sort.
const (
	SortSeverity = "severity" // CRITICAL first, then age within a severity
	SortAge      = "age"      // oldest shard date first
	SortEst      = "est"      // largest est_minutes first
	SortFile     = "file"     // lexicographic by File
)

// severityRank orders severities most-severe first. Unknown severities sort last.
var severityRank = map[string]int{"CRITICAL": 0, "HIGH": 1, "MEDIUM": 2, "LOW": 3}

func rankOf(sev string) int {
	if r, ok := severityRank[strings.ToUpper(sev)]; ok {
		return r
	}
	return len(severityRank) // unknown -> last
}

// Sort orders recs in place by the given key. An unknown key is a hard error so
// a typo'd --sort flag fails loudly instead of silently returning unsorted data.
// Every ordering is total (ties broken by File then Date) so output is
// deterministic across runs.
func Sort(recs []Record, key string) error {
	var less func(a, b Record) bool
	switch key {
	case SortSeverity:
		less = func(a, b Record) bool {
			if ra, rb := rankOf(a.Severity), rankOf(b.Severity); ra != rb {
				return ra < rb
			}
			if a.Date != b.Date {
				return a.Date < b.Date // older first within a severity
			}
			return a.File < b.File
		}
	case SortAge:
		less = func(a, b Record) bool {
			if a.Date != b.Date {
				return a.Date < b.Date // older first
			}
			return a.File < b.File
		}
	case SortEst:
		less = func(a, b Record) bool {
			if a.EstMinutes != b.EstMinutes {
				return a.EstMinutes > b.EstMinutes // largest first
			}
			return a.File < b.File
		}
	case SortFile:
		less = func(a, b Record) bool {
			if a.File != b.File {
				return a.File < b.File
			}
			return a.Date < b.Date
		}
	default:
		return fmt.Errorf("unknown sort key %q (want severity|age|est|file)", key)
	}
	sort.SliceStable(recs, func(i, j int) bool { return less(recs[i], recs[j]) })
	return nil
}

// SyncShards regenerates the shard store under itemsDir from the authoritative
// README table, reusing tdmigrate's migrate path (parse README -> write shards).
// It is the "resync stale shards from the authoritative source" step the
// shard-reading commands run when invoked with --sync. Diagnostic output is
// discarded; only migrate's stderr is forwarded.
func SyncShards(readme, itemsDir string, stderr io.Writer) error {
	code := tdmigrate.Run([]string{"migrate", "--readme", readme, "--items", itemsDir}, io.Discard, stderr)
	if code != 0 {
		return fmt.Errorf("shard sync failed: td-migrate migrate exited %d", code)
	}
	return nil
}
