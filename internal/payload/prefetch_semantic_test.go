package payload

import (
	"context"
	"encoding/json"
	"errors"
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
	shedRefs := 0
	for _, d := range dropped {
		require.Contains(t, ledger, "dropped "+d.Path+" ", "every shed snippet is named in the ledger")
		if d.Tier == PrefetchTierReference {
			shedRefs++
		}
	}
	require.Equal(t, 1, shedRefs)
	for _, p := range []string{"sim1.go", "sim2.go", "sim3.go"} {
		require.Contains(t, ledger, "dropped "+p+" ", "every similarity snippet is shed")
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
	write(t, dir, "pkg/a.go", "package pkg\n")
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
		// a directory, not a file: `git show` would print its tree listing
		{Path: "pkg", StartLine: 1, EndLine: 1, Name: "Tree"},
	}}
	g := newGitRunner(context.Background(), dir)
	WithSemanticRetriever(fake, 10)(g)

	section, spans, st := g.buildPrefetch(base, head)

	require.True(t, st.Present, "precondition: the reference snippet of consumer.go is kept")
	require.Equal(t, 1, st.Snippets, "no similarity hit survives validation")
	require.Equal(t, 7, st.Dropped)
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
		"pkg|" + dropReasonUnreadable,
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

// Epic 80 T3: a nil *semindex.Client in a Retriever interface is not == nil, so
// it must be caught by the option or the stage calls Retrieve on a nil client.
func TestWithSemanticRetriever_TypedNilRetrieverIsInert(t *testing.T) {
	dir, base, head := semanticRepo(t)
	b := NewRangeBuilder(context.Background(), dir, base, head,
		WithSemanticRetriever((*semindex.Client)(nil), 5))

	// require.Nil accepts a typed nil, so compare the interface itself.
	require.True(t, b.g.retriever == nil, "a typed-nil retriever must leave the stage off")
	require.Empty(t, b.prefetch())
	require.Equal(t, PrefetchStatus{}, b.PrefetchStatus())
}

func TestWithSemanticRetriever_TopKBelowOneIsRaisedToOne(t *testing.T) {
	for _, topK := range []int{0, -3} {
		t.Run(fmt.Sprint(topK), func(t *testing.T) {
			dir, base, head := semanticRepo(t)
			fake := &fakeRetriever{hits: []semindex.Snippet{similarHit()}}
			g := newGitRunner(context.Background(), dir)
			WithSemanticRetriever(fake, topK)(g)

			_, _, st := g.buildPrefetch(base, head)

			require.Equal(t, 1, fake.calls)
			require.Equal(t, 1, fake.topK, "topK<1 must reach Retrieve as 1")
			require.Empty(t, st.DegradeReason)
		})
	}
}

func keys(m map[string][]LineRange) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestBuildPrefetch_BrokenReferenceLookupStillRunsTheSimilarityStage(t *testing.T) {
	dir, base, head := semanticRepo(t)
	// An invalid grep.threads makes `git grep` exit 128 while every other
	// git command this pass runs still works: the reference lookup BREAKS.
	gitCmd(t, dir, "config", "grep.threads", "-1")
	fake := &fakeRetriever{hits: []semindex.Snippet{similarHit()}}
	g := newGitRunner(context.Background(), dir)
	WithSemanticRetriever(fake, 5)(g)

	section, spans, st := g.buildPrefetch(base, head)

	require.Equal(t, 1, fake.calls, "a broken reference lookup must not skip the similarity stage")
	require.True(t, st.Failed, "the broken lookup is still reported")
	require.True(t, st.Present)
	require.Contains(t, section, "(similarity to ReadCache)")
	require.Contains(t, spans, "similar.go")
}

// Epic 35.16.12.1 T3: a configured retriever that fails records WHY in
// PrefetchStatus.DegradeReason and fails open to zero similarity snippets, on
// every error class and whether or not a reference snippet still ships.
func TestBuildPrefetch_RetrieverFailureRecordsOneDegradeReason(t *testing.T) {
	classes := []semindex.Class{
		semindex.ClassInvalidArgument, semindex.ClassInsecureScheme, semindex.ClassUnreachable,
		semindex.ClassTimeout, semindex.ClassCanceled, semindex.ClassNotFound,
		semindex.ClassStatus, semindex.ClassDecode, semindex.ClassContract,
		"secret-guard-blocked", // internal/fanout's guard rides the same *Error
	}
	repos := map[string]func(*testing.T) (string, string, string){
		"no reference hit":   semanticRepo,
		"reference snippets": prefetchRepo,
	}
	for name, repo := range repos {
		for _, cl := range classes {
			t.Run(name+"/"+string(cl), func(t *testing.T) {
				dir, base, head := repo(t)
				// A hit returned alongside the error is never trusted.
				fake := &fakeRetriever{
					hits: []semindex.Snippet{similarHit()},
					err:  fmt.Errorf("wrapped: %w", &semindex.Error{Class: cl, Stage: "embedding"}),
				}
				g := newGitRunner(context.Background(), dir)
				WithSemanticRetriever(fake, 3)(g)

				section, spans, st := g.buildPrefetch(base, head)

				require.Equal(t, 1, fake.calls)
				require.Equal(t, string(cl), st.DegradeReason)
				require.NotContains(t, section, "similarity")
				require.NotContains(t, spans, "similar.go")
				require.False(t, st.Failed, "a retriever failure is not a broken reference lookup")
			})
		}
	}
}

