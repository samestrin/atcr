package history

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// canonicalSeverities is the fixed, high-to-low severity column order, matching
// reconcile.SeverityRank. These columns are always rendered (even when zero) so
// the table shape is stable across queries; any non-canonical severity present
// in the data is appended as an extra column after these.
var canonicalSeverities = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}

// normalizeSeverity upper-cases and trims a severity label so counting is
// case-insensitive (mirrors reconcile.NormalizeSeverity).
func normalizeSeverity(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// sanitizeCell makes an untrusted string safe to embed in a markdown table cell.
// A literal pipe is escaped to "\|" (otherwise it opens a spurious column) and
// any control character — newline, carriage return, tab, and the rest — becomes
// a space (otherwise it splits or mangles the row). Package names derive from
// finding file paths, which on POSIX may legally contain a pipe or newline, and
// severity labels are reviewer/model-generated free text, so neither may be
// written raw.
func sanitizeCell(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '|':
			b.WriteString(`\|`)
		case unicode.IsControl(r):
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Table is the struct behind RenderTable: finding counts by severity per
// package. The markdown table and `atcr history --json` are both rendered from
// it, so the two can never disagree about a count.
type Table struct {
	// Columns is the severity column order: the four canonical severities
	// (always present) followed by any other severities in the data, sorted.
	Columns []string `json:"columns"`
	// Rows holds one row per package, sorted by package. Never nil.
	Rows []TableRow `json:"rows"`
	// Totals is the grand-total count per column (every column present).
	Totals map[string]int `json:"totals"`
	// Total is the grand total across every column.
	Total int `json:"total"`
}

// TableRow is one package's counts. Counts carries every column, zeros
// included, so each row has the same shape.
type TableRow struct {
	Package string         `json:"package"`
	Counts  map[string]int `json:"counts"`
	Total   int            `json:"total"`
}

// BuildTable counts recs by severity per package. An empty record set yields a
// table with the canonical columns and no rows. Severities are counted
// case-insensitively, and a blank one counts as UNKNOWN.
func BuildTable(recs []Record) Table {
	// counts[package][severity] = n
	counts := map[string]map[string]int{}
	extraSet := map[string]bool{}
	canonical := map[string]bool{}
	for _, s := range canonicalSeverities {
		canonical[s] = true
	}

	for _, r := range recs {
		sev := normalizeSeverity(r.Severity)
		if sev == "" {
			sev = "UNKNOWN"
		}
		if counts[r.Package] == nil {
			counts[r.Package] = map[string]int{}
		}
		counts[r.Package][sev]++
		if !canonical[sev] {
			extraSet[sev] = true
		}
	}

	extras := make([]string, 0, len(extraSet))
	for s := range extraSet {
		extras = append(extras, s)
	}
	sort.Strings(extras)
	columns := append(append([]string{}, canonicalSeverities...), extras...)

	packages := make([]string, 0, len(counts))
	for p := range counts {
		packages = append(packages, p)
	}
	sort.Strings(packages)

	t := Table{Columns: columns, Rows: make([]TableRow, 0, len(packages)), Totals: map[string]int{}}
	for _, c := range columns {
		t.Totals[c] = 0
	}
	for _, p := range packages {
		row := TableRow{Package: p, Counts: make(map[string]int, len(columns))}
		for _, c := range columns {
			n := counts[p][c]
			row.Counts[c] = n
			row.Total += n
			t.Totals[c] += n
		}
		t.Total += row.Total
		t.Rows = append(t.Rows, row)
	}
	return t
}

// RenderTable renders a markdown table of finding counts by severity (columns)
// per package (rows), with a per-package Total column and a grand-Total row.
// Packages are sorted alphabetically. Columns are the four canonical severities
// (always shown) followed by any other severities present, sorted. An empty
// record set renders as the empty string — the caller decides how to phrase
// "no history".
func RenderTable(recs []Record) string {
	if len(recs) == 0 {
		return ""
	}
	t := BuildTable(recs)

	var b strings.Builder

	// Header row.
	b.WriteString("| Package |")
	for _, c := range t.Columns {
		fmt.Fprintf(&b, " %s |", sanitizeCell(c))
	}
	b.WriteString(" Total |\n")

	// Alignment row: package left, counts right-aligned.
	b.WriteString("|---------|")
	for range t.Columns {
		b.WriteString("------:|")
	}
	b.WriteString("------:|\n")

	// One row per package.
	for _, row := range t.Rows {
		fmt.Fprintf(&b, "| %s |", sanitizeCell(row.Package))
		for _, c := range t.Columns {
			fmt.Fprintf(&b, " %d |", row.Counts[c])
		}
		fmt.Fprintf(&b, " %d |\n", row.Total)
	}

	// Grand-total row.
	b.WriteString("| **Total** |")
	for _, c := range t.Columns {
		fmt.Fprintf(&b, " %d |", t.Totals[c])
	}
	fmt.Fprintf(&b, " %d |\n", t.Total)

	return b.String()
}
