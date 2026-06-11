package registry

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentsWithFallbacks builds a Registry whose agents carry the given
// fallback edges (name -> fallback; "" means none).
func agentsWithFallbacks(edges map[string]string) *Registry {
	reg := &Registry{
		Providers: map[string]Provider{"p": {APIKeyEnv: "KEY"}},
		Agents:    map[string]AgentConfig{},
	}
	for name, fb := range edges {
		reg.Agents[name] = AgentConfig{Provider: "p", Model: "m", Fallback: fb}
	}
	return reg
}

func TestFallbackChain_ValidLinear(t *testing.T) {
	reg := agentsWithFallbacks(map[string]string{"bruce": "greta", "greta": "kai", "kai": ""})
	assert.NoError(t, reg.ValidateFallbacks())
}

func TestFallbackChain_NoFallback(t *testing.T) {
	reg := agentsWithFallbacks(map[string]string{"bruce": ""})
	assert.NoError(t, reg.ValidateFallbacks())
}

func TestFallbackChain_TwoNodeCycle(t *testing.T) {
	reg := agentsWithFallbacks(map[string]string{"bruce": "greta", "greta": "bruce"})
	err := reg.ValidateFallbacks()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fallback cycle detected")
	// Full path present, regardless of which node DFS entered first.
	hasPath := err.Error() == "fallback cycle detected: bruce -> greta -> bruce" ||
		err.Error() == "fallback cycle detected: greta -> bruce -> greta"
	assert.True(t, hasPath, "cycle error must contain the full path, got: %s", err)
}

func TestFallbackChain_ThreeNodeCycle(t *testing.T) {
	reg := agentsWithFallbacks(map[string]string{"bruce": "greta", "greta": "kai", "kai": "bruce"})
	err := reg.ValidateFallbacks()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fallback cycle detected")
	assert.Contains(t, err.Error(), "->")
}

func TestFallbackChain_SelfRef(t *testing.T) {
	reg := agentsWithFallbacks(map[string]string{"bruce": "bruce"})
	err := reg.ValidateFallbacks()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fallback cycle detected: bruce -> bruce")
}

func TestFallbackChain_DanglingRef(t *testing.T) {
	reg := agentsWithFallbacks(map[string]string{"bruce": "unknown-agent"})
	err := reg.ValidateFallbacks()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent 'bruce' fallback references unknown agent 'unknown-agent'")
}

func TestFallbackChain_DiamondNoCycle(t *testing.T) {
	// A -> C and B -> C share a fallback target; gray->black edges must not
	// be flagged as cycles.
	reg := agentsWithFallbacks(map[string]string{"a": "c", "b": "c", "c": ""})
	assert.NoError(t, reg.ValidateFallbacks())
}

func TestFallbackChain_LongChainIntoSharedTail(t *testing.T) {
	reg := agentsWithFallbacks(map[string]string{
		"a": "c", "b": "c", "c": "d", "d": "",
	})
	assert.NoError(t, reg.ValidateFallbacks())
}

func TestFallbackChain_ValidatedAtLoadTime(t *testing.T) {
	t.Run("cycle rejected at load", func(t *testing.T) {
		_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m, fallback: greta}
  greta: {provider: p, model: m, fallback: bruce}
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fallback cycle detected")
	})
	t.Run("dangling rejected at load", func(t *testing.T) {
		_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m, fallback: ghost}
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown agent 'ghost'")
	})
	t.Run("valid chain loads", func(t *testing.T) {
		_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m, fallback: greta}
  greta: {provider: p, model: m}
`))
		assert.NoError(t, err)
	})
}

func TestFallbackChain_LargeGraphTerminates(t *testing.T) {
	// O(V+E) sanity: a 1000-node linear chain validates quickly.
	edges := map[string]string{}
	for i := 0; i < 1000; i++ {
		next := ""
		if i < 999 {
			next = fmt.Sprintf("a%d", i+1)
		}
		edges[fmt.Sprintf("a%d", i)] = next
	}
	assert.NoError(t, agentsWithFallbacks(edges).ValidateFallbacks())
}
