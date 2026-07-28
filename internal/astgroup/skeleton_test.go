package astgroup

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const skeletonSrc = `package p

import "fmt"

type Mode string

type Config struct {
	A int
	B string
}

const (
	X Mode = "x"
	Y Mode = "y"
)

func Simple() {}

func Multi(
	a int,
	b string,
) (int, error) {
	if a > 0 {
		for i := 0; i < a; i++ {
			fmt.Println(i)
		}
	}
	switch b {
	case "x":
	}
	return a, nil
}

func Generic[T interface{ ~int }](v T) T { return v }
`

func parseGoForSkeleton(t *testing.T, src string) Node {
	t.Helper()
	h := NewHost()
	t.Cleanup(func() { _ = h.Close() })
	p, err := h.Parser("go")
	require.NoError(t, err)
	root, err := p.Parse([]byte(src))
	require.NoError(t, err)
	return root
}

func headersOf(entries []SkeletonEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Header)
	}
	return out
}

func TestFileSkeleton_ExtractsTopLevelHeaders(t *testing.T) {
	root := parseGoForSkeleton(t, skeletonSrc)

	entries := FileSkeleton(root, skeletonSrc)

	// Import declarations are excluded: they carry no structural signal a
	// reviewer needs and would be pure noise in every skeleton. The bare grouped
	// `const (` opener is excluded for the same reason (its specs live on the
	// lines below, which declHeader does not slice).
	require.Equal(t, []string{
		"type Mode string",
		"type Config struct",
		"func Simple()",
		"func Multi( a int, b string, ) (int, error)",
		"func Generic[T interface{ ~int }](v T) T",
	}, headersOf(entries))
}

func TestFileSkeleton_CarriesKindNameAndLine(t *testing.T) {
	root := parseGoForSkeleton(t, skeletonSrc)

	entries := FileSkeleton(root, skeletonSrc)
	require.NotEmpty(t, entries)

	byName := map[string]SkeletonEntry{}
	for _, e := range entries {
		if e.Name != "" {
			byName[e.Name] = e
		}
	}

	simple, ok := byName["Simple"]
	require.True(t, ok, "func Simple should be named in the skeleton")
	require.Equal(t, "func", simple.Kind)
	require.Equal(t, 17, simple.StartLine, "1-based line of `func Simple() {}`")

	// gendecl nodes carry no Name (the Go parser emits names only for FuncDecl),
	// so the header text is the only identifier the reviewer gets.
	var gendecls []SkeletonEntry
	for _, e := range entries {
		if e.Kind == "gendecl" {
			gendecls = append(gendecls, e)
		}
	}
	// type Mode and type Config remain; the bare grouped `const (` opener is
	// filtered as signal-free noise.
	require.Len(t, gendecls, 2)
	for _, g := range gendecls {
		require.Empty(t, g.Name)
	}
}

func TestFileSkeleton_MethodsIncludeReceiver(t *testing.T) {
	src := "package p\n\ntype T struct{}\n\nfunc (t *T) Do(x int) error { return nil }\n"
	root := parseGoForSkeleton(t, src)

	entries := FileSkeleton(root, src)

	require.Contains(t, headersOf(entries), "func (t *T) Do(x int) error")
}

func TestFileSkeleton_EmptyAndNoDeclarations(t *testing.T) {
	src := "package p\n"
	root := parseGoForSkeleton(t, src)

	require.Empty(t, FileSkeleton(root, src))
	require.Empty(t, FileSkeleton(Node{}, ""))
}

func TestFileSkeleton_NodeRangeOutsideSourceIsSkipped(t *testing.T) {
	// A tree whose line numbers do not correspond to src must not panic or
	// index out of range — the payload builder parses HEAD content, and a
	// mismatch (CRLF, truncation) must degrade to an empty skeleton.
	root := Node{
		Kind: "file", StartLine: 1, EndLine: 500,
		Children: []Node{{Kind: "func", Name: "Ghost", StartLine: 400, EndLine: 420}},
	}

	require.Empty(t, FileSkeleton(root, "package p\n"))
}

