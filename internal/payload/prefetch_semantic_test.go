package payload

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/semindex"
)

// Epic 35.16.12.1 T1: the similarity tier beneath the reference pre-fetch.
//
// fakeRetriever stands in for semindex.Client. It records every call so a test
// can pin the query set and order, and returns a fixed hit list.
type fakeRetriever struct {
	hits    []semindex.Snippet
	err     error
	calls   int
	queries []string
	topK    int
}

func (f *fakeRetriever) Retrieve(_ context.Context, queries []string, topK int) ([]semindex.Snippet, error) {
	f.calls++
	f.queries = append([]string(nil), queries...)
	f.topK = topK
	return f.hits, f.err
}

// prefetchSimilar resembles ReadStore but never references it, so the reference
// pass cannot reach it: it is the resemblance-only case AC9 is about.
const prefetchSimilar = `package store

func ReadCache(key string) (string, error) {
	return "", nil
}
`

// semanticRepo builds a two-commit repo whose diff changes ReadStore's return
// shape and leaves a lookalike that nothing references.
func semanticRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = initRepo(t)
	write(t, dir, "store.go", prefetchStoreV1)
	write(t, dir, "similar.go", prefetchSimilar)
	base = commitAll(t, dir, "v1: store and a lookalike")
	write(t, dir, "store.go", prefetchStoreV2)
	head = commitAll(t, dir, "v2: change ReadStore return shape")
	return dir, base, head
}

// similarHit is the index's view of prefetchSimilar's ReadCache, matching HEAD.
func similarHit() semindex.Snippet {
	return semindex.Snippet{
		Path: "similar.go", StartLine: 3, EndLine: 5, Name: "ReadCache",
		Content: "func ReadCache(key string) (string, error) {\n\treturn \"\", nil\n}",
		Score:   0.9,
	}
}

func TestBuildPrefetch_SimilaritySnippetWithNoReferenceHit(t *testing.T) {
	dir, base, head := semanticRepo(t)
	fake := &fakeRetriever{hits: []semindex.Snippet{similarHit()}}
	g := newGitRunner(context.Background(), dir)
	WithSemanticRetriever(fake, 5)(g)

	section, spans, st := g.buildPrefetch(base, head)

	require.Equal(t, 1, fake.calls, "the similarity stage must run although the reference pass found no consumer")
	require.Equal(t, []string{"func ReadStore(path string) (string, error)"}, fake.queries,
		"the query is the changed symbol's declaration text")
	require.Equal(t, 5, fake.topK)
	require.True(t, st.Present)
	require.Equal(t, 1, st.Snippets)
	require.Contains(t, section, "similar.go:3-5 (similarity to ReadCache)")
	require.Contains(t, section, "L4: \treturn \"\", nil")
	require.Equal(t, []LineRange{{Start: 3, End: 5}}, spans["similar.go"],
		"a validated similarity snippet is groundable on its HEAD span")
}

func TestBuildPrefetch_NoRetrieverLeavesTheReferencePassUnchanged(t *testing.T) {
	dir, base, head := semanticRepo(t)
	g := newGitRunner(context.Background(), dir)

	section, spans, st := g.buildPrefetch(base, head)

	require.Empty(t, section)
	require.Nil(t, spans)
	require.Equal(t, PrefetchStatus{}, st)
}

func TestSemanticQueries_CapsAtFortyInSymbolOrder(t *testing.T) {
	var symbols []changedSymbol
	var want []string
	for i := 0; i < 50; i++ {
		s := changedSymbol{Name: fmt.Sprintf("Sym%02d", i), Signature: fmt.Sprintf("func Sym%02d() int", i)}
		switch {
		case i%7 == 3:
			s.Mocked = true // cue-derived: never queried
		case i%5 == 1:
			s.Signature = "" // no header: the name is sent instead
		}
		symbols = append(symbols, s)
		if s.Mocked || len(want) >= maxChangedSymbols {
			continue
		}
		if s.Signature == "" {
			want = append(want, s.Name)
		} else {
			want = append(want, s.Signature)
		}
	}

	got := semanticQueries(symbols)

	require.Len(t, got, maxChangedSymbols)
	require.Equal(t, want, got, "queries follow symbol order, skip Mocked and fall back to the name")
	require.Equal(t, "func Sym00() int", got[0])
	require.Equal(t, "Sym01", got[1])
	require.NotContains(t, got, "func Sym03() int")
}

