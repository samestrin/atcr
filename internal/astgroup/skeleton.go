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
func FileSkeleton(root Node, src []byte) []SkeletonEntry {
	if len(root.Children) == 0 || len(src) == 0 {
		return nil
	}
	lines := strings.Split(string(src), "\n")

	var entries []SkeletonEntry
	for _, ch := range root.Children {
		if !skeletonKinds[ch.Kind] {
			continue
		}
		header, ok := declHeader(lines, ch)
		if !ok || isImportHeader(header) {
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
	text := strings.Join(lines[n.StartLine-1:n.EndLine], "\n")
	if i := bodyBraceIndex(text); i >= 0 {
		text = text[:i]
	} else {
		text = lines[n.StartLine-1]
	}
	header := strings.Join(strings.Fields(text), " ")
	if header == "" {
		return "", false
	}
	return header, true
}

// bodyBraceIndex returns the index of the brace that opens a declaration's body,
// or -1 when there is none. Braces nested inside parentheses or brackets are
// skipped so that a generic constraint (`func F[T interface{ ~int }](v T) T`) or
// a struct-typed parameter does not truncate the signature at the wrong place.
func bodyBraceIndex(text string) int {
	depth := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case '{':
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
func Cyclomatic(root Node) int {
	return 1 + countBranches(root)
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
