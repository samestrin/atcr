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

	entries := FileSkeleton(root, []byte(skeletonSrc))

	// Import declarations are excluded: they carry no structural signal a
	// reviewer needs and would be pure noise in every skeleton.
	require.Equal(t, []string{
		"type Mode string",
		"type Config struct",
		"const (",
		"func Simple()",
		"func Multi( a int, b string, ) (int, error)",
		"func Generic[T interface{ ~int }](v T) T",
	}, headersOf(entries))
}

func TestFileSkeleton_CarriesKindNameAndLine(t *testing.T) {
	root := parseGoForSkeleton(t, skeletonSrc)

	entries := FileSkeleton(root, []byte(skeletonSrc))
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
	require.Len(t, gendecls, 3)
	for _, g := range gendecls {
		require.Empty(t, g.Name)
	}
}

func TestFileSkeleton_MethodsIncludeReceiver(t *testing.T) {
	src := "package p\n\ntype T struct{}\n\nfunc (t *T) Do(x int) error { return nil }\n"
	root := parseGoForSkeleton(t, src)

	entries := FileSkeleton(root, []byte(src))

	require.Contains(t, headersOf(entries), "func (t *T) Do(x int) error")
}

func TestFileSkeleton_EmptyAndNoDeclarations(t *testing.T) {
	src := "package p\n"
	root := parseGoForSkeleton(t, src)

	require.Empty(t, FileSkeleton(root, []byte(src)))
	require.Empty(t, FileSkeleton(Node{}, nil))
}

func TestFileSkeleton_NodeRangeOutsideSourceIsSkipped(t *testing.T) {
	// A tree whose line numbers do not correspond to src must not panic or
	// index out of range — the payload builder parses HEAD content, and a
	// mismatch (CRLF, truncation) must degrade to an empty skeleton.
	root := Node{
		Kind: "file", StartLine: 1, EndLine: 500,
		Children: []Node{{Kind: "func", Name: "Ghost", StartLine: 400, EndLine: 420}},
	}

	require.Empty(t, FileSkeleton(root, []byte("package p\n")))
}

func TestFileSkeleton_CRLFSourceHasNoStrayCarriageReturn(t *testing.T) {
	// git can hand back CRLF content. A stray \r surviving into a header would
	// corrupt the rendered payload line the builder emits.
	src := "package p\r\n\r\nfunc A(x int) error {\r\n\treturn nil\r\n}\r\n"
	root := parseGoForSkeleton(t, src)

	entries := FileSkeleton(root, []byte(src))

	require.Equal(t, []string{"func A(x int) error"}, headersOf(entries))
	for _, e := range entries {
		require.NotContains(t, e.Header, "\r")
		require.NotContains(t, e.Header, "\n")
	}
}

func TestFileSkeleton_BodylessFuncKeepsFullSignature(t *testing.T) {
	// A declaration with no body brace (assembly-backed func) must fall back to
	// the whole first line, not be truncated to nothing.
	src := "package p\n\nfunc add(x, y int) int\n\nvar (\n\tA = 1\n)\n"
	root := parseGoForSkeleton(t, src)

	require.Equal(t, []string{"func add(x, y int) int", "var ("}, headersOf(FileSkeleton(root, []byte(src))))
}

func TestFileSkeleton_HeadersAreSingleLine(t *testing.T) {
	// The payload builder renders one line per entry and relies on that: a
	// header containing a newline could inject a line that collides with a
	// payload section marker.
	root := parseGoForSkeleton(t, skeletonSrc)

	for _, e := range FileSkeleton(root, []byte(skeletonSrc)) {
		require.NotContains(t, e.Header, "\n", "header %q must be single-line", e.Header)
	}
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

func TestMaxFuncCyclomatic_NoFunctionsScoresZero(t *testing.T) {
	root := parseGoForSkeleton(t, "package p\n\ntype T struct{ A int }\n")

	require.Equal(t, 0, MaxFuncCyclomatic(root), "no function means nothing to measure, not a score of 1")
	require.Equal(t, 0, MaxFuncCyclomatic(Node{}))
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
	}, headersOf(FileSkeleton(root, []byte(src))))
}
