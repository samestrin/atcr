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

// The `chunk_byte_budget < agentBudget` conjunct is the binding-key discriminator —
// the whole subject of the commit that replaced `agentBudget > 0` with it — and its
// own RED test cannot pin it: that test sets ChunkByteBudget = 0, so its silence is
// already guaranteed by the sibling `> 0` conjunct and survives this one's deletion.
// Pin it where the two conjuncts separate: a POSITIVE ceiling sitting at or ABOVE
// the agent's own budget, with both in the 1..7 band so the block really is dropped.
// The drop is then the window's doing and blaming chunk_byte_budget would accuse an
// innocent key whose prescribed remedy (unset it) is a no-op.
func TestBuildSlots_DropWarningDoesNotBlameAChunkBudgetAboveTheAgentBudget(t *testing.T) {
	const declared = 12289 // effectiveTokens 1 → agentBudget 3, in the 1..7 drop band
	const planBytes = 64 * 1024
	// Positive (so the `> 0` conjunct is satisfied and cannot stand in for this one)
	// and ABOVE agentBudget, so chunkPlanBudget stays the agent's own budget.
	const chunkBudget = int64(4)

	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.PayloadByteBudget = 512 * 1024
	cfg.Settings.ChunkByteBudget = chunkBudget
	cfg.Settings.MaxSprintPlanBytes = planBytes

	agentBudget := payload.EffectiveByteBudget("unlisted-small-model", ptrInt(declared), defaultMaxTokens)
	require.Equal(t, int64(3), agentBudget,
		"precondition: the agent's OWN budget must be the binding one")
	require.Greater(t, chunkBudget, agentBudget,
		"precondition: the operator ceiling must sit ABOVE the agent budget — that is what makes it non-binding")

	var slots []Slot
	out := captureStderr(t, func() {
		var err error
		slots, _, err = buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, planBytes), true)
		require.NoError(t, err)
	})

	require.NotEmpty(t, slots)
	require.NotContains(t, slots[0].Primary.Prompt, "SCOPE CONSTRAINT",
		"precondition: the block really is dropped here — otherwise the warning has nothing to stay silent about")

	assert.NotContains(t, out, "chunk_byte_budget",
		"chunk_byte_budget sits above the agent budget and did not bind — the warning must not accuse it or prescribe raising a ceiling that is already too high to matter")
}

// warnOversized is the resume-path suppressor every sibling warning honours:
// PrepareResume rebuilds pending slots for a run whose operator was already told
// this during the original preparation, so re-emitting it makes a resume look like
// a fresh misconfiguration. Deleting the conjunct leaves the suite green because
// every other case in this file passes true.
func TestBuildSlots_ChunkDropWarningIsSilentOnTheResumeRebuild(t *testing.T) {
	const declared = 128000
	const planBytes = 64 * 1024
	const chunkBudget = int64(4)

	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.PayloadByteBudget = 512 * 1024
	cfg.Settings.ChunkByteBudget = chunkBudget
	cfg.Settings.MaxSprintPlanBytes = planBytes

	out := captureStderr(t, func() {
		// warnOversized=false — the resume rebuild path.
		_, _, err := buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, planBytes), false)
		require.NoError(t, err)
	})

	assert.NotContains(t, out, "chunk_byte_budget",
		"the resume rebuild must stay quiet — the operator was already warned during the original preparation")
}

// `len(scopeConstraint) > 0` is what keeps the warning about a DROPPED plan rather
// than an absent one. With no --sprint-plan there is nothing to drop, so
// chunkScopeConstraint is "" for the ordinary reason and the remaining conjuncts are
// all satisfied — deleting this one makes every unscoped chunked review with a tiny
// chunk_byte_budget report a scoping loss that never happened.
func TestBuildSlots_ChunkDropWarningIsSilentWithoutASprintPlan(t *testing.T) {
	const declared = 128000
	const chunkBudget = int64(4)

	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.PayloadByteBudget = 512 * 1024
	cfg.Settings.ChunkByteBudget = chunkBudget
	cfg.Settings.MaxSprintPlanBytes = 64 * 1024

	out := captureStderr(t, func() {
		// Empty scope constraint — no --sprint-plan was passed.
		_, _, err := buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.NoError(t, err)
	})

	assert.NotContains(t, out, "chunk_byte_budget",
		"no --sprint-plan was given, so nothing was dropped — the warning must not report a scoping loss that never happened")
}

