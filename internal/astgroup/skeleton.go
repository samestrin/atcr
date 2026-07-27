package astgroup

import "strings"

// SkeletonEntry is one top-level declaration header extracted from a parsed
// source tree. Header is the declaration's signature line with the body elided —
// enough for a reviewer to see the file's resolved shape without paying for its
// full text.
type SkeletonEntry struct {
	Kind      string
	Name      string
	StartLine int
	Header    string
}

// skeletonKinds are the top-level declaration kinds a skeleton renders. Only
// direct children of the file root are considered, so nested funclits and
// control-flow blocks never appear.
var skeletonKinds = map[string]bool{"func": true, "gendecl": true}

// branchKinds are the node kinds that add a decision point to a function's
// control flow. Counting them is the McCabe cyclomatic measure. The Go parser
// emits no per-case node (a switch's arms are folded into the switch), so a
// switch contributes one regardless of arm count; the other kinds are listed for
// the languages whose plugins emit them.
var branchKinds = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true,
	"case": true, "except": true, "catch": true,
}

// FileSkeleton returns one entry per top-level func/gendecl declaration in root,
// in source order, with each header sliced from src.
//
// Import declarations are excluded: they are gendecl nodes like any other, but
// carry no structural signal a reviewer needs and would be pure noise. A node
// whose line range falls outside src is skipped rather than clamped — a
// tree/source mismatch means the skeleton would be wrong, and a missing entry is
// safer than a misleading one.
func FileSkeleton(root Node, src string) []SkeletonEntry {
	if len(root.Children) == 0 || len(src) == 0 {
		return nil
	}
	lines := strings.Split(src, "\n")

	var entries []SkeletonEntry
	for _, ch := range root.Children {
		if !skeletonKinds[ch.Kind] {
			continue
		}
		header, ok := declHeader(lines, ch)
		if !ok || isImportHeader(header) || isGroupedDeclOpener(header) {
			continue
		}
		entries = append(entries, SkeletonEntry{
			Kind:      ch.Kind,
			Name:      ch.Name,
			StartLine: ch.StartLine,
			Header:    header,
		})
	}
	return entries
}

// isImportHeader reports whether a gendecl header is an import declaration.
// The Go parser gives gendecl nodes no Name, so the source text is the only
// available discriminator.
func isImportHeader(header string) bool {
	return header == "import" || strings.HasPrefix(header, "import ") ||
		strings.HasPrefix(header, "import(")
}

// isGroupedDeclOpener reports whether a gendecl header is a bare grouped
// var/const/type opener (`var (`, `const (`, `type (`). declHeader slices only
// the opener line of a parenthesized block, so the specs on the lines below are
// never rendered — the header collapses to the bare keyword and carries no more
// structural signal than an `import (` block, yet still consumes a skeleton
// budget slot. Like imports, these are filtered so real func/type headers are
// not displaced. A single-line grouped block (`const ( X = 1 )`) keeps its specs
// on the opener line and so does not match.
func isGroupedDeclOpener(header string) bool {
	switch header {
	case "var (", "const (", "type (":
		return true
	}
	return false
}

// declHeader slices n's declaration header out of lines: everything from its
// start line up to the brace that opens its body, or the first line alone when
// the declaration has no body brace (a grouped `const (`/`var (` block, or a
// single-line type alias). The result is whitespace-collapsed onto one line so
// that a multi-line signature stays one skeleton entry, and so that no rendered
// line can begin with a payload marker prefix.
//
// ok is false when n's line range does not fit inside lines.
func declHeader(lines []string, n Node) (string, bool) {
	if n.StartLine < 1 || n.EndLine < n.StartLine || n.EndLine > len(lines) {
		return "", false
	}
	var text string
	if n.Kind == "func" {
		// A signature may span lines, so scan the whole declaration for the brace
		// that opens the body, skipping inline struct/interface types that can sit
		// at depth 0 in a result position.
		text = strings.Join(lines[n.StartLine-1:n.EndLine], "\n")
		if i := bodyBraceIndex(text); i >= 0 {
			text = text[:i]
		} else {
			// No depth-0 body brace was found (an unbalanced or unterminated
			// signature). Fall back to the first line, stripping a trailing `{`
			// so a false negative degrades to a truncated-but-clean signature
			// rather than leaking body text — mirroring the gendecl path below.
			text = strings.TrimRight(lines[n.StartLine-1], " \t\r")
			text = strings.TrimSuffix(text, "{")
		}
	} else {
		// A type/const/var declaration's header is its first line. A trailing `{`
		// there opens the declaration's own body (`type Config struct {`) and is
		// dropped; a line that merely CONTAINS braces is a complete declaration
		// (`type Alias = map[string]struct{}`) and is kept verbatim.
		text = strings.TrimRight(lines[n.StartLine-1], " \t\r")
		text = strings.TrimSuffix(text, "{")
	}
	header := strings.Join(strings.Fields(text), " ")
	if header == "" {
		return "", false
	}
	return header, true
}

