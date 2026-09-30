package doctor

import (
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A tool-loop agent's payload is sized against its output cap plus the
// replayed-reasoning reserve, so doctor must judge its budget the same way: a
// window that funds a single-shot agent can leave a tool-loop agent nothing.
func TestZeroBudgetVerdict_ToolLoopAgentCountsTheReasoningReserve(t *testing.T) {
	const window, cap = 32768, 10000
	w := window
	require.Positive(t, payload.EffectiveByteBudget("m", &w, cap),
		"precondition: the plain output-cap reservation still funds a payload")
	require.Zero(t, payload.EffectiveByteBudget("m", &w, payload.SizingOutputTokens(true, cap)),
		"precondition: the tool-loop reservation closes the budget")

	status, hint, fired := zeroBudgetVerdict("m", window, cap, cap, StatusOK, true)
	require.True(t, fired, "review sizes this agent to zero input budget, so doctor must say so")
	assert.Equal(t, StatusOKWarning, status)
	assert.Contains(t, hint, "reasoning")
	assert.Contains(t, hint, zeroBudgetRemedy)

	_, _, fired = zeroBudgetVerdict("m", window, cap, cap, StatusOK, false)
	assert.False(t, fired, "a single-shot agent on the same window keeps its input budget")
}

// Resolve marks an agent as running the tool loop only when its LANE requests
// tools (a fallback inherits the primary's tools, as review's buildFallbackAgent
// does) and the agent's OWN model declares function calling.
func TestResolve_AgentTargetCarriesToolLoop(t *testing.T) {
	provs := map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}}
	for _, tc := range []struct {
		name                    string
		headTools, headFC, fbFC bool
		fbOwnTools              bool
		wantHead, wantFallback  bool
	}{
		{name: "tool lane, both capable", headTools: true, headFC: true, fbFC: true, wantHead: true, wantFallback: true},
		{name: "tools without function calling degrades", headTools: true, headFC: false, fbFC: false},
		{name: "fallback inherits the lane's tools", headTools: true, headFC: true, fbFC: true, fbOwnTools: false, wantHead: true, wantFallback: true},
		{name: "fallback's own tools ignored on a non-tool lane", headTools: false, headFC: true, fbFC: true, fbOwnTools: true},
		{name: "incapable fallback degrades on a tool lane", headTools: true, headFC: true, fbFC: false, wantHead: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := regWith(provs, map[string]registry.AgentConfig{
				"a":  {Provider: "p", Model: "m1", Tools: tc.headTools, SupportsFC: tc.headFC, Fallback: "fb"},
				"fb": {Provider: "p", Model: "m2", Tools: tc.fbOwnTools, SupportsFC: tc.fbFC},
			})
			res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
			require.NoError(t, err)
			got := map[string]bool{}
			for _, at := range res.Agents {
				got[at.Agent] = at.ToolLoop
			}
			assert.Equal(t, tc.wantHead, got["a"], "head")
			assert.Equal(t, tc.wantFallback, got["fb"], "fallback")
		})
	}
}

// An agent reached through two lanes is sized for the tool loop if EITHER lane
// runs it there: doctor's row is per agent, and the tool-loop reservation is the
// one that can close its budget.
func TestResolve_ToolLoopIsTrueWhenAnyLaneRunsIt(t *testing.T) {
	provs := map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}}
	reg := regWith(provs, map[string]registry.AgentConfig{
		"plain":  {Provider: "p", Model: "m1", Fallback: "shared"},
		"tooly":  {Provider: "p", Model: "m2", Tools: true, SupportsFC: true, Fallback: "shared"},
		"shared": {Provider: "p", Model: "m3", SupportsFC: true},
	})
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"plain", "tooly"}})
	require.NoError(t, err)
	for _, at := range res.Agents {
		if at.Agent == "shared" {
			assert.True(t, at.ToolLoop)
			return
		}
	}
	t.Fatal("shared fallback missing from the resolution")
}
