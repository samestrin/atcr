package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectConfig_SASTIsOffByDefault(t *testing.T) {
	cfg, err := LoadProjectConfig(writeProject(t, "agents: [greta]\n"))
	require.NoError(t, err)
	assert.Nil(t, cfg.SAST)
	assert.False(t, cfg.SAST.IsEnabled(), "an absent sast block is off")
}

func TestProjectConfig_SASTEnabledLoads(t *testing.T) {
	cfg, err := LoadProjectConfig(writeProject(t, "agents: [greta]\nsast:\n  enabled: true\n"))
	require.NoError(t, err)
	assert.True(t, cfg.SAST.IsEnabled())

	cfg, err = LoadProjectConfig(writeProject(t, "agents: [greta]\nsast:\n  enabled: false\n"))
	require.NoError(t, err)
	assert.False(t, cfg.SAST.IsEnabled())
}

func TestProjectConfig_SASTUnknownKeyIsRejected(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, "agents: [greta]\nsast:\n  enable: true\n"))
	require.Error(t, err, "a misspelled key must not silently leave the scanner off")
}
