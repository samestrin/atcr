package payload

import (
	"testing"

	"github.com/samestrin/atcr/internal/astgroup"
	"github.com/stretchr/testify/require"
)

// prefetchStoreSrc is a HEAD-side Go file whose line numbers the hand-built
// trees below index into. Trees are constructed by hand rather than parsed so
// these tests need no wasm host and stay fast enough to run on every save —
// astgroup.Node is a plain struct, so this exercises the real
// EnclosingSymbolName / FileSkeleton contracts against a known shape.
//
// Line map: 1 package, 2 blank, 3 func ReadStore header, 4 if, 5 return,
// 6 close if, 7 return nil, 8 close func, 9 blank, 10 func Unrelated.
const prefetchStoreSrc = `package store

func ReadStore(path string) ([]byte, error) {
	if path == "" {
		return nil, errBadPath
	}
	return nil, nil
}

func Unrelated() {}
`

// prefetchStoreTree mirrors prefetchStoreSrc. Kinds are drawn from
// astgroup.blockKinds ("file", "func", "if") so the covering-chain walk behaves
// exactly as it does on a parsed tree.
func prefetchStoreTree() astgroup.Node {
	return astgroup.Node{Kind: "file", StartLine: 1, EndLine: 10, Children: []astgroup.Node{
		{Kind: "func", Name: "ReadStore", StartLine: 3, EndLine: 8, Children: []astgroup.Node{
			{Kind: "if", StartLine: 4, EndLine: 6},
		}},
		{Kind: "func", Name: "Unrelated", StartLine: 10, EndLine: 10},
	}}
}

func TestExtractChangedSymbols_NamesTheTouchedFunctionWithItsSignature(t *testing.T) {
	// A one-line edit inside ReadStore's `if` arm. The symbol must resolve to the
	// enclosing FUNCTION, not the anonymous if, and must carry the declaration
	// header — AC5 is about a changed signature or return shape, and a consumer
	// cannot be judged against a bare name.
	got := extractChangedSymbols(prefetchStoreSrc, []LineRange{{Start: 5, End: 5}}, prefetchStoreTree(), false)

	require.Len(t, got, 1, "one changed range inside one function yields one symbol")
	require.Equal(t, "ReadStore", got[0].Name)
	require.Contains(t, got[0].Signature, "func ReadStore(path string) ([]byte, error)",
		"the signature must carry the parameter and return shape a call site has to agree with")
	require.False(t, got[0].Mocked, "a non-test file contributes no mock targets")
}

func TestExtractChangedSymbols_IgnoresFunctionsTheDiffDidNotTouch(t *testing.T) {
	// Scoping to the touched declarations is what keeps the later `git grep`
	// argv — and so the AC4 latency budget — proportional to the diff rather
	// than to the file.
	got := extractChangedSymbols(prefetchStoreSrc, []LineRange{{Start: 5, End: 5}}, prefetchStoreTree(), false)

	for _, s := range got {
		require.NotEqual(t, "Unrelated", s.Name, "an untouched sibling declaration must not enter the set")
	}
}

// prefetchTestSrc is a changed TEST file that patches the real ReadStore.
// Line map: 1 package, 2 blank, 3 func TestRead header, 4 patched := ReadStore,
// 5 ReadStore = ..., 6 _ = patched, 7 close func.
const prefetchTestSrc = `package store

func TestRead(t *testing.T) {
	patched := ReadStore
	ReadStore = func(string) ([]byte, error) { return nil, nil }
	_ = patched
}
`

func prefetchTestTree() astgroup.Node {
	return astgroup.Node{Kind: "file", StartLine: 1, EndLine: 7, Children: []astgroup.Node{
		{Kind: "func", Name: "TestRead", StartLine: 3, EndLine: 7},
	}}
}

func TestExtractChangedSymbols_CollectsMockTargetsFromChangedTestLines(t *testing.T) {
	// AC6: a changed test file that patches a symbol must pull the symbol in, so
	// the REAL implementation can be placed beside the test that replaces it. A
	// mock that does not model what the real function does is only legible when
	// both are on the page.
	got := extractChangedSymbols(prefetchTestSrc, []LineRange{{Start: 4, End: 5}}, prefetchTestTree(), true)

	var mocked []string
	for _, s := range got {
		if s.Mocked {
			mocked = append(mocked, s.Name)
		}
	}
	require.Contains(t, mocked, "ReadStore", "the patched symbol must be retrievable")
}

func TestExtractChangedSymbols_MockCueScanIsTestFileOnly(t *testing.T) {
	// The cue scan over-collects by design (the reference lookup filters out
	// whatever is declared nowhere), so it must not run on production files —
	// there a line mentioning "patch" is ordinary prose or a real identifier.
	got := extractChangedSymbols(prefetchTestSrc, []LineRange{{Start: 4, End: 5}}, prefetchTestTree(), false)

	for _, s := range got {
		require.False(t, s.Mocked, "a non-test file must contribute no mock targets")
	}
}

func TestExtractChangedSymbols_NoChangedRangesYieldsNothing(t *testing.T) {
	// The laziness contract starts here: a diff that touches no parseable
	// declaration must produce no symbol, so no `git grep` and no candidate file
	// read ever happens downstream.
	require.Empty(t, extractChangedSymbols(prefetchStoreSrc, nil, prefetchStoreTree(), false))
}
