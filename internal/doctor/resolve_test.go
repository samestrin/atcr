package doctor

import (
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// regWith builds a registry from a provider map and agent map for tests.
func regWith(providers map[string]registry.Provider, agents map[string]registry.AgentConfig) *registry.Registry {
	return &registry.Registry{Providers: providers, Agents: agents}
}

// targetForAgent returns the resolved Target a named agent row maps to.
func targetForAgent(t *testing.T, res *Resolution, name string) Target {
	t.Helper()
	for _, at := range res.Agents {
		if at.Agent == name {
			return res.Targets[at.TargetIdx]
		}
	}
	t.Fatalf("agent %q not present in resolution", name)
	return Target{}
}

func TestResolve_DedupsSharedTarget(t *testing.T) {
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{
			"a": {Provider: "p", Model: "m1"},
			"b": {Provider: "p", Model: "m1"}, // same provider+model+base_url as a
		},
	)
	proj := &registry.ProjectConfig{Agents: []string{"a", "b"}}

	res, err := Resolve(reg, proj)
	require.NoError(t, err)
	assert.Len(t, res.Targets, 1, "shared (provider,model,base_url) collapses to one target")
	assert.Len(t, res.Agents, 2, "both agents still get their own row")
	assert.Equal(t, targetForAgent(t, res, "a"), targetForAgent(t, res, "b"))
}

func TestResolve_DistinctModelsAreSeparateTargets(t *testing.T) {
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{
			"a": {Provider: "p", Model: "m1"},
			"b": {Provider: "p", Model: "m2"},
		},
	)
	proj := &registry.ProjectConfig{Agents: []string{"a", "b"}}

	res, err := Resolve(reg, proj)
	require.NoError(t, err)
	assert.Len(t, res.Targets, 2)
}

func TestResolve_IncludesFallbackAgents(t *testing.T) {
	reg := regWith(
		map[string]registry.Provider{
			"p1": {APIKeyEnv: "K1", BaseURL: "https://a.example/v1"},
			"p2": {APIKeyEnv: "K2", BaseURL: "https://b.example/v1"},
		},
		map[string]registry.AgentConfig{
			"a": {Provider: "p1", Model: "m1", Fallback: "b"},
			"b": {Provider: "p2", Model: "m2"},
		},
	)
	proj := &registry.ProjectConfig{Agents: []string{"a"}}

	res, err := Resolve(reg, proj)
	require.NoError(t, err)
	// Effective roster includes the fallback agent as its own row + target.
	assert.Len(t, res.Targets, 2)
	names := map[string]bool{}
	for _, at := range res.Agents {
		names[at.Agent] = true
	}
	assert.True(t, names["a"] && names["b"], "fallback agent b must be tested too")
	assert.Equal(t, []string{"a", "b"}, res.Paths["a"], "listed agent path = self + fallback chain")
}

func TestResolve_SerialLaneMarked(t *testing.T) {
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{
			"par": {Provider: "p", Model: "m1"},
			"ser": {Provider: "p", Model: "m2"},
		},
	)
	proj := &registry.ProjectConfig{Agents: []string{"par"}, SerialAgents: []string{"ser"}}

	res, err := Resolve(reg, proj)
	require.NoError(t, err)
	serialOf := map[string]bool{}
	for _, at := range res.Agents {
		serialOf[at.Agent] = at.Serial
	}
	assert.False(t, serialOf["par"])
	assert.True(t, serialOf["ser"])
	assert.Contains(t, res.Paths, "par")
	assert.Contains(t, res.Paths, "ser")
}

func TestResolve_SerialOnlyRosterValid(t *testing.T) {
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"ser": {Provider: "p", Model: "m"}},
	)
	proj := &registry.ProjectConfig{SerialAgents: []string{"ser"}}

	res, err := Resolve(reg, proj)
	require.NoError(t, err)
	assert.Len(t, res.Agents, 1)
	assert.Len(t, res.Targets, 1)
}