// bodyBraceIndex returns the index of the brace that opens a declaration's body,
// or -1 when there is none.
//
// Kinds of content skipped so the signature is not truncated or stalled at the
// wrong place:
//   - line (`//`) and block (`/* */`) comments, so a brace, paren, or bracket
//     appearing in prose never affects brace matching;
//   - double-quoted, backtick, and rune literals (escapes honored for
//     double-quote and rune; backtick raw strings have none), so the same
//     characters inside literal text are ignored;
//   - braces nested inside parentheses or brackets, e.g. a struct-typed
//     parameter or a generic constraint `func F[T interface{ ~int }](v T) T`;
//   - a brace introducing an inline struct/interface TYPE, which can appear at
//     depth 0 in a result position — `func Any() interface{}`,
//     `func New() chan struct{}`, `type Alias = map[string]struct{}`. Truncating
//     there would emit an actively wrong signature like "func Any() interface".
func bodyBraceIndex(text string) int {
	depth := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '/':
			if i+1 < len(text) && text[i+1] == '/' {
				if j := strings.IndexByte(text[i:], '\n'); j >= 0 {
					i += j
				} else {
					i = len(text)
				}
				continue
			}
			if i+1 < len(text) && text[i+1] == '*' {
				if j := strings.Index(text[i+2:], "*/"); j >= 0 {
					i += 2 + j + 1
				} else {
					i = len(text)
				}
				continue
			}
		case '"', '`':
			i = skipStringLiteral(text, i)
		case '\'':
			i = skipRuneLiteral(text, i)
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case '{':
			if depth != 0 {
				continue
			}
			if !opensInlineType(text[:i]) {
				return i
			}
			// Skip the inline type's body and keep looking for the real one.
			end := matchingBrace(text, i)
			if end < 0 {
				return -1
			}
			i = end
		}
	}
	return -1
}

// skipStringLiteral returns the index of the quote closing the double-quoted
// or backtick string opened at text[start], or len(text)-1 when unterminated
// (so the caller's loop runs to the end without finding a body brace).
// Backslash escapes are honored only for double-quoted strings; backtick raw
// strings have none.
func skipStringLiteral(text string, start int) int {
	q := text[start]
	for i := start + 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			if q == '"' {
				i++
			}
		case q:
			return i
		}
	}
	return len(text) - 1
}

// skipRuneLiteral returns the index of the `'` closing the rune literal opened
// at text[start], or len(text)-1 when unterminated. A backslash escapes the
// following byte.
func skipRuneLiteral(text string, start int) int {
	for i := start + 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '\'':
			return i
		}
	}
	return len(text) - 1
}

// opensInlineType reports whether the brace following prefix belongs to an
// inline struct/interface type rather than a declaration body.
//
// The keyword must appear as a WHOLE token, not merely as a trailing substring:
// a named type like `*astruct` ends in the letters "struct" but is not an inline
// type, and mistaking its body brace for one makes matchingBrace consume the
// whole function body. A real inline type's keyword is preceded by start-of-text
// or a non-identifier byte (space, `*`, `]`, `.`), which is the discriminator.
func opensInlineType(prefix string) bool {
	t := strings.TrimRight(prefix, " \t\n")
	return endsWithKeyword(t, "struct") || endsWithKeyword(t, "interface")
}

// endsWithKeyword reports whether t ends with kw as a whole identifier token —
// kw is a suffix of t and is not immediately preceded by an identifier byte.
func endsWithKeyword(t, kw string) bool {
	if !strings.HasSuffix(t, kw) {
		return false
	}
	if len(t) == len(kw) {
		return true
	}
	return !isIdentByte(t[len(t)-len(kw)-1])
}

// isIdentByte reports whether b can appear inside a Go identifier.
func isIdentByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// matchingBrace returns the index of the `}` closing the `{` at open, or -1 when
// the text is unbalanced.
func matchingBrace(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// Cyclomatic returns the McCabe cyclomatic complexity of the tree rooted at
// root: the number of branch-kind nodes plus one. A tree with no branches scores
// 1, matching the convention that straight-line code has a single path.
//
// Applied to a FILE node this sums every branch in the file rather than scoring
// any one function, so it is not comparable to a per-function complexity
// threshold. Use MaxFuncCyclomatic to compare against such a threshold.
func Cyclomatic(root Node) int {
	return 1 + countBranches(root)
}

// MaxFuncCyclomatic returns the highest McCabe score of any single function in
// the tree, which is the measure conventional thresholds are written against.
//
// A file's branch total grows with its length, so thresholding on it would flag
// every long file regardless of whether any part of it is actually hard to
// follow. The maximum over functions instead answers "does this file contain a
// function too branchy to review from hunks alone", which is the question a
// payload-escalation decision turns on. Files with no function (or an empty
// tree) score 0, distinguishing "nothing to measure" from a real score of 1.
func MaxFuncCyclomatic(root Node) int {
	max := 0
	var walk func(n Node)
	walk = func(n Node) {
		if n.Kind == "func" || n.Kind == "funclit" {
			if c := Cyclomatic(n); c > max {
				max = c
			}
			// Keep descending: a funclit not nested under a func (e.g. one
			// assigned to a package-level var) is still measured as its own
			// unit, and a method on a nested type still surfaces here. Note a
			// parent func ABSORBS its closures' complexity: Cyclomatic sums
			// branch nodes over the whole subtree, so a func whose branches
			// all live in nested funclits scores them as its own (matching
			// gocyclo).
		}
		for i := range n.Children {
			walk(n.Children[i])
		}
	}
	walk(root)
	return max
}

func countBranches(n Node) int {
	total := 0
	if branchKinds[n.Kind] {
		total++
	}
	for i := range n.Children {
		total += countBranches(n.Children[i])
	}
	return total
}
