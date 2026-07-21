package registry

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExampleRegistriesLoad guards the shipped example registries (Epic 7.0)
// against drift: both must load and validate, the with-executor example must
// resolve a defaulted executor, and the without-executor example must leave
// Executor nil (the backward-compatible default).
func TestExampleRegistriesLoad(t *testing.T) {
	examples := filepath.Join("..", "..", "examples")

	withExec, err := LoadRegistry(filepath.Join(examples, "registry-with-executor.yaml"))
	require.NoError(t, err, "registry-with-executor.yaml must load and validate")
	require.NotNil(t, withExec.Executor)
	assert.Equal(t, RoleExecutor, withExec.Executor.Role)
	assert.Equal(t, "MEDIUM", withExec.Executor.MinSeverity)
	assert.Equal(t, "fixer", withExec.Executor.Persona)
	assert.NotEmpty(t, withExec.Executor.Model)
	_, ok := withExec.Providers[withExec.Executor.Provider]
	assert.True(t, ok, "executor provider must reference a defined provider")
	// Agent mode (Epic 7.4): the example documents the option with agent_mode off
	// (the snippet-path default) and an explicit max_tool_calls budget.
	assert.False(t, withExec.Executor.AgentMode, "with-executor example keeps agent_mode off by default")
	require.NotNil(t, withExec.Executor.MaxToolCalls, "with-executor example sets an explicit max_tool_calls")
	assert.Equal(t, 10, *withExec.Executor.MaxToolCalls)

	noExec, err := LoadRegistry(filepath.Join(examples, "registry-without-executor.yaml"))
	require.NoError(t, err, "registry-without-executor.yaml must load and validate")
	assert.Nil(t, noExec.Executor, "no executor block means no fix generation")
}

// TestRegistryExamples_Valid guards the shipped example registries (Epic 9.0)
// against drift after the language-field additions: both example files must load
// and validate clean through internal/registry, and at least one agent in each
// must carry a canonical Language scope ("go", dotless + lowercased) so the docs'
// worked examples stay in lockstep with what the loader actually accepts.
func TestRegistryExamples_Valid(t *testing.T) {
	examples := filepath.Join("..", "..", "examples")

	for _, name := range []string{"registry-without-executor.yaml", "registry-with-executor.yaml"} {
		reg, err := LoadRegistry(filepath.Join(examples, name))
		require.NoErrorf(t, err, "%s must load and validate after language additions", name)

		var hasLanguage bool
		for agentName, a := range reg.Agents {
			for _, lang := range a.Language {
				assert.Equalf(t, NormalizeLanguageToken(lang), lang,
					"%s: agent %q language entry %q must be stored canonical (dotless, lowercased)", name, agentName, lang)
				hasLanguage = true
			}
		}
		assert.Truef(t, hasLanguage, "%s must declare a language scope on at least one agent", name)
	}
}

// TestTwoTierExecutorExamples_Load guards the worked two-tier example (Sprint
// 32.1, Story 5 / AC 05-02): the cheap-tier and frontier-tier example registries
// must both load and validate clean through the real loader (the AC's "dry-run
// load", not a hand-rolled YAML check), and must demonstrate a meaningful ceiling
// contrast — the cheap tier carries a positive max_estimated_minutes ceiling, the
// frontier tier none. This is what an operator copy-pastes to assemble the
// two-independent-runs workflow, so a drift in field name, default, or validation
// range fails here before it becomes a copy-paste trap.
func TestTwoTierExecutorExamples_Load(t *testing.T) {
	examples := filepath.Join("..", "..", "examples")

	// Tier 1 — cheap/local tier: loads clean and resolves a positive ceiling.
	tier1, err := LoadRegistry(filepath.Join(examples, "registry-with-executor.yaml"))
	require.NoError(t, err, "tier-1 (cheap) example must load and validate")
	require.NotNil(t, tier1.Executor, "tier-1 example must define an executor")
	tier1Ceiling := tier1.Executor.EffectiveMaxEstimatedMinutes()
	assert.Positive(t, tier1Ceiling, "tier-1 (cheap) example must set a positive max_estimated_minutes ceiling")

	// Tier 2 — frontier tier: loads clean and resolves "no ceiling" (unlimited).
	tier2, err := LoadRegistry(filepath.Join(examples, "registry-with-executor-tier2.yaml"))
	require.NoError(t, err, "tier-2 (frontier) example must load and validate")
	require.NotNil(t, tier2.Executor, "tier-2 example must define an executor")
	tier2Ceiling := tier2.Executor.EffectiveMaxEstimatedMinutes()
	assert.Zero(t, tier2Ceiling, "tier-2 (frontier) example must set no ceiling (unlimited)")

	// The two tiers must demonstrate a meaningful contrast, not the same value
	// (AC 05-02 Error Scenario 2): the cheap tier is bounded while the frontier
	// tier is unlimited, so their effective ceilings must differ.
	assert.NotEqual(t, tier1Ceiling, tier2Ceiling,
		"the two tiers must show a meaningful ceiling contrast, not the same value")
}
