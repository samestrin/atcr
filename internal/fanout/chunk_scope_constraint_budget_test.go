package fanout

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// embeddedScopeBlock returns the WHOLE SCOPE CONSTRAINT block a prompt carries —
// wrapper instruction plus framed plan — not just the plan body. That is the
// quantity prepended UNCOUNTED to every chunk, so it is the quantity the chunk
// budget has to cover.
//
// The trailing blank line is part of the block: payload.ScopeConstraint closes
// with "\n----- END SPRINT PLAN -----\n\n", and those two bytes ride every chunk
// like the rest of it. Stopping at the END marker undercounted the block by 2,
// which is invisible to a LessOrEqual bound but makes an exact byte assertion
// against the recorded budget off by exactly that much.
func embeddedScopeBlock(t *testing.T, prompt string) string {
	t.Helper()
	const head = "## SCOPE CONSTRAINT\n"
	const end = "\n----- END SPRINT PLAN -----\n\n"
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

// TestBuildSlots_ChunkedScopeReservationBindsUnderInheritedChunkBudget is the
// same (A) reservation as above, on the config every real run actually has.
//
// The test above sets chunk_byte_budget BELOW the agent budget, which is the one
// regime where clamping to `cfg.Settings.ChunkByteBudget` binds. Production never
// looks like that by default: ResolveSettings makes an unset chunk_byte_budget
// inherit payload_byte_budget (precedence.go), so it resolves to 524288 — larger
// than a 32768-token agent's whole budget. The byte reservation then does not
// bind (the ceiling is above the budget) and the `ml -= ml/8` line fallback does
// not run either (it is gated on chunk_byte_budget <= 0, which a resolved config
// never is), so the block rides every chunk entirely uncounted.
//
// The reservation must therefore come from the budget that actually sizes the
// call — min(agentBudget, chunk_byte_budget) — not from the operator ceiling
// alone. Asserted against the agent budget because that is the quantity a
// too-large chunk overruns here.
func TestBuildSlots_ChunkedScopeReservationBindsUnderInheritedChunkBudget(t *testing.T) {
	const declared = 32768
	const planBytes = 64 * 1024
	// The resolved default: chunk_byte_budget is unset, so it inherits
	// payload_byte_budget. Both are set here because this fixture builds Settings
	// directly rather than through ResolveSettings.
	const defaultBudget = int64(512 * 1024)

	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.PayloadByteBudget = defaultBudget
	cfg.Settings.ChunkByteBudget = defaultBudget
	cfg.Settings.MaxSprintPlanBytes = planBytes

	agentBudget := payload.EffectiveByteBudget("unlisted-small-model", ptrInt(declared), defaultMaxTokens)
	require.Less(t, agentBudget, defaultBudget,
		"precondition: the inherited chunk ceiling must EXCEED this agent's budget, or the regression regime is not reproduced")

	slots, _, err := buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, planBytes), true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: this diff must still split into chunks")

	for i, s := range slots {
		blockBytes := len(embeddedScopeBlock(t, s.Primary.Prompt))
		// Same avgBytesPerLine ratio the clamp derives the budget with, so this is
		// the diff ceiling one chunk can carry.
		chunkBytes := s.Primary.chunkMaxLines * 48
		assert.LessOrEqual(t, int64(blockBytes+chunkBytes), agentBudget,
			"chunk slot %d: plan block (%d B) + diff ceiling (%d B) must fit the agent budget (%d B) — the uncounted block must be reserved against min(agentBudget, chunk_byte_budget), not against the inherited ceiling alone",
			i, blockBytes, chunkBytes, agentBudget)
	}
}

