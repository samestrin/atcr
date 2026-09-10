package payload

import (
	"context"
	"strconv"
	"strings"
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

// prefetchDomainSrc is a changed test file whose mocked symbol is an ordinary
// domain name that happens to contain a cue word ("patch"). Line map: 1 package,
// 2 blank, 3 func header, 4 stubbed := ApplyPatchSet, 5 assignment, 6 close.
const prefetchDomainSrc = `package store

func TestApply(t *testing.T) {
	stubbed := ApplyPatchSet
	ApplyPatchSet = nil
}
`

func prefetchDomainTree() astgroup.Node {
	return astgroup.Node{Kind: "file", StartLine: 1, EndLine: 6, Children: []astgroup.Node{
		{Kind: "func", Name: "TestApply", StartLine: 3, EndLine: 6},
	}}
}

func TestExtractChangedSymbols_DomainSymbolContainingACueIsStillRetrievable(t *testing.T) {
	// Rejecting every token that CONTAINS a cue word also rejects real symbols —
	// ApplyPatchSet, DoubleBuffer, Inspector — and losing one loses the AC6
	// snippet entirely. The rejection must key on the double's own naming form
	// (a prefix, or a whole token), not on a bare substring.
	got := extractChangedSymbols(prefetchDomainSrc, []LineRange{{Start: 4, End: 4}}, prefetchDomainTree(), true)

	var mocked []string
	for _, s := range got {
		if s.Mocked {
			mocked = append(mocked, s.Name)
		}
	}
	require.Contains(t, mocked, "ApplyPatchSet",
		"a domain symbol must not be discarded for containing a cue word")
	require.NotContains(t, mocked, "stubbed",
		"the narrowing must still reject a token that names the double itself")
}

func TestParseGrepHits_AttributesHitsAndExcludesChangedFiles(t *testing.T) {
	out := strings.Join([]string{
		"internal/store/consumer.go:42:\tif _, err := ReadStore(p); err != nil {",
		"internal/store/store.go:10:func ReadStore(path string) ([]byte, error) {",
		"internal/other/x.go:7:\tWriteStore(p)",
	}, "\n")

	got := parseGrepHits(out, []string{"ReadStore", "WriteStore"},
		map[string]bool{"internal/store/store.go": true}, 10)

	require.Len(t, got, 2, "the changed file's own hit must be dropped")
	require.Equal(t, "internal/store/consumer.go", got[0].Path)
	require.Equal(t, 42, got[0].Line)
	require.Equal(t, "ReadStore", got[0].Symbol,
		"a hit must record which changed symbol it was retrieved for")
	require.Equal(t, "internal/other/x.go", got[1].Path)
	require.Equal(t, "WriteStore", got[1].Symbol)
}

func TestParseGrepHits_CapsSitesPerSymbol(t *testing.T) {
	// A very common name (Close, Run, New) would otherwise crowd every other
	// symbol out of the byte cap before the ledger ever ranks anything.
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, "pkg/f.go:"+strconv.Itoa(i)+":\tClose()")
	}

	got := parseGrepHits(strings.Join(lines, "\n"), []string{"Close"}, nil, 3)

	require.Len(t, got, 3, "one symbol may not contribute more than its cap")
}

func TestParseGrepHits_SkipsMalformedLines(t *testing.T) {
	out := strings.Join([]string{
		"",
		"no-colons-at-all",
		"pkg/f.go:notanumber:\tReadStore()",
		"pkg/f.go:9:\tReadStore()",
	}, "\n")

	got := parseGrepHits(out, []string{"ReadStore"}, nil, 10)

	require.Len(t, got, 1, "only the well-formed hit survives")
	require.Equal(t, 9, got[0].Line)
}

const prefetchStoreV1 = `package store

func ReadStore(path string) ([]byte, error) {
	return nil, nil
}
`

const prefetchStoreV2 = `package store

func ReadStore(path string) (string, error) {
	return "", nil
}
`

// prefetchConsumer is NEVER changed by the fixture's diff, and bears no textual
// resemblance to the store — it is related only by CALLING ReadStore. That is
// what makes it the AC5 case: similarity retrieval cannot reach it.
const prefetchConsumer = `package store

func Reconcile(path string) error {
	data, err := ReadStore(path)
	if err != nil {
		return err
	}
	_ = data
	return nil
}
`

// prefetchRepo builds a two-commit repo whose diff changes ReadStore's RETURN
// SHAPE in store.go and leaves its consumer untouched.
func prefetchRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = initRepo(t)
	write(t, dir, "store.go", prefetchStoreV1)
	write(t, dir, "consumer.go", prefetchConsumer)
	base = commitAll(t, dir, "v1: store and its consumer")
	write(t, dir, "store.go", prefetchStoreV2)
	head = commitAll(t, dir, "v2: change ReadStore return shape")
	return dir, base, head
}

func TestReferenceHits_RetrievesConsumerInAnUntouchedFile(t *testing.T) {
	// AC5: when a changed symbol's return shape changes, its consumers are
	// reached by REFERENCE. consumer.go is absent from the diff entirely, so no
	// payload mode would ever show it — only the reference lookup can.
	dir, _, _ := prefetchRepo(t)
	g := newGitRunner(context.Background(), dir)

	hits := g.referenceHits([]changedSymbol{{Name: "ReadStore"}}, map[string]bool{"store.go": true})

	var paths []string
	for _, h := range hits {
		paths = append(paths, h.Path)
	}
	require.Contains(t, paths, "consumer.go",
		"a consumer in a file the diff never touched must be retrieved")
	require.NotContains(t, paths, "store.go",
		"the changed file is already in the payload verbatim")
}

func TestReferenceHits_IsLazyAndSpendsOneProcessForEverySymbol(t *testing.T) {
	dir, _, _ := prefetchRepo(t)
	g := newGitRunner(context.Background(), dir)

	require.Empty(t, g.referenceHits(nil, nil))
	require.Zero(t, g.execCount,
		"a run whose diff cites no resolvable symbol must never read a source file")

	g.referenceHits([]changedSymbol{{Name: "ReadStore"}, {Name: "Reconcile"}}, nil)
	require.Equal(t, 1, g.execCount,
		"every symbol must resolve in ONE git grep, not one process per symbol (AC4)")
}

func TestReferenceHits_UnresolvableSymbolFailsOpenToEmpty(t *testing.T) {
	// `git grep` exits non-zero when it matched nothing, which gitRunner.output
	// reports identically to a real git failure. Pre-fetching is an ADDITIONAL
	// review input, so both must degrade to empty context rather than fail.
	dir, _, _ := prefetchRepo(t)
	g := newGitRunner(context.Background(), dir)

	require.Empty(t, g.referenceHits([]changedSymbol{{Name: "NoSuchSymbolAnywhere"}}, nil))
}
