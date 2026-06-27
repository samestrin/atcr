package tdmigrate

import (
	"fmt"
	"regexp"
	"strings"
)

var dateRe = regexp.MustCompile(`\[(\d{4}-\d{2}-\d{2})\]`)

// ParseReadme splits the technical-debt README into its static preamble
// (everything before the first dated "### [..." section) and the ordered list
// of Items parsed from every dated section's table. IDs are assigned
// sequentially (TD-0001, TD-0002, ...) in document order.
func ParseReadme(content string) (preamble string, items []Item, err error) {
	lines := strings.Split(content, "\n")

	firstSection := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "### [") {
			firstSection = i
			break
		}
	}
	if firstSection < 0 {
		return content, nil, nil
	}
	preamble = strings.Join(lines[:firstSection], "\n")

	order := 0
	i := firstSection
	for i < len(lines) {
		if !strings.HasPrefix(lines[i], "### [") {
			i++
			continue
		}
		section := strings.TrimPrefix(lines[i], "### ")
		date := ""
		if m := dateRe.FindStringSubmatch(section); m != nil {
			date = m[1]
		}
		i++

		// Skip blank lines between the header and the table.
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
		// A column-header row must follow; otherwise this section has no table.
		if i >= len(lines) || !isTableRow(lines[i]) {
			continue
		}
		headerCells := splitRow(lines[i])
		hasReview := len(headerCells) >= 11
		i++
		// Consume the separator row if present.
		if i < len(lines) && isSeparatorRow(lines[i]) {
			i++
		}
		// Data rows run until a blank line, the next section, or EOF.
		for i < len(lines) && isTableRow(lines[i]) && !isSeparatorRow(lines[i]) {
			cells := splitRow(lines[i])
			lineNo := i + 1
			i++
			if len(cells) < 9 {
				return "", nil, fmt.Errorf("line %d: malformed table row (%d columns): %q", lineNo, len(cells), strings.TrimSpace(lines[lineNo-1]))
			}
			status, serr := boxToStatus(cells[1])
			if serr != nil {
				return "", nil, fmt.Errorf("line %d: %w", lineNo, serr)
			}
			order++
			it := Item{
				ID:            fmt.Sprintf("TD-%04d", order),
				Order:         order,
				Section:       section,
				Date:          date,
				Group:         cells[0],
				Status:        status,
				Severity:      cells[2],
				File:          cells[3],
				Problem:       cells[4],
				Fix:           cells[5],
				Category:      cells[6],
				EstMinutes:    cells[7],
				Source:        cells[8],
				HasReviewCols: hasReview,
			}
			if hasReview && len(cells) >= 11 {
				it.Reviewers = cells[9]
				it.Confidence = cells[10]
			}
			items = append(items, it)
		}
	}
	return preamble, items, nil
}

const (
	headerRow9  = "| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |"
	sepRow9     = "|-------|---|----------|------|---------|-----|----------|-------------|--------|"
	headerRow11 = "| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |"
	sepRow11    = "|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|"
)

// RenderTable regenerates the dated section tables from items, grouped by their
// Section header in first-seen order. It is the inverse of the table-parsing
// half of ParseReadme and is used to prove round-trip fidelity (the
// items -> README "summary generator").
func RenderTable(items []Item) string {
	var order []string
	groups := map[string][]Item{}
	for _, it := range items {
		if _, ok := groups[it.Section]; !ok {
			order = append(order, it.Section)
		}
		groups[it.Section] = append(groups[it.Section], it)
	}

	var b strings.Builder
	for _, section := range order {
		its := groups[section]
		hasReview := its[0].HasReviewCols
		b.WriteString("### " + section + "\n\n")
		if hasReview {
			b.WriteString(headerRow11 + "\n" + sepRow11 + "\n")
		} else {
			b.WriteString(headerRow9 + "\n" + sepRow9 + "\n")
		}
		for _, it := range its {
			box, _ := statusToBox(it.Status)
			if hasReview {
				fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
					esc(it.Group), box, it.Severity, esc(it.File), esc(it.Problem), esc(it.Fix),
					it.Category, it.EstMinutes, esc(it.Source), esc(it.Reviewers), it.Confidence)
			} else {
				fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
					esc(it.Group), box, it.Severity, esc(it.File), esc(it.Problem), esc(it.Fix),
					it.Category, it.EstMinutes, esc(it.Source))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func isTableRow(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "|")
}

func isSeparatorRow(line string) bool {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "|") {
		return false
	}
	for _, r := range s {
		if r != '|' && r != '-' && r != ':' && r != ' ' {
			return false
		}
	}
	return true
}

// splitRow splits a Markdown table row into trimmed cells, honoring "\|" as a
// literal pipe inside a cell.
func splitRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")

	var cells []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == '|' {
			cur.WriteByte('|')
			i++
			continue
		}
		if s[i] == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(s[i])
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	return cells
}

// esc escapes pipe characters so a cell value cannot break the table grid.
func esc(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}
