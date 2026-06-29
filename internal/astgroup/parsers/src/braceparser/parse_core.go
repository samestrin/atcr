// Package main is the shared brace-block structural parser. One Go source is
// compiled once per target language (build tag ts|php|rust|bash) into a wasip1
// reactor .wasm loaded by the internal/astgroup wazero host. It recovers block
// structure for brace-delimited languages by tracking { } depth (plus string,
// comment, and heredoc state so braces inside literals do not fabricate blocks)
// and names each block from the nearest preceding block-introducing keyword.
//
// It is a heuristic structural parser for GROUPING ONLY, not a grammar: a
// misparse degrades to line-proximity grouping for that finding and can never
// break a reconcile. It emits the same kind/name/start_line/end_line/children
// JSON contract as the Go and Python plugins, so the host stays language-
// agnostic. Structure — not physical line text — drives the host's Merkle hash,
// so blank-line / reformat drift does not change a block's hash.
//
// The scanner (this file) and the per-language tables (configs.go) carry no
// build tag, so they are unit-tested on the host; only `active` selection
// (active_*.go) and the wasm ABI (main.go) are build-constrained.
package main

import (
	"bytes"
	"strings"
)

// node mirrors internal/astgroup.Node; the JSON tags are the wire contract.
type node struct {
	Kind      string `json:"kind"`
	Name      string `json:"name,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Children  []node `json:"children,omitempty"`
}

// blockKeyword maps a block-introducing keyword to the host block kind it
// produces. named=true captures the identifier following the keyword as the
// block's Name so sibling blocks of identical shape still hash distinctly.
type blockKeyword struct {
	word  string
	kind  string
	named bool
}

// langConfig is the per-language parameterization of the otherwise language-
// agnostic brace scanner. The differences between the four target languages
// live entirely in this data, never in the scanner's control flow.
type langConfig struct {
	name                string
	lineComments        []string // line-comment introducers, e.g. {"//"} or {"//", "#"} or {"#"}
	blockOpen           string   // block-comment open, e.g. "/*" ("" disables)
	blockClose          string   // block-comment close, e.g. "*/"
	strChars            string   // string delimiters, e.g. "\"'`" (ts) or "\"" (rust)
	rawStrings          bool     // Rust raw strings: r"...", r#"..."#
	charLiterals        bool     // Rust char literals 'x' / '\n' (so ' is not a string/lifetime)
	arrowFunc           bool     // treat => as an (anonymous) function-block introducer (TS/JS)
	funcParen           bool     // treat `ident(...) {` as a named function block (TS methods, Bash name())
	heredocs            bool     // enable heredoc bodies (Bash <<, PHP <<<)
	heredocOp           string   // heredoc operator: "<<" (Bash) or "<<<" (PHP)
	paramExpand         bool     // treat ${...} as opaque (Bash) so its braces never open/close a block
	braceExpand         bool     // treat {a,b} / {1..N} brace expansion as opaque (Bash) so its braces never open/close a block
	commentWordBoundary bool     // a "#" line comment requires a preceding word boundary (Bash: keeps $#, ${#a} out of comments)
	keywords            []blockKeyword
}

// scanner states.
const (
	stNormal = iota
	stLineComment
	stBlockComment
	stString
	stRawString
	stHeredoc
	stParamExp
)

