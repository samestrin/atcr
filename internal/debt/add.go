package debt

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/samestrin/atcr/internal/tdmigrate"
)

// Section identifies the `### [date] From <SourceType>: <label>` provenance
// block a new item is filed under in the authoritative README table.
type Section struct {
	Date       string // YYYY-MM-DD
	SourceType string // Sprint | Review
	Label      string
}

// validate schema-checks a Section, reusing the same enum the shard schema
// enforces so a bad source type fails before any file is touched.
func (s Section) validate() error {
	if strings.TrimSpace(s.Date) == "" {
		return fmt.Errorf("section date is required")
	}
	if s.SourceType != tdmigrate.SourceTypeSprint && s.SourceType != tdmigrate.SourceTypeReview {
		return fmt.Errorf("invalid section source_type %q (want Sprint|Review)", s.SourceType)
	}
	if strings.TrimSpace(s.Label) == "" {
		return fmt.Errorf("section label is required")
	}
	return nil
}

func (s Section) header() string {
	return fmt.Sprintf("### [%s] From %s: %s", s.Date, s.SourceType, s.Label)
}

// The 9-column header/separator emitted when creating a brand-new section. It
// matches the shape tdmigrate.ParseREADME skips (first cell "Group"); the
// checkbox column header is intentionally blank, as in the existing table.
const (
	newSectionHeaderRow = "| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |"
	newSectionSepRow    = "|-------|---|----------|------|---------|-----|----------|-------------|--------|"
)

// cellClean makes a value safe to embed in a Markdown table cell: newlines
// collapse to spaces and literal pipes become "/", mirroring the canonical
// TD-table contract used by tdmigrate.GenerateTable so the row round-trips.
func cellClean(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "/")
	return strings.TrimSpace(s)
}

// renderRow renders a validated item as a 9-cell Markdown table row.
func renderRow(it tdmigrate.Item) (string, error) {
	box, err := tdmigrate.StatusToCheckbox(it.Status)
	if err != nil {
		return "", err
	}
	cells := []string{
		cellClean(it.Group),
		box,
		it.Severity,
		cellClean(it.File),
		cellClean(it.Problem),
		cellClean(it.Fix),
		cellClean(it.Category),
		strconv.Itoa(it.EstMinutes),
		cellClean(it.Source),
	}
	return "| " + strings.Join(cells, " | ") + " |", nil
}

// insertRow returns content with a new item row filed under sec. If a section
// with sec's exact header already exists, the row is appended after that
// section's last table row; otherwise a new section (header + table header +
// separator + row) is appended at the end. The item and section are validated
// first so an invalid write fails loudly and never corrupts the table.
func insertRow(content string, sec Section, it tdmigrate.Item) (string, error) {
	if err := sec.validate(); err != nil {
		return "", err
	}
	if err := it.Validate(); err != nil {
		return "", err
	}
	row, err := renderRow(it)
	if err != nil {
		return "", err
	}

	lines := strings.Split(content, "\n")
	hdr := sec.header()

	secIdx := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) == hdr {
			secIdx = i
			break
		}
	}

	if secIdx < 0 {
		return appendNewSection(content, hdr, row), nil
	}

	// Find the last table (pipe) line within this section: from just after the
	// header until the next `### ` header or EOF.
	lastPipe := -1
	for i := secIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "### ") {
			break
		}
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
			lastPipe = i
		}
	}

	if lastPipe < 0 {
		// Section header with no table beneath it (unusual, e.g. a stub). Rebuild
		// the block: header row + separator + the new row, inserted right after
		// the section header line.
		block := []string{newSectionHeaderRow, newSectionSepRow, row}
		lines = insertAt(lines, secIdx+1, block)
		return strings.Join(lines, "\n"), nil
	}

	lines = insertAt(lines, lastPipe+1, []string{row})
	return strings.Join(lines, "\n"), nil
}

// insertAt returns lines with block spliced in at index i.
func insertAt(lines []string, i int, block []string) []string {
	out := make([]string, 0, len(lines)+len(block))
	out = append(out, lines[:i]...)
	out = append(out, block...)
	out = append(out, lines[i:]...)
	return out
}

// appendNewSection appends a fresh section to content, guaranteeing exactly one
// blank line before the new header regardless of the source's trailing
// whitespace.
func appendNewSection(content, hdr, row string) string {
	trimmed := strings.TrimRight(content, "\n")
	var b strings.Builder
	b.WriteString(trimmed)
	b.WriteString("\n\n")
	b.WriteString(hdr)
	b.WriteString("\n\n")
	b.WriteString(newSectionHeaderRow)
	b.WriteString("\n")
	b.WriteString(newSectionSepRow)
	b.WriteString("\n")
	b.WriteString(row)
	b.WriteString("\n")
	return b.String()
}

// AppendItem files a new technical-debt item into the authoritative README
// table (the write-master) under sec, then regenerates the shard store from the
// updated README so the item is immediately visible to the shard-reading
// commands. It never writes a shard directly — a shard-only write would be
// destroyed by the next migrate.
func AppendItem(readmePath, itemsDir string, sec Section, it tdmigrate.Item, stderr io.Writer) error {
	data, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("read README: %w", err)
	}
	updated, err := insertRow(string(data), sec, it)
	if err != nil {
		return err
	}
	if err := os.WriteFile(readmePath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write README: %w", err)
	}
	if err := SyncShards(readmePath, itemsDir, stderr); err != nil {
		return fmt.Errorf("regenerate shards after add: %w", err)
	}
	return nil
}