func TestFileSkeleton_CRLFSourceHasNoStrayCarriageReturn(t *testing.T) {
	// git can hand back CRLF content. A stray \r surviving into a header would
	// corrupt the rendered payload line the builder emits.
	src := "package p\r\n\r\nfunc A(x int) error {\r\n\treturn nil\r\n}\r\n"
	root := parseGoForSkeleton(t, src)

	entries := FileSkeleton(root, src)

	require.Equal(t, []string{"func A(x int) error"}, headersOf(entries))
	for _, e := range entries {
		require.NotContains(t, e.Header, "\r")
		require.NotContains(t, e.Header, "\n")
	}
}

func TestFileSkeleton_BodylessFuncKeepsFullSignature(t *testing.T) {
	// A declaration with no body brace (assembly-backed func) must fall back to
	// the whole first line, not be truncated to nothing. The trailing bare grouped
	// `var (` opener is filtered as signal-free noise.
	src := "package p\n\nfunc add(x, y int) int\n\nvar (\n\tA = 1\n)\n"
	root := parseGoForSkeleton(t, src)

	require.Equal(t, []string{"func add(x, y int) int"}, headersOf(FileSkeleton(root, src)))
}

func TestFileSkeleton_HeadersAreSingleLine(t *testing.T) {
	// The payload builder renders one line per entry and relies on that: a
	// header containing a newline could inject a line that collides with a
	// payload section marker.
	root := parseGoForSkeleton(t, skeletonSrc)

	for _, e := range FileSkeleton(root, skeletonSrc) {
		require.NotContains(t, e.Header, "\n", "header %q must be single-line", e.Header)
	}
}

func TestFileSkeleton_GroupedDeclOpenersAreFiltered(t *testing.T) {
	// A grouped `var (` / `const (` / `type (` block renders only its opener line,
	// so declHeader collapses it to the bare keyword `var (` etc. — no names, no
	// types, zero structural signal. Two var blocks and a const block would emit
	// three indistinguishable headers that consume skeleton budget and can displace
	// the func headers the skeleton exists to show. Like `import (`, they are filtered.
	src := `package p

var (
	A = 1
	B = 2
)

const (
	X = "x"
)

var (
	C = 3
)

func Keep() {}
`
	root := parseGoForSkeleton(t, src)

	headers := headersOf(FileSkeleton(root, src))
	require.Equal(t, []string{"func Keep()"}, headers,
		"bare grouped decl openers must be filtered, leaving only real signal")
}

func TestFileSkeleton_IdentifierEndingInStructKeywordIsNotInlineType(t *testing.T) {
	// opensInlineType must match `struct`/`interface` as whole tokens, not as a
	// trailing SUBSTRING. `*astruct` ends in "struct" but is an ordinary named
	// type: mistaking its body brace for an inline type makes matchingBrace eat
	// the whole function body, bodyBraceIndex returns -1, and declHeader would
	// leak raw body code (and a truncated `}` string literal) into the header.
	src := `package p

type astruct struct{ x int }
type myinterface interface{ M() }

func A(k string) *astruct {
	if k == "}" {
		return nil
	}
	return &astruct{}
}

func B() myinterface {
	return nil
}

func C() astruct { return astruct{} }
`
	root := parseGoForSkeleton(t, src)

	require.Equal(t, []string{
		"type astruct struct{ x int }",
		"type myinterface interface{ M() }",
		"func A(k string) *astruct",
		"func B() myinterface",
		"func C() astruct",
	}, headersOf(FileSkeleton(root, src)))
}

func TestOpensInlineType_RequiresWholeTokenKeyword(t *testing.T) {
	// Whole-token keyword preceded by a non-identifier byte (or start-of-text)
	// is a real inline type; a keyword that is the tail of a longer identifier
	// is not.
	require.True(t, opensInlineType("func Any() interface"))
	require.True(t, opensInlineType("func New() chan struct"))
	require.True(t, opensInlineType("func F() *struct"))
	require.True(t, opensInlineType("struct"))
	require.False(t, opensInlineType("func N() *astruct"))
	require.False(t, opensInlineType("func B() myinterface"))
}

