package registry

import (
	"fmt"
	"strings"
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
	// Sorted iteration enters at "bruce", so the path is deterministic.
	assert.Contains(t, err.Error(), "fallback cycle detected: bruce -> greta -> kai -> bruce")
	assert.ErrorIs(t, err, ErrFallbackCycle)
}

func TestFallbackChain_CycleReachedViaPrefix(t *testing.T) {
	// a leads into the cycle b -> c -> b; the reported path must start at
	// the repeated node, not at the entry point.
	reg := agentsWithFallbacks(map[string]string{"a": "b", "b": "c", "c": "b"})
	err := reg.ValidateFallbacks()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fallback cycle detected: b -> c -> b")
	assert.NotContains(t, err.Error(), "a ->", "lead-in prefix must be trimmed from the cycle path")
}

func TestFallbackChain_SentinelErrors(t *testing.T) {
	dangling := agentsWithFallbacks(map[string]string{"bruce": "ghost"})
	assert.ErrorIs(t, dangling.ValidateFallbacks(), ErrDanglingFallback)

	cycle := agentsWithFallbacks(map[string]string{"bruce": "bruce"})
	assert.ErrorIs(t, cycle.ValidateFallbacks(), ErrFallbackCycle)
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

func TestFallbackChain_CrossTierCycleAttributedToProject(t *testing.T) {
	// "alpha" is user-tier (DFS enters here first, alphabetically); "zephyr"
	// is project-tier. The cycle error must be attributed to the project node
	// so attribute() prefixes the error with .atcr/registry.yaml, not registry.yaml.
	reg := agentsWithFallbacks(map[string]string{"alpha": "zephyr", "zephyr": "alpha"})
	reg.stampSource(SourceUser)
	reg.AgentSource["zephyr"] = EntrySource{Tier: SourceProject, File: projectRegistryLabel}

	cycleErr := reg.ValidateFallbacks()
	require.Error(t, cycleErr)
	assert.ErrorIs(t, cycleErr, ErrFallbackCycle)

	// attribute() wraps the error with the file that defined the attributed node.
	// With the fix, "zephyr" (project) is chosen; without it "alpha" (user) is.
	attributed := reg.attribute(cycleErr)
	assert.True(t, strings.HasPrefix(attributed.Error(), ".atcr/registry.yaml:"),
		"cross-tier cycle must be attributed to project node, got: %s", attributed.Error())
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
