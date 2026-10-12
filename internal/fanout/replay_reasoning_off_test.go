package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// replayReasoningOff is true only for the one legal opt-out value; unset (the
// default, replay on) and any other string are false.
func TestReplayReasoningOff_OnlyTheOffValue(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{registry.ReplayReasoningOff, true},
		{"", false},
		{"on", false},
		{"OFF", false},
	}
	for _, tc := range cases {
		t.Run("value="+tc.value, func(t *testing.T) {
			assert.Equal(t, tc.want, replayReasoningOff(registry.AgentConfig{ReplayReasoning: tc.value}))
		})
	}
}

// The opt-out is per agent: a fallback sends its OWN replay_reasoning, never
// the primary's, in either direction.
func TestBuildFallbackAgent_ReplayReasoningOffIsNotInherited(t *testing.T) {
	cases := []struct {
		name       string
		primaryOff bool
		fbValue    string
		want       bool
	}{
		{"primary off, fallback unset", true, "", false},
		{"primary on, fallback off", false, registry.ReplayReasoningOff, true},
		{"both off", true, registry.ReplayReasoningOff, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := sizingRosterConfig()
			kai := cfg.Registry.Agents["kai"]
			kai.ReplayReasoning = tc.fbValue
			cfg.Registry.Agents["kai"] = kai
			primary := Agent{Name: "greta", ReplayReasoningOff: tc.primaryOff}

			fb, _, err := buildFallbackAgent(cfg, primary, "kai", false, fallbackRefit{})
			require.NoError(t, err)
			assert.Equal(t, tc.want, fb.ReplayReasoningOff)
		})
	}
}