// parseSource scans src under cfg and returns the structural node tree rooted at
// a "file" node. It is allocation-light and single-pass; every byte advances the
// cursor by at most a small fixed lookahead so line counting stays exact across
// every state (comments, strings, heredoc bodies included).
func parseSource(src []byte, cfg langConfig) node {
	stack := []node{{Kind: "file", StartLine: 1, EndLine: 1}}
	line := 1
	lineStart := 0

	var header []byte
	headerLine := 1
	headerStarted := false
	parenDepth := 0 // open '(' run in the current header; a ';' inside it (C-style for) does not end the statement

	resetHeader := func() {
		header = header[:0]
		headerStarted = false
		parenDepth = 0
	}
	addHeader := func(b byte) {
		if b == '\n' || b == '\r' || b == '\t' {
			b = ' '
		}
		if b == ' ' && !headerStarted {
			return
		}
		if !headerStarted {
			headerStarted = true
			headerLine = line
		}
		header = append(header, b)
	}
	openBlock := func() {
		kind, name := classifyHeader(string(header), cfg)
		start := line
		if headerStarted {
			start = headerLine
		}
		stack = append(stack, node{Kind: kind, Name: name, StartLine: start, EndLine: line})
		resetHeader()
	}
	closeBlock := func() {
		if len(stack) <= 1 {
			resetHeader()
			return
		}
		child := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		child.EndLine = line
		parent := &stack[len(stack)-1]
		parent.Children = append(parent.Children, child)
		resetHeader()
	}

	state := stNormal
	var strDelim byte
	escape := false
	rawHashes := 0
	var heredocTag string
	heredocStrip := false
	heredocPending := false
	paramDepth := 0
	var paramQuote byte
	paramEscape := false
	arithDepth := 0 // bash `((...))` / `$((...))` nesting; a `<<` inside is a shift, not a heredoc

	for i := 0; i < len(src); i++ {
		c := src[i]

		switch state {
		case stLineComment:
			if c == '\n' {
				state = stNormal
			}

		case stBlockComment:
			if cfg.blockClose != "" && matchAt(src, i, cfg.blockClose) {
				i += len(cfg.blockClose) - 1
				state = stNormal
			}

		case stString:
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == strDelim {
				state = stNormal
			}

		case stRawString:
			if c == '"' && hasHashes(src, i+1, rawHashes) {
				i += rawHashes
				state = stNormal
			}

		case stHeredoc:
			if c == '\n' {
				if heredocLineMatches(src[lineStart:i], heredocTag, heredocStrip) {
					state = stNormal
				}
			}

		case stParamExp:
			// Inside a ${...} parameter expansion: count nested braces so the
			// matching close returns to normal without ever touching the block
			// stack. Braces inside quoted strings are ignored so that patterns
			// like ${var/"}"/"{"} do not desync the brace depth.
			if paramQuote != 0 {
				if paramEscape {
					paramEscape = false
				} else if c == '\\' {
					paramEscape = true
				} else if c == paramQuote {
					paramQuote = 0
				}
			} else if c == '{' {
				paramDepth++
			} else if c == '}' {
				paramDepth--
				if paramDepth <= 0 {
					state = stNormal
				}
			} else if c == '"' || c == '\'' {
				paramQuote = c
				paramEscape = false
			}

		default: // stNormal
			switch {
			case cfg.blockOpen != "" && matchAt(src, i, cfg.blockOpen):
				i += len(cfg.blockOpen) - 1
				state = stBlockComment
			case lineCommentStarts(src, i, cfg):
				state = stLineComment
			case cfg.rawStrings && c == 'r' && rawStringStart(src, i):
				rawHashes = countHashes(src, i+1)
				i += 1 + rawHashes // consume 'r' and the '#' run; the '"' is consumed next iter as part of the body
				state = stRawString
			case cfg.charLiterals && c == '\'':
				if n := charLiteralLen(src, i); n > 0 {
					i += n - 1 // consume whole 'x' / '\n' literal; not a string, not a lifetime
				}
				// else: a lone ' (lifetime / apostrophe) — treat as ordinary text.
			case cfg.paramExpand && c == '$' && i+1 < len(src) && src[i+1] == '{':
				i++ // consume the '{'; depth tracks nested ${...}
				paramDepth = 1
				state = stParamExp
			case strings.IndexByte(cfg.strChars, c) >= 0:
				state = stString
				strDelim = c
				escape = false
			case cfg.heredocs && arithDepth == 0 && matchAt(src, i, cfg.heredocOp) && isHeredocStart(src, i+len(cfg.heredocOp)):
				tag, strip, consumed := parseHeredoc(src, i+len(cfg.heredocOp), cfg.heredocOp == "<<<")
				heredocTag = tag
				heredocStrip = strip
				heredocPending = true
				i += len(cfg.heredocOp) + consumed - 1
			case cfg.heredocs && cfg.heredocOp == "<<" && c == '(' && i+1 < len(src) && src[i+1] == '(':
				// Bash arithmetic `((...))` / `$((...))`: a `<<` inside is a left-shift,
				// never a heredoc. Track depth so heredoc detection is suppressed within
				// (the `<<` then falls through to header text, which is harmless).
				arithDepth++
				parenDepth += 2
				addHeader('(')
				addHeader('(')
				i++
			case cfg.heredocs && cfg.heredocOp == "<<" && arithDepth > 0 && c == ')' && i+1 < len(src) && src[i+1] == ')':
				arithDepth--
				if parenDepth >= 2 {
					parenDepth -= 2
				} else {
					parenDepth = 0
				}
				addHeader(')')
				addHeader(')')
				i++
			case c == '(':
				parenDepth++
				addHeader(c)
			case c == ')':
				if parenDepth > 0 {
					parenDepth--
				}
				addHeader(c)
			case c == '{':
				if cfg.braceExpand {
					if end := bashBraceExpansionEnd(src, i); end >= 0 {
						i = end // consume the brace expansion; its braces never touch the block stack
						break
					}
				}
				openBlock()
			case c == '}':
				closeBlock()
			case c == ';':
				// A ';' ends the statement (resetting the header) only at paren
				// depth 0. Inside an unclosed '(' run it is part of the header — a
				// C-style `for (i=0; i<n; i++)` keeps its `for` keyword for naming.
				if parenDepth == 0 {
					resetHeader()
				} else {
					addHeader(c)
				}
			default:
				addHeader(c)
			}
		}

		if c == '\n' {
			line++
			lineStart = i + 1
			if heredocPending {
				heredocPending = false
				state = stHeredoc
			}
		}
	}

	for len(stack) > 1 {
		closeBlock()
	}
	root := stack[0]
	if line > root.EndLine {
		root.EndLine = line
	}
	return root
}

