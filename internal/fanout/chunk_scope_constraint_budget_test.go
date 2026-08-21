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

// A chunk_byte_budget below 8 drops the SCOPE CONSTRAINT entirely and says nothing.
//
// chunkPlanBudget = min(agentBudget, chunk_byte_budget) = the ceiling, and
// capScopeConstraintForBudget funds the plan at budget/8 — which is 0 for any
// ceiling in 1..7, so it returns "" and the review runs UNSCOPED while the agent's
// own window is perfectly healthy. Registry validation only rejects a negative
// value (precedence.go), so nothing upstream catches it either. The operator asked
// for scoping, got none, and has no signal that it happened: "" means both "no plan
// was given" and "the budget could not fund one byte of it", and this is the one
// configuration where those two meanings genuinely separate.
func TestBuildSlots_WarnsWhenChunkByteBudgetCannotFundOnePlanByte(t *testing.T) {
	const declared = 128000
	const planBytes = 64 * 1024
	// 4 is in 1..7: positive (so it is not the "unlimited" sentinel) but too small
	// to fund planCap = budget/8 = 0.
	const chunkBudget = int64(4)

	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.PayloadByteBudget = 512 * 1024
	cfg.Settings.ChunkByteBudget = chunkBudget
	cfg.Settings.MaxSprintPlanBytes = planBytes

	agentBudget := payload.EffectiveByteBudget("unlisted-small-model", ptrInt(declared), defaultMaxTokens)
	require.Greater(t, agentBudget, int64(0),
		"precondition: the agent's OWN budget must be healthy — the drop must be attributable to the chunk ceiling alone")

	var slots []Slot
	out := captureStderr(t, func() {
		var err error
		slots, _, err = buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, planBytes), true)
		require.NoError(t, err)
	})

	require.NotEmpty(t, slots)
	require.NotContains(t, slots[0].Primary.Prompt, "SCOPE CONSTRAINT",
		"precondition: this budget really does drop the block — otherwise the warning has nothing to report")

	assert.Contains(t, out, "chunk_byte_budget",
		"dropping the operator's scoping must name the setting that caused it")
	assert.Contains(t, out, "greta",
		"the warning must name the agent whose scoping was dropped")
}

// The same warning must accuse only the key that actually BOUND. When the agent's OWN
// budget is the binding one — declared window 12289 leaves effectiveTokens 1, so
// EffectiveByteBudget returns 3 — and chunk_byte_budget is UNSET, chunkPlanBudget still
// equals agentBudget and the drop still happens, but "chunk_byte_budget (3 B) is too
// small ... Raise chunk_byte_budget, or unset it" blames an innocent key and prescribes
// a no-op remedy (unsetting an already-unset key). That case belongs to the existing
// zero-budget/overflow reporting, not to this warning.
func TestBuildSlots_DropWarningDoesNotBlameAnUnsetChunkByteBudget(t *testing.T) {
	const declared = 12289 // effectiveTokens 1 → agentBudget 3, in the 1..7 drop band
	const planBytes = 64 * 1024

	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.PayloadByteBudget = 512 * 1024
	cfg.Settings.ChunkByteBudget = 0 // unset: inherits payload_byte_budget only upstream
	cfg.Settings.MaxSprintPlanBytes = planBytes

	agentBudget := payload.EffectiveByteBudget("unlisted-small-model", ptrInt(declared), defaultMaxTokens)
	require.Equal(t, int64(3), agentBudget,
		"precondition: the agent's OWN budget must be the binding one, inside the 1..7 band that drops the block")

	out := captureStderr(t, func() {
		_, _, err := buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, planBytes), true)
		require.NoError(t, err)
	})

	assert.NotContains(t, out, "chunk_byte_budget",
		"chunk_byte_budget is unset here — the binding budget is the agent's own window, so the warning must not accuse it or prescribe unsetting it")
}

// The RUN-LEVEL payload_byte_budget cap must answer the too-small-to-fund-one-byte
// condition the same way the per-agent cap does: DROP the block. Routing through
// capScopeConstraintPlan directly capped the body to 0 and left the BEGIN/END frame
// standing — and the per-agent cap then received that frame as a NON-empty block and
// capped it further rather than dropping it, shipping the "constrain your findings to
// these work items" wrapper over an empty list. payload_byte_budget in 1..7 is valid
// per registry validation (only < 0 is rejected), so budget/8 floors to 0 here.
func TestBuildSlots_RunLevelCapDropsTheBlockWhenItCannotFundOnePlanByte(t *testing.T) {
	const declared = 128000
	const planBytes = 64 * 1024

	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 4 // positive, so not the "unlimited" sentinel; /8 floors to 0
	cfg.Settings.MaxSprintPlanBytes = planBytes

	slots, _, err := buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, planBytes), true)
	require.NoError(t, err)
	require.NotEmpty(t, slots)

	assert.NotContains(t, slots[0].Primary.Prompt, "BEGIN SPRINT PLAN",
		"a payload_byte_budget too small to fund one plan byte must DROP the block, not blank its body and leave the frame standing")
}

// The chunk-drop warning must not fire on the SINGLE-CHUNK fall-through. It is
// emitted before chunkDiff runs and before the `len(chunks) > 1` commit, so a diff
// that yields one chunk still triggers it — and that path then renders
// agentScopeConstraint (the payload-tier block, fully populated), not the dropped
// chunkScopeConstraint. The operator is told the --sprint-plan scoping did not
// apply when it did, and will re-run or discard valid findings.
func TestBuildSlots_ChunkDropWarningStaysSilentOnTheSingleChunkFallThrough(t *testing.T) {
	const declared = 128000
	const planBytes = 64 * 1024
	// Same 1..7 band as the warning's own test: positive, but too small to fund
	// planCap = budget/8, so chunkScopeConstraint really is "".
	const chunkBudget = int64(4)

	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.PayloadByteBudget = 512 * 1024
	cfg.Settings.ChunkByteBudget = chunkBudget
	cfg.Settings.MaxSprintPlanBytes = planBytes

	// One file, 5 added lines — well under the minChunkLines floor (64) that a
	// chunkBudget of 4 clamps the per-chunk line budget to, so chunkDiff returns
	// exactly ONE chunk and the persona falls through to the bulk path.
	payloads := map[string]modePayload{"blocks": {Text: fileSeg("only.go", 5), FileCount: 1}}

	var slots []Slot
	out := captureStderr(t, func() {
		var err error
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, planBytes), true)
		require.NoError(t, err)
	})

	require.Len(t, slots, 1, "precondition: a single-chunk diff must produce exactly one bulk slot")
	require.Contains(t, slots[0].Primary.Prompt, "SCOPE CONSTRAINT",
		"precondition: the bulk fall-through renders agentScopeConstraint, so the scoping DID apply on this path")

	assert.NotContains(t, out, "the block was DROPPED",
		"the chunk-drop warning must not fire on the single-chunk fall-through: that path ships agentScopeConstraint, so reporting the scoping as dropped is false")
}