func TestCyclomatic_CountsBranchNodesPlusOne(t *testing.T) {
	root := parseGoForSkeleton(t, skeletonSrc)

	// skeletonSrc has one if, one for, one switch -> 3 branch nodes + 1.
	require.Equal(t, 4, Cyclomatic(root))
}

func TestCyclomatic_StraightLineCodeIsOne(t *testing.T) {
	src := "package p\n\nfunc A() int { return 1 }\n"
	root := parseGoForSkeleton(t, src)

	require.Equal(t, 1, Cyclomatic(root))
}

func TestCyclomatic_CountsNestedBranches(t *testing.T) {
	src := `package p

func A(n int) int {
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			if i > 10 {
				return i
			}
		}
	}
	return 0
}
`
	root := parseGoForSkeleton(t, src)

	// for + if + if = 3 branch nodes + 1.
	require.Equal(t, 4, Cyclomatic(root))
}

func TestMaxFuncCyclomatic_IsPerFunctionNotWholeFile(t *testing.T) {
	// Twenty trivial functions: the whole-file branch SUM is 21, but no single
	// function is complex. A per-function measure must report 1, or every long
	// file would read as complex regardless of how simple its parts are.
	var b strings.Builder
	b.WriteString("package p\n")
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&b, "\nfunc F%d(n int) int {\n\tif n > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n", i)
	}
	src := b.String()
	root := parseGoForSkeleton(t, src)

	require.Equal(t, 21, Cyclomatic(root), "whole-file sum grows with file length")
	require.Equal(t, 2, MaxFuncCyclomatic(root), "each function has exactly one branch")
}

func TestMaxFuncCyclomatic_FindsTheBranchiestFunction(t *testing.T) {
	src := `package p

func Simple() int { return 1 }

func Branchy(n int) int {
	if n > 1 {
		return 1
	}
	if n > 2 {
		return 2
	}
	for i := 0; i < n; i++ {
		if i > 3 {
			return i
		}
	}
	switch n {
	case 1:
	}
	return 0
}
`
	root := parseGoForSkeleton(t, src)

	// Branchy: if, if, for, if, switch = 5 branches + 1.
	require.Equal(t, 6, MaxFuncCyclomatic(root))
}

func TestMaxFuncCyclomaticInRanges_OnlyCountsOverlappingFunctions(t *testing.T) {
	// A file with one trivial function and one very branchy one. Scoping the
	// complexity to the CHANGED region must report only the score of the function
	// the change overlaps: a one-line edit in the trivial helper must NOT surface
	// the branchy function's score (Epic 35.1 TD — the file-wide max fired on
	// unchanged complex code elsewhere in the file).
	src := `package p

func Trivial() int { return 1 }

func Branchy(n int) int {
	if n > 1 {
		return 1
	}
	if n > 2 {
		return 2
	}
	for i := 0; i < n; i++ {
		if i > 3 {
			return i
		}
	}
	return 0
}
`
	root := parseGoForSkeleton(t, src)
	full := MaxFuncCyclomatic(root) // == Branchy's score, the file-wide max
	require.Greater(t, full, 1, "Branchy must be the branchier function")

	// Trivial() is on line 3; a change overlapping only it scores 1.
	require.Equal(t, 1, MaxFuncCyclomaticInRanges(root, [][2]int{{3, 3}}),
		"a change overlapping only the trivial function must not report Branchy's score")
	// A change overlapping Branchy (its first `if` is on line 6) reports its score.
	require.Equal(t, full, MaxFuncCyclomaticInRanges(root, [][2]int{{6, 7}}),
		"a change overlapping the branchy function reports its full score")
	// No changed ranges → nothing overlaps → 0.
	require.Equal(t, 0, MaxFuncCyclomaticInRanges(root, nil))
}