func TestCapPrefetchSnippets_MixedTiersShedEverySimilarityBeforeAnyReference(t *testing.T) {
	snips := []PrefetchSnippet{
		prefetchSnippet("ref1.go", "ReadStore", PrefetchTierReference, 300),
		prefetchSnippet("sim1.go", "ReadCache", PrefetchTierSimilarity, 200),
		prefetchSnippet("ref2.go", "ReadStore", PrefetchTierReference, 300),
		prefetchSnippet("sim2.go", "ReadBlob", PrefetchTierSimilarity, 900),
		prefetchSnippet("ref3.go", "ReadStore", PrefetchTierReference, 300),
		prefetchSnippet("sim3.go", "ReadFile", PrefetchTierSimilarity, 100),
	}
	// Room for two reference snippets and nothing else: every similarity snippet
	// and exactly one reference snippet must go.
	markers := int64(len(prefetchSectionStart) + 1 + len(prefetchUntrustedNotice) + len(prefetchSectionEnd) + 1)
	budget := int64(renderedBytes(snips[0])+renderedBytes(snips[2])) + markers + 600

	kept, dropped := capPrefetchSnippets(snips, budget)

	for _, k := range kept {
		require.Equal(t, PrefetchTierReference, k.Tier, "a similarity snippet survived while a reference one was shed: %s", k.Path)
	}
	require.Len(t, kept, 2, "precondition: the cap must bind into the reference tier")
	require.Len(t, dropped, 4)
	ledger := renderPrefetchDropLedger(dropped)
	for _, p := range []string{"sim1.go", "sim2.go", "sim3.go", "ref3.go"} {
		require.Contains(t, ledger, "dropped "+p+" ", "every shed snippet is named in the ledger")
	}
	require.Contains(t, ledger, "tier similarity")
	require.Contains(t, ledger, "tier reference")
}

func TestBuildPrefetch_SimilarityHitsAreValidatedAtHead(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "store.go", prefetchStoreV1)
	write(t, dir, "consumer.go", prefetchConsumer)
	write(t, dir, "similar.go", prefetchSimilar)
	write(t, dir, "vendor/lib.go", prefetchSimilar)
	write(t, dir, ".atcrignore", "vendor/\n")
	base := commitAll(t, dir, "v1")
	write(t, dir, "store.go", prefetchStoreV2)
	head := commitAll(t, dir, "v2: change ReadStore return shape")

	fake := &fakeRetriever{hits: []semindex.Snippet{
		// stale: the index remembers a body HEAD no longer holds
		{Path: "similar.go", StartLine: 3, EndLine: 5, Name: "ReadCache", Content: "func ReadCache(key int) bool {\n\treturn false\n}"},
		// ignored by .atcrignore, though tracked
		{Path: "vendor/lib.go", StartLine: 3, EndLine: 5, Name: "ReadCache", Content: similarHit().Content},
		// a file this diff changed: already in the payload
		{Path: "store.go", StartLine: 3, EndLine: 5, Name: "ReadStore"},
		// a span past the end of the HEAD file
		{Path: "similar.go", StartLine: 30, EndLine: 40, Name: "Gone"},
		// a path that does not exist at HEAD
		{Path: "missing.go", StartLine: 1, EndLine: 3, Name: "Nope"},
		// the region the reference pass already shows
		{Path: "consumer.go", StartLine: 3, EndLine: 10, Name: "Reconcile"},
	}}
	g := newGitRunner(context.Background(), dir)
	WithSemanticRetriever(fake, 10)(g)

	section, spans, st := g.buildPrefetch(base, head)

	require.True(t, st.Present, "precondition: the reference snippet of consumer.go is kept")
	require.Equal(t, 1, st.Snippets, "no similarity hit survives validation")
	require.Equal(t, 6, st.Dropped)
	require.NotContains(t, section, "(similarity to")

	reasons := map[string]string{}
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, prefetchNotePrefix+"dropped ") {
			continue
		}
		require.Contains(t, line, "tier similarity")
		rest := strings.TrimPrefix(line, prefetchNotePrefix+"dropped ")
		p, _, _ := strings.Cut(rest, " ")
		_, reason, _ := strings.Cut(rest, ") - ")
		reasons[p+"|"+reason] = line
	}
	for _, want := range []string{
		"similar.go|" + dropReasonStale,
		"vendor/lib.go|" + dropReasonIgnored,
		"store.go|" + dropReasonChanged,
		"similar.go|" + dropReasonOutOfRange,
		"missing.go|" + dropReasonUnreadable,
		"consumer.go|" + dropReasonOverlap,
	} {
		require.Contains(t, reasons, want, "ledger:\n%s", section)
	}

	require.Equal(t, []string{"consumer.go"}, keys(spans), "only the reference snippet is groundable")
}

