package fanout

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A zero input budget funds not even one byte of plan. Capping the SCOPE
// CONSTRAINT block to 0 bytes leaves the BEGIN/END frame and the wrapper
// instruction ("Constrain your findings to files and changes directly related to
// these work items") intact around an EMPTY work-item list — so the reviewer is
// told to suppress everything below "genuinely critical", and returns a
// structurally clean StatusOK review with near-zero findings that no truncation
// signal catches.
//
// The codebase already answers this exact condition twice, both by DROPPING the
// block: payload.ScopeConstraint (sprintplan.go, maxBytes <= 0) and
// refitFallbackPayload (review.go, planCap < 1). This pins the primary per-agent
// path to the same answer.
func TestBuildOneAgent_ZeroBudgetDropsScopeConstraintRatherThanBlankingIt(t *testing.T) {
	cfg := zeroBudgetCfg(t, OverflowChunk)
	cfg.Settings.MaxSprintPlanBytes = 64 * 1024

	agent, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, 4096))
	require.NoError(t, err)
	require.Zero(t, agent.EffectiveBudget, "precondition: a 12288-token window funds no input budget")

	assert.NotContains(t, agent.Prompt, "BEGIN SPRINT PLAN",
		"a zero-budget agent must not receive a SCOPE CONSTRAINT frame wrapped around an empty plan body")
	assert.NotContains(t, agent.Prompt, "SCOPE CONSTRAINT",
		"the wrapper instruction must be dropped with the plan it refers to, not left instructing the model to obey nothing")
}

// The fallback-refit sibling of the primary-path case above. refitFallbackPayload
// re-caps the inherited SCOPE CONSTRAINT only when fbBudget > 0, so at fbBudget == 0
// the cap was skipped ENTIRELY and the backup inherited the PRIMARY's plan uncapped —
// up to min(max_sprint_plan_bytes, primary budget/8), 64 KiB at shipped defaults.
// That is the exact failure the block's own comment describes ("a plan sized for a
// 128k primary would ride whole into a 32k backup"), and the `planCap < 1` drop
// below it covered fbBudget 1..7 but never 0, the one budget that most needs it.
//
// Newly reachable: fbBudget is computed from the fallback's own resolved max_tokens,
// so a max_tokens declaration alone can close it.
func TestBuildFallbackAgent_RefitDropsScopeConstraintAtZeroBudget(t *testing.T) {
	cfg := refitRoster(t, 512000, OverflowTruncate)
	kai := cfg.Registry.Agents["kai"]
	w := 12288 // exactly defaultMaxTokens + promptOverheadTokens → effective budget 0
	kai.ContextWindowTokens = &w
	cfg.Registry.Agents["kai"] = kai
	require.Zero(t, payload.EffectiveByteBudget("unlisted-backup-model", &w, defaultMaxTokens),
		"precondition: this window funds no input budget at all — the arm the cap was skipped on")

	scope, _ := payload.ScopeConstraint(strings.Repeat("plan line\n", 8000), registry.DefaultMaxSprintPlanBytes)

	var slots []Slot
	var err error
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, markedOversizedPayload(), ReviewRange{Base: "a", Head: "b"}, "blocks", scope, true)
	})
	require.NoError(t, err)
	require.Len(t, slots[0].Fallbacks, 1)
	fb := slots[0].Fallbacks[0]

	assert.NotContains(t, fb.Prompt, "BEGIN SPRINT PLAN",
		"a zero-budget backup must not inherit the primary's plan uncapped — fbBudget 0 must behave like fbBudget 1..7 and drop the block")
}
