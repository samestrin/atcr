package payload

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// AC7 asks that a discarded candidate be RECORDED rather than silent, because a
// reviewer shown N snippets cannot otherwise tell that apart from N candidates
// having existed. retrieveSnippets discards on seven paths; the file cap and the
// unreadable blob already have tests, and three shipped with none:
//
//	dropReasonOversized   — a candidate over maxAnalyzeFileBytes
//	dropReasonOutOfRange  — a span sliceLines refuses
//	dropReasonOverlap     — a region an earlier snippet already showed
//
// Each is a path where context was lost and the promise that the loss is named
// went unverified. The assertions below check the RENDERED section as well as
// the slice, because a ledger entry that never reaches the reviewer is the same
// silence AC7 rejects.

// dropLedgerReasons returns the set of reasons present in a drop ledger.
func dropLedgerReasons(drops []PrefetchDrop) map[string]bool {
	out := make(map[string]bool, len(drops))
	for _, d := range drops {
		out[dropReason(d)] = true
	}
	return out
}

func TestRetrieveSnippets_OversizedCandidateIsLedgered(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "store.go", prefetchStoreV1)
	// Over maxAnalyzeFileBytes (1 MiB). The oversize check runs BEFORE the parse,
	// so the content only has to be large, not unparseable.
	write(t, dir, "huge.go",
		"package store\n"+strings.Repeat("// filler line pushing this file past the analyze ceiling\n", 20000))
	base := commitAll(t, dir, "seed an oversized candidate")
	write(t, dir, "store.go", prefetchStoreV2)
	head := commitAll(t, dir, "change ReadStore return shape")

	hits := []refHit{{Path: "huge.go", Line: 2, Symbol: "ReadStore"}}

	kept, dropped := newGitRunner(context.Background(), dir).retrieveSnippets(base, head, hits, nil)

	require.Empty(t, kept, "an oversized candidate must not be parsed or shipped")
	require.True(t, dropLedgerReasons(dropped)[dropReasonOversized],
		"a candidate skipped for size must say so; without the record it is indistinguishable from one that never matched")
	require.Contains(t, renderPrefetchSection(kept, dropped), "huge.go",
		"the rendered ledger must NAME the file whose size discarded it")
}

func TestRetrieveSnippets_OutOfRangeSpanIsLedgered(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "store.go", prefetchStoreV1)
	write(t, dir, "small.go", "package store\n\nfunc Small() int {\n\treturn 1\n}\n")
	base := commitAll(t, dir, "seed a small candidate")
	write(t, dir, "store.go", prefetchStoreV2)
	head := commitAll(t, dir, "change ReadStore return shape")

	// A hit far past the end of the file. snippetSpan falls back to a radius
	// around the cited line, and sliceLines refuses a span that starts beyond the
	// file — the shape a stale grep result or a concurrent rewrite produces.
	hits := []refHit{{Path: "small.go", Line: 9999, Symbol: "Small"}}

	kept, dropped := newGitRunner(context.Background(), dir).retrieveSnippets(base, head, hits, nil)

	require.Empty(t, kept, "a span outside the file cannot be shown")
	require.True(t, dropLedgerReasons(dropped)[dropReasonOutOfRange],
		"a refused span must be recorded; silently dropping it hides that the reviewer lost this candidate entirely")
	require.Contains(t, renderPrefetchSection(kept, dropped), "small.go",
		"the rendered ledger must NAME the file whose span was refused")
}

func TestRetrieveSnippets_OverlappingRegionIsLedgered(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "store.go", prefetchStoreV1)
	// Two consumed symbols inside ONE enclosing block, so the second hit resolves
	// to a region the first snippet already showed.
	write(t, dir, "both.go",
		"package store\n\nfunc UsesBoth(p string) {\n\t_, _ = SymA(p)\n\t_, _ = SymB(p)\n}\n")
	base := commitAll(t, dir, "seed one block consuming two symbols")
	write(t, dir, "store.go", prefetchStoreV2)
	head := commitAll(t, dir, "change ReadStore return shape")

	// Distinct symbols, so the per-symbol emission ceiling cannot intercept first
	// and make this pass for the wrong reason.
	hits := []refHit{
		{Path: "both.go", Line: 4, Symbol: "SymA"},
		{Path: "both.go", Line: 5, Symbol: "SymB"},
	}

	kept, dropped := newGitRunner(context.Background(), dir).retrieveSnippets(base, head, hits, nil)

	require.Len(t, kept, 1, "the two hits resolve to one region, so only one snippet is emitted")
	require.True(t, dropLedgerReasons(dropped)[dropReasonOverlap],
		"the suppressed second hit must be recorded: its symbol got no snippet of its own, and AC7 asks that the reviewer be told")
	require.Contains(t, renderPrefetchSection(kept, dropped), "both.go",
		"the rendered ledger must NAME the overlapping candidate")
}
