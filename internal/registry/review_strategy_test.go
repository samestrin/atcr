package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 14.3 (AC1): review_strategy toggles bulk (default) vs chunked fan-out.
// It resolves once per run through the same precedence chain as payload_mode.

func TestReviewStrategy_DefaultsToBulk(t *testing.T) {
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, nil)
	assert.Equal(t, DefaultReviewStrategy, s.ReviewStrategy)
	assert.Equal(t, "bulk", s.ReviewStrategy, "the embedded default keeps API cost bounded")
}

func TestReviewStrategy_RegistryOverride(t *testing.T) {
	reg := loadRegistryWith(t, "review_strategy: chunked\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, "chunked", s.ReviewStrategy, "registry global wins over embedded default")
}

func TestReviewStrategy_ProjectOverridesRegistry(t *testing.T) {
	reg := loadRegistryWith(t, "review_strategy: chunked\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nreview_strategy: bulk\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	assert.Equal(t, "bulk", s.ReviewStrategy, "project config wins over registry")
}

func TestReviewStrategy_InvalidRejectedAtResolve(t *testing.T) {
	// The project tier and a directly-constructed proj bypass the load-time
	// check, so ResolveSettings must re-reject an unknown strategy.
	_, err := ResolveSettings(CLIOverrides{}, &ProjectConfig{ReviewStrategy: "turbo"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "review_strategy")
}

func TestReviewStrategy_InvalidRejectedAtLoad(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m}
review_strategy: turbo
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "review_strategy")
}
