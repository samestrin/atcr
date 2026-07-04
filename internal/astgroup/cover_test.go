package astgroup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/require"
)

// TestEnclosingSymbolName verifies the nearest-enclosing-NAMED-block walk: from
// the deepest covering block up to the root, return the first node with a
// non-empty Name (a func/method/class/type), skipping unnamed control-flow blocks
// (if/for/…). Returns ok=false when no covering node is named or the line is
// outside the root — the AC2 graceful-degradation signal.
func TestEnclosingSymbolName(t *testing.T) {
	tree := Node{Kind: "file", StartLine: 1, EndLine: 20, Children: []Node{
		{Kind: "func", Name: "A", StartLine: 3, EndLine: 8, Children: []Node{
			{Kind: "if", StartLine: 5, EndLine: 7},
		}},
		{Kind: "func", Name: "B", StartLine: 12, EndLine: 18},
	}}

	// Nested unnamed `if` inside func A → walk up past the if to the named func.
	name, ok := EnclosingSymbolName(tree, 6)
	require.True(t, ok)
	require.Equal(t, "A", name)

	// Directly in func A body (not in the if) → func A.
	name, ok = EnclosingSymbolName(tree, 4)
	require.True(t, ok)
	require.Equal(t, "A", name)

	// In func B.
	name, ok = EnclosingSymbolName(tree, 15)
	require.True(t, ok)
	require.Equal(t, "B", name)

	// In the file but between funcs → only the unnamed file node covers → omit.
	name, ok = EnclosingSymbolName(tree, 10)
	require.False(t, ok)
	require.Equal(t, "", name)

	// Line outside the whole file → omit.
	name, ok = EnclosingSymbolName(tree, 99)
	require.False(t, ok)
	require.Equal(t, "", name)
}

// TestEnclosingSymbolName_DeepestNamedWins verifies that when several named blocks
// nest (class > method > unnamed loop), the DEEPEST named block wins — the anchor
// should be the tightest identifier around the finding, not the outermost.
func TestEnclosingSymbolName_DeepestNamedWins(t *testing.T) {
	tree := Node{Kind: "file", StartLine: 1, EndLine: 30, Children: []Node{
		{Kind: "class", Name: "C", StartLine: 2, EndLine: 28, Children: []Node{
			{Kind: "func", Name: "M", StartLine: 5, EndLine: 12, Children: []Node{
				{Kind: "for", StartLine: 7, EndLine: 10},
			}},
		}},
	}}

	// Inside the unnamed for, inside method M, inside class C → M (deepest named).
	name, ok := EnclosingSymbolName(tree, 8)
	require.True(t, ok)
	require.Equal(t, "M", name)

	// In class C but before method M → C.
	name, ok = EnclosingSymbolName(tree, 3)
	require.True(t, ok)
	require.Equal(t, "C", name)

	// A named func nested in a named func → the inner name wins.
	nested := Node{Kind: "file", StartLine: 1, EndLine: 20, Children: []Node{
		{Kind: "func", Name: "Outer", StartLine: 2, EndLine: 18, Children: []Node{
			{Kind: "func", Name: "Inner", StartLine: 6, EndLine: 12},
		}},
	}}
	name, ok = EnclosingSymbolName(nested, 8)
	require.True(t, ok)
	require.Equal(t, "Inner", name)
}

// TestGrouper_EnclosingSymbol exercises the full path: parse a real source file
// via the wazero-backed Host and resolve a finding's line to its enclosing symbol.
func TestGrouper_EnclosingSymbol(t *testing.T) {
	dir := t.TempDir()
	src := `package p

func Classify() {
	if true {
		x := 1
		_ = x
	}
}
`
	path := filepath.Join(dir, "code.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	g := NewGrouper(dir)
	defer func() { _ = g.Close() }()

	// Line 5 (x := 1) sits inside an if inside func Classify → "Classify".
	require.Equal(t, "Classify", g.EnclosingSymbol(reconcile.Finding{File: "code.go", Line: 5}))

	// Line 1 (package clause) is in no named block → "".
	require.Equal(t, "", g.EnclosingSymbol(reconcile.Finding{File: "code.go", Line: 1}))

	// File-level finding (Line <= 0) → "".
	require.Equal(t, "", g.EnclosingSymbol(reconcile.Finding{File: "code.go", Line: 0}))

	// Unsupported extension → "" (no parser, AC2 degradation).
	require.Equal(t, "", g.EnclosingSymbol(reconcile.Finding{File: "README.md", Line: 3}))

	// Missing file → "".
	require.Equal(t, "", g.EnclosingSymbol(reconcile.Finding{File: "nope.go", Line: 3}))
}
