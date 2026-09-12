package payload

import (
	"testing"

	"github.com/samestrin/atcr/internal/astgroup"
	"github.com/stretchr/testify/require"
)

// snippetSpan and sliceLines decide which region of an untouched file is sliced
// out and shipped to a provider, and the span they return is threaded into
// prefetchSpans — so it is also what the Epic 14.1 gate treats as groundable. An
// off-by-one that escapes their clamps is either a panic on a real repository or
// a snippet cut from the wrong place, marked groundable.
//
// TestSnippetSpan_ClampsBelowLineOne reads as though it covers the first of
// these and does not: it calls snippetSpan with line 2, so what it actually
// pins is the FALLBACK WINDOW's lower clamp (the `lo < 1` at 958). The
// non-positive-LINE arm above it was never exercised.

func TestSnippetSpan_NonPositiveLineClampsToTheFirstLine(t *testing.T) {
	// A zero or negative cited line reaches here from a stale grep result or a
	// hand-built refHit. The span is sliced against a 1-based line array, so a
	// non-positive start is an index error waiting to happen rather than a
	// smaller snippet.
	for _, line := range []int{0, -1, -500} {
		start, end := snippetSpan(astgroup.Node{}, line)

		require.GreaterOrEqual(t, start, 1,
			"snippetSpan(%d) returned start %d: a span is sliced against a 1-based array, so this must clamp rather than underflow", line, start)
		require.GreaterOrEqual(t, end, start,
			"snippetSpan(%d) returned an inverted span %d..%d", line, start, end)
		require.LessOrEqual(t, end-start+1, maxSnippetLines,
			"the clamped window must still respect the snippet ceiling")
	}
}

// The re-centre path's own `lo < 1` guard (prefetch.go:978) is deliberately NOT
// covered here, because it cannot fire. Reaching it needs the re-centre branch,
// which needs a span wider than maxSnippetLines (40). The no-parser fallback
// window is 2*snippetFallbackRadius+1 = 17 lines, so it never qualifies; and
// when a covering block IS found, astgroup.CoveringBlock's own guard requires
// block.StartLine > 0, so the clamp at 975-977 has already raised lo to
// blockStart >= 1 by the time that check runs. Coverage agrees: the blocks on
// either side of it (975-977 and 981-982) are exercised and it is not.
//
// A test that reached it would have to hand-build a block claiming a start line
// below 1, which the producer cannot emit — proving the fixture behaves, not the
// code.

func TestSliceLines_ClampsAndRefusesOutOfRangeRequests(t *testing.T) {
	lines := []string{"one", "two", "three", "four", "five"}

	cases := []struct {
		name       string
		lines      []string
		start, end int
		wantOK     bool
		wantS      int
		wantE      int
		wantBody   string
		why        string
	}{
		{
			name: "nil line slice is refused", lines: nil, start: 1, end: 2,
			why: "a file that split to nothing has no region to show; returning a body here would slice an empty array",
		},
		{
			name: "empty line slice is refused", lines: []string{}, start: 1, end: 2,
			why: "same arm as nil — an empty file is not a zero-length snippet, it is no snippet",
		},
		{
			name: "start below one is clamped up", lines: lines, start: 0, end: 2,
			wantOK: true, wantS: 1, wantE: 2, wantBody: "one\ntwo",
			why: "a non-positive start must become line 1, not underflow the 1-based slice",
		},
		{
			name: "negative start is clamped up", lines: lines, start: -7, end: 1,
			wantOK: true, wantS: 1, wantE: 1, wantBody: "one",
			why: "the same clamp, from further out",
		},
		{
			name: "end past the file is clamped down", lines: lines, start: 4, end: 99,
			wantOK: true, wantS: 4, wantE: 5, wantBody: "four\nfive",
			why: "the RETURNED end must be the clamped one: the caller threads it into prefetchSpans, so an unclamped 99 would mark non-existent lines groundable",
		},
		{
			name: "start past the file is refused", lines: lines, start: 99, end: 120,
			why: "nothing of this file was shown, so there is no span to record",
		},
		{
			name: "inverted range is refused", lines: lines, start: 4, end: 2,
			why: "an inverted span is a caller error, not an empty snippet",
		},
		{
			name: "an in-range request is returned whole", lines: lines, start: 2, end: 4,
			wantOK: true, wantS: 2, wantE: 4, wantBody: "two\nthree\nfour",
			why: "the accepted case — without it, a function refusing everything would satisfy every rejection above",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, s, e, ok := sliceLines(tc.lines, tc.start, tc.end)

			require.Equal(t, tc.wantOK, ok, tc.why)
			require.Equal(t, tc.wantS, s,
				"sliceLines must report the CLAMPED start, since that is what becomes the groundable span")
			require.Equal(t, tc.wantE, e,
				"sliceLines must report the CLAMPED end, since that is what becomes the groundable span")
			require.Equal(t, tc.wantBody, body,
				"a refused request must yield an empty body, never a partial slice")
		})
	}
}
