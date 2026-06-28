package tdmigrate

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// GenerateTable renders shards back into the README technical-debt ToC table.
// Sections are emitted newest-date-first (then by label) for a stable, human-
// readable ordering; the per-item data is preserved verbatim so a parse of the
// output is semantically equal to the parse of the original (AC2 round-trip).
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

// droppedNotesWarnings is implemented in the GREEN step.
func droppedNotesWarnings(shards []Shard) []string {
	return nil
}

func sectionHasReviewers(s Shard) bool {
	for _, it := range s.Items {
		if len(it.Reviewers) > 0 || it.Confidence != "" {
			return true
		}
	}
	return false
}
