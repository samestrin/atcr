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
