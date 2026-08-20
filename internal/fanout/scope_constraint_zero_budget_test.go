package fanout

import (
	"testing"

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
