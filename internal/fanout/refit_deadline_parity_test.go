package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A re-fit fallback must keep DEADLINE parity with the slot it substitutes into.
//
// Epic 35.16.5.4 T4 forces a re-packed fallback's ChunkTotal to 1 so chunk_count
// and the diff-cache sizing token describe the ONE payload it ships rather than
// the persona's split. But ChunkTotal is overloaded: invokeAgent also feeds it to
// scaledTimeoutSecs (engine.go) to scale the per-call deadline. Forcing it to 1
// therefore silently stripped the deadline multiplier too, so a fallback
// substituting into a 20-chunk persona got the BASE timeout while the primary it
// replaced got up to chunkTimeoutCeilingFactor times it.
//
// The record and the deadline are different questions and must be answered
// separately: chunk_count still reads 1 (the payload really is one chunk), while
// TimeoutSecs is pre-scaled at construction by the PRIMARY's ChunkTotal.
// Pre-scaling is safe precisely because ChunkTotal is 1 — scaledTimeoutSecs is a
// no-op at chunkTotal <= 1, so invokeAgent cannot scale it a second time.
func TestBuildFallbackAgent_RefitKeepsDeadlineParityWithAMultiChunkPrimary(t *testing.T) {
	// A window big enough that each partitioned chunk still exceeds kai's 32768
	// default (so the re-fit arm actually fires), but small enough that the payload
	// partitions at all. Too small and every chunk holds one file that already fits
	// the backup — the slot splits but nothing re-packs, and the test asserts
	// nothing. At 80000 the split is 3 chunks of 2+ files, each over kai's budget.
	const window = 80000
	cfg := refitRoster(t, window, OverflowTruncate)

	gretaBudget := payload.EffectiveByteBudget("unlisted-small-model", ptrInt(window), defaultMaxTokens)
	kaiBudget := payload.EffectiveByteBudget("unlisted-backup-model", nil, defaultMaxTokens)
	require.Greater(t, gretaBudget, kaiBudget,
		"precondition: the backup's budget must be the smaller one, or nothing overflows")

	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, markedOversizedPayload(), ReviewRange{Base: "a", Head: "b"}, "blocks", "", true, true)
	})
	require.NoError(t, err)
	require.Greater(t, len(slots), 1,
		"precondition: this payload must partition into MORE THAN ONE baseline chunk, or the deadline multiplier is 1 and this test asserts nothing")

	s := slots[0]
	require.Len(t, s.Fallbacks, 1)
	primary, fb := s.Primary, s.Fallbacks[0]

	require.Greater(t, primary.ChunkTotal, 1,
		"precondition: the primary must carry the persona's multi-chunk split")
	require.True(t, fb.rePacked, "precondition: the backup's budget must force a re-fit")

	// T4 is unchanged: the RECORD still describes the one payload actually sent.
	assert.Equal(t, 1, fb.ChunkTotal, "chunk_count must still describe the single re-fit payload")
	assert.Equal(t, 0, fb.chunkMaxLines, "the bulk sentinel must still stand for a re-fit payload")

	// The deadline, however, must match what the primary's slot gets.
	base := cfg.Registry.Agents["kai"].EffectiveTimeoutSecs(cfg.Settings)
	want := scaledTimeoutSecs(base, primary.ChunkTotal)
	require.Greater(t, want, base,
		"precondition: a multi-chunk primary must actually scale the deadline, or there is nothing to lose")
	assert.Equal(t, want, fb.TimeoutSecs,
		"a re-fit fallback must inherit the slot's deadline scaling even though its chunk_count is 1")

	// And invokeAgent must not scale it a SECOND time: ChunkTotal 1 makes
	// scaledTimeoutSecs a no-op, which is what makes pre-scaling safe.
	assert.Equal(t, fb.TimeoutSecs, scaledTimeoutSecs(fb.TimeoutSecs, fb.ChunkTotal),
		"pre-scaled deadline must survive invokeAgent's own scaling pass unchanged")
}

// A re-fit fallback on the BULK path (no split) must be untouched by the change:
// its primary's ChunkTotal is not a multi-chunk count, so there is no multiplier
// to inherit and the deadline stays the fallback's own base.
func TestBuildFallbackAgent_RefitOnBulkPathKeepsItsBaseDeadline(t *testing.T) {
	cfg := refitRoster(t, 128000, OverflowTruncate)
	slot := buildRefitSlot(t, cfg)
	fb := slot.Fallbacks[0]

	require.True(t, fb.rePacked, "precondition: this case must actually re-fit")
	require.LessOrEqual(t, slot.Primary.ChunkTotal, 1,
		"precondition: the bulk path carries no multi-chunk split")

	base := cfg.Registry.Agents["kai"].EffectiveTimeoutSecs(cfg.Settings)
	assert.Equal(t, base, fb.TimeoutSecs,
		"with no split to inherit, a re-fit fallback keeps its own base timeout")
}
