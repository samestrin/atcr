package fanout

import (
	"fmt"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The declared window and the global payload_byte_budget interact ASYMMETRICALLY
// by design: the bulk path clamps to min(per-agent budget, global cap), while
// ChunkMaxLines derives the per-chunk LINE budget from the unclamped window. That
// asymmetry is documented in production prose but was untestable in practice,
// because sizingRosterConfig() sets PayloadByteBudget = 0 and every declared-window
// test inherits it — so a change that started clamping ChunkMaxLines, or stopped
// clamping the bulk budget, would pass the whole suite silently.
//
// It is also the interaction that decides whether the epic's headline benefit is
// real: at the shipped default of 524288 the bulk path saturates near a
// 162000-token declaration, and everything above that buys no extra payload.

func TestBuildSlots_BulkClampsToGlobalBudgetButRecordsFullDeclaration(t *testing.T) {
	const globalCap = 100000

	cfg := declaredWindowRoster(t, 512000)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = globalCap

	declaredBudget := payload.EffectiveByteBudget("unlisted-small-model", ptrInt(512000), defaultMaxTokens)
	require.Greater(t, declaredBudget, int64(globalCap),
		"precondition: the declaration's own budget must exceed the global cap, or the clamp is untested")

	agent, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	assert.Equal(t, int64(globalCap), agent.EffectiveBudget,
		"the bulk applied budget is min(per-agent budget, payload_byte_budget) — the global cap wins here")
	assert.Equal(t, 512000, agent.ResolvedWindow,
		"the recorded window stays the FULL declaration: the cap bounds the payload, it does not rewrite what the model can hold")
}

func TestBuildSlots_EveryStrategyRecordsTheSameCappedEffectiveBudget(t *testing.T) {
	// status.json's effective_budget meant two different things depending on which
	// payload strategy ran: the bulk path recorded the APPLIED budget (global-capped,
	// scope-reservation-reduced) while the chunked and baseline paths recorded the
	// RAW per-agent budget, so one roster could report a multi-megabyte budget on a
	// 100000-byte-capped run and a consumer comparing two agents was comparing
	// unlike quantities. Only the recorded byte budget is unified here — the
	// per-chunk LINE regime stays derived from the unclamped window, the deliberate
	// asymmetry the sibling test above pins.
	const globalCap = 100000
	rng := ReviewRange{Base: "a", Head: "b"}

	declaredBudget := payload.EffectiveByteBudget("unlisted-small-model", ptrInt(512000), defaultMaxTokens)
	require.Greater(t, declaredBudget, int64(globalCap),
		"precondition: the declaration's own budget must exceed the global cap, or nothing is being capped")

	t.Run("chunked", func(t *testing.T) {
		cfg := declaredWindowRoster(t, 512000)
		cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
		cfg.Settings.ReviewStrategy = "chunked"
		cfg.Settings.PayloadByteBudget = globalCap

		slots, _, err := buildSlots(cfg, map[string]modePayload{"blocks": {Text: diffOfNFiles(60, 900), FileCount: 60}}, rng, "", "", true)
		require.NoError(t, err)
		require.Greater(t, len(slots), 1, "precondition: the diff must actually split into chunk slots")

		assert.Equal(t, int64(globalCap), slots[0].Primary.EffectiveBudget,
			"a chunked slot must record the same globally-capped budget the bulk path records")
		assert.Equal(t, 512000, slots[0].Primary.ResolvedWindow,
			"the recorded window still stays the FULL declaration")
	})

	t.Run("baseline", func(t *testing.T) {
		entries := make([]payload.FileEntry, 0, 20)
		for i := 0; i < 20; i++ {
			entries = append(entries, baselineEntry(fmt.Sprintf("f%02d.go", i), 20000))
		}
		cfg := declaredWindowRoster(t, 512000)
		cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
		cfg.Settings.PayloadByteBudget = globalCap

		slots, _, err := buildSlots(cfg, baselinePayloads(entries), rng, string(payload.ModeFiles), "", true, true)
		require.NoError(t, err)
		require.Greater(t, len(slots), 1, "precondition: 400000 bytes must split at a 100000-byte cap")

		assert.Equal(t, int64(globalCap), slots[0].Primary.EffectiveBudget,
			"a baseline chunk slot must record the budget its partition actually used, not the uncapped one")
	})
}

func TestBuildSlots_ChunkedLineBudgetIsNotClampedByGlobalBudget(t *testing.T) {
	const globalCap = 100000

	// Same roster and same global cap as the bulk case above, so the only
	// difference is the strategy — which is what makes the asymmetry visible
	// rather than merely asserted in a comment.
	capped := declaredWindowRoster(t, 512000)
	capped.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	capped.Settings.ReviewStrategy = "chunked"
	capped.Settings.PayloadByteBudget = globalCap

	uncapped := declaredWindowRoster(t, 512000)
	uncapped.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	uncapped.Settings.ReviewStrategy = "chunked"
	uncapped.Settings.PayloadByteBudget = 0

	// Enough lines to split even at the declaration's large line budget, so both
	// runs produce chunk slots carrying a recorded line regime.
	wantLines := payload.ChunkMaxLines("unlisted-small-model", ptrInt(512000), defaultMaxTokens)
	payloads := map[string]modePayload{"blocks": {Text: diffOfNFiles(60, 900), FileCount: 60}}
	rng := ReviewRange{Base: "a", Head: "b"}

	cappedSlots, _, err := buildSlots(capped, payloads, rng, "", "", true)
	require.NoError(t, err)
	require.Greater(t, len(cappedSlots), 1, "precondition: this diff must split at the declared line budget")

	uncappedSlots, _, err := buildSlots(uncapped, payloads, rng, "", "", true)
	require.NoError(t, err)

	assert.Equal(t, wantLines, cappedSlots[0].Primary.chunkMaxLines,
		"the per-chunk LINE budget is derived from the unclamped window — payload_byte_budget must not shrink it")
	assert.Equal(t, uncappedSlots[0].Primary.chunkMaxLines, cappedSlots[0].Primary.chunkMaxLines,
		"a global cap must leave the chunked line regime identical to an uncapped run")
	assert.Len(t, cappedSlots, len(uncappedSlots),
		"and therefore must not change how many chunks the diff splits into")
}
