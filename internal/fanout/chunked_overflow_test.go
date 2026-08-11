package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A window too small to reserve output headroom (EffectiveByteBudget == 0) is
// honest degradation, and the bulk path says so: it records
// degradation_action="overflow" and warns. The chunked path reached the same
// state silently — ChunkMaxLines clamps to the minChunkLines floor, so the run
// looked like an ordinary "chunk" while shipping chunks the model cannot hold.
// The two paths must agree; an operator whose declaration is unusably small
// gets the same signal whichever strategy is configured.
func TestBuildSlots_ChunkedTinyWindowRecordsOverflowNotSilentClamp(t *testing.T) {
	require.Equal(t, int64(0), payload.EffectiveByteBudget("unlisted-small-model", ptrInt(1), defaultMaxTokens),
		"precondition: a 1-token declaration leaves no room for input after the output reservation")

	cfg := declaredWindowRoster(t, 1)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"

	diff := diffOfNFiles(4, 100) // 400+ lines > the 64-line floor → must split
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 4}}

	var slots []Slot
	var err error
	out := captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	})
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: at the 64-line floor this diff must split into chunks")

	for i, s := range slots {
		assert.Equal(t, "overflow", s.Primary.DegradationAction,
			"chunk slot %d must record overflow, not a clean chunk, when the window reserves no input budget", i)
	}
	assert.Contains(t, out, "window too small to reserve output headroom",
		"the chunked path must emit the same operator warning as the bulk path")
}

// The ordinary chunked path must keep recording "chunk": the overflow marking
// is conditional on a zero budget, not a blanket relabel of every chunked run.
func TestBuildSlots_ChunkedUsableWindowStillRecordsChunk(t *testing.T) {
	cfg := declaredWindowRoster(t, 128000)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"

	diff := diffOfNFiles(12, 900) // 10800 lines > the 8437-line budget → splits
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 12}}

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: this diff must still split at the declared budget")

	for i, s := range slots {
		assert.Equal(t, "chunk", s.Primary.DegradationAction,
			"chunk slot %d on a usable window is a no-loss chunk, not an overflow", i)
	}
}
