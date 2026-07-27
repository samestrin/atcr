package astgroup

import (
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