// classifyHeader maps the accumulated header text preceding a '{' to a block
// kind and (optional) name, using the nearest preceding block-introducing
// keyword. Falls back to an anonymous "block" so object/array literals and other
// brace groups never false-merge with real declarations.
func classifyHeader(h string, cfg langConfig) (kind, name string) {
	bestIdx := -1
	var bestKw blockKeyword
	for _, kw := range cfg.keywords {
		if idx := lastWholeWord(h, kw.word); idx > bestIdx {
			bestIdx = idx
			bestKw = kw
		}
	}
	// Rust `impl Trait for Foo {` must classify as the impl block, not as a
	// `for` loop. When the winning keyword is `for` and `impl` precedes it,
	// prefer the impl keyword so the name resolves to Foo.
	if bestKw.word == "for" {
		if idx := lastWholeWord(h, "impl"); idx >= 0 {
			bestIdx = idx
			bestKw = blockKeyword{word: "impl", kind: "class", named: true}
		}
	}
	if cfg.arrowFunc {
		if a := strings.LastIndex(h, "=>"); a > bestIdx {
			// Only honor `=>` as an arrow-function header when it sits at
			// parenthesis depth 0. Inline arrows inside control-flow headers
			// such as `for (x of items.map(i => i.id))` must keep their
			// control kind, not be misclassified as func.
			depth := 0
			for i := 0; i < a; i++ {
				switch h[i] {
				case '(':
					depth++
				case ')':
					if depth > 0 {
						depth--
					}
				}
			}
			if depth == 0 {
				return "func", ""
			}
		}
	}
	if bestIdx >= 0 {
		if bestKw.named {
			name = identAfter(h, bestIdx+len(bestKw.word))
		}
		return bestKw.kind, name
	}
	if cfg.funcParen {
		if id, ok := funcParenName(h); ok {
			return "func", id
		}
	}
	return "block", ""
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// lastWholeWord returns the index of the last whole-word occurrence of word in
// h (identifier boundaries on both sides), or -1.
func lastWholeWord(h, word string) int {
	from := len(h)
	for {
		idx := strings.LastIndex(h[:from], word)
		if idx < 0 {
			return -1
		}
		beforeOK := idx == 0 || !isIdentByte(h[idx-1])
		afterPos := idx + len(word)
		afterOK := afterPos >= len(h) || !isIdentByte(h[afterPos])
		if beforeOK && afterOK {
			return idx
		}
		from = idx
		if from == 0 {
			return -1
		}
	}
}

// skipGenericList returns the position just after a balanced `<...>` generic
// parameter list starting at pos, or pos unchanged if no `<` is present.
func skipGenericList(h string, pos int) int {
	if pos >= len(h) || h[pos] != '<' {
		return pos
	}
	depth := 1
	pos++
	for pos < len(h) && depth > 0 {
		switch h[pos] {
		case '<':
			depth++
		case '>':
			depth--
		}
		pos++
	}
	return pos
}

// identAfter returns the identifier starting after pos (skipping spaces and any
// generic parameter list). For Rust `impl Trait for Foo` it skips to the name
// after `for` so sibling impl blocks hash by the implemented type.
func identAfter(h string, pos int) string {
	skipSpaces := func() {
		for pos < len(h) && h[pos] == ' ' {
			pos++
		}
	}
	readIdent := func() string {
		start := pos
		for pos < len(h) && isIdentByte(h[pos]) {
			pos++
		}
		return h[start:pos]
	}

	skipSpaces()
	pos = skipGenericList(h, pos)
	skipSpaces()
	name := readIdent()
	skipSpaces()
	pos = skipGenericList(h, pos)
	skipSpaces()
	if strings.HasPrefix(h[pos:], "for ") {
		pos += len("for ")
		skipSpaces()
		pos = skipGenericList(h, pos)
		skipSpaces()
		name = readIdent()
	}
	return name
}

// funcParenName recognizes a `name(...)` function header (Bash name(), TS
// methods): the trimmed header ends in ')', and the identifier immediately
// before the first '(' is the name. Leading modifier words (async, public,
// static, get, set, etc.) are skipped so TS class methods are named correctly.
func funcParenName(h string) (string, bool) {
	t := strings.TrimSpace(h)
	if !strings.HasSuffix(t, ")") {
		return "", false
	}
	open := strings.IndexByte(t, '(')
	if open <= 0 {
		return "", false
	}
	prefix := strings.TrimSpace(t[:open])
	// Extract the last identifier token from prefix.
	end := len(prefix)
	for end > 0 && prefix[end-1] == ' ' {
		end--
	}
	start := end
	for start > 0 && isIdentByte(prefix[start-1]) {
		start--
	}
	if start == end {
		return "", false
	}
	name := prefix[start:end]
	// Reserved control words must not be misclassified as function names.
	switch name {
	case "catch", "with", "switch":
		return "", false
	}
	// If there is any leading text, it must be only modifier words/whitespace;
	// arbitrary expressions like `return foo()` must not become functions.
	modifiers := strings.Fields(prefix[:start])
	for _, m := range modifiers {
		switch m {
		case "async", "public", "private", "protected", "static", "get", "set",
			"readonly", "abstract", "override":
			// allowed modifier
		default:
			return "", false
		}
	}
	return name, true
}

func matchAt(src []byte, i int, s string) bool {
	if s == "" || i+len(s) > len(src) {
		return false
	}
	return bytes.Equal(src[i:i+len(s)], []byte(s))
}

func matchAnyPrefix(src []byte, i int, prefixes []string) bool {
	for _, p := range prefixes {
		if matchAt(src, i, p) {
			return true
		}
	}
	return false
}

// lineCommentStarts reports whether a line comment begins at src[i]. For the "#"
// marker under commentWordBoundary (Bash), the '#' must sit at a word boundary so
// `$#`, `${#a}`, and `x#y` are NOT treated as comments — mishandling them would
// swallow the rest of the line and can desync the brace stack.
func lineCommentStarts(src []byte, i int, cfg langConfig) bool {
	for _, m := range cfg.lineComments {
		if !matchAt(src, i, m) {
			continue
		}
		if m == "#" && cfg.commentWordBoundary && !hashAtWordBoundary(src, i) {
			continue
		}
		return true
	}
	return false
}

// hashAtWordBoundary reports whether the byte before i is a shell word boundary,
// i.e. a '#' there introduces a comment rather than continuing a token like `$#`.
func hashAtWordBoundary(src []byte, i int) bool {
	if i == 0 {
		return true
	}
	switch src[i-1] {
	case ' ', '\t', '\n', '\r', ';', '&', '|', '(', '`':
		return true
	}
	return false
}

// bashBraceExpansionEnd reports the index of the '}' that closes a Bash brace
// expansion beginning at src[i]=='{', or -1 if it is not an expansion. An
// expansion has no whitespace immediately after '{' and contains a top-level
// ',' or '..' before its matching '}' on the same line (e.g. {a,b}, file{1,2},
// {1..10}). A `{ ...; }` group command (space/newline after '{') returns -1 so
// it still opens a real block.
func bashBraceExpansionEnd(src []byte, i int) int {
	if i+1 >= len(src) {
		return -1
	}
	switch src[i+1] {
	case ' ', '\t', '\n', '\r':
		return -1 // group command, not expansion
	}
	depth := 0
	sep := false
	for j := i; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				if sep {
					return j
				}
				return -1
			}
		case ',':
			if depth == 1 {
				sep = true
			}
		case '.':
			if depth == 1 && j+1 < len(src) && src[j+1] == '.' {
				sep = true
			}
		case '\n', '\r':
			return -1 // multi-line: not a simple expansion; treat as a block
		}
	}
	return -1 // unmatched: degrade to a block
}

