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

// hunkHeader carries `@@ -a,b +c,d @@`, where either count may be omitted
// (meaning 1). Parsed by hand rather than with a regexp so a malformed header
// produces a diagnostic naming the header, not a silent non-match.
//
// The COUNTS are load-bearing, not decoration. They are the only thing that says
// where a hunk body ends, and without that the parser cannot tell a file header
// from a body line that looks like one — a removed line whose own content begins
// `-- ` (an SQL, Lua or Haskell comment; a `git format-patch` signature) renders
// as `--- ...` and is byte-identical to the start of a `--- a/path` header.
//
// Getting that wrong is not a one-line misclassification. Treating it as a header
// resets the file state mid-hunk, and the whole map comes back EMPTY with no
// error: IsAddedLine is then false everywhere, condition 3 never fires, and an
// `outside_diff: true` expectation is satisfied by a report citing an added line.
// The tier's headline measurement fails open, on a case that parses cleanly.
//
// The counts bound the body only. Line NUMBERS still come from walking the body's
// own prefixes, so a miscounted header cannot silently shift them.
type hunkHeader struct {
	baseStart int
	headStart int
	baseCount int
	headCount int
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
	// baseLeft and headLeft are the hunk's UNCONSUMED line counts. Their sum being
	// positive is what "inside a hunk body" means; when both hit zero the body is
	// over and the next line is header or trailing text again.
	var baseLeft, headLeft int
	inHunk := func() bool { return baseLeft > 0 || headLeft > 0 }

	sc := bufio.NewScanner(bytes.NewReader(diff))
	// A case's diff may carry a long minified or generated line; the default 64 KiB
	// token limit would fail the whole parse on one such line, which reads as a
	// malformed diff rather than a long one.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for sc.Scan() {
		line := sc.Text()

		// INSIDE A HUNK BODY, prefixes are classified as content FIRST. This
		// ordering is the whole fix for the header collision described on
		// hunkHeader: within a body, "--- old comment" is a removed line, never a
		// file header, and only the declared counts can tell us we are still in a
		// body. Outside one, the same bytes are a header.
		if inHunk() {
			switch {
			case strings.HasPrefix(line, `\`):
				// "\ No newline at end of file" is a MARKER, not content, and is not
				// counted against either side's remaining lines.
			case line == "" || line[0] == ' ':
				// A context line. Some tools strip the single leading space from a
				// blank context line, so a bare empty line inside a body is context
				// too — counting it keeps the counters aligned with the file.
				baseLine++
				headLine++
				baseLeft--
				headLeft--
			case line[0] == '+':
				addLine(m.added, headFileKey(headPath, basePath), headLine)
				headLine++
				headLeft--
			case line[0] == '-':
				addLine(m.removed, baseFileKey(headPath, basePath), baseLine)
				baseLine++
				baseLeft--
			default:
				// A body that ends early: the declared counts are not exhausted but
				// this line belongs to no side. Close the hunk and re-read the line
				// as header text rather than guessing.
				baseLeft, headLeft = 0, 0
				if err := readOutsideHunk(line, &headPath, &basePath, &baseLine, &headLine, &baseLeft, &headLeft); err != nil {
					return DiffLineMap{}, err
				}
			}
			// A side whose declared count was wrong must not drive the other
			// negative and re-open the body later.
			if baseLeft < 0 {
				baseLeft = 0
			}
			if headLeft < 0 {
				headLeft = 0
			}
			continue
		}

		if err := readOutsideHunk(line, &headPath, &basePath, &baseLine, &headLine, &baseLeft, &headLeft); err != nil {
			return DiffLineMap{}, err
		}
	}
	if err := sc.Err(); err != nil {
		return DiffLineMap{}, fmt.Errorf("reading diff: %w", err)
	}
	return m, nil
}

// readOutsideHunk handles a line that is NOT inside a hunk body: a file header, a
// hunk header, or extended-header noise. It is a function rather than inline code
// because a hunk whose declared counts run out early has to re-read its current
// line this way, and two copies of the header rules would be two things to keep
// in step.
func readOutsideHunk(line string, headPath, basePath *string, baseLine, headLine, baseLeft, headLeft *int) error {
	switch {
	case strings.HasPrefix(line, "diff --git "):
		// A new file section: forget the previous file's state so a stray line
		// cannot be attributed to the wrong file.
		*headPath, *basePath = "", ""

	case strings.HasPrefix(line, "--- "):
		*basePath = stripDiffPathPrefix(strings.TrimPrefix(line, "--- "))

	case strings.HasPrefix(line, "+++ "):
		*headPath = stripDiffPathPrefix(strings.TrimPrefix(line, "+++ "))

	case strings.HasPrefix(line, "@@"):
		if *basePath == "" && *headPath == "" {
			return fmt.Errorf("diff has a hunk with no file header before it: %q", line)
		}
		h, err := parseHunkHeader(line)
		if err != nil {
			return err
		}
		*baseLine, *headLine = h.baseStart, h.headStart
		*baseLeft, *headLeft = h.baseCount, h.headCount

	case line == "" || strings.HasPrefix(line, `\`):
		// Padding between sections, or a stray no-newline marker. Not content.

	case line == "--" || line == "-- ":
		// `git format-patch`'s signature separator, which precedes the git version
		// in a mailed patch. Matched EXACTLY rather than by prefix: a removed line
		// whose content is a bare `-` renders as `--` too, but that can only occur
		// inside a hunk, where the body arm claims it first.

	case line[0] == '+' || line[0] == '-':
		// `+++ ` and `--- ` were matched above, so anything still starting with
		// '+' or '-' out here is a body line with no hunk to belong to.
		return fmt.Errorf("diff has a body line outside any hunk: %q", line)

	default:
		// index lines, mode lines, "new file mode", "similarity index", a
		// format-patch signature, commit prose — none of it is content.
	}
	return nil
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
	baseStart, baseCount, err := parseHunkRange(fields[0][1:])
	if err != nil {
		return fail()
	}
	headStart, headCount, err := parseHunkRange(fields[1][1:])
	if err != nil {
		return fail()
	}
	return hunkHeader{
		baseStart: baseStart, headStart: headStart,
		baseCount: baseCount, headCount: headCount,
	}, nil
}

// parseHunkRange reads one `start[,count]` half of a hunk range. An omitted count
// means 1, which is what git emits for a single-line hunk.
//
// The count bounds the BODY; it never advances a line number. Numbers come from
// walking the body's own prefixes, so a miscounted header cannot silently shift
// every line in the hunk — the reason the count was originally discarded here.
// What discarding it also cost was any way to tell where a body ends, which is
// what the header-collision bug on hunkHeader turned on.
//
// A negative start, or a zero start on a non-empty side, is rejected rather than
// accepted: both yield line numbers no reported finding can ever match, so the
// case would score zero everywhere with no diagnostic. Zero IS legal on an empty
// side — `@@ -0,0 +1,3 @@` is how git writes a created file.
func parseHunkRange(spec string) (start, count int, err error) {
	count = 1
	if i := strings.IndexByte(spec, ','); i >= 0 {
		count, err = strconv.Atoi(spec[i+1:])
		if err != nil {
			return 0, 0, err
		}
		spec = spec[:i]
	}
	start, err = strconv.Atoi(spec)
	if err != nil {
		return 0, 0, err
	}
	if start < 0 || count < 0 {
		return 0, 0, fmt.Errorf("negative hunk range %q", spec)
	}
	if start == 0 && count > 0 {
		return 0, 0, fmt.Errorf("hunk range %q starts at line 0 but declares %d line(s); lines are 1-based", spec, count)
	}
	return start, count, nil
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
