package fanout

import (
	"fmt"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The declared window and the global payload_byte_budget interact on BOTH paths:
// the bulk path clamps its byte budget to min(per-agent budget, global cap), and
// the chunked path clamps its derived per-chunk LINE budget to the same ceiling.
//
// That was not always true. The line budget used to be derived from the
// unclamped window, which let a legal 10,000,000-token declaration derive
// ~728,000 lines (~34.9 MB) per chunk — past any real proxy request-body limit
// — and epic 35.16.5.1 recorded the gap as technical debt. The asymmetry is
// closed; the tests below pin both halves.
//
// They are worth their weight because sizingRosterConfig() sets
// PayloadByteBudget = 0 and every declared-window test inherits it, so a change
// that stopped clamping either path would otherwise pass the whole suite
// silently.
//
// This is also the interaction that decides whether the epic's headline benefit
// is real: at the shipped default of 524288 the bulk path saturates near a
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
	// unlike quantities. This case covers the recorded BYTE budget only; the
	// per-chunk LINE regime is pinned by the sibling test below.
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

func TestBuildSlots_ChunkedLineBudgetIsClampedByGlobalBudget(t *testing.T) {
	const globalCap = 100000

	// Same roster and same global cap as the bulk case above, so the only
	// difference is the strategy — which is what makes the two paths' agreement
	// visible rather than merely asserted in a comment.
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
	derived := payload.ChunkMaxLines("unlisted-small-model", ptrInt(512000), defaultMaxTokens)
	// 100000 bytes / 48 bytes-per-line = 2083 lines. A literal, not a second call
	// to the clamp: an expectation computed by the code under test asserts nothing.
	const wantCappedLines = 2083
	require.Greater(t, derived, wantCappedLines,
		"precondition: the declaration must derive a line budget the global cap actually binds")

	payloads := map[string]modePayload{"blocks": {Text: diffOfNFiles(60, 900), FileCount: 60}}
	rng := ReviewRange{Base: "a", Head: "b"}

	cappedSlots, _, err := buildSlots(capped, payloads, rng, "", "", true)
	require.NoError(t, err)
	require.Greater(t, len(cappedSlots), 1, "precondition: this diff must split at the clamped line budget")

	uncappedSlots, _, err := buildSlots(uncapped, payloads, rng, "", "", true)
	require.NoError(t, err)
	require.Greater(t, len(uncappedSlots), 1, "precondition: this diff must split at the declared line budget too")

	assert.Equal(t, wantCappedLines, cappedSlots[0].Primary.chunkMaxLines,
		"the per-chunk LINE budget must be clamped to payload_byte_budget — an unclamped 10M-token declaration would otherwise derive a ~34.9 MB chunk")
	assert.Equal(t, derived, uncappedSlots[0].Primary.chunkMaxLines,
		"with NO global cap configured the derived budget stands: the clamp must not invent a ceiling")
	assert.Greater(t, len(cappedSlots), len(uncappedSlots),
		"a smaller line budget must split the same diff into more chunks — proving the clamp reached dispatch, not just the recorded number")
}
