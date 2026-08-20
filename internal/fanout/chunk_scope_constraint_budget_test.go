package fanout

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// embeddedScopeBlock returns the WHOLE SCOPE CONSTRAINT block a prompt carries —
// wrapper instruction plus framed plan — not just the plan body. That is the
// quantity prepended UNCOUNTED to every chunk, so it is the quantity the chunk
// budget has to cover.
func embeddedScopeBlock(t *testing.T, prompt string) string {
	t.Helper()
	const head = "## SCOPE CONSTRAINT\n"
	const end = "\n----- END SPRINT PLAN -----"
	i := strings.Index(prompt, head)
	require.GreaterOrEqual(t, i, 0, "prompt missing SCOPE CONSTRAINT header")
	j := strings.Index(prompt[i:], end)
	require.GreaterOrEqual(t, j, 0, "prompt missing SCOPE CONSTRAINT END marker")
	return prompt[i : i+j+len(end)]
}

// chunk_byte_budget is the operator's statement of how many bytes may ride ONE
// call. The per-chunk LINE budget is clamped to it, but the SCOPE CONSTRAINT plan
// prepended to every chunk was still capped at the PAYLOAD tier (agentBudget/8,
// bounded by max_sprint_plan_bytes), which never consults it — and the ml/8 line
// reservation that was supposed to cover the plan assumed the two budgets were the
// same key. With the generated config's own suggested chunk_byte_budget (65536)
// and the default payload_byte_budget (524288), every chunk of a large-window
// agent carried ~57 KB of diff plus up to 64 KB of uncounted plan against a stated
// 64 KB ceiling.
func TestBuildSlots_ChunkedScopeConstraintFitsChunkByteBudget(t *testing.T) {
	const declared = 128000
	const chunkBudget = int64(64 * 1024)

	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.PayloadByteBudget = 512 * 1024
	cfg.Settings.ChunkByteBudget = chunkBudget
	cfg.Settings.MaxSprintPlanBytes = 64 * 1024

	slots, _, err := buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, 64*1024), true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: the clamped budget must still split this diff into chunks")

	for i, s := range slots {
		// (B) the plan is capped against the chunk ceiling, not the payload tier.
		// Asserted separately from the total below because the total alone does not
		// discriminate: reserving the block's bytes without re-capping the plan also
		// keeps the pair inside the ceiling — by letting a 50 KB plan starve the diff
		// down to 13 KB, which is the other half of what the cap exists to prevent.
		assert.LessOrEqual(t, len(embeddedScopePlan(t, s.Primary.Prompt)), int(chunkBudget/8),
			"chunk slot %d: the plan cap must be derived from chunk_byte_budget, not from the payload-tier agent budget", i)

		blockBytes := len(embeddedScopeBlock(t, s.Primary.Prompt))
		// The chunker packs up to chunkMaxLines lines at the same avgBytesPerLine
		// ratio the clamp derived that budget with, so this is the diff ceiling one
		// chunk can carry.
		chunkBytes := s.Primary.chunkMaxLines * 48
		assert.LessOrEqual(t, int64(blockBytes+chunkBytes), chunkBudget,
			"chunk slot %d: plan (%d B) + diff ceiling (%d B) must fit chunk_byte_budget (%d B) — the plan cap must come from the budget that sizes the call, not from the payload tier",
			i, blockBytes, chunkBytes, chunkBudget)
	}
}
