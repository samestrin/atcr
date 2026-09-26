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

// declaredRegistry resolves agents that share one endpoint and model and differ only
// in the fields the response_format probes key on.
func declaredRegistry(t *testing.T, agents map[string]registry.AgentConfig) *Resolution {
	t.Helper()
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{"p": {BaseURL: "http://one-endpoint", APIKeyEnv: "K"}},
		Agents:    map[string]registry.AgentConfig{},
	}
	proj := &registry.ProjectConfig{}
	for _, name := range []string{"a", "b"} {
		ac, ok := agents[name]
		if !ok {
			continue
		}
		ac.Provider, ac.Model = "p", "same-model"
		reg.Agents[name] = ac
		proj.Agents = append(proj.Agents, name)
	}
	res, err := Resolve(reg, proj)
	require.NoError(t, err)
	return res
}

// A declared agent's calls carry response_format and an undeclared agent's do not, so
// one probe cannot speak for both: the JSON-mode invocation would never be the one
// probed, the MaxTokens identity bug recurring for a new field.
func TestResolve_ResponseFormatSplitsADeclaredAndUndeclaredAgent(t *testing.T) {
	res := declaredRegistry(t, map[string]registry.AgentConfig{
		"a": {ResponseFormat: registry.ResponseFormatJSONObject},
		"b": {},
	})

	require.Len(t, res.Targets, 2, "declared and undeclared agents make different calls")
	assert.Equal(t, registry.ResponseFormatJSONObject, targetForAgent(t, res, "a").ResponseFormat)
	assert.Empty(t, targetForAgent(t, res, "b").ResponseFormat)
}

func TestResolve_AgreeingResponseFormatDeclarationsShareOneTarget(t *testing.T) {
	res := declaredRegistry(t, map[string]registry.AgentConfig{
		"a": {ResponseFormat: registry.ResponseFormatJSONObject},
		"b": {ResponseFormat: registry.ResponseFormatJSONObject},
	})

	assert.Len(t, res.Targets, 1, "sharers that agree still collapse to one probe")
}

// A MaxTokens difference already split these two; the new segment must not change a
// count that was already 2.
func TestResolve_ResponseFormatWithADistinctCapStaysTwoTargets(t *testing.T) {
	res := declaredRegistry(t, map[string]registry.AgentConfig{
		"a": {ResponseFormat: registry.ResponseFormatJSONObject, MaxTokens: capOf(8000)},
		"b": {},
	})

	assert.Len(t, res.Targets, 2)
}

// Two declared agents, one running the tool loop and one not: only the tool-loop one
// makes the tools+response_format call, so only it may run the combined probe.
func TestResolve_DeclaredToolLoopAgentGetsItsOwnTarget(t *testing.T) {
	res := declaredRegistry(t, map[string]registry.AgentConfig{
		"a": {ResponseFormat: registry.ResponseFormatJSONObject, Tools: true, SupportsFC: true},
		"b": {ResponseFormat: registry.ResponseFormatJSONObject},
	})

	require.Len(t, res.Targets, 2)
	assert.True(t, targetForAgent(t, res, "a").Tools)
	assert.False(t, targetForAgent(t, res, "b").Tools)
}

// tools without supports_function_calling degrades to single-shot, so it runs no tool
// loop and a combined probe would test a call the real run never makes.
func TestResolve_ToolsWithoutFunctionCallingIsNotAToolLoop(t *testing.T) {
	res := declaredRegistry(t, map[string]registry.AgentConfig{
		"a": {ResponseFormat: registry.ResponseFormatJSONObject, Tools: true},
	})

	require.Len(t, res.Targets, 1)
	assert.False(t, res.Targets[0].Tools)
}

// The Tools segment joins the key only for declared agents. Undeclared agents dedupe
// exactly as before this field existed, so doctor output does not change for agents
// that never opted in.
func TestResolve_UndeclaredAgentsKeepTodaysDedupRegardlessOfTools(t *testing.T) {
	res := declaredRegistry(t, map[string]registry.AgentConfig{
		"a": {Tools: true, SupportsFC: true},
		"b": {},
	})

	require.Len(t, res.Targets, 1, "an undeclared tool-loop agent must not split its target")
	assert.False(t, res.Targets[0].Tools, "and Tools stays unset where it is not identity")
	assert.Empty(t, res.Targets[0].ResponseFormat)
}
