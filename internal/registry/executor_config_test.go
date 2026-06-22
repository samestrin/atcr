package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// executorBaseProviders is a minimal valid registry the executor block is appended to.
const executorBaseProviders = `
providers:
  anthropic:
    api_key_env: ANTHROPIC_API_KEY
agents:
  bruce:
    provider: anthropic
    model: claude-sonnet-4-6
    role: reviewer
`

func TestExecutor_AbsentByDefault(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, executorBaseProviders))
	require.NoError(t, err)
	assert.Nil(t, reg.Executor, "executor must be nil when no executor block is configured")
}

func TestExecutor_ParsedWhenPresent(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, executorBaseProviders+`
executor:
  name: opus
  provider: anthropic
  model: claude-opus-4-8
  persona: fixer
  role: executor
`))
	require.NoError(t, err)
	require.NotNil(t, reg.Executor)
	assert.Equal(t, "opus", reg.Executor.Name)
	assert.Equal(t, "anthropic", reg.Executor.Provider)
	assert.Equal(t, "claude-opus-4-8", reg.Executor.Model)
	assert.Equal(t, "fixer", reg.Executor.Persona)
	assert.Equal(t, RoleExecutor, reg.Executor.Role)
}

func TestExecutor_DefaultsApplied(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, executorBaseProviders+`
executor:
  provider: anthropic
  model: claude-opus-4-8
`))
	require.NoError(t, err)
	require.NotNil(t, reg.Executor)
	assert.Equal(t, RoleExecutor, reg.Executor.Role, "role defaults to executor")
	assert.Equal(t, DefaultExecutorPersona, reg.Executor.Persona, "persona defaults to fixer")
	assert.Equal(t, DefaultFixMinSeverity, reg.Executor.MinSeverity, "min_severity_for_fix defaults to MEDIUM")
	assert.False(t, reg.Executor.BatchFixes, "batch_fixes defaults to false (per-finding MVP)")
}

func TestExecutor_MinSeverityForFixExplicitAndNormalized(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, executorBaseProviders+`
executor:
  provider: anthropic
  model: claude-opus-4-8
  min_severity_for_fix: high
`))
	require.NoError(t, err)
	require.NotNil(t, reg.Executor)
	assert.Equal(t, "HIGH", reg.Executor.MinSeverity)
}

func TestExecutor_MissingProvider(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, executorBaseProviders+`
executor:
  model: claude-opus-4-8
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider")
}

func TestExecutor_UnknownProvider(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, executorBaseProviders+`
executor:
  provider: nope
  model: claude-opus-4-8
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

func TestExecutor_MissingModel(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, executorBaseProviders+`
executor:
  provider: anthropic
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model")
}

func TestExecutor_InvalidRole(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, executorBaseProviders+`
executor:
  provider: anthropic
  model: claude-opus-4-8
  role: reviewer
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "executor")
}

func TestExecutor_InvalidMinSeverityForFix(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, executorBaseProviders+`
executor:
  provider: anthropic
  model: claude-opus-4-8
  min_severity_for_fix: BLOCKER
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "min_severity_for_fix")
}

func TestExecutor_InvalidFixTimeout(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, executorBaseProviders+`
executor:
  provider: anthropic
  model: claude-opus-4-8
  fix_timeout: -5
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fix_timeout")
}
