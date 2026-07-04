package debt

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

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

// renderRow renders a validated item as a 9-cell Markdown table row.
func renderRow(it tdmigrate.Item) (string, error) {
	box, err := tdmigrate.StatusToCheckbox(it.Status)
	if err != nil {
		return "", err
	}
	cells := []string{
		sanitizeCell(it.Group),
		box,
		it.Severity,
		sanitizeCell(it.File),
		sanitizeCell(it.Problem),
		sanitizeCell(it.Fix),
		sanitizeCell(it.Category),
		strconv.Itoa(it.EstMinutes),
		sanitizeCell(it.Source),
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
	// header until the next heading of ANY level (`#`, `##`, `###`) or EOF. Using
	// any `#`-prefixed line as the boundary (not just `### `) means the scan can
	// never cross into an unrelated following section's table if a dated section
	// is not the trailing block.
	lastPipe := -1
	for i := secIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
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
//
// AppendItem acquires the shared README lock used by the TD tooling so
// concurrent writers (other atcr debt add processes, /resolve-td sessions,
// group_td, etc.) serialize their read-modify-write cycles and never clobber
// each other's rows.
func AppendItem(readmePath, itemsDir string, sec Section, it tdmigrate.Item, stderr io.Writer) error {
	repoRoot, err := repoRootFromReadme(readmePath)
	if err != nil {
		return fmt.Errorf("resolve repo root for README lock: %w", err)
	}
	return withReadmeLock(repoRoot, "atcr-debt-add", func() error {
		return appendItemUnlocked(readmePath, itemsDir, sec, it, stderr)
	})
}

func appendItemUnlocked(readmePath, itemsDir string, sec Section, it tdmigrate.Item, stderr io.Writer) error {
	data, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("read README: %w", err)
	}
	updated, err := insertRow(string(data), sec, it)
	if err != nil {
		return err
	}
	// Validate that the mutated README still parses cleanly BEFORE touching disk.
	// The subsequent SyncShards re-migrates the whole README, so if any
	// pre-existing drift elsewhere would make migrate fail, catch it here and
	// abort with both README and shards untouched.
	if _, err := tdmigrate.ParseREADME(updated); err != nil {
		return fmt.Errorf("refusing to write README: updated table does not parse cleanly (pre-existing drift elsewhere in the README?): %w", err)
	}
	if err := os.WriteFile(readmePath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write README: %w", err)
	}
	// After a successful README write, a failure in SyncShards or RefreshStats
	// would leave the authoritative README ahead of the derived stores. Roll
	// back to the original bytes on a best-effort basis so AppendItem remains
	// atomic: either the row is committed and all derived stores are refreshed,
	// or the README is restored to its pre-call state.
	if err := SyncShards(readmePath, itemsDir, stderr); err != nil {
		_ = os.WriteFile(readmePath, data, 0o644) // best-effort rollback
		return fmt.Errorf("regenerate shards after add: %w", err)
	}
	// Keep the authoritative README self-consistent: refresh its Stats table and
	// Last Modified summary so they agree with the row just appended. A README
	// without a Stats block is left untouched.
	if err := RefreshStats(readmePath, sec.Date); err != nil {
		_ = os.WriteFile(readmePath, data, 0o644) // best-effort rollback
		return fmt.Errorf("refresh README stats after add: %w", err)
	}
	return nil
}

// RefreshStats recomputes the README's `## Stats` table and `**Last Modified:**`
// summary line from the items currently in the table, so they stay consistent
// after an add. modDate stamps the Last Modified date. A README that has no
// Stats block (or no Last Modified line) is left untouched — this is a
// best-effort refresh of an existing summary, not an inserter.
//
// The Stats table and Last Modified line are replaced independently so that any
// notes or sections between them are preserved.
func RefreshStats(readmePath, modDate string) error {
	data, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("read README: %w", err)
	}
	content := string(data)

	statsIdx := strings.Index(content, "## Stats")
	lmIdx := strings.Index(content, "**Last Modified:**")
	if statsIdx < 0 || lmIdx < 0 || lmIdx < statsIdx {
		return nil // no recognizable stats block to refresh
	}

	lines := strings.Split(content, "\n")
	statsLine := -1
	lmLine := -1
	for i, line := range lines {
		if statsLine < 0 && strings.HasPrefix(line, "## Stats") {
			statsLine = i
		}
		if strings.HasPrefix(line, "**Last Modified:**") {
			lmLine = i
		}
	}
	if statsLine < 0 || lmLine < 0 || lmLine <= statsLine {
		return nil
	}

	// The Stats table ends at the first blank line between it and the Last
	// Modified line (or immediately before the Last Modified line if none).
	statsEnd := lmLine
	for i := statsLine + 2; i < lmLine; i++ {
		if strings.TrimSpace(lines[i]) == "" {
			statsEnd = i
			break
		}
	}

	shards, err := tdmigrate.ParseREADME(content)
	if err != nil {
		return fmt.Errorf("parse README for stats: %w", err)
	}
	sum := Summarize(Flatten(shards), time.Time{}, 0)

	outLines := make([]string, 0, len(lines))
	outLines = append(outLines, lines[:statsLine]...)
	outLines = append(outLines, renderStatsTable(sum))
	outLines = append(outLines, lines[statsEnd:lmLine]...)
	outLines = append(outLines, renderLastModified(sum, modDate))
	outLines = append(outLines, lines[lmLine+1:]...)

	updated := strings.Join(outLines, "\n")
	if _, err := tdmigrate.ParseREADME(updated); err != nil {
		return fmt.Errorf("refusing to write README: refreshed stats would corrupt the table: %w", err)
	}
	if err := os.WriteFile(readmePath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write README stats: %w", err)
	}
	return nil
}

// renderStatsBlock renders the `## Stats` table and the Last Modified summary
// line (no trailing newline) from an aggregated Summary.
func renderStatsBlock(sum Summary, modDate string) string {
	return renderStatsTable(sum) + "\n" + renderLastModified(sum, modDate)
}

// renderStatsTable renders just the `## Stats` Markdown table (including a
// trailing newline after the last row).
func renderStatsTable(sum Summary) string {
	var b strings.Builder
	b.WriteString("## Stats\n\n")
	b.WriteString("| Severity | Open | Deferred | Resolved |\n")
	b.WriteString("|----------|------|----------|----------|\n")
	for _, c := range sum.BySeverity {
		fmt.Fprintf(&b, "| %s | %d | %d | %d |\n", c.Severity, c.Open, c.Deferred, c.Resolved)
	}
	return b.String()
}

// renderLastModified renders the `**Last Modified:**` summary line without a
// trailing newline.
func renderLastModified(sum Summary, modDate string) string {
	return fmt.Sprintf("**Last Modified:** %s | **Open Items:** %d | **Deferred Items:** %d | **Resolved Items:** %d | **Total Items:** %d",
		modDate, sum.Open, sum.Deferred, sum.Resolved, sum.Total)
}
