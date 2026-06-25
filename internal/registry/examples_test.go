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
