package audit

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// canonicalSeverities is the fixed, high-to-low severity column order, matching
// reconcile.SeverityRank. These columns are always rendered (even when zero) so
// the report shape is stable across PRs; any non-canonical severity present in
// the data is appended as an extra column after these.
var canonicalSeverities = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}

// normalizeSeverity upper-cases and trims a severity label so counting is
// case-insensitive (mirrors reconcile.NormalizeSeverity).
func normalizeSeverity(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// sanitizeCell makes an untrusted string safe to embed in a markdown table cell:
// a literal pipe becomes "\|" (otherwise it opens a spurious column) and any
// control character (newline, carriage return, tab, ...) becomes a space
// (otherwise it splits or mangles the row). Base/head SHAs are git-derived and
// normally safe, but the ledger is an on-disk artifact that could be tampered
// with, so cells are never written raw (mirrors internal/history.sanitizeCell).
func sanitizeCell(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
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

// shortSHA abbreviates a full SHA to 12 chars for a readable one-page report.
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// RenderReport renders a one-page markdown compliance report for the review runs
// recorded against PR pr. Each record is one run: a table row with the run
// timestamp (UTC, RFC3339), truncated base/head SHAs, and finding counts by
// severity, followed by a grand-total row. Runs are ordered oldest-first so the
// report reads as a chronological audit trail; generatedAt stamps the header.
//
// An empty record set renders a header plus a "no runs" note, but the command
// handles the no-records case before calling this (it is an error condition per
// Epic 19.1 AC3), so in practice recs is non-empty.
func RenderReport(recs []Record, pr int, generatedAt time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Audit Report — PR #%d\n\n", pr)
	fmt.Fprintf(&b, "_Generated %s_\n\n", generatedAt.UTC().Format(time.RFC3339))

	if len(recs) == 0 {
		b.WriteString("No review runs recorded for this PR.\n")
		return b.String()
	}

	// Sort a copy oldest-first so the report is chronological and deterministic
	// regardless of ledger order (the ledger is append order, but two concurrent
	// runs can interleave).
	sorted := make([]Record, len(recs))
	copy(sorted, recs)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	// Columns: the four canonical severities always, plus any extra severities
	// present in the data (sorted), matching internal/history.RenderTable.
	canonical := map[string]bool{}
	for _, s := range canonicalSeverities {
		canonical[s] = true
	}
	extraSet := map[string]bool{}
	for _, r := range sorted {
		for sev := range r.Findings {
			n := normalizeSeverity(sev)
			if n != "" && !canonical[n] {
				extraSet[n] = true
			}
		}
	}
	extras := make([]string, 0, len(extraSet))
	for s := range extraSet {
		extras = append(extras, s)
	}
	sort.Strings(extras)
	columns := append(append([]string{}, canonicalSeverities...), extras...)

	fmt.Fprintf(&b, "%d review run(s) recorded for PR #%d.\n\n", len(sorted), pr)

	// Header row.
	b.WriteString("| Run (UTC) | Base | Head |")
	for _, c := range columns {
		fmt.Fprintf(&b, " %s |", sanitizeCell(c))
	}
	b.WriteString(" Total |\n")

	// Alignment row: text columns left, counts right-aligned.
	b.WriteString("|-----------|------|------|")
	for range columns {
		b.WriteString("------:|")
	}
	b.WriteString("------:|\n")

	// One row per run, plus a grand-total accumulator.
	grand := map[string]int{}
	grandTotal := 0
	for _, r := range sorted {
		// Normalize this run's findings into canonical-keyed counts so column
		// lookups are case-insensitive.
		counts := map[string]int{}
		for sev, n := range r.Findings {
			counts[normalizeSeverity(sev)] += n
		}
		fmt.Fprintf(&b, "| %s | %s | %s |",
			sanitizeCell(r.Timestamp.UTC().Format(time.RFC3339)),
			sanitizeCell(shortSHA(r.Base)),
			sanitizeCell(shortSHA(r.Head)))
		rowTotal := 0
		for _, c := range columns {
			n := counts[c]
			fmt.Fprintf(&b, " %d |", n)
			rowTotal += n
			grand[c] += n
		}
		fmt.Fprintf(&b, " %d |\n", rowTotal)
		grandTotal += rowTotal
	}

	// Grand-total row.
	b.WriteString("| **Total** | | |")
	for _, c := range columns {
		fmt.Fprintf(&b, " %d |", grand[c])
	}
	fmt.Fprintf(&b, " %d |\n", grandTotal)

	return b.String()
}
