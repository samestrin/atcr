package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptrInt(i int) *int     { return &i }
func ptrI64(i int64) *int64 { return &i }

// toolCfg builds a single-tool-agent ReviewConfig with a tool-capable fallback
// whose own config is non-tool, to prove lane (not own-config) inheritance.
func toolCfg() *ReviewConfig {
	return &ReviewConfig{
		Registry: &registry.Registry{
			Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: "http://x"}},
			Agents: map[string]registry.AgentConfig{
				"greta": {Provider: "p", Model: "m", Persona: "greta", Temperature: ptrF(0.7),
					Tools: true, MaxTurns: ptrInt(5), ToolBudgetBytes: ptrI64(8192), Fallback: "kai"},
				"kai": {Provider: "p", Model: "m2", Persona: "kai", Temperature: ptrF(0.7),
					Tools: false}, // own config is non-tool; must NOT govern the fallback
			},
		},
		Project:  &registry.ProjectConfig{Agents: []string{"greta"}},
		Settings: registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600},
	}
}

// AC 01-02 Scenario 4: Agent struct populated from AgentConfig tool fields.
func TestBuildAgent_PropagatesToolFields(t *testing.T) {
	cfg := toolCfg()
	payloads := map[string]modePayload{"blocks": {Text: "x", FileCount: 1}}

	a, _, err := buildAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"})
	require.NoError(t, err)
	assert.True(t, a.Tools)
	assert.Equal(t, 5, a.MaxTurns)
	assert.EqualValues(t, 8192, a.ToolBudgetBytes)
}

// AC 01-05 Scenario 4 + AC 04-03: fallback inherits the lane's (primary's) tool
// settings, NOT the fallback's own config.
func TestBuildFallbackAgent_InheritsLaneToolSettings(t *testing.T) {
	cfg := toolCfg()
	primary := Agent{Tools: true, MaxTurns: 5, ToolBudgetBytes: 8192, Prompt: "p", PayloadMode: "blocks"}

	fb, err := buildFallbackAgent(cfg, primary, "kai")
	require.NoError(t, err)
	assert.True(t, fb.Tools, "fallback inherits lane tools=true despite its own tools=false")
	assert.Equal(t, 5, fb.MaxTurns)
	assert.EqualValues(t, 8192, fb.ToolBudgetBytes)
}

// Non-tool primary yields non-tool fallback (no spurious tool enablement).
func TestBuildFallbackAgent_NonToolPrimaryStaysNonTool(t *testing.T) {
	cfg := toolCfg()
	primary := Agent{Tools: false, Prompt: "p", PayloadMode: "blocks"}

	fb, err := buildFallbackAgent(cfg, primary, "kai")
	require.NoError(t, err)
	assert.False(t, fb.Tools)
	assert.Equal(t, 0, fb.MaxTurns)
	assert.EqualValues(t, 0, fb.ToolBudgetBytes)
}
