package tdmigrate

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// GenerateTable renders shards back into the README technical-debt ToC table.
// Sections are emitted newest-date-first (then by label) for a stable, human-
// readable ordering; shard-level data is preserved, but per-item table cells are
// intentionally lossy single-line summaries (e.g., pipe → slash, newline → space)
// so the regenerated table stays structurally valid. Round-trip equality holds at
// the shard layer (table → shards → table), not at the verbatim table-cell level;
// SCHEMA.md documents the canonical shard schema.
//
// A section that carries any reviewer/confidence data is rendered with the
// 11-column reconciled layout; otherwise the 9-column layout is used — matching
// how the original table distinguishes reconciled from plain sections.
func GenerateTable(shards []Shard) (string, error) {
	ordered := make([]Shard, len(shards))
	copy(ordered, shards)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Date != ordered[j].Date {
			return ordered[i].Date > ordered[j].Date // newest first
		}
		return ordered[i].Label < ordered[j].Label
	})

	var b strings.Builder
	for _, s := range ordered {
		fmt.Fprintf(&b, "### [%s] From %s: %s\n\n", s.Date, s.SourceType, s.Label)
		wide := sectionHasReviewers(s)
		if wide {
			b.WriteString("| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |\n")
			b.WriteString("|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|\n")
		} else {
			b.WriteString("| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |\n")
			b.WriteString("|-------|---|----------|------|---------|-----|----------|-------------|--------|\n")
		}
		for _, it := range s.Items {
			box, err := StatusToCheckbox(it.Status)
			if err != nil {
				return "", fmt.Errorf("shard %s/%s: %w", s.Date, s.Label, err)
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |",
				cell(it.Group), box, cell(it.Severity), cell(it.File), cell(it.Problem), cell(it.Fix),
				cell(it.Category), strconv.Itoa(it.EstMinutes), cell(it.Source))
			if wide {
				fmt.Fprintf(&b, " %s | %s |", cell(strings.Join(it.Reviewers, ", ")), cell(it.Confidence))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// cell makes a value safe to place in a single Markdown table cell. The TD table
// (a one-line-per-item ToC summary) cannot represent a literal `|` or a newline,
// so — matching the canonical TD-table contract used elsewhere in the repo — a
// pipe is replaced with `/` and newlines are collapsed to spaces. This keeps the
// regenerated table structurally valid (no phantom columns) rather than silently
// corrupting it. The shards under items/ remain the lossless source of truth;
// only the generated ToC summary is single-lined.
func cell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "|", "/")
	return strings.TrimSpace(s)
}

func sectionHasReviewers(s Shard) bool {
	for _, it := range s.Items {
		if len(it.Reviewers) > 0 || it.Confidence != "" {
			return true
		}
	}
	return false
}
