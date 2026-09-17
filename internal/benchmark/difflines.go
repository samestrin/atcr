package benchmark

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// DiffLineMap classifies every line of a case's change.diff as added, removed, or
// untouched. It is what turns `outside_diff` from a label an author asserts into a
// measurement the scorer can check (FORMAT.md, "The matching rule", condition 3).
//
// Without it, a tolerance window that happens to brush the change would credit a
// reviewer who read nothing but added and removed lines with an out-of-diff
// finding — scoring the one metric this whole tier exists to produce, on exactly
// the behavior it exists to detect the absence of.
//
// The two sides are keyed in DIFFERENT coordinate spaces, and deliberately so:
// added lines by their HEAD line number, removed lines by their BASE line number.
// That is the only honest mapping, because a removed line has no head-side number
// at all. It also means condition 3 reduces to the added-line check alone — see
// IsAddedLine.
type DiffLineMap struct {
	added   map[string]map[int]bool
	removed map[string]map[int]bool
}

// hunkHeader matches the counts in `@@ -a,b +c,d @@`, where either count may be
// omitted (meaning 1). Parsed by hand rather than with a regexp so a malformed
// header produces a diagnostic naming the header, not a silent non-match.
type hunkHeader struct {
	baseStart int
	headStart int
}

// ParseDiffLineMap builds the line map for a unified diff.
//
// An EMPTY diff is not an error: it is a case that changes nothing, which
// MaterializeCase already refuses with a message naming the case. A parser that
// also rejected it would produce a worse diagnostic for the same defect.
func ParseDiffLineMap(diff []byte) (DiffLineMap, error) {
	m := DiffLineMap{
		added:   map[string]map[int]bool{},
		removed: map[string]map[int]bool{},
	}
	var headPath, basePath string
	var baseLine, headLine int
	inHunk := false

	sc := bufio.NewScanner(bytes.NewReader(diff))
	// A case's diff may carry a long minified or generated line; the default 64 KiB
	// token limit would fail the whole parse on one such line, which reads as a
	// malformed diff rather than a long one.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			// A new file section: forget the previous file's state so a stray body
			// line cannot be attributed to the wrong file.
			headPath, basePath, inHunk = "", "", false

		case strings.HasPrefix(line, "--- "):
			basePath = stripDiffPathPrefix(strings.TrimPrefix(line, "--- "))
			inHunk = false

		case strings.HasPrefix(line, "+++ "):
			headPath = stripDiffPathPrefix(strings.TrimPrefix(line, "+++ "))
			inHunk = false

		case strings.HasPrefix(line, "@@"):
			if basePath == "" && headPath == "" {
				return DiffLineMap{}, fmt.Errorf("diff has a hunk with no file header before it: %q", line)
			}
			h, err := parseHunkHeader(line)
			if err != nil {
				return DiffLineMap{}, err
			}
			baseLine, headLine = h.baseStart, h.headStart
			inHunk = true

		case strings.HasPrefix(line, `\`):
			// "\ No newline at end of file" is a MARKER, not content. Counting it
			// would shift every subsequent line in the hunk by one.

		case line == "":
			// A bare empty line inside a hunk is a context line whose single leading
			// space some tools strip. Outside a hunk it is padding. Treating it as
			// context inside a hunk keeps the counters aligned with the file.
			if inHunk {
				baseLine++
				headLine++
			}

		default:
			switch line[0] {
			case '+':
				if !inHunk {
					return DiffLineMap{}, fmt.Errorf("diff has a body line outside any hunk: %q", line)
				}
				addLine(m.added, headFileKey(headPath, basePath), headLine)
				headLine++
			case '-':
				if !inHunk {
					return DiffLineMap{}, fmt.Errorf("diff has a body line outside any hunk: %q", line)
				}
				addLine(m.removed, baseFileKey(headPath, basePath), baseLine)
				baseLine++
			case ' ':
				if inHunk {
					baseLine++
					headLine++
				}
			default:
				// index lines, mode lines, "new file mode", "similarity index",
				// and anything else in the extended header. Not content.
			}
		}
	}
	if err := sc.Err(); err != nil {
		return DiffLineMap{}, fmt.Errorf("reading diff: %w", err)
	}
	return m, nil
}

// parseHunkHeader reads `@@ -a[,b] +c[,d] @@ ...`. An omitted count means 1, which
// is what git emits for a single-line hunk.
func parseHunkHeader(line string) (hunkHeader, error) {
	fail := func() (hunkHeader, error) {
		return hunkHeader{}, fmt.Errorf("diff has an unparseable hunk header: %q", line)
	}
	rest := strings.TrimPrefix(line, "@@")
	end := strings.Index(rest, "@@")
	if end < 0 {
		return fail()
	}
	fields := strings.Fields(rest[:end])
	if len(fields) != 2 || !strings.HasPrefix(fields[0], "-") || !strings.HasPrefix(fields[1], "+") {
		return fail()
	}
	baseStart, err := parseHunkStart(fields[0][1:])
	if err != nil {
		return fail()
	}
	headStart, err := parseHunkStart(fields[1][1:])
	if err != nil {
		return fail()
	}
	return hunkHeader{baseStart: baseStart, headStart: headStart}, nil
}

// parseHunkStart reads the `start[,count]` half of a hunk range, returning start.
// The count is deliberately discarded: the body's own line prefixes are what
// advance the counters, so trusting a declared count would let a miscounted header
// silently shift every line number in the hunk.
func parseHunkStart(spec string) (int, error) {
	if i := strings.IndexByte(spec, ','); i >= 0 {
		spec = spec[:i]
	}
	return strconv.Atoi(spec)
}

// stripDiffPathPrefix removes the `a/` or `b/` prefix git writes by default and
// normalizes the /dev/null sentinel to the empty string. Trailing tab-separated
// metadata (timestamps, which some diff producers append) is dropped too.
func stripDiffPathPrefix(p string) string {
	if i := strings.IndexByte(p, '\t'); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimSpace(p)
	if p == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		return p[2:]
	}
	return p
}

// headFileKey / baseFileKey pick the name a line belongs under. A created file has
// no base path and a deleted file has no head path, so each side falls back to the
// other: a deleted file's removed lines still belong to the only name that file
// ever had.
func headFileKey(headPath, basePath string) string {
	if headPath != "" {
		return headPath
	}
	return basePath
}

func baseFileKey(headPath, basePath string) string {
	if basePath != "" {
		return basePath
	}
	return headPath
}

func addLine(index map[string]map[int]bool, file string, line int) {
	if index[file] == nil {
		index[file] = map[int]bool{}
	}
	index[file][line] = true
}

// IsAddedLine reports whether head-side line of file was ADDED by the diff. This
// is the predicate condition 3 needs, and the added side alone is sufficient:
// a reported finding cites a head-state line, and a removed line has no head-state
// line number, so no citation can ever land on one. Exposing RemovedLines
// separately keeps that reasoning checkable rather than making it an unstated
// assumption of a single combined predicate.
func (m DiffLineMap) IsAddedLine(file string, line int) bool {
	return m.added[file][line]
}

// AddedLines returns the head-side line numbers the diff added to file, sorted.
func (m DiffLineMap) AddedLines(file string) []int { return sortedLines(m.added[file]) }

// RemovedLines returns the BASE-side line numbers the diff removed from file,
// sorted. Base-side because that is the only space a removed line has a number in.
func (m DiffLineMap) RemovedLines(file string) []int { return sortedLines(m.removed[file]) }

// Files returns every path the diff touches, sorted, head-side where one exists.
// Sorted so a caller iterating them is deterministic — Go map order is not.
func (m DiffLineMap) Files() []string {
	seen := map[string]bool{}
	for f := range m.added {
		seen[f] = true
	}
	for f := range m.removed {
		seen[f] = true
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func sortedLines(set map[int]bool) []int {
	if len(set) == 0 {
		return nil
	}
	out := make([]int, 0, len(set))
	for l := range set {
		out = append(out, l)
	}
	sort.Ints(out)
	return out
}