func TestMaxFuncCyclomatic_NoFunctionsScoresZero(t *testing.T) {
	root := parseGoForSkeleton(t, "package p\n\ntype T struct{ A int }\n")

	require.Equal(t, 0, MaxFuncCyclomatic(root), "no function means nothing to measure, not a score of 1")
	require.Equal(t, 0, MaxFuncCyclomatic(Node{}))
}

func TestMaxFuncCyclomatic_ParentAbsorbsNestedClosureBranches(t *testing.T) {
	// Cyclomatic sums branch nodes over a node's whole subtree, so a function
	// whose only branches live inside nested closures absorbs them — matching
	// gocyclo. A dispatch-style func declaring many one-branch closures
	// therefore escalates on its own score.
	src := `package p

func Dispatch(n int) int {
	f := func() int {
		if n > 1 {
			return 1
		}
		return 0
	}
	g := func() int {
		if n > 2 {
			return 2
		}
		return 0
	}
	return f() + g()
}
`
	root := parseGoForSkeleton(t, src)

	// Dispatch's own body has zero branches, but the two closures contribute
	// one if each to its subtree: 2 branches + 1.
	require.Equal(t, 3, MaxFuncCyclomatic(root), "parent absorbs its closures' complexity")
}

func TestBodyBraceIndex_SkipsBlockComment(t *testing.T) {
	// A `{` inside a block comment must not be mistaken for the body brace, and
	// must not stop the scan from finding the real one after the comment.
	text := `func F() int /* {x} */ {`

	require.Equal(t, strings.LastIndex(text, "{"), bodyBraceIndex(text))
}

func TestBodyBraceIndex_LineCommentParenDoesNotStallSearch(t *testing.T) {
	// A `(` inside a line comment must not increment the paren depth counter,
	// or the real body brace after it is never found (depth stays > 0).
	text := "func F(\n\ta int, // opens a paren ( here\n\tb string,\n) int {"

	require.Equal(t, strings.LastIndex(text, "{"), bodyBraceIndex(text))
}

func TestBodyBraceIndex_DoubleQuoteEscapedQuoteDoesNotEndStringEarly(t *testing.T) {
	// `\"` inside a double-quoted string is an escaped quote, not the string's
	// end — and the `(` inside the string must not affect paren depth either.
	text := `"a\"(" {`

	require.Equal(t, strings.LastIndex(text, "{"), bodyBraceIndex(text))
}

func TestBodyBraceIndex_BacktickStringContentIsIgnored(t *testing.T) {
	text := "`(` {"

	require.Equal(t, strings.LastIndex(text, "{"), bodyBraceIndex(text))
}

func TestBodyBraceIndex_RuneLiteralContentIsIgnored(t *testing.T) {
	text := `'(' {`

	require.Equal(t, strings.LastIndex(text, "{"), bodyBraceIndex(text))
}

func TestFileSkeleton_BlockCommentBeforeBodyDoesNotTruncateHeader(t *testing.T) {
	// End-to-end version of TestBodyBraceIndex_SkipsBlockComment through the
	// public FileSkeleton API: the header must include the full signature up
	// to the real body brace, not truncate mid-comment.
	src := "package p\n\nfunc F() int /* {x} */ {\n\treturn 1\n}\n"
	root := parseGoForSkeleton(t, src)

	require.Equal(t, []string{"func F() int /* {x} */"}, headersOf(FileSkeleton(root, src)))
}

func TestFileSkeleton_InlineTypeInResultDoesNotTruncateSignature(t *testing.T) {
	// A depth-0 `{` can belong to an inline struct/interface TYPE rather than the
	// declaration body. Truncating there emits an actively wrong signature.
	src := `package p

func Any() interface{} { return nil }

func New() chan struct{} { return nil }

func M() map[string]struct{ A int } { return nil }

type Alias = map[string]struct{}
`
	root := parseGoForSkeleton(t, src)

	require.Equal(t, []string{
		"func Any() interface{}",
		"func New() chan struct{}",
		"func M() map[string]struct{ A int }",
		"type Alias = map[string]struct{}",
	}, headersOf(FileSkeleton(root, src)))
}