func TestBuildPrefetch_SimilaritySpanAndBodyComeFromHead(t *testing.T) {
	dir, base, head := semanticRepo(t)
	hit := similarHit()
	// The index chunk differs from HEAD only in trailing whitespace and line
	// endings, which is not staleness. The body shown must still be HEAD's.
	hit.Content = "func ReadCache(key string) (string, error) {  \r\n\treturn \"\", nil\r\n}\n"
	g := newGitRunner(context.Background(), dir)
	WithSemanticRetriever(&fakeRetriever{hits: []semindex.Snippet{hit}}, 3)(g)

	section, _, st := g.buildPrefetch(base, head)

	require.Equal(t, 1, st.Snippets)
	require.NotContains(t, section, "\r")
	require.Contains(t, section, "L3: func ReadCache(key string) (string, error) {\n")
}

func TestBuildPrefetch_RetrieverErrorFailsOpen(t *testing.T) {
	dir, base, head := prefetchRepo(t)
	fake := &fakeRetriever{err: &semindex.Error{Class: semindex.ClassUnreachable, Stage: "embedding"}}
	g := newGitRunner(context.Background(), dir)
	WithSemanticRetriever(fake, 3)(g)

	section, _, st := g.buildPrefetch(base, head)

	require.Equal(t, 1, fake.calls)
	require.True(t, st.Present, "the reference snippet still ships")
	require.Equal(t, 1, st.Snippets)
	require.NotContains(t, section, "similarity")
}

func TestRangeBuilder_ZeroPrefetchBytesDisablesTheSimilarityStage(t *testing.T) {
	dir, base, head := semanticRepo(t)
	fake := &fakeRetriever{hits: []semindex.Snippet{similarHit()}}
	b := NewRangeBuilder(context.Background(), dir, base, head,
		WithMaxPrefetchBytes(0), WithSemanticRetriever(fake, 5))

	require.Empty(t, b.prefetch())
	require.Zero(t, fake.calls, "max_prefetch_bytes: 0 must disable the similarity stage too")
	require.True(t, b.PrefetchStatus().Disabled)
}

func TestWithSemanticRetriever_NilRetrieverIsInert(t *testing.T) {
	dir, base, head := semanticRepo(t)
	b := NewRangeBuilder(context.Background(), dir, base, head, WithSemanticRetriever(nil, 5))

	require.Empty(t, b.prefetch())
	require.Equal(t, PrefetchStatus{}, b.PrefetchStatus())
}

func keys(m map[string][]LineRange) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
