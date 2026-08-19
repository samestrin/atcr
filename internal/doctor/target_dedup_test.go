package doctor

import (
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func capOf(v int) *int { return &v }

func sharedEndpointRegistry(caps ...int) (*registry.Registry, *registry.ProjectConfig) {
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{
			"p": {BaseURL: "http://one-endpoint", APIKeyEnv: "K"},
		},
		Agents: map[string]registry.AgentConfig{},
	}
	proj := &registry.ProjectConfig{}
	for i, c := range caps {
		name := string(rune('a' + i))
		ac := registry.AgentConfig{Provider: "p", Model: "same-model"}
		if c > 0 {
			ac.MaxTokens = capOf(c)
		}
		reg.Agents[name] = ac
		proj.Agents = append(proj.Agents, name)
	}
	return reg, proj
}

// Target identity gained the declared max_tokens because a probe is only evidence
// about the invocation it reproduces. But probe() DISCARDS the declaration whenever
// --max-tokens is set, so under the flag those "distinct" targets resolve to the same
// cap and produce byte-identical invocations — same base_url, model, resolved cap and
// nonce. The widened key then buys nothing and costs one extra live call per agent.
//
// That is not merely wasteful: against a quota-limited upstream the tail of those
// duplicate calls returns 429, the agent is classified rate_limited rather than
// healthy, and doctor exits 1 — reporting a broken roster it broke itself.
func TestResolve_FlagOverrideCollapsesTargetsThatWouldProbeIdentically(t *testing.T) {
	reg, proj := sharedEndpointRegistry(2000, 8000, 32000)

	res, err := ResolveWithCap(reg, proj, 4096)
	require.NoError(t, err)

	assert.Len(t, res.Targets, 1,
		"an explicit --max-tokens overrides every declaration, so all three agents make "+
			"the SAME call and must share one probe")
	assert.Equal(t, 4096, res.Targets[0].MaxTokens,
		"and the target must carry the cap actually probed, not a declaration the flag overrode")
}

// Without the flag the declarations DO decide the invocation, so distinct caps stay
// distinct probes — the property the widened key was added for.
func TestResolve_WithoutTheFlagDistinctDeclarationsStayDistinctTargets(t *testing.T) {
	reg, proj := sharedEndpointRegistry(2000, 8000, 32000)

	res, err := ResolveWithCap(reg, proj, 0)
	require.NoError(t, err)

	assert.Len(t, res.Targets, 3,
		"each declared cap is a different invocation and owes its own probe")
}

// Agents that AGREE still dedupe, which is the common case.
func TestResolve_AgreeingDeclarationsShareOneTarget(t *testing.T) {
	reg, proj := sharedEndpointRegistry(8000, 8000)

	res, err := ResolveWithCap(reg, proj, 0)
	require.NoError(t, err)

	assert.Len(t, res.Targets, 1, "identical declarations reproduce one invocation")
}

// Resolve keeps its two-argument shape for callers that have no override.
func TestResolve_DefaultsToNoOverride(t *testing.T) {
	reg, proj := sharedEndpointRegistry(2000, 8000)

	res, err := Resolve(reg, proj)
	require.NoError(t, err)

	assert.Len(t, res.Targets, 2, "Resolve is ResolveWithCap with no override")
}
