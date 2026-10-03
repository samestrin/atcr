package fanout

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// mergeResultGroup folds Salvaged, counts UnparseableChunks and sums
// ThinkOnlyAttempts — but never folded ThinkSuppressed, so the merged persona
// record silently inherited chunk 0's value (out starts from g[0]). statusFor then
// copies it to AgentStatus and cli/benchmark_repostate.go reads it as a PERSONA-WIDE
// fact, so the whole row was dropped from the score on bin 0's signal alone.
//
// This is the converse direction: a LATER bin suppressed while chunk 0 ran clean
// read as false persona-wide, which made Summary.ContributedNothingCount and
// summarizeStatuses silently UNDERCOUNT (TD internal/fanout/chunker.go:493).
func TestMergeResultGroup_FoldsThinkSuppressedFromAnyBin(t *testing.T) {
	t.Parallel()

	later := mergeResultGroup([]Result{
		{Agent: "greta", Status: StatusOK, Content: "real findings"},
		{Agent: "greta", Status: StatusOK, Content: "\x3cthink\x3eonly\x3c/think\x3e", UnparseableResponse: true, ThinkSuppressed: true},
	}, nil)
	assert.True(t, later.ThinkSuppressed,
		"a think-suppressed bin anywhere in the group is a fact about the persona; reading only g[0] hid it "+
			"and made the contributed-nothing tallies undercount")

	// And the already-visible direction stays visible: folding must not lose bin 0's.
	first := mergeResultGroup([]Result{
		{Agent: "greta", Status: StatusOK, Content: "\x3cthink\x3eonly\x3c/think\x3e", UnparseableResponse: true, ThinkSuppressed: true},
		{Agent: "greta", Status: StatusOK, Content: "real findings"},
	}, nil)
	assert.True(t, first.ThinkSuppressed, "the fold is an OR, so bin 0's signal survives it too")

	// A clean persona must not acquire the flag.
	clean := mergeResultGroup([]Result{
		{Agent: "greta", Status: StatusOK, Content: "a"},
		{Agent: "greta", Status: StatusOK, Content: "b"},
	}, nil)
	assert.False(t, clean.ThinkSuppressed, "an OR-fold over no suppressed bins is false, not a spurious flag")
}

// The persona-wide flag answers "did any bin get eaten by the strip", which is the
// right question for the record and the WRONG question for the score. Scoring needs
// "did the persona contribute nothing", and the denominator for that is already on
// disk: ThinkSuppressed is documented as set only alongside UnparseableResponse
// (engine.go:420-427), so UnparseableChunks counts the bins that produced nothing
// and ChunkCount says how many there were.
//
// Mirrors WholePersonaSalvaged deliberately, including its fail-closed arm for an
// absent denominator: an unmeasurable claim withholds coverage rather than granting
// it (TD internal/scorecard/trust.go:1019).
func TestWholePersonaThinkSuppressed(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		st   AgentStatus
		want bool
	}{
		"not suppressed at all": {AgentStatus{}, false},
		"unchunked persona — no denominator, whole reply was a think run": {
			AgentStatus{ThinkSuppressed: true}, true,
		},
		"chunked, every bin produced nothing": {
			AgentStatus{ThinkSuppressed: true, ChunkCount: 2, UnparseableChunks: 2}, true,
		},
		"chunked, one bin of eight — the seven siblings contributed, so the row must be scored": {
			AgentStatus{ThinkSuppressed: true, ChunkCount: 8, UnparseableChunks: 1}, false,
		},
		"chunked, seven of eight unparseable but one parsed — still a partial loss": {
			AgentStatus{ThinkSuppressed: true, ChunkCount: 8, UnparseableChunks: 7}, false,
		},
		"chunk count absent on a chunked-looking record — withhold rather than guess": {
			AgentStatus{ThinkSuppressed: true, UnparseableChunks: 3}, true,
		},
		// mergeChunkResults short-circuits a one-element group (chunker.go:237) without
		// entering mergeResultGroup, which is the only writer of UnparseableChunks — and a
		// re-fit fallback stamps ChunkTotal=1, which engine.go copies to ChunkCount. So a
		// persona whose WHOLE reply the strip ate arrives here with the numerator never
		// populated, not with a measured zero. Withhold, exactly as the absent-denominator
		// arm above and as WholePersonaSalvaged's absent-bin-index arm do.
		"numerator never populated — one-element group, so 0 means unmeasured not measured-zero": {
			AgentStatus{ThinkSuppressed: true, ChunkCount: 1, UnparseableChunks: 0}, true,
		},
	} {
		assert.Equal(t, tc.want, WholePersonaThinkSuppressed(tc.st), name)
	}
}
