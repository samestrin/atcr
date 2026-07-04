package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/require"
)

// fakeResolver is a Grouper that also resolves a fixed enclosing symbol, so the
// stamping logic can be tested without a wazero parse.
type fakeResolver struct{ name string }

func (f fakeResolver) GroupKey(reclib.Finding) string        { return "" }
func (f fakeResolver) EnclosingSymbol(reclib.Finding) string { return f.name }

// plainGrouper implements only reclib.Grouper (no EnclosingSymbol), modelling a
// proximity-only / AST-disabled grouper: stamping must be a no-op.
type plainGrouper struct{}

func (plainGrouper) GroupKey(reclib.Finding) string { return "" }

func TestSafeSymbolAnchor(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"classifyHeader", true},
		{"Foo::bar", true}, // scope resolution is table-safe
		{"parseValue2", true},
		{"", false},           // empty
		{"operator()", false}, // parens break the (symbol) parse
		{"a|b", false},        // pipe breaks the TD table
		{"has space", false},  // whitespace
		{"tab\tname", false},  // control/whitespace
		{"line\nbreak", false},
		{"open(", false},
		{"close)", false},
	}
	for _, c := range cases {
		require.Equalf(t, c.ok, safeSymbolAnchor(c.name), "safeSymbolAnchor(%q)", c.name)
	}
}

func TestStripSymbolAnchors(t *testing.T) {
	cases := []struct{ in, want string }{
		{"(classifyHeader) nil deref", "nil deref"}, // single anchor removed
		{"(a) (b) problem", "problem"},              // greedy: multiple anchors removed
		{"no anchor here", "no anchor here"},        // no anchor
		{"", ""},                                    // empty
		{"(x) ", ""},                                // anchor with empty remainder
		{"(bad name) foo", "(bad name) foo"},        // space in name => not a well-formed anchor
		{"(a|b) foo", "(a|b) foo"},                  // pipe in name => not well-formed
		{"(unterminated foo", "(unterminated foo"},  // no closing paren
		{"(x)nospace", "(x)nospace"},                // no space after ')'
		{"()  foo", "()  foo"},                      // empty name => not well-formed
	}
	for _, c := range cases {
		require.Equalf(t, c.want, StripSymbolAnchors(c.in), "StripSymbolAnchors(%q)", c.in)
	}
	// Round-trip: stripping exactly inverts stamping for any safe name.
	orig := "boundary bug"
	stamped := "(" + "parseValue" + ") " + orig
	require.Equal(t, orig, StripSymbolAnchors(stamped))
}

func TestStampSymbolAnchors_PrependsSafeName(t *testing.T) {
	jf := []JSONFinding{{File: "x.go", Line: 5, Problem: "nil deref possible"}}
	stampSymbolAnchors(jf, fakeResolver{name: "classifyHeader"})
	require.Equal(t, "(classifyHeader) nil deref possible", jf[0].Problem)
}

func TestStampSymbolAnchors_OmitsUnsafeName(t *testing.T) {
	jf := []JSONFinding{{File: "x.cpp", Line: 5, Problem: "leak"}}
	stampSymbolAnchors(jf, fakeResolver{name: "operator()"})
	require.Equal(t, "leak", jf[0].Problem, "unsafe name must leave Problem byte-identical")
}

func TestStampSymbolAnchors_OmitsWhenNoSymbol(t *testing.T) {
	jf := []JSONFinding{{File: "x.md", Line: 5, Problem: "typo"}}
	stampSymbolAnchors(jf, fakeResolver{name: ""})
	require.Equal(t, "typo", jf[0].Problem)
}

func TestStampSymbolAnchors_Idempotent(t *testing.T) {
	jf := []JSONFinding{{File: "x.go", Line: 5, Problem: "(classifyHeader) nil deref"}}
	stampSymbolAnchors(jf, fakeResolver{name: "classifyHeader"})
	require.Equal(t, "(classifyHeader) nil deref", jf[0].Problem, "already-anchored Problem must not be double-stamped")
}

func TestStampSymbolAnchors_NonResolverGrouper_NoOp(t *testing.T) {
	jf := []JSONFinding{{File: "x.go", Line: 5, Problem: "boom"}}
	stampSymbolAnchors(jf, plainGrouper{})
	require.Equal(t, "boom", jf[0].Problem)
	stampSymbolAnchors(jf, nil) // nil grouper (AST grouping disabled) must also no-op
	require.Equal(t, "boom", jf[0].Problem)
}

// TestStampSymbolAnchors_RealGrouper confirms the concrete *lazyGrouper returned
// by astGrouperFor satisfies the symbolResolver assertion and stamps end-to-end
// through a real wazero parse.
func TestStampSymbolAnchors_RealGrouper(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nfunc Anchor() {\n\tx := 1\n\t_ = x\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "code.go"), []byte(src), 0o644))

	grouper, cleanup := astGrouperFor(dir)
	defer cleanup()

	jf := []JSONFinding{{File: "code.go", Line: 4, Problem: "unused write"}}
	stampSymbolAnchors(jf, grouper)
	require.Equal(t, "(Anchor) unused write", jf[0].Problem)
}
