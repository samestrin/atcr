package astgroup

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

// TestCoveringBlock_DistinguishesSiblingNonBlockWrappers reproduces the cover.go
// bug where coveringChain discarded the accumulated sibling position when it
// descended through a non-block covering child. Two sibling expression
// statements, each wrapping an identically-shaped anonymous function literal,
// must yield DISTINCT structural addresses so their group keys do not collapse.
func TestCoveringBlock_DistinguishesSiblingNonBlockWrappers(t *testing.T) {
	// file
	//   exprstmt (3-5)  -> call -> funclit { return }
	//   exprstmt (7-9)  -> call -> funclit { return }
	mkWrapper := func(start, end int) Node {
		return Node{Kind: "exprstmt", StartLine: start, EndLine: end, Children: []Node{
			{Kind: "call", StartLine: start, EndLine: end, Children: []Node{
				{Kind: "funclit", StartLine: start, EndLine: end, Children: []Node{
					{Kind: "return", StartLine: start + 1, EndLine: start + 1},
				}},
			}},
		}}
	}
	tree := Node{Kind: "file", StartLine: 1, EndLine: 20, Children: []Node{
		mkWrapper(3, 5),
		mkWrapper(7, 9),
	}}

	_, addrA, okA := CoveringBlock(tree, 4)
	_, addrB, okB := CoveringBlock(tree, 8)
	require.True(t, okA)
	require.True(t, okB)
	require.NotEqual(t, addrA, addrB,
		"sibling non-block wrappers must produce distinct covering-block addresses")
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

// TestGrouper_MemoizesKeyAndHash verifies the per-finding caching: many findings
// in one covering block compute the structural Merkle hash exactly once (cached
// by address) and every queried line is memoized, instead of re-walking and
// re-hashing the subtree on every finding.
func TestGrouper_MemoizesKeyAndHash(t *testing.T) {
	dir := t.TempDir()
	src := `package p

func Big() {
	a := 1
	b := 2
	c := 3
	d := 4
	_ = a + b + c + d
}
`
	path := filepath.Join(dir, "big.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	g := NewGrouper(dir)
	defer func() { _ = g.Close() }()

	var keys []string
	for line := 4; line <= 9; line++ {
		k := g.GroupKey(reconcile.Finding{File: "big.go", Line: line})
		require.NotEmpty(t, k)
		keys = append(keys, k)
	}
	// All findings sit in Big()'s body block → identical key.
	for _, k := range keys[1:] {
		require.Equal(t, keys[0], k, "findings in one block must share a key")
	}

	cp, ok := g.canonicalPath(filepath.Clean("big.go"))
	require.True(t, ok)
	pf := g.cache[cp]
	require.NotNil(t, pf)
	require.Len(t, pf.hashByAddr, 1, "Merkle hash memoized once for the shared covering block")
	require.Len(t, pf.keyByLine, 6, "each queried finding line memoized")

	// Re-querying a cached line returns the same key (fast path).
	require.Equal(t, keys[0], g.GroupKey(reconcile.Finding{File: "big.go", Line: 4}))
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
	linkSupported := true
	if err := os.Symlink(real, link); err != nil {
		linkSupported = false
	}

	g := NewGrouper(root)
	defer func() { _ = g.Close() }()

	kRel := g.GroupKey(reconcile.Finding{File: "real.go", Line: 4})
	kAbs := g.GroupKey(reconcile.Finding{File: real, Line: 4})

	require.NotEmpty(t, kRel)
	require.Equal(t, kRel, kAbs, "relative and absolute spellings must share a group key")

	expectedCache := 1
	if linkSupported {
		kLink := g.GroupKey(reconcile.Finding{File: "link.go", Line: 4})
		require.Equal(t, kRel, kLink, "symlink spelling must share a group key")
	} else {
		// Symlinks unavailable on this platform; only relative+absolute dedup.
		expectedCache = 1
	}

	// Only one parse should have been performed for the unique on-disk file.
	require.Equal(t, expectedCache, len(g.cache), "canonical path should deduplicate cache entries")
}

// TestGrouper_RetriesTransientReadErrors verifies that a transient read failure
// is not cached permanently; the next GroupKey call retries and succeeds.
func TestGrouper_RetriesTransientReadErrors(t *testing.T) {
	root := t.TempDir()
	src := "package p\n\nfunc F() {\n\tx := 1\n}\n"
	path := filepath.Join(root, "code.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	g := NewGrouper(root)
	defer func() { _ = g.Close() }()

	var calls int
	transientErr := errors.New("transient: resource temporarily unavailable")
	g.readFile = func(name string) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, transientErr
		}
		return os.ReadFile(name)
	}

	// First call hits the transient error and falls back to proximity.
	require.Empty(t, g.GroupKey(reconcile.Finding{File: "code.go", Line: 4}))
	// Second call retries and produces a structural key.
	require.NotEmpty(t, g.GroupKey(reconcile.Finding{File: "code.go", Line: 4}))
	require.Equal(t, 2, calls, "second GroupKey should retry the read")
}

// TestGrouper_ParsesDistinctFilesConcurrently proves that parsing one file does
// not block parsing another: treeFor must not hold the grouper mutex across the
// slow read+parse. An instrumented readFile blocks until BOTH reads are in
// flight; if parsing were serialized under one mutex the second read could never
// start while the first holds the lock, and the test times out.
func TestGrouper_ParsesDistinctFilesConcurrently(t *testing.T) {
	dir := t.TempDir()
	srcA := "package p\n\nfunc A() {\n\tx := 1\n\t_ = x\n}\n"
	srcB := "package q\n\nfunc B() {\n\ty := 2\n\t_ = y\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte(srcA), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte(srcB), 0o644))

	g := NewGrouper(dir)
	defer func() { _ = g.Close() }()

	// Warm the parser instance up front so the timed window below measures
	// read+parse concurrency, not one-time wasm module instantiation (slow,
	// especially under -race).
	_, err := g.host.Parser("go")
	require.NoError(t, err)

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseAll()

	g.readFile = func(name string) ([]byte, error) {
		entered <- struct{}{}
		<-release
		return os.ReadFile(name)
	}

	done := make(chan string, 2)
	go func() { done <- g.GroupKey(reconcile.Finding{File: "a.go", Line: 4}) }()
	go func() { done <- g.GroupKey(reconcile.Finding{File: "b.go", Line: 4}) }()

	// Both reads must be in flight concurrently within a short window.
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			releaseAll()
			t.Fatal("reads were serialized: second file's read never started while the first held the lock")
		}
	}
	releaseAll()

	for i := 0; i < 2; i++ {
		select {
		case k := <-done:
			require.NotEmpty(t, k)
		case <-time.After(30 * time.Second):
			t.Fatal("GroupKey did not complete")
		}
	}
}

// TestGrouper_SatisfiesReconcileInterface is a compile-time assertion that the
// astgroup grouper plugs into the reconcile seam.
func TestGrouper_SatisfiesReconcileInterface(t *testing.T) {
	var _ reconcile.Grouper = (*Grouper)(nil)
}
