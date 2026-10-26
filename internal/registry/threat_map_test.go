package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// threat_map.agent (Epic 37.0) loads at both file tiers, the project tier
// overrides the registry tier, and a blank value falls through.
func TestThreatMap_ResolvesAcrossTiers(t *testing.T) {
	reg := loadRegistryWith(t, "threat_map:\n  agent: bruce\n")
	assert.Equal(t, "bruce", resolve(t, CLIOverrides{}, nil, reg).ThreatMap.Agent)

	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nthreat_map:\n  agent: scout\n"))
	require.NoError(t, err)
	assert.Equal(t, "scout", resolve(t, CLIOverrides{}, proj, reg).ThreatMap.Agent, "the project tier wins")

	blank, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nthreat_map:\n  agent: \"  \"\n"))
	require.NoError(t, err)
	assert.Equal(t, "bruce", resolve(t, CLIOverrides{}, blank, reg).ThreatMap.Agent, "a blank agent falls through")

	assert.Empty(t, resolve(t, CLIOverrides{}, nil, nil).ThreatMap.Agent, "unset by default")
}

// The block is strict like every other: a misspelled key fails the load.
func TestThreatMap_UnknownKeyRejected(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nthreat_map:\n  agnet: bruce\n"))
	require.Error(t, err)
}