// rawStringStart reports whether src[i]=='r' begins a Rust raw string: r" or r#.
func rawStringStart(src []byte, i int) bool {
	// 'r' must not be the tail of an identifier (e.g. `for`, `var`).
	if i > 0 && isIdentByte(src[i-1]) {
		return false
	}
	j := i + 1
	if j >= len(src) {
		return false
	}
	if src[j] == '"' {
		return true
	}
	for j < len(src) && src[j] == '#' {
		j++
	}
	return j < len(src) && src[j] == '"' && j > i+1
}

func countHashes(src []byte, i int) int {
	n := 0
	for i+n < len(src) && src[i+n] == '#' {
		n++
	}
	return n
}

// hasHashes reports whether src has exactly n '#' starting at i (used to close a
// raw string r#"..."# with the matching hash count).
func hasHashes(src []byte, i, n int) bool {
	for k := 0; k < n; k++ {
		if i+k >= len(src) || src[i+k] != '#' {
			return false
		}
	}
	return true
}

// charLiteralLen returns the byte length of a Rust char literal starting at the
// opening quote src[i]=='\”, or 0 if it is not a char literal (e.g. a lifetime
// 'a or a label). Handles '\n', '\\', '\u{...}', '\x..', and any single char.
func charLiteralLen(src []byte, i int) int {
	n := len(src)
	if i+1 >= n {
		return 0
	}
	if src[i+1] == '\\' {
		if i+2 >= n {
			return 0
		}
		switch src[i+2] {
		case 'u':
			// '\u{...}' — unicode escape; consume up to closing quote.
			if i+3 >= n || src[i+3] != '{' {
				return 0
			}
			j := i + 4
			for j < n && src[j] != '}' {
				j++
			}
			if j >= n || j+1 >= n || src[j+1] != '\'' {
				return 0
			}
			return j - i + 2
		case 'x':
			// '\xNN' — byte escape.
			if i+5 >= n || src[i+5] != '\'' {
				return 0
			}
			return 6
		default:
			// '\X' — single-character escape.
			if i+3 < n && src[i+3] == '\'' {
				return 4
			}
			return 0
		}
	}
	// 'X' — single char then closing quote.
	if i+2 < n && src[i+2] == '\'' {
		return 3
	}
	return 0
}

