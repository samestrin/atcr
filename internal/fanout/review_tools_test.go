package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptrInt(i int) *int     { return &i }
func ptrI64(i int64) *int64 { return &i }

// toolCfg builds a single-tool-agent ReviewConfig with a fallback whose own
// config is non-tool, to prove lane (not own-config) inheritance. greta is
// tool-capable; kai is non-tool-capable (supports_function_calling=false) so the
// per-agent degrade decision can be exercised on the fallback independently.
func toolCfg() *ReviewConfig {
	return &ReviewConfig{
		Registry: &registry.Registry{
			Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: "http://x"}},
			Agents: map[string]registry.AgentConfig{
				"greta": {Provider: "p", Model: "m", Persona: "greta", Temperature: ptrF(0.7),
					Tools: true, SupportsFC: true, MaxTurns: ptrInt(5), ToolBudgetBytes: ptrI64(8192), Fallback: "kai"},
				"kai": {Provider: "p", Model: "m2", Persona: "kai", Temperature: ptrF(0.7),
					Tools: false, SupportsFC: false}, // own config is non-tool + incapable; must NOT govern lane Tools
				"zoe": {Provider: "p", Model: "m3", Persona: "zoe", Temperature: ptrF(0.7),
					Tools: false, SupportsFC: true}, // tool-capable fallback model
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

// AC 04-03 Spec / EC3: the fallback inherits the lane's Tools setting but its OWN
// model's function-calling capability (SupportsFC), never the primary's. A
// tool-capable primary with a non-tool-capable fallback yields fb.Tools=true,
// fb.SupportsFC=false → the fallback will degrade per-agent at invoke time.
func TestBuildFallbackAgent_OwnCapabilityNotInheritedFromLane(t *testing.T) {
	cfg := toolCfg()
	primary := Agent{Tools: true, SupportsFC: true, MaxTurns: 5, ToolBudgetBytes: 8192, Prompt: "p", PayloadMode: "blocks"}

	fb, err := buildFallbackAgent(cfg, primary, "kai")
	require.NoError(t, err)
	assert.True(t, fb.Tools, "lane tools inherited")
	assert.False(t, fb.SupportsFC, "fallback uses its own incapable model, not the primary's capability")
}

// AC 04-03 S3: a tool-capable fallback (own SupportsFC=true) inherits lane Tools
// and stays capable → it would run the loop, not degrade.
func TestBuildFallbackAgent_CapableFallbackKeepsCapability(t *testing.T) {
	cfg := toolCfg()
	primary := Agent{Tools: true, SupportsFC: true, MaxTurns: 5, ToolBudgetBytes: 8192, Prompt: "p", PayloadMode: "blocks"}

	fb, err := buildFallbackAgent(cfg, primary, "zoe")
	require.NoError(t, err)
	assert.True(t, fb.Tools)
	assert.True(t, fb.SupportsFC, "capable fallback model keeps its capability")
}

// AC 04-02 S4: primary build threads SupportsFC from the agent's own config.
func TestBuildAgent_PropagatesSupportsFC(t *testing.T) {
	cfg := toolCfg()
	payloads := map[string]modePayload{"blocks": {Text: "x", FileCount: 1}}

	a, _, err := buildAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"})
	require.NoError(t, err)
	assert.True(t, a.SupportsFC, "greta declares supports_function_calling=true")
}
