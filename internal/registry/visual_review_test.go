package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// visual_review.agent (Epic 36.0.2) loads at both file tiers, the project
// tier overrides the registry tier, and a blank value falls through.
func TestVisualReview_ResolvesAcrossTiers(t *testing.T) {
	reg := loadRegistryWith(t, "visual_review:\n  agent: bruce\n")
	assert.Equal(t, "bruce", resolve(t, CLIOverrides{}, nil, reg).VisualReview.Agent)

	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nvisual_review:\n  agent: scout\n"))
	require.NoError(t, err)
	assert.Equal(t, "scout", resolve(t, CLIOverrides{}, proj, reg).VisualReview.Agent, "the project tier wins")

	blank, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nvisual_review:\n  agent: \"  \"\n"))
	require.NoError(t, err)
	assert.Equal(t, "bruce", resolve(t, CLIOverrides{}, blank, reg).VisualReview.Agent, "a blank agent falls through")

	assert.Empty(t, resolve(t, CLIOverrides{}, nil, nil).VisualReview.Agent, "unset by default")
}

// The block is strict like every other: a misspelled key fails the load.
func TestVisualReview_UnknownKeyRejected(t *testing.T) {
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nvisual_review:\n  agnet: bruce\n"))
	require.Error(t, err)
}

// IsLoopback accepts localhost, 127.0.0.0/8 and ::1 only; an empty or
// unparsable base_url is the remote default and is not loopback.
func TestProvider_IsLoopback(t *testing.T) {
	cases := map[string]bool{
		"http://localhost:11434/v1":  true,
		"http://LOCALHOST/v1":        true,
		"http://127.0.0.1:8080/v1":   true,
		"http://127.10.20.30/v1":     true,
		"http://[::1]:8080/v1":       true,
		"https://api.openai.com/v1":  false,
		"http://10.0.0.5:11434/v1":   false,
		"http://localhost.evil.com/": false,
		"http://128.0.0.1/v1":        false,
		"":                           false,
		"   ":                        false,
		"://bad":                     false,
	}
	for raw, want := range cases {
		assert.Equal(t, want, Provider{BaseURL: raw}.IsLoopback(), raw)
	}
}
