package payload

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildPrefetch has four early-return arms that decide what PrefetchStatus the
// manifest records, and the type exists precisely because those outcomes are
// otherwise byte-identical on disk. This covers the one of them that is
// reachable through the function's own control flow, and records what stands in
// the way of the other three so the next reader does not re-derive it.
//
//   - len(files) == 0           — covered here.
//   - rangeChunks error         — needs git to fail on a range that
//     changedFilesMemo just READ successfully. Both run against the same two
//     revs in the same repo, so a failure of one and not the other is not
//     constructible from a test fixture.
//   - lookupFailed              — needs a head that resolves for
//     changedFilesMemo but breaks `git grep`. An unresolvable head returns at
//     the FIRST arm instead (that is what TestRangeBuilder_PrefetchStatus...'s
//     "a broken lookup is recorded as failed, not absent" subtest exercises, via
//     changedFilesMemo at the top of the function — not this arm).
//     referenceHits' own failure mode IS pinned directly, by
//     TestReferenceHits_BrokenLookupIsDistinguishableFromNoMatch.
//   - section == ""             — needs renderPrefetchSection to receive an
//     empty kept list AND an empty drop list. Every rejection between the hits
//     and the render records a PrefetchDrop (unreadable, oversized, file cap,
//     not-declared, out of range, overlap, byte cap), so once len(hits) > 0 the
//     drop ledger is non-empty and the section renders.
//
// Those three are left untested deliberately. A test that reached them would
// have to stub the git runner into a state production cannot produce, which
// proves the stub behaves, not the code.
func TestBuildPrefetch_EmptyChangeSetIsAbsentNotFailed(t *testing.T) {
	dir, base, _ := prefetchRepo(t)
	g := newGitRunner(context.Background(), dir)

	// base..base: the range is well-formed and readable, it simply contains no
	// changed files. This is the arm that separates "nothing to retrieve from"
	// from "the lookup broke".
	section, spans, st := g.buildPrefetch(base, base)

	require.Empty(t, section, "no changed files means no section to inject")
	require.Empty(t, spans, "nothing retrieved, so nothing becomes groundable")

	require.False(t, st.Failed,
		"an empty change set is not a malfunction; reporting it as failed would send an operator hunting a broken git grep that never ran")
	require.False(t, st.Present)
	require.False(t, st.Disabled,
		"pre-fetching was ENABLED here — it simply had nothing to work on, and conflating the two hides the operator's own setting")
	require.Equal(t, PrefetchStatus{}, st,
		"the empty-change-set arm must return the zero value, so the manifest records it as 'ran, found nothing' rather than any of the failure shapes")
}