// isHeredocStart reports whether a heredoc tag plausibly begins at src[j] (after
// the heredoc operator): an optional -/~ then optional quote then an identifier
// char. Guards against treating a bit-shift `a << b` as a heredoc.
func isHeredocStart(src []byte, j int) bool {
	for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
		j++
	}
	if j < len(src) && (src[j] == '-' || src[j] == '~') {
		j++
	}
	if j < len(src) && (src[j] == '\'' || src[j] == '"') {
		j++
	}
	return j < len(src) && (isIdentByte(src[j]) && !(src[j] >= '0' && src[j] <= '9'))
}

// parseHeredoc reads a heredoc tag starting at src[j] (just after the operator),
// returning the tag, whether leading whitespace is stripped from the terminator,
// and the number of bytes consumed from j. stripIndent is true for operators
// like PHP's <<< that always allow indented closers; it is also forced true by
// bash's <<- / <<~ strip operators.
func parseHeredoc(src []byte, j int, stripIndent bool) (tag string, strip bool, consumed int) {
	start := j
	strip = stripIndent
	for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
		j++
	}
	if j < len(src) && (src[j] == '-' || src[j] == '~') {
		strip = true
		j++
	}
	if j < len(src) && (src[j] == '\'' || src[j] == '"') {
		j++
	}
	ts := j
	for j < len(src) && isIdentByte(src[j]) {
		j++
	}
	tag = string(src[ts:j])
	return tag, strip, j - start
}

// heredocLineMatches reports whether the given physical line is the heredoc
// terminator. After optionally stripping leading tabs (<<-/<<~), the line must
// equal tag, or begin with tag followed by a non-identifier byte — the latter
// covers PHP's `EOT;` / `EOT,` / `EOT)` closings where the marker is followed by
// punctuation. The non-identifier guard keeps `EOThername` from matching `EOT`.
func heredocLineMatches(lineBytes []byte, tag string, strip bool) bool {
	if tag == "" {
		return false
	}
	s := strings.TrimRight(string(lineBytes), "\r")
	if strip {
		s = strings.TrimLeft(s, " \t")
	}
	if s == tag {
		return true
	}
	if strings.HasPrefix(s, tag) {
		return !isIdentByte(s[len(tag)])
	}
	return false
}