func TestBuildPrefetch_UnclassifiedRetrieverErrorStillDegrades(t *testing.T) {
	dir, base, head := semanticRepo(t)
	fake := &fakeRetriever{err: errors.New("boom")}
	g := newGitRunner(context.Background(), dir)
	WithSemanticRetriever(fake, 3)(g)

	section, _, st := g.buildPrefetch(base, head)

	require.Empty(t, section)
	require.Equal(t, PrefetchStatus{DegradeReason: semanticDegradeUnclassified}, st)
}

func TestBuildPrefetch_WorkingRetrieverRecordsNoDegradeReason(t *testing.T) {
	dir, base, head := semanticRepo(t)
	g := newGitRunner(context.Background(), dir)
	WithSemanticRetriever(&fakeRetriever{}, 3)(g)

	_, _, st := g.buildPrefetch(base, head)

	require.Empty(t, st.DegradeReason, "an empty result is not a failure")
	b, err := json.Marshal(st)
	require.NoError(t, err)
	require.NotContains(t, string(b), "degrade_reason")
}

// Epic 80 T2: a similarity span longer than maxSnippetLines is dropped, not
// clamped. A clamped body would no longer match the whole indexed chunk, so the
// stale compare could never pass; the hit is ledgered as oversized before any
// body is sliced from HEAD.
func TestSimilaritySnippets_SpanOverMaxSnippetLinesIsDropped(t *testing.T) {
	dir := initRepo(t)
	var long strings.Builder
	long.WriteString("package big\n\nfunc Big() {\n")
	for i := 0; i < maxSnippetLines+5; i++ {
		fmt.Fprintf(&long, "\t_ = %d\n", i)
	}
	long.WriteString("}\n")
	write(t, dir, "big.go", long.String())
	head := commitAll(t, dir, "v1")
	lines := splitSnippetLines(long.String())

	fit := semindex.Snippet{Path: "big.go", StartLine: 3, EndLine: 3 + maxSnippetLines - 1, Name: "Fits"}
	fit.Content = strings.Join(lines[fit.StartLine-1:fit.EndLine], "\n")
	over := semindex.Snippet{Path: "big.go", StartLine: 3, EndLine: len(lines), Name: "Big"}
	over.Content = strings.Join(lines[over.StartLine-1:over.EndLine], "\n")
	require.Greater(t, over.EndLine-over.StartLine+1, maxSnippetLines, "precondition: the span is over the cap")

	g := newGitRunner(context.Background(), dir)
	WithSemanticRetriever(&fakeRetriever{hits: []semindex.Snippet{over, fit}}, 5)(g)
	symbols := []changedSymbol{{Name: "Big", Signature: "func Big()"}}

	out, dropped, reason := g.similaritySnippets(head, symbols, nil, nil)

	require.Empty(t, reason)
	require.Equal(t, []PrefetchDrop{{Path: "big.go", Symbol: "Big", Tier: PrefetchTierSimilarity, Reason: dropReasonOversized}}, dropped,
		"a span over maxSnippetLines is dropped as oversized, even though its Content matches HEAD")
	require.Len(t, out, 1, "a span of exactly maxSnippetLines is kept")
	require.Equal(t, "Fits", out[0].Symbol)
	require.Equal(t, maxSnippetLines, out[0].End-out[0].Start+1)
}

// Epic 80 T2: semindex.Snippet carries the indexed chunk text and no content
// hash, so a hit whose Content is empty cannot be checked for staleness at all.
// It is shown anyway, with HEAD's body, rather than dropped: the body never
// comes from the index, so the worst case is a less similar region, never a
// wrong one.
func TestSimilaritySnippets_EmptyContentCannotBeCheckedForStalenessAndIsShown(t *testing.T) {
	dir, _, head := semanticRepo(t)
	hit := similarHit()
	hit.Content = ""
	g := newGitRunner(context.Background(), dir)
	WithSemanticRetriever(&fakeRetriever{hits: []semindex.Snippet{hit}}, 5)(g)
	symbols := []changedSymbol{{Name: "ReadStore", Signature: "func ReadStore(path string) (string, error)"}}

	out, dropped, reason := g.similaritySnippets(head, symbols, nil, nil)

	require.Empty(t, reason)
	require.Empty(t, dropped, "an unverifiable hit is not dropped as stale")
	require.Len(t, out, 1)
	require.Equal(t, "func ReadCache(key string) (string, error) {\n\treturn \"\", nil\n}", out[0].Body,
		"the body shown is HEAD's")
}
