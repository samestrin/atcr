package fanout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/registry"
)

// The vision agent is selected only when it declares supports_vision and is
// on a loopback host, or with allowCloud on any host (Epic 36.0.2 T3).
func TestSelectVisionAgent(t *testing.T) {
	t.Parallel()
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{
			"local":   {APIKeyEnv: "K", BaseURL: "http://127.0.0.1:11434/v1"},
			"cloud":   {APIKeyEnv: "K", BaseURL: "https://api.example.com/v1"},
			"default": {APIKeyEnv: "K"},
		},
		Agents: map[string]registry.AgentConfig{
			"eye":     {Provider: "local", Model: "m", SupportsVision: true},
			"remote":  {Provider: "cloud", Model: "m", SupportsVision: true},
			"blank":   {Provider: "default", Model: "m", SupportsVision: true},
			"textual": {Provider: "local", Model: "m"},
			"orphan":  {Provider: "gone", Model: "m", SupportsVision: true},
		},
	}
	cases := []struct {
		name       string
		agent      string
		allowCloud bool
		wantErr    error
		loopback   bool
	}{
		{"loopback vision agent", "eye", false, nil, true},
		{"loopback vision agent with allowCloud", "eye", true, nil, true},
		{"non-loopback refused", "remote", false, ErrVisionAgentRefused, false},
		{"non-loopback with allowCloud", "remote", true, nil, false},
		{"no supports_vision", "textual", false, ErrVisionAgentRefused, false},
		{"no supports_vision with allowCloud", "textual", true, ErrVisionAgentRefused, false},
		{"empty base_url refused", "blank", false, ErrVisionAgentRefused, false},
		{"empty base_url with allowCloud", "blank", true, nil, false},
		{"unknown agent", "nobody", true, ErrVisionAgentRefused, false},
		{"unknown provider", "orphan", true, ErrVisionAgentRefused, false},
		{"unset key", "", true, ErrVisionAgentUnset, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			settings := registry.Settings{VisualReview: registry.VisualReview{Agent: tc.agent}}
			got, err := SelectVisionAgent(reg, settings, tc.allowCloud)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				assert.Empty(t, got.Name)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.agent, got.Name)
			assert.True(t, got.Agent.SupportsVision)
			assert.Equal(t, tc.loopback, got.Loopback)
		})
	}
}

// A nil registry refuses the agent rather than panicking, like the
// threat-map mapper's no-registry guard.
func TestSelectVisionAgent_NoRegistry(t *testing.T) {
	t.Parallel()
	settings := registry.Settings{VisualReview: registry.VisualReview{Agent: "eye"}}
	got, err := SelectVisionAgent(nil, settings, true)
	require.ErrorIs(t, err, ErrVisionAgentRefused)
	assert.Empty(t, got.Name)
}
