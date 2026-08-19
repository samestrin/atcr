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
	assert.Contains(t, out, "leaves no input budget once the",
		"the chunked path must emit the same operator warning as the bulk path")
	assert.Contains(t, out, "may overflow")
}

// The zero-budget state has TWO reachable causes now that the output cap is
// per-agent: a context_window_tokens declaration too small, and a max_tokens
// declaration too large for the window the model resolves. The second is the
// documented normal case — contextwindow defaults any model absent from the static
// table to 32768, which is what a proxy alias resolves, and max_tokens: 32000 (the
// value doctor's own worked example uses) leaves nothing for input.
//
// The warning named only context_window_tokens, so an operator who reached this state
// by declaring max_tokens is pointed at a knob they may never have set — and the run
// meanwhile ships a single smallest file that comes back as a false-clean review.
func TestBuildSlots_ZeroBudgetWarningNamesTheOutputCapToo(t *testing.T) {
	require.Equal(t, int64(0), payload.EffectiveByteBudget("unlisted-small-model", nil, 32000),
		"precondition: an undeclared window (32768 default) with a 32000-token cap leaves no input budget")

	cfg := sizingRosterConfig()
	greta := cfg.Registry.Agents["greta"]
	greta.MaxTokens = ptrInt(32000) // the cap, not the window, is what closes the budget
	require.Nil(t, greta.ContextWindowTokens,
		"precondition: greta must NOT declare a window — the operator's only declaration here is max_tokens")
	cfg.Registry.Agents["greta"] = greta
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}

	var slots []Slot
	var err error
	out := captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "", true)
	})
	require.NoError(t, err)
	require.Len(t, slots, 1)
	require.Equal(t, "overflow", slots[0].Primary.DegradationAction,
		"precondition: this must be the zero-budget arm, or the warning under test never fires")

	require.Contains(t, out, "leaves no input budget once the", "precondition: the zero-budget warning must fire")
	assert.Contains(t, out, "max_tokens",
		"the warning must name the knob that actually closed the budget, not only the one that did not")
}

// The chunked twin owes the same remedy: the operator's next action does not depend
// on which review_strategy happens to be configured.
func TestBuildSlots_ChunkedZeroBudgetWarningNamesBothKnobs(t *testing.T) {
	cfg := sizingRosterConfig()
	greta := cfg.Registry.Agents["greta"]
	greta.MaxTokens = ptrInt(32000)
	cfg.Registry.Agents["greta"] = greta
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"

	payloads := map[string]modePayload{"blocks": {Text: diffOfNFiles(4, 100), FileCount: 4}}

	var slots []Slot
	var err error
	out := captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	})
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: at the line floor this diff must split into chunks")

	require.Contains(t, out, "leaves no input budget once the", "precondition: the zero-budget warning must fire")
	assert.Contains(t, out, "max_tokens",
		"the chunked warning must name both knobs — it named neither")
	assert.Contains(t, out, "context_window_tokens",
		"and must still name the window, which is the other way into this state")
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
