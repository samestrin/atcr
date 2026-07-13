//go:build wasip1

// Command pyparser is the Python-language structural parser plugin, compiled to
// a WebAssembly reactor (GOOS=wasip1 GOARCH=wasm, -buildmode=c-shared) and
// loaded by the internal/astgroup wazero host.
//
// It is a lightweight indentation + keyword block-structure parser, NOT a full
// Python grammar: enough to recover the nesting of defs, classes, and compound
// statements so a finding's line can be mapped to its smallest covering block.
// It emits the same kind/name/start_line/end_line/children JSON contract as the
// Go plugin, so the host stays language-agnostic. Structure (kinds, names,
// nesting) — not physical line text — drives the host's Merkle hash, so blank
// lines and reindentation that shift line numbers do not change the hash.
//
// Memory protocol matches the goparser plugin and the astgroup host.
// Regenerate the vendored .wasm via internal/astgroup/parsers/build.sh.
package main

import (
	"strings"

	"github.com/samestrin/atcr/internal/astgroup/parsers/src/guestabi"
)

type node struct {
	Kind      string `json:"kind"`
	Name      string `json:"name,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Children  []node `json:"children,omitempty"`
}

// alloc/free/parse are the wasip1 reactor ABI entrypoints the astgroup host
// calls. Go requires //go:wasmexport functions in each command's own package
// main, so these thin wrappers just delegate to the shared guestabi bodies (see
// the guestabi package doc for the pin map and its GC assumptions).

//go:wasmexport alloc
func alloc(n int32) int32 { return guestabi.Alloc(n) }

//go:wasmexport free
func free(p int32) { guestabi.Free(p) }

//go:wasmexport parse
func parse(ptr int32, n int32) int64 {
	buf, ok := guestabi.Lookup(ptr)
	if !ok || int(n) < 0 || int(n) > len(buf) {
		return guestabi.Emit(node{Kind: "error", Name: "bad pointer"})
	}
	src := string(buf[:n])

	lines := significantLines(src)
	root := node{Kind: "module", StartLine: 1, EndLine: 1}
	if len(lines) > 0 {
		kids, _ := build(lines, 0, lines[0].indent)
		root.Children = kids
		root.EndLine = lines[len(lines)-1].lineno
	}
	return guestabi.Emit(root)
}

type pline struct {
	indent int
	lineno int
	text   string // stripped of leading whitespace and trailing inline comment
}

// significantLines returns non-blank, non-comment-only lines with their
// indentation (tabs expanded to 8) and 1-based line number. Lines inside a
// triple-quoted string (docstrings, multi-line literals) are skipped on a
// best-effort basis so a def/class/# appearing as string CONTENT is not misparsed
// as real code. The skip relies on scanTripleQuotes, which is quote/escape-aware
// (epic 22.3): a """/”' embedded in a single/double-quoted string, and a triple
// quote inside a # comment, no longer flip the state machine. It remains a
// heuristic — raw/byte/f-string prefixes are not modelled — so adversarial source
// using those can still mis-slice, but ordinary code no longer does (PoC-grade,
// not a tokenizer).
func significantLines(src string) []pline {
	var out []pline
	delim := ""     // active triple-quote delimiter spanning lines, "" when outside
	depth := 0      // open (), [], {} nesting carried across physical lines
	var cont *pline // logical line being assembled while depth > 0
	for i, raw := range strings.Split(src, "\n") {
		startInString := delim != ""
		delim = scanTripleQuotes(raw, delim)
		if startInString {
			continue // entire physical line is inside a multi-line string literal
		}
		indent, j := leadingIndent(raw)
		body := strings.TrimRight(raw[j:], " \t\r")

		if cont != nil {
			// Assembling a bracket-continued logical line (e.g. a def/class header
			// or call whose parens span lines): fold this physical line's content
			// onto it and keep counting brackets until balanced, so a multi-line
			// header is classified as one line ending in ':' rather than scattering
			// its parameter lines and body into sibling blocks.
			stripped := stripComment(raw)
			if t := strings.TrimSpace(stripped); t != "" {
				cont.text += " " + t
			}
			depth += bracketDelta(stripped)
			if depth <= 0 {
				depth = 0
				cont.text = strings.TrimRight(cont.text, " \t\r")
				out = append(out, *cont)
				cont = nil
			}
			continue
		}

		if body == "" || strings.HasPrefix(body, "#") {
			continue
		}
		if d := bracketDelta(stripComment(body)); d > 0 {
			// Header opens more brackets than it closes: start folding continuation
			// lines. The logical line keeps the first physical line's indent/lineno.
			depth = d
			head := strings.TrimRight(stripComment(body), " \t\r")
			pl := pline{indent: indent, lineno: i + 1, text: head}
			cont = &pl
			continue
		}
		out = append(out, pline{indent: indent, lineno: i + 1, text: body})
	}
	if cont != nil {
		// Unbalanced brackets at EOF: emit the partial logical line as-is.
		cont.text = strings.TrimRight(cont.text, " \t\r")
		out = append(out, *cont)
	}
	return out
}

// leadingIndent returns the visual indentation of raw (tabs expanded to 8) and
// the byte offset where the first non-whitespace character begins.
func leadingIndent(raw string) (indent, start int) {
	for start < len(raw) {
		switch raw[start] {
		case ' ':
			indent++
		case '\t':
			indent += 8 - (indent % 8)
		default:
			return indent, start
		}
		start++
	}
	return indent, start
}

// bracketDelta returns the net change in (), [], {} nesting contributed by s. It
// is a heuristic that does not exclude brackets inside string/char literals, so a
// line embedding an unbalanced bracket inside a string can mis-count; that is
// acceptable for a structural pre-pass whose only job is to fold a multi-line
// header or literal into one logical line.
func bracketDelta(s string) int {
	d := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			d++
		case ')', ']', '}':
			d--
		}
	}
	return d
}

// scanLine advances pyparser's line-scanning state across one physical line. It
// carries the active triple-quoted-string delimiter (delim; "" when outside such
// a string) and reports the byte offset where an unquoted `#` comment begins
// (len(line) when the line has none). It is quote- and escape-aware (epic 22.3):
//   - A `#` inside an open triple-quoted span, or inside a single-line '...'/"..."
//     literal, is string content — not a comment — so the comment offset is only
//     taken when the scan is outside every string at that position.
//   - A `”'`/`"""` token inside a single-line string literal is content too, so it
//     does not open a spurious multi-line span that swallows the code that follows.
//   - A backslash escapes the next byte inside a single-line string, so an escaped
//     quote does not prematurely close it.
//
// It remains a heuristic (see the package doc): raw/byte/f-string prefixes are not
// modelled, and single-line strings are not carried across physical lines (a
// backslash line-continuation inside one is out of scope).
func scanLine(line, delim string) (endDelim string, commentAt int) {
	var q byte // active single-line string quote (' or "), 0 when outside one
	for i := 0; i < len(line); {
		if delim != "" {
			if strings.HasPrefix(line[i:], delim) {
				delim = ""
				i += 3
				continue
			}
			i++
			continue
		}
		if q != 0 {
			// Inside a single-line '...' or "..." literal: a backslash escapes the
			// next byte, and only the matching quote closes the string. A `#` or a
			// triple-quote token here is string content, not a comment or a new span.
			switch line[i] {
			case '\\':
				i += 2
				continue
			case q:
				q = 0
			}
			i++
			continue
		}
		if strings.HasPrefix(line[i:], `"""`) {
			delim = `"""`
			i += 3
			continue
		}
		if strings.HasPrefix(line[i:], `'''`) {
			delim = `'''`
			i += 3
			continue
		}
		switch line[i] {
		case '#':
			return delim, i
		case '"', '\'':
			q = line[i]
		}
		i++
	}
	return delim, len(line)
}

// scanTripleQuotes advances the triple-quoted-string state across one physical
// line and returns the state at line end (delim is the active delimiter at line
// start, "" if outside). It is comment-aware via scanLine: a triple-quote token
// appearing inside a `#` comment (epic 22.3) does not flip the state machine.
func scanTripleQuotes(line, delim string) string {
	end, _ := scanLine(line, delim)
	return end
}

// build consumes consecutive lines at exactly `indent` and returns their nodes.
func build(lines []pline, i, indent int) ([]node, int) {
	var out []node
	for i < len(lines) {
		cur := lines[i]
		if cur.indent < indent {
			break
		}
		if cur.indent > indent {
			// Defensive: a deeper line without a header (continuation or
			// inconsistent indent). Attach as a leaf at this level.
			out = append(out, node{Kind: "stmt", StartLine: cur.lineno, EndLine: cur.lineno})
			i++
			continue
		}
		kind, name := classify(cur.text)
		n := node{Kind: kind, Name: name, StartLine: cur.lineno, EndLine: cur.lineno}
		i++
		if isHeader(cur.text) && i < len(lines) && lines[i].indent > indent {
			kids, ni := build(lines, i, lines[i].indent)
			n.Children = kids
			i = ni
			n.EndLine = maxEnd(n)
		}
		out = append(out, n)
	}
	return out, i
}

func maxEnd(n node) int {
	m := n.EndLine
	for _, c := range n.Children {
		if e := maxEnd(c); e > m {
			m = e
		}
	}
	return m
}

// isHeader reports whether a line opens a nested block: it ends with ':' AND its
// first token is a compound-statement keyword (classify returns a non-"stmt"
// kind). Requiring the keyword — not merely a trailing ':' — stops a bare dict
// key ("key":), a slice (arr[1:), or an annotation line from being misread as a
// block opener and fabricating spurious child blocks that corrupt the structural
// hash. (PoC limitation: match/case are soft keywords absent from
// compoundKeywords, so their suites are not nested.)
func isHeader(text string) bool {
	t := stripComment(text)
	t = strings.TrimRight(t, " \t")
	if !strings.HasSuffix(t, ":") {
		return false
	}
	kind, _ := classify(text)
	return kind != "stmt"
}

var compoundKeywords = []string{"def", "class", "if", "elif", "else", "for", "while", "with", "try", "except", "finally"}

func classify(text string) (kind, name string) {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "async ") {
		t = strings.TrimSpace(t[len("async "):])
	}
	for _, kw := range compoundKeywords {
		if t == kw || strings.HasPrefix(t, kw+" ") || strings.HasPrefix(t, kw+":") || strings.HasPrefix(t, kw+"(") {
			switch kw {
			case "def":
				return "func", identAfter(t, "def")
			case "class":
				return "class", identAfter(t, "class")
			case "elif":
				return "if", ""
			default:
				return kw, ""
			}
		}
	}
	return "stmt", ""
}

// identAfter returns the identifier following keyword kw (up to '(' or ':').
func identAfter(t, kw string) string {
	rest := strings.TrimSpace(t[len(kw):])
	end := len(rest)
	for i, r := range rest {
		if r == '(' || r == ':' || r == ' ' {
			end = i
			break
		}
	}
	return rest[:end]
}

// stripComment returns text with any trailing `#` comment removed. It is
// comment-aware via scanLine: a `#` inside a triple-quoted string span on this
// line is treated as string content, not a comment start (epic 22.3).
func stripComment(text string) string {
	_, at := scanLine(text, "")
	return text[:at]
}

func main() {}
