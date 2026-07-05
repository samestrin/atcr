package tdmigrate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// sectionHeader matches `### [YYYY-MM-DD] From <Sprint|Review>: <label>`.
var sectionHeader = regexp.MustCompile(`^### \[(\d{4}-\d{2}-\d{2})\] From (Sprint|Review): (.+)$`)

// driftHeader matches a dated `From <type>:` header whose type is NOT a
// recognized Sprint|Review variant. Such a header would otherwise be ignored and
// every item beneath it silently dropped, so it is treated as a hard error
// (loud-failure mandate) rather than silently skipped.
var driftHeader = regexp.MustCompile(`^### \[\d{4}-\d{2}-\d{2}\] From ([^:]+):`)

// malformedHeader matches any dated section header line that neither
// sectionHeader nor driftHeader recognized — e.g. one missing the colon after
// the source type entirely, or with a badly formed date. Without this catch-all,
// such a line is silently skipped (not recognized as a section boundary), and
// any data rows beneath it get mis-attributed to whichever shard was previously
// open, or dropped entirely if none was.
var malformedHeader = regexp.MustCompile(`^### \[`)

// ParseREADME parses the technical-debt README table into per-source shards.
// Anything before the first section header (title, Stats table, How-to-Use) is
// ignored. A data row that does not split into exactly 9 or 11 cells, or whose
// checkbox/est_minutes cannot be parsed, is a hard error (zero-data-loss: a
// malformed row must fail loudly, never be silently dropped). A malformed
// section header (bad date, or missing the colon after the source type) is
// likewise a hard error rather than being silently skipped.
func ParseREADME(content string) ([]Shard, error) {
	var shards []Shard
	var cur *Shard

	// flush closes out the currently open section. A section with zero
	// parseable data rows is a hard error rather than being silently dropped:
	// an empty section usually means every row beneath it failed to parse as
	// expected (e.g. a wrong header/table shape), which would otherwise look
	// identical to "this section legitimately has no items."
	flush := func() error {
		if cur != nil {
			if len(cur.Items) == 0 {
				return fmt.Errorf("section %q (%s) has zero parseable data rows", cur.Label, cur.Date)
			}
			shards = append(shards, *cur)
		}
		cur = nil
		return nil
	}

	for n, line := range strings.Split(content, "\n") {
		if m := sectionHeader.FindStringSubmatch(line); m != nil {
			if err := flush(); err != nil {
				return nil, fmt.Errorf("line %d: %w", n+1, err)
			}
			cur = &Shard{Date: m[1], SourceType: m[2], Label: strings.TrimSpace(m[3])}
			continue
		}
		if dm := driftHeader.FindStringSubmatch(line); dm != nil {
			return nil, fmt.Errorf("line %d: unrecognized section source type %q (want Sprint|Review): %q",
				n+1, strings.TrimSpace(dm[1]), strings.TrimSpace(line))
		}
		if malformedHeader.MatchString(line) {
			return nil, fmt.Errorf("line %d: malformed section header (want `### [YYYY-MM-DD] From Sprint|Review: <label>`): %q",
				n+1, strings.TrimSpace(line))
		}
		if cur == nil {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			// Not a table row — but a row that lost its leading pipe (e.g. a
			// copy-paste error) still has the right interior pipe-separated cell
			// count and a trailing pipe. Catch that case loudly instead of
			// silently treating it as prose.
			if strings.HasSuffix(trimmed, "|") {
				if cells := splitRow(trimmed); (len(cells) == 9 || len(cells) == 11) &&
					!isHeaderRow(cells) && !isSeparatorRow(cells) {
					return nil, fmt.Errorf("line %d: data row is missing its leading pipe: %q", n+1, trimmed)
				}
			}
			continue
		}
		cells := splitRow(line)
		if isHeaderRow(cells) || isSeparatorRow(cells) {
			continue
		}
		it, err := rowToItem(cells)
		if err != nil {
			return nil, fmt.Errorf("line %d (%q): %w", n+1, strings.TrimSpace(line), err)
		}
		cur.Items = append(cur.Items, it)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return shards, nil
}

// splitRow strips the outer pipes and trims each cell of a Markdown table row.
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func isHeaderRow(cells []string) bool {
	return len(cells) > 0 && cells[0] == "Group"
}

var sepCell = regexp.MustCompile(`^:?-+:?$`)

func isSeparatorRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if c != "" && !sepCell.MatchString(c) {
			return false
		}
	}
	return true
}

func rowToItem(cells []string) (Item, error) {
	if len(cells) != 9 && len(cells) != 11 {
		return Item{}, fmt.Errorf("expected 9 or 11 cells, got %d", len(cells))
	}
	status, err := CheckboxToStatus(cells[1])
	if err != nil {
		return Item{}, err
	}
	est := 0
	if e := strings.TrimSpace(cells[7]); e != "" {
		v, err := strconv.Atoi(e)
		if err != nil {
			return Item{}, fmt.Errorf("est_minutes %q is not an integer", cells[7])
		}
		est = v
	}
	it := Item{
		Group:      cells[0],
		Status:     status,
		Severity:   NormalizeSeverity(cells[2]),
		File:       cells[3],
		Problem:    cells[4],
		Fix:        cells[5],
		Category:   cells[6],
		EstMinutes: est,
		Source:     cells[8],
	}
	if len(cells) == 11 {
		it.Reviewers = splitReviewers(cells[9])
		it.Confidence = cells[10]
	}
	return it, nil
}

// splitReviewers parses a comma-separated reviewer cell into a trimmed slice,
// returning nil (not an empty slice) when the cell is empty so that round-trips
// against 9-column sections compare equal.
func splitReviewers(cell string) []string {
	var out []string
	for _, r := range strings.Split(cell, ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}
