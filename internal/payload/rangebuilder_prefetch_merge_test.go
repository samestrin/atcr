package payload

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithPrefetchedSpans_MergesRetrievedSpansWithoutOverwritingChangedFiles
// pins the two arms of the prefetch-span merge that the grounding widening
// rests on, neither of which any existing test reaches.
//
// The nil-ChangedLines arm matters because a range whose diff yields no
// grounding data at all (a binary-only or mode-only change) still ships a
// Context Definitions block, and dropping the retrieved spans there would make
// every finding about a retrieved consumer ungrounded on exactly the ranges
// where the reviewer has least else to go on.
//
// The already-changed-path arm is the boundary PrefetchOnly grounding depends
// on: a retrieved span must never displace a genuinely changed file's ranges,
// because PrefetchOnly narrows the gate to the exact retrieved lines, so an
// overwrite would SHRINK the groundable region of a file the patch really did
// change. The guard is unreachable through today's producers (parseGrepHits
// excludes changed paths), which is precisely why it needs a direct test — the
// arm exists as defense-in-depth against a future producer without that
// exclusion, and nothing else would notice if it were deleted.
//
// The `changedLines` error return in BuildChangedLines is left uncovered on
// purpose: RangeBuilder holds a concrete *gitRunner, so there is no seam to
// inject a diff failure through once validate() has passed, and the arm is a
// bare error passthrough.
func TestWithPrefetchedSpans_MergesRetrievedSpansWithoutOverwritingChangedFiles(t *testing.T) {
	t.Run("a nil grounding map still receives the retrieved spans", func(t *testing.T) {
		b := &RangeBuilder{prefetchSpans: map[string][]LineRange{
			"internal/consumer.go": {{Start: 40, End: 52}},
		}}

		cl := b.withPrefetchedSpans(nil)

		require.NotNil(t, cl, "a nil map must be created, not returned as nil and silently dropped")
		fc, ok := cl["internal/consumer.go"]
		require.True(t, ok, "the retrieved path must be groundable")
		assert.True(t, fc.PrefetchOnly, "a path the patch never touched must be marked PrefetchOnly")
		assert.Equal(t, []LineRange{{Start: 40, End: 52}}, fc.Ranges)
	})

	t.Run("a path that is both changed and retrieved keeps its changed entry", func(t *testing.T) {
		changed := FileChange{
			Ranges:      []LineRange{{Start: 1, End: 200}},
			ChangedText: []string{"x := 1"},
		}
		b := &RangeBuilder{prefetchSpans: map[string][]LineRange{
			"internal/both.go": {{Start: 40, End: 52}},
		}}

		cl := b.withPrefetchedSpans(ChangedLines{"internal/both.go": changed})

		fc := cl["internal/both.go"]
		assert.False(t, fc.PrefetchOnly,
			"a genuinely changed file must not be demoted to PrefetchOnly — that narrows the gate to the retrieved span alone")
		assert.Equal(t, []LineRange{{Start: 1, End: 200}}, fc.Ranges,
			"the changed ranges must win: a 13-line retrieved span replacing them would shrink the groundable region")
		assert.Equal(t, []string{"x := 1"}, fc.ChangedText, "evidence matching must survive the merge")
	})

	t.Run("no retrieved spans leaves the grounding map untouched", func(t *testing.T) {
		b := &RangeBuilder{}

		assert.Nil(t, b.withPrefetchedSpans(nil), "with nothing retrieved, a nil map stays nil rather than becoming empty-non-nil")
	})
}