// TestBuildSlots_ChunkedEffectiveBudgetReservesTheBlockItShips pins the chunked
// sizing record against the prompt it describes.
//
// effective_budget reserved len(agentScopeConstraint) — the PAYLOAD-tier block —
// while renderAgent ships chunkScopeConstraint, capped against
// min(agentBudget, chunk_byte_budget). The two diverge whenever the operator sets
// a chunk ceiling below the agent budget, and the record then understates the
// budget the prompt was actually sized to. Three consumers read that number and
// draw a false conclusion from it; the sharpest is the AC4 fallback gate
// (`fbBudget < primary.EffectiveBudget`), which SKIPS the overflow check and the
// truncate re-fit for every fallback budget sitting between the understated
// figure and the real one.
func TestBuildSlots_ChunkedEffectiveBudgetReservesTheBlockItShips(t *testing.T) {
	const declared = 128000
	const chunkBudget = int64(64 * 1024)
	const payloadBudget = int64(512 * 1024)
	const planBytes = 64 * 1024

	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.PayloadByteBudget = payloadBudget
	cfg.Settings.ChunkByteBudget = chunkBudget
	cfg.Settings.MaxSprintPlanBytes = planBytes

	agentBudget := payload.EffectiveByteBudget("unlisted-small-model", ptrInt(declared), defaultMaxTokens)
	require.Less(t, chunkBudget, agentBudget,
		"precondition: the chunk ceiling must sit BELOW the agent budget, or the two scope blocks do not diverge")

	slots, _, err := buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, planBytes), true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: this diff must still split into chunks")

	capped := agentBudget
	if payloadBudget < capped {
		capped = payloadBudget
	}
	for i, s := range slots {
		shipped := int64(len(embeddedScopeBlock(t, s.Primary.Prompt)))
		assert.Equal(t, capped-shipped, s.Primary.EffectiveBudget,
			"chunk slot %d: effective_budget must reserve the block the prompt CARRIES (%d B), not the payload-tier block it does not", i, shipped)
	}
}

// TestBuildSlots_ChunkClampFloorKeepsTheCeilingWhenTheBlockEatsTheBudget covers
// the `chunkClampBudget = 1` floor, and covers it as a LOAD-BEARING guard rather
// than as an executed line.
//
// Reserving the block's bytes can drive the remaining chunk budget to zero or
// below whenever chunk_byte_budget is at or under the block's own length. A
// non-positive budget is not "a very tight ceiling" to ClampLinesToByteBudget —
// it is the settings-tier "no cap configured" sentinel, and the function returns
// maxLines UNTOUCHED for it (payload/sizing.go). So without the floor the tightest
// possible ceiling does not clamp hardest; it deletes the ceiling outright and the
// chunks revert to the raw model window, which is the opposite of what the
// operator asked for and the largest possible overshoot.
//
// The floor turns that into the intended degradation: a budget of 1 clamps to the
// minChunkLines floor. Asserted as the literal 64 rather than read back from the
// unexported constant, so a change to either the floor or this behavior has to be
// acknowledged here.
func TestBuildSlots_ChunkClampFloorKeepsTheCeilingWhenTheBlockEatsTheBudget(t *testing.T) {
	const declared = 128000
	const planBytes = 64 * 1024
	// Small enough that the scope block (fixed ~700-byte wrapper + a plan capped to
	// chunkPlanBudget/8) is LONGER than the whole ceiling, which is what drives the
	// subtraction non-positive. Still >= 8, so the plan cap funds at least one byte
	// and the block is capped rather than dropped.
	const chunkBudget = int64(512)
	const wantLines = 64 // payload.minChunkLines

	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.PayloadByteBudget = 512 * 1024
	cfg.Settings.ChunkByteBudget = chunkBudget
	cfg.Settings.MaxSprintPlanBytes = planBytes

	unclamped := payload.ChunkMaxLines("unlisted-small-model", ptrInt(declared), defaultMaxTokens)
	require.Equal(t, 8437, unclamped,
		"precondition: the raw model window derives 8437 lines — the value the chunks revert to if the floor is removed")

	slots, _, err := buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, planBytes), true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: this diff must still split into chunks")

	blockBytes := len(embeddedScopeBlock(t, slots[0].Primary.Prompt))
	require.Greater(t, int64(blockBytes), chunkBudget,
		"precondition: the block (%d B) must exceed the ceiling (%d B), or the reservation never goes non-positive and the floor is untested",
		blockBytes, chunkBudget)

	for i, s := range slots {
		assert.Equal(t, wantLines, s.Primary.chunkMaxLines,
			"chunk slot %d: a budget the block exhausts must clamp to the minChunkLines floor, not fall back to the unclamped %d-line window", i, unclamped)
	}
}
