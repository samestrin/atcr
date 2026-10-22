package localdebt

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

// tdsync.go reads a technical-debt README table for `atcr debt sync-outcomes`
// (Epic 35.16.11.3.2 T4). Seats close debt by ticking a row in that table, and
// until this existed nothing carried the outcome back to the ledger, so every
// record stayed open and the scorecard's confirmation half had nothing to count.
//
// The join is EXPLICIT and nothing else: a row is matched to a ledger record only
// when it carries a `debt:<16-hex id>` token in its Problem or Source cell. Rows
// written before the convention carry no id, and matching them by file and
// problem text was measured on 2026-10-22 to join 0 of 374 closed rows, so this
// parser never tries.

// TDRow is one item row of a technical-debt table.
type TDRow struct {
	// Line is the 1-based line of the row in the README.
	Line int
	// Section is the nearest preceding `###` heading, verbatim ("" when none).
	Section string
	// Marker is the row's checkbox cell, e.g. "[x]".
	Marker string
	// IDs are the distinct, lowercased ids of every well-formed debt:<id> token
	// in the Problem and Source cells, in reading order.
	IDs []string
	// BadTokens are debt: tokens whose id is not exactly 16 hex characters. A row
	// carrying one is never matched, since the id it meant cannot be known.
	BadTokens []string
	// Problem is the row's Problem cell, trimmed (escaped pipes left as written).
	Problem string
}

// TDMarkerStatus maps a TD checkbox marker to the ledger status it records.
//
// The mapping follows the marker set `llm-support td-stats` / `td-filter` read,
// which is the TD README's legend:
//
//	[ ]       open           -> not an outcome
//	[/]       deferred       -> not an outcome (deferred is not counted, and is
//	                            written by other paths, not by a TD sync)
//	[x] [X]   resolved       -> StatusResolved
//	[-]       unreproducible -> StatusUnreproducible ("closed without a fix")
//
// The legend has no marker for wontfix or attempts-exhausted, so a sync never
// writes either: record those with `atcr debt resolve <id> --status ...`, which
// also takes the typed --reason both require. Any other marker is unknown.
//
// terminal reports whether the marker is a counted outcome the sync records;
// known reports whether the marker is in the legend at all.
func TDMarkerStatus(marker string) (status string, terminal, known bool) {
	switch marker {
	case "[x]", "[X]":
		return StatusResolved, true, true
	case "[-]":
		return StatusUnreproducible, true, true
	case "[ ]":
		return "", false, true
	case "[/]":
		return StatusDeferred, false, true
	default:
		return "", false, false
	}
}

var (
	tdMarkerCell = regexp.MustCompile(`^\[.\]$`)
	tdToken      = regexp.MustCompile(`debt:[0-9A-Za-z]*`)
)

// ParseTDRows reads every item row of every TD table in r.
//
// A table's column positions come from its header row: the Problem and Source
// cells are found by name, so a table with extra columns is read correctly. A
// table whose header has no Problem column (the Stats table) is not an item
// table and yields no rows. A headerless table is read in the canonical TD
// layout `| Group | [ ] | Severity | File | Problem | Fix | Category | Est |
// Source | Attempts |`, counted from the checkbox cell.
func ParseTDRows(r io.Reader) ([]TDRow, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), MaxRecordBytes)

	var (
		rows    []TDRow
		section string
		inTable bool
		header  bool // the current table has a header row
		probIdx = -1
		srcIdx  = -1
		lineNo  int
	)
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "|") {
			inTable, header, probIdx, srcIdx = false, false, -1, -1
			if strings.HasPrefix(line, "### ") {
				section = line
			}
			continue
		}
		cells := splitTDCells(line)
		first := !inTable
		inTable = true

		box := -1
		for i, c := range cells {
			if tdMarkerCell.MatchString(c) {
				box = i
				break
			}
		}
		if box < 0 {
			if first {
				header = true
				for i, c := range cells {
					switch strings.ToLower(c) {
					case "problem":
						probIdx = i
					case "source":
						srcIdx = i
					}
				}
			}
			continue // header or separator row
		}
		p, s := probIdx, srcIdx
		if !header {
			p, s = box+3, box+7
		} else if probIdx < 0 {
			continue // a checkbox in a non-item table (Stats)
		}
		row := TDRow{Line: lineNo, Section: section, Marker: cells[box]}
		if p < len(cells) {
			row.Problem = cells[p]
		}
		seen := map[string]bool{}
		for _, idx := range []int{p, s} {
			if idx < 0 || idx >= len(cells) {
				continue
			}
			for _, loc := range tdToken.FindAllStringIndex(cells[idx], -1) {
				// A token glued to a preceding word ("xdebt:...") is not a token.
				if loc[0] > 0 && isTokenChar(cells[idx][loc[0]-1]) {
					continue
				}
				tok := cells[idx][loc[0]:loc[1]]
				id := strings.ToLower(strings.TrimPrefix(tok, "debt:"))
				if len(id) != 16 || strings.Trim(id, "0123456789abcdef") != "" {
					row.BadTokens = append(row.BadTokens, tok)
					continue
				}
				if !seen[id] {
					seen[id] = true
					row.IDs = append(row.IDs, id)
				}
			}
		}
		rows = append(rows, row)
	}
	return rows, sc.Err()
}

// splitTDCells splits a markdown table row into trimmed cells, honoring `\|` as
// a literal pipe inside a cell. The leading and trailing pipes bound the row and
// produce no cell.
func splitTDCells(line string) []string {
	line = strings.TrimPrefix(line, "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}
	var cells []string
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' && i+1 < len(line) && line[i+1] == '|' {
			b.WriteString(`\|`)
			i++
			continue
		}
		if line[i] == '|' {
			cells = append(cells, strings.TrimSpace(b.String()))
			b.Reset()
			continue
		}
		b.WriteByte(line[i])
	}
	return append(cells, strings.TrimSpace(b.String()))
}

func isTokenChar(c byte) bool {
	return c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