func TestResolve_UnknownProviderErrors(t *testing.T) {
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {Provider: "missing", Model: "m"}},
	)
	proj := &registry.ProjectConfig{Agents: []string{"a"}}

	_, err := Resolve(reg, proj)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider")
}

func TestResolve_UnknownAgentErrors(t *testing.T) {
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {Provider: "p", Model: "m"}},
	)
	proj := &registry.ProjectConfig{Agents: []string{"ghost"}}

	_, err := Resolve(reg, proj)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost")
}

// atcr doctor probed every target at opts.MaxTokens (default 2048) and never consulted
// the agent's declared max_tokens, so an agent that declares 32000 was probed at 2048,
// classified ok_warning, and told to "raise --max-tokens" — the very cap it had already
// raised. config.go cites that hint as the field's motivation, so the doctor
// contradicting it is the worst possible surface for the miss.
//
// Like ContextWindowTokens the declaration is per-AGENT while probes run per-TARGET.
// Collapsing sharers onto the LARGEST declaration fixed the false hint but bought a
// false NEGATIVE with it: `atcr review` resolves the cap PER AGENT (resolveMaxTokens),
// so an agent probed at a sharer's 32000 is invoked for real at its own — far smaller —
// cap. The marker-absent ok_warning the probe exists to raise was then suppressed for
// the smaller declarer, and doctor exited 0 on an agent that truncates to zero findings
// on the real run. "Extra headroom cannot make a smaller declarer's marker emission
// fail" answers false positives only; this is the other direction.
//
// The resolved cap is therefore part of a target's IDENTITY, not a property merged
// across its sharers: a probe is only evidence about the invocation it actually
// reproduces. Sharers that agree on their cap still dedupe.
func TestResolve_EachDistinctDeclaredCapIsItsOwnProbeTarget(t *testing.T) {
	big, small := 32000, 4000
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{
			"a": {Provider: "p", Model: "m", MaxTokens: &small},
			"b": {Provider: "p", Model: "m", MaxTokens: &big},
			"d": {Provider: "p", Model: "m"},     // same model, NO declaration
			"c": {Provider: "p", Model: "other"}, // undeclared, different model
		},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a", "b", "d", "c"}})
	require.NoError(t, err)

	// The original motivation still holds: the agent that already raised its cap is
	// probed AT that cap, never at the caller's smaller default.
	assert.Equal(t, big, targetForAgent(t, res, "b").MaxTokens,
		"an agent that declared 32000 must be probed at 32000, not told to raise a cap it already raised")

	// The other direction, which the collapse inverted.
	assert.Equal(t, small, targetForAgent(t, res, "a").MaxTokens,
		"a smaller declarer must be probed at ITS cap — that is the invocation review will make")
	assert.Equal(t, 0, targetForAgent(t, res, "d").MaxTokens,
		"an undeclared sharer must not inherit a co-tenant's cap; 0 leaves the caller's own default")

	assert.NotEqual(t, targetForAgent(t, res, "a"), targetForAgent(t, res, "b"),
		"two caps on one model are two different invocations, so they are two probes")
	assert.Equal(t, 0, targetForAgent(t, res, "c").MaxTokens)
}

// Splitting on the cap must not un-dedupe the ordinary case: sharers that agree (the
// overwhelmingly common shape, including all-undeclared) still collapse to one probe.
func TestResolve_SharersWithTheSameCapStillDedupe(t *testing.T) {
	cap1, cap2 := 16000, 16000
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{
			"a": {Provider: "p", Model: "m", MaxTokens: &cap1},
			"b": {Provider: "p", Model: "m", MaxTokens: &cap2},
		},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a", "b"}})
	require.NoError(t, err)

	assert.Len(t, res.Targets, 1, "an equal cap is the same invocation — one probe, as before")
	assert.Equal(t, targetForAgent(t, res, "a"), targetForAgent(t, res, "b"))
}
