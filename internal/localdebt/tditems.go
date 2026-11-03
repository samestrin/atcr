package localdebt

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// tditems.go reads every cell of a technical-debt README table, for `atcr debt
// import` (epic 95). ParseTDRows (tdsync.go) reads only what a sync needs: the
// marker, the debt:<id> tokens and the Problem cell. An import needs the whole row.
//
// Three item-table layouts are in use, all told apart by their header row:
//
//	9 columns   | Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
//	10 columns  ... | Source | Attempts |
//	11 columns  ... | Source | Reviewers | Confidence |
//
// ParseTDItems and ParseTDRows agree on which rows are items, so an import and a
// sync count from one definition.

// TDItem is one item row of a technical-debt table, every cell read.
type TDItem struct {
	// Line is the 1-based line of the row in the README.
	Line int
	// Section is the nearest preceding `###` heading, verbatim ("" when none).
	Section string
	// Marker is the row's checkbox cell, e.g. "[x]".
	Marker string

	// The cells below are trimmed, with each escaped pipe `\|` read as `|`. A
	// column the table does not have reads as the zero value.
	Group    string
	Severity string
	// File is the File cell without its `:line` suffix. A cell with no suffix,
	// or one whose text after the last colon is not a line number
	// ("registry.yaml:vera"), is all file, with FileLine 0.
	File string
	// FileLine is the line from the File cell's `:N` or `:N-M` suffix (the start
	// of a range), or 0 when it has none.
	FileLine   int
	Problem    string
	Fix        string
	Category   string
	EstMinutes int
	Source     string
	Attempts   int
	// Reviewers is the Reviewers cell split on commas, nil when empty.
	Reviewers  []string
	Confidence string

	// Invalid says why the row cannot be imported as written, "" when it can:
	// a cell count other than the header's (an unescaped pipe, usually, which
	// leaves every cell after it in the wrong column, so no cell is filled), or a
	// count cell (Est Minutes, Attempts) that is not a whole number >= 0.
	Invalid string
}

// tdCanonicalHeader is the layout a headerless table is read in, as ParseTDRows
// reads one.
var tdCanonicalHeader = []string{"group", "", "severity", "file", "problem", "fix", "category", "est minutes", "source", "attempts"}

// tdFileLine matches a File cell's `:N` or `:N-M` suffix.
var tdFileLine = regexp.MustCompile(`^(.+):([0-9]+)(?:-[0-9]+)?$`)

// ParseTDItems reads every item row of every TD table in r, with every cell.
//
// Columns are found by header name, so the three layouts and any column order
// read alike. A table whose header has no Problem column (the Stats table) is not
// an item table and yields no rows; a headerless table is read in the canonical
// 10-column layout. A malformed item row is returned with Invalid set, never
// dropped, so the result counts every item row in r.
func ParseTDItems(r io.Reader) ([]TDItem, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), MaxRecordBytes)

	var (
		items   []TDItem
		section string
		inTable bool
		header  []string // lowercased header cells; nil for a headerless table
		lineNo  int
	)
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "|") {
			inTable, header = false, nil
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
				header = make([]string, len(cells))
				for i, c := range cells {
					header[i] = strings.ToLower(c)
				}
			}
			continue // header or separator row
		}
		cols := header
		if cols == nil {
			cols = tdHeaderlessColumns(box, len(cells))
		} else if !slices.Contains(cols, "problem") {
			continue // a checkbox in a non-item table (Stats)
		}
		items = append(items, tdItemFromCells(lineNo, section, cells, box, cols))
	}
	return items, sc.Err()
}

// tdItemFromCells reads one item row against its table's column names.
func tdItemFromCells(lineNo int, section string, cells []string, box int, cols []string) TDItem {
	it := TDItem{Line: lineNo, Section: section, Marker: cells[box]}
	if len(cells) != len(cols) {
		it.Invalid = fmt.Sprintf("row has %d cells, its table has %d columns", len(cells), len(cols))
		return it
	}
	var bad []string
	count := func(col, v string) int {
		if v == "" {
			return 0
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			bad = append(bad, fmt.Sprintf("%s %q is not a whole number >= 0", col, v))
			return 0
		}
		return n
	}
	for i, name := range cols {
		v := strings.ReplaceAll(cells[i], `\|`, "|")
		switch name {
		case "group":
			it.Group = v
		case "severity":
			it.Severity = v
		case "file":
			it.File = v
			if m := tdFileLine.FindStringSubmatch(v); m != nil {
				if n, err := strconv.Atoi(m[2]); err == nil {
					it.File, it.FileLine = m[1], n
				}
			}
		case "problem":
			it.Problem = v
		case "fix":
			it.Fix = v
		case "category":
			it.Category = v
		case "est minutes", "est":
			it.EstMinutes = count("Est Minutes", v)
		case "source":
			it.Source = v
		case "attempts":
			it.Attempts = count("Attempts", v)
		case "reviewers":
			for _, r := range strings.Split(v, ",") {
				if r = strings.TrimSpace(r); r != "" {
					it.Reviewers = append(it.Reviewers, r)
				}
			}
		case "confidence":
			it.Confidence = v
		}
	}
	it.Invalid = strings.Join(bad, "; ")
	return it
}

// tdHeaderlessColumns names a headerless row's columns: the canonical layout,
// placed by the checkbox cell as ParseTDRows places it, with any cells before
// Group unnamed. A row one cell short of it is the canonical layout without
// Attempts (the 9-column layout).
func tdHeaderlessColumns(box, cells int) []string {
	canon := tdCanonicalHeader
	if box == 0 {
		canon = canon[1:] // no Group cell
	}
	cols := append(make([]string, max(box-1, 0)), canon...)
	if cells == len(cols)-1 {
		cols = cols[:len(cols)-1]
	}
	return cols
}