// buildChain's scopeConstraint argument on the chunked-DIFF path is INERT, and no
// mutation test in this package can pin it: substituting the payload-tier block
// there changes no output. The argument reaches only fallbackRefit.scopeConstraint,
// which is read in exactly one place — refitFallbackPayload — and that runs only
// when refit.canRefit(), i.e. when the slot carries a FileEntry re-pack list. The
// chunked-diff path splits TEXT on diff markers and has no entry list at all, so it
// passes nil and the fallback inherits its primary's prompt verbatim.
//
// Pin that coupling here rather than asserting the block's bytes: a byte assertion
// on the fallback prompt is guaranteed by prompt INHERITANCE, so it passes with the
// argument mutated and pins nothing. If Epic 35.16.5.4's re-fit is ever extended to
// thread entries into this call, the first assertion below fails — and that is the
// point at which the argument becomes live and needs a real byte assertion.
func TestBuildSlots_ChunkedFallbackDeclinesTheRefitSoTheScopeArgumentIsInert(t *testing.T) {
	const declared = 128000
	const planBytes = 64 * 1024
	const chunkBudget = int64(64 * 1024)

	cfg := declaredWindowRoster(t, declared)
	greta := cfg.Registry.Agents["greta"]
	greta.Fallback = "kai"
	cfg.Registry.Agents["greta"] = greta
	// A fallback whose own window is far smaller than the primary's is exactly the
	// state the truncate re-fit exists for — so if the chunked path could re-fit,
	// this config is where it would.
	kai := cfg.Registry.Agents["kai"]
	kai.ContextWindowTokens = ptrInt(16384)
	cfg.Registry.Agents["kai"] = kai
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.OnOverflow = OverflowTruncate
	cfg.Settings.PayloadByteBudget = 512 * 1024
	cfg.Settings.ChunkByteBudget = chunkBudget
	cfg.Settings.MaxSprintPlanBytes = planBytes

	slots, _, err := buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, planBytes), true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: the clamped budget must still split this diff into chunks")
	require.NotEmpty(t, slots[0].Fallbacks, "precondition: greta must resolve a fallback chain")

	assert.Empty(t, slots[0].entries,
		"the chunked-diff path has no FileEntry list, so its fallback declines the re-fit — this is what makes buildChain's scopeConstraint argument unreachable on this path")

	for i, fb := range slots[0].Fallbacks {
		assert.Equal(t, slots[0].Primary.Prompt, fb.Prompt,
			"fallback %d (%s): with no re-pack source the fallback must inherit the primary's prompt verbatim — a differing prompt means a re-fit ran and refit.scopeConstraint became live", i, fb.Name)
	}
}

// The RUN-LEVEL drop is the exact condition the chunk-tier drop warns about, one
// tier up — and it was silent. With payload_byte_budget in 1..7 (legal: registry
// validation rejects only negatives) budget/8 floors to 0, capScopeConstraintForBudget
// returns "" and the block is dropped RUN-WIDE, for every agent. The chunk-tier
// warning cannot cover for it either: that one gates on len(scopeConstraint) > 0,
// which this site has just emptied. Meanwhile resolveScopeConstraint's own warning
// still says the plan was "truncated before injection" while nothing was injected,
// so the operator concludes from the absence of output that --sprint-plan was
// honoured and every reviewer in the run reviews unscoped.
func TestBuildSlots_WarnsWhenPayloadByteBudgetCannotFundOnePlanByte(t *testing.T) {
	const declared = 128000
	const planBytes = 64 * 1024

	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	// Positive, so not the "unlimited" sentinel; /8 floors to 0.
	cfg.Settings.PayloadByteBudget = 4
	cfg.Settings.MaxSprintPlanBytes = planBytes

	var slots []Slot
	out := captureStderr(t, func() {
		var err error
		slots, _, err = buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, planBytes), true)
		require.NoError(t, err)
	})

	require.NotEmpty(t, slots)
	require.NotContains(t, slots[0].Primary.Prompt, "SCOPE CONSTRAINT",
		"precondition: this budget really does drop the block run-wide")

	assert.Contains(t, out, "payload_byte_budget",
		"dropping the operator's scoping run-wide must name the setting that caused it, the way the chunk-tier warning names chunk_byte_budget")
	assert.Contains(t, out, "DROPPED",
		"and say the block was dropped, not truncated")
}

// The run-level warning must stay quiet on the resume rebuild, like every sibling.
func TestBuildSlots_RunLevelDropWarningIsSilentOnTheResumeRebuild(t *testing.T) {
	const declared = 128000
	const planBytes = 64 * 1024

	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 4
	cfg.Settings.MaxSprintPlanBytes = planBytes

	out := captureStderr(t, func() {
		_, _, err := buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, planBytes), false)
		require.NoError(t, err)
	})

	assert.NotContains(t, out, "payload_byte_budget",
		"the resume rebuild must stay quiet — the operator was already warned during the original preparation")
}
