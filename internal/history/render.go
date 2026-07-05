package history

import (
	"fmt"
	"sort"
	"strings"
)

// canonicalSeverities is the fixed, high-to-low severity column order, matching
// reconcile.SeverityRank. These columns are always rendered (even when zero) so
// the table shape is stable across queries; any non-canonical severity present
// in the data is appended as an extra column after these.
var canonicalSeverities = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}

// normalizeSeverity upper-cases and trims a severity label so counting is
// case-insensitive (mirrors reconcile.NormalizeSeverity).
func normalizeSeverity(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

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

	var b strings.Builder

	// Header row.
	b.WriteString("| Package |")
	for _, c := range columns {
		fmt.Fprintf(&b, " %s |", c)
	}
	b.WriteString(" Total |\n")

	// Alignment row: package left, counts right-aligned.
	b.WriteString("|---------|")
	for range columns {
		b.WriteString("------:|")
	}
	b.WriteString("------:|\n")

	// One row per package.
	grand := map[string]int{}
	grandTotal := 0
	for _, p := range packages {
		fmt.Fprintf(&b, "| %s |", p)
		rowTotal := 0
		for _, c := range columns {
			n := counts[p][c]
			fmt.Fprintf(&b, " %d |", n)
			rowTotal += n
			grand[c] += n
		}
		fmt.Fprintf(&b, " %d |\n", rowTotal)
		grandTotal += rowTotal
	}

	// Grand-total row.
	b.WriteString("| **Total** |")
	for _, c := range columns {
		fmt.Fprintf(&b, " %d |", grand[c])
	}
	fmt.Fprintf(&b, " %d |\n", grandTotal)

	return b.String()
}
