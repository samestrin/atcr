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

// unknownHeader catches any remaining `### [date] From ...` shape that did not
// match sectionHeader or driftHeader (e.g., a colonless provenance line). This
// prevents items beneath it from being silently re-attributed to the previous
// shard.
var unknownHeader = regexp.MustCompile(`^### \[\d{4}-\d{2}-\d{2}\] From `)

// ParseREADME parses the technical-debt README table into per-source shards.
// Anything before the first section header (title, Stats table, How-to-Use) is
// ignored. A data row that does not split into exactly 9 or 11 cells, or whose
// checkbox/est_minutes cannot be parsed, is a hard error (zero-data-loss: a
// malformed row must fail loudly, never be silently dropped).
func ParseREADME(content string) ([]Shard, error) {
	var shards []Shard
	var cur *Shard
	var curLine int

	flush := func() error {
		if cur == nil {
			return nil
		}
		if len(cur.Items) == 0 {
			return fmt.Errorf("line %d: section %q has no parseable items", curLine, cur.Label)
		}
		shards = append(shards, *cur)
		return nil
	}

	for n, line := range strings.Split(content, "\n") {
		if m := sectionHeader.FindStringSubmatch(line); m != nil {
			if err := flush(); err != nil {
				return nil, err
			}
			curLine = n + 1
			cur = &Shard{Date: m[1], SourceType: m[2], Label: strings.TrimSpace(m[3])}
			continue
		}
		if dm := driftHeader.FindStringSubmatch(line); dm != nil {
			return nil, fmt.Errorf("line %d: unrecognized section source type %q (want Sprint|Review): %q",
				n+1, strings.TrimSpace(dm[1]), strings.TrimSpace(line))
		}
		if unknownHeader.MatchString(line) {
			return nil, fmt.Errorf("line %d: malformed section header (missing Sprint|Review label?): %q",
				n+1, strings.TrimSpace(line))
		}
		if cur == nil || !strings.HasPrefix(strings.TrimSpace(line), "|") {
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
// Literal | inside cells is prevented at generate-time by cell() in generate.go,
// so this simple split is sufficient for all toolchain-produced rows.
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
