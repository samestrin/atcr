package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Plan 19.10 (F4/AC4): on_overflow selects the degradation policy
// (chunk/truncate/fallback/fail) when a per-agent payload exceeds budget. It
// resolves once per run through the same precedence chain as review_strategy.

func TestOnOverflowValid(t *testing.T) {
	for _, v := range []string{"chunk", "truncate", "fallback", "fail", "", "  "} {
		assert.True(t, onOverflowValid(v), "%q should be config-valid (all four ladder values + unset)", v)
	}
	for _, v := range []string{"yolo", "shed", "CHUNK", "chunked"} {
		assert.False(t, onOverflowValid(v), "%q should be rejected", v)
	}
}

func TestLoadProjectConfig_OnOverflow_DefaultUnset(t *testing.T) {
	// No on_overflow key: the loader leaves it unset ("") — defaulting to "chunk"
	// happens in ResolveSettings, not the loader.
	cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)
	assert.Equal(t, "", cfg.OnOverflow)
}

func TestLoadProjectConfig_OnOverflow_ExplicitValid(t *testing.T) {
	for _, v := range []string{"chunk", "truncate", "fallback", "fail"} {
		t.Run(v, func(t *testing.T) {
			cfg, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\non_overflow: "+v+"\n"))
			require.NoError(t, err)
			assert.Equal(t, v, cfg.OnOverflow)
		})
	}
}

func TestLoadProjectConfig_OnOverflow_Invalid(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\non_overflow: yolo\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "on_overflow")
	assert.Contains(t, err.Error(), "chunk, truncate, fallback, fail")
}

func TestLoadProjectConfig_OnOverflow_TypoKey_StrictModeRejects(t *testing.T) {
	// A typo'd key must be rejected by strict KnownFields(true) decoding, proving
	// the struct tag is wired correctly (not that a stray key was silently kept).
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\non_overlow: chunk\n"))
	require.Error(t, err)
}

func TestRegistryValidate_OnOverflow_Invalid(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m}
on_overflow: yolo
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "on_overflow")
}

func TestResolveSettings_OnOverflow_DefaultsToChunk(t *testing.T) {
	s, err := ResolveSettings(CLIOverrides{}, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, DefaultOnOverflow, s.OnOverflow)
	assert.Equal(t, "chunk", s.OnOverflow)
}

func TestResolveSettings_OnOverflow_RegistryOverridesDefault(t *testing.T) {
	reg := loadRegistryWith(t, "on_overflow: truncate\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, "truncate", s.OnOverflow, "registry global wins over embedded default")
}

func TestResolveSettings_OnOverflow_ProjectOverridesRegistry(t *testing.T) {
	reg := loadRegistryWith(t, "on_overflow: truncate\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\non_overflow: fail\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, "fail", s.OnOverflow, "project config wins over registry")
}

func TestResolveSettings_OnOverflow_WhitespaceTreatedAsUnset(t *testing.T) {
	// A whitespace-only value at any tier is "unset" and falls through, so the
	// resolved value is the embedded default.
	s, err := ResolveSettings(CLIOverrides{}, &ProjectConfig{OnOverflow: "   "}, &Registry{OnOverflow: "  "})
	require.NoError(t, err)
	assert.Equal(t, "chunk", s.OnOverflow)
}

func TestResolveSettings_OnOverflow_DirectlyConstructedInvalidRejected(t *testing.T) {
	// A directly-constructed proj (bypassing the file loader) with an out-of-range
	// value is caught by the post-resolution sanity check.
	_, err := ResolveSettings(CLIOverrides{}, &ProjectConfig{OnOverflow: "turbo"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "on_overflow")
}
