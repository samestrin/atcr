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
