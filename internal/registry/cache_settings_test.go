package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrecedence_CacheMaxBytesDefault(t *testing.T) {
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, nil)
	assert.Equal(t, int64(DefaultCacheMaxBytes), s.CacheMaxBytes, "embedded 50 MB default used when nothing overrides")
}

func TestPrecedence_CacheMaxBytesChain(t *testing.T) {
	reg := loadRegistryWith(t, "cache_max_bytes: 1048576\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ncache_max_bytes: 2097152\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, int64(2097152), s.CacheMaxBytes, "project config wins over registry")
}

func TestPrecedence_CacheMaxBytesRegistryOverridesEmbedded(t *testing.T) {
	reg := loadRegistryWith(t, "cache_max_bytes: 1048576\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, int64(1048576), s.CacheMaxBytes, "registry wins over embedded default")
}

func TestPrecedence_CacheMaxBytesExplicitZeroIsUnbounded(t *testing.T) {
	// 0 is the documented unbounded escape hatch (parity with payload_byte_budget
	// and max_parallel) and must survive default application.
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ncache_max_bytes: 0\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, nil)
	assert.Equal(t, int64(0), s.CacheMaxBytes, "explicit 0 overrides the embedded default (unbounded)")
}

func TestProjectConfig_CacheMaxBytesNegativeRejected(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ncache_max_bytes: -1\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache_max_bytes")
}

func TestRegistry_CacheMaxBytesNegativeRejected(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m}
cache_max_bytes: -5
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache_max_bytes")
}
