package registry

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureDeprecations(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := DeprecationWriter
	DeprecationWriter = &buf
	deprecationMu.Lock()
	deprecationWarned = map[string]bool{}
	deprecationMu.Unlock()
	t.Cleanup(func() { DeprecationWriter = old })
	return &buf
}

func TestDeprecatedKey_ExecutorFixTimeout(t *testing.T) {
	buf := captureDeprecations(t)
	body := executorBaseProviders + "executor:\n  provider: anthropic\n  model: m\n  fix_timeout: 120\n"
	for i := 0; i < 2; i++ {
		reg, err := LoadRegistry(writeRegistry(t, body))
		require.NoError(t, err)
		require.NotNil(t, reg.Executor.TimeoutSecs)
		assert.Equal(t, 120, *reg.Executor.TimeoutSecs)
		assert.Nil(t, reg.Executor.LegacyTimeoutSecs)
	}
	assert.Equal(t, "warning: config key 'executor.fix_timeout' is deprecated; use 'executor.fix_timeout_secs'\n", buf.String(), "one warning per run")
}

func TestExecutorFixTimeoutSecs_NewKeyNoWarning(t *testing.T) {
	buf := captureDeprecations(t)
	reg, err := LoadRegistry(writeRegistry(t, executorBaseProviders+"executor:\n  provider: anthropic\n  model: m\n  fix_timeout_secs: 90\n"))
	require.NoError(t, err)
	assert.Equal(t, 90, *reg.Executor.TimeoutSecs)
	assert.Empty(t, buf.String())
}

func TestDeprecatedKey_ExecutorBothSetRejected(t *testing.T) {
	captureDeprecations(t)
	_, err := LoadRegistry(writeRegistry(t, executorBaseProviders+"executor:\n  provider: anthropic\n  model: m\n  fix_timeout: 1\n  fix_timeout_secs: 2\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fix_timeout")
}

func TestDeprecatedKey_RegistryCacheMaxBytes(t *testing.T) {
	buf := captureDeprecations(t)
	reg, err := LoadRegistry(writeRegistry(t, executorBaseProviders+"cache_max_bytes: 1234\n"))
	require.NoError(t, err)
	require.NotNil(t, reg.CacheMaxBytes)
	assert.EqualValues(t, 1234, *reg.CacheMaxBytes)
	assert.Contains(t, buf.String(), "'cache_max_bytes' is deprecated; use 'max_cache_bytes'")

	reg, err = LoadRegistry(writeRegistry(t, executorBaseProviders+"max_cache_bytes: 99\n"))
	require.NoError(t, err)
	assert.EqualValues(t, 99, *reg.CacheMaxBytes)
}

func TestDeprecatedKey_ProjectCacheMaxBytes(t *testing.T) {
	buf := captureDeprecations(t)
	cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ncache_max_bytes: 777\n"))
	require.NoError(t, err)
	assert.EqualValues(t, 777, *cfg.CacheMaxBytes)
	assert.Equal(t, 1, bytes.Count(buf.Bytes(), []byte("deprecated")))

	cfg, err = LoadProjectConfig(writeProject(t, "agents: [bruce]\nmax_cache_bytes: 5\n"))
	require.NoError(t, err)
	assert.EqualValues(t, 5, *cfg.CacheMaxBytes)

	_, err = LoadProjectConfig(writeProject(t, "agents: [bruce]\nmax_cache_bytes: 5\ncache_max_bytes: 6\n"))
	require.Error(t, err)
}
