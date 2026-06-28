package astgroup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/require"
)

func TestSmallestCovering(t *testing.T) {
	tree := Node{Kind: "file", StartLine: 1, EndLine: 20, Children: []Node{
		{Kind: "func", Name: "A", StartLine: 3, EndLine: 8, Children: []Node{
			{Kind: "if", StartLine: 5, EndLine: 7},
		}},
		{Kind: "func", Name: "B", StartLine: 12, EndLine: 18},
	}}

	// Line inside the nested if → deepest node is the if.
	cov := SmallestCovering(tree, 6)
	require.NotNil(t, cov)
	require.Equal(t, "if", cov.Kind)

	// Line in func A but outside the if → func A.
	cov = SmallestCovering(tree, 4)
	require.NotNil(t, cov)
	require.Equal(t, "A", cov.Name)

	// Line in func B.
	cov = SmallestCovering(tree, 15)
	require.Equal(t, "B", cov.Name)

	// Line in the file but between funcs (line 10) → the file node itself.
	cov = SmallestCovering(tree, 10)
	require.Equal(t, "file", cov.Kind)

	// Line outside the whole file.
	require.Nil(t, SmallestCovering(tree, 99))
}

func TestMerkleHash_InvariantToLineNumbers(t *testing.T) {
	// Same structure, different line numbers (whitespace / blank-line drift).
	a := Node{Kind: "func", Name: "F", StartLine: 10, EndLine: 14, Children: []Node{
		{Kind: "return", StartLine: 11, EndLine: 11},
	}}
	b := Node{Kind: "func", Name: "F", StartLine: 40, EndLine: 44, Children: []Node{
		{Kind: "return", StartLine: 41, EndLine: 41},
	}}
	require.Equal(t, MerkleHash(a), MerkleHash(b), "line numbers must not affect the structural hash")
}

func TestMerkleHash_DistinguishesNameAndShape(t *testing.T) {
	f := Node{Kind: "func", Name: "F", StartLine: 1, EndLine: 2}
	g := Node{Kind: "func", Name: "G", StartLine: 1, EndLine: 2}
	require.NotEqual(t, MerkleHash(f), MerkleHash(g), "different names hash differently")

	shallow := Node{Kind: "func", Name: "F", StartLine: 1, EndLine: 2}
	deep := Node{Kind: "func", Name: "F", StartLine: 1, EndLine: 4, Children: []Node{{Kind: "if", StartLine: 2, EndLine: 3}}}
	require.NotEqual(t, MerkleHash(shallow), MerkleHash(deep), "different shapes hash differently")
}

// TestGrouper_GroupsAcrossLineDrift is the AC3 behavior: two findings inside the
// same function but at drifted line numbers produce the same GroupKey.
func TestGrouper_GroupsAcrossLineDrift(t *testing.T) {
	dir := t.TempDir()
	src := `package p

func Target() {
	x := 1
	y := 2
	_ = x + y
}

func Other() {
	z := 3
	_ = z
}
`
	path := filepath.Join(dir, "code.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	g := NewGrouper(dir)
	defer func() { _ = g.Close() }()

	// Two findings inside Target() at different lines.
	k1 := g.GroupKey(reconcile.Finding{File: "code.go", Line: 4})
	k2 := g.GroupKey(reconcile.Finding{File: "code.go", Line: 6})
	require.NotEmpty(t, k1)
	require.Equal(t, k1, k2, "findings in the same function group despite a line gap")

	// A finding in Other() differs.
	k3 := g.GroupKey(reconcile.Finding{File: "code.go", Line: 10})
	require.NotEmpty(t, k3)
	require.NotEqual(t, k1, k3, "findings in different functions do not group")
}

func TestGrouper_EmptyKeyTriggersFallback(t *testing.T) {
	g := NewGrouper(t.TempDir())
	defer func() { _ = g.Close() }()

	// File-level finding (Line <= 0): no structural key.
	require.Empty(t, g.GroupKey(reconcile.Finding{File: "code.go", Line: 0}))
	// Unsupported language.
	require.Empty(t, g.GroupKey(reconcile.Finding{File: "README.md", Line: 5}))
	// Missing file.
	require.Empty(t, g.GroupKey(reconcile.Finding{File: "nope.go", Line: 5}))
}

func TestGrouper_RefusesPathOutsideRoot(t *testing.T) {
	root := t.TempDir()
	// A real Go file outside root that a traversal would otherwise reach.
	outside := filepath.Dir(root)
	secret := filepath.Join(outside, "secret.go")
	require.NoError(t, os.WriteFile(secret, []byte("package p\nfunc S() {}\n"), 0o644))
	defer func() { _ = os.Remove(secret) }()

	g := NewGrouper(root)
	defer func() { _ = g.Close() }()

	// "../secret.go" escapes root → refused → empty key (proximity fallback).
	require.Empty(t, g.GroupKey(reconcile.Finding{File: "../secret.go", Line: 2}))
	// Absolute path outside root → refused.
	require.Empty(t, g.GroupKey(reconcile.Finding{File: secret, Line: 2}))
}

// TestGrouper_CanonicalPathDeduplicatesSpellings verifies that relative,
// absolute, and symlinked spellings of the same on-disk file share one parsed
// tree cache entry and produce the same group key.
func TestGrouper_CanonicalPathDeduplicatesSpellings(t *testing.T) {
	root := t.TempDir()
	src := "package p\n\nfunc F() {\n\tx := 1\n}\n"
	real := filepath.Join(root, "real.go")
	require.NoError(t, os.WriteFile(real, []byte(src), 0o644))
	link := filepath.Join(root, "link.go")
	require.NoError(t, os.Symlink(real, link))

	g := NewGrouper(root)
	defer func() { _ = g.Close() }()

	kRel := g.GroupKey(reconcile.Finding{File: "real.go", Line: 4})
	kAbs := g.GroupKey(reconcile.Finding{File: real, Line: 4})
	kLink := g.GroupKey(reconcile.Finding{File: "link.go", Line: 4})

	require.NotEmpty(t, kRel)
	require.Equal(t, kRel, kAbs, "relative and absolute spellings must share a group key")
	require.Equal(t, kRel, kLink, "symlink spelling must share a group key")

	// Only one parse should have been performed despite three GroupKey calls.
	require.Equal(t, 1, len(g.cache), "canonical path should deduplicate cache entries")
}

// TestGrouper_SatisfiesReconcileInterface is a compile-time assertion that the
// astgroup grouper plugs into the reconcile seam.
func TestGrouper_SatisfiesReconcileInterface(t *testing.T) {
	var _ reconcile.Grouper = (*Grouper)(nil)
}
