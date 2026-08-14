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
func TestBuildOneAgent_PropagatesToolFields(t *testing.T) {
	cfg := toolCfg()
	payloads := map[string]modePayload{"blocks": {Text: "x", FileCount: 1}}

	a, _, err := buildOneAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"}, "", "")
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

	fb, _, err := buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{})
	require.NoError(t, err)
	assert.True(t, fb.Tools, "fallback inherits lane tools=true despite its own tools=false")
	assert.Equal(t, 5, fb.MaxTurns)
	assert.EqualValues(t, 8192, fb.ToolBudgetBytes)
}

// Epic 35.2 baseline coverage attributes a fallback-served slot to the tag of
// the agent that SERVED it (Epic 35.16.5.4 T3). On the NO-REFIT arm the fallback
// reviews the same chunk as its primary, so reusing primary.Prompt verbatim is
// what makes that attribution sound: a fallback rendered from a different
// payload without a re-fit would make the primary's tag vouch for files the
// fallback never saw, and those files would be recorded then silently skipped on
// the next scan. The re-fit arm is the sanctioned exception — it renders a
// smaller payload BY DESIGN and carries the smaller tag it actually kept, so
// attribution follows IT. Pin both arms: the verbatim invariant on the no-refit
// arm, and its deliberate violation on the re-fit arm.
func TestBuildFallbackAgent_ReusesPrimaryPayloadVerbatim(t *testing.T) {
	cfg := toolCfg()
	primary := Agent{Prompt: "// FILE:a.go\npackage a\n", PayloadMode: "files", chunkFiles: []string{"a.go"}}

	fb, _, err := buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{})
	require.NoError(t, err)
	assert.Equal(t, primary.Prompt, fb.Prompt,
		"a fallback with no re-pack source must review the SAME payload as the primary it substitutes for — baseline coverage attributes it to the serving tag")
	assert.Equal(t, primary.PayloadMode, fb.PayloadMode, "same payload implies the same mode")

	// The complementary arm: a fallback built WITH a re-pack source must render a
	// DIFFERENT prompt and carry the smaller tag of the files it actually kept.
	// If the re-fit arm is reverted, rePacked stays false and this fails.
	cfg2 := refitRoster(t, 128000, OverflowTruncate)
	var slots []Slot
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg2, markedOversizedPayload(), ReviewRange{Base: "a", Head: "b"}, "", "", true, true)
	})
	require.NoError(t, err)
	require.NotEmpty(t, slots)
	s := slots[0]
	require.Len(t, s.Fallbacks, 1, "precondition: the fallback chain is attached")
	require.True(t, s.Fallbacks[0].rePacked, "precondition: this fixture's fallback re-fits (fails if the re-fit arm is reverted)")
	fb2 := s.Fallbacks[0]
	assert.NotEqual(t, s.Primary.Prompt, fb2.Prompt,
		"a re-fit fallback renders a different payload than its primary — the verbatim pin above covers only the no-refit arm")
	assert.Less(t, len(fb2.chunkFiles), len(s.Primary.chunkFiles),
		"a re-fit fallback carries the smaller tag of the files it actually kept, so attribution follows the server")
}

// Non-tool primary yields non-tool fallback (no spurious tool enablement).
func TestBuildFallbackAgent_NonToolPrimaryStaysNonTool(t *testing.T) {
	cfg := toolCfg()
	primary := Agent{Tools: false, Prompt: "p", PayloadMode: "blocks"}

	fb, _, err := buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{})
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

	fb, _, err := buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{})
	require.NoError(t, err)
	assert.True(t, fb.Tools, "lane tools inherited")
	assert.False(t, fb.SupportsFC, "fallback uses its own incapable model, not the primary's capability")
}

// AC 04-03 S3: a tool-capable fallback (own SupportsFC=true) inherits lane Tools
// and stays capable → it would run the loop, not degrade.
func TestBuildFallbackAgent_CapableFallbackKeepsCapability(t *testing.T) {
	cfg := toolCfg()
	primary := Agent{Tools: true, SupportsFC: true, MaxTurns: 5, ToolBudgetBytes: 8192, Prompt: "p", PayloadMode: "blocks"}

	fb, _, err := buildFallbackAgent(cfg, primary, "zoe", true, fallbackRefit{})
	require.NoError(t, err)
	assert.True(t, fb.Tools)
	assert.True(t, fb.SupportsFC, "capable fallback model keeps its capability")
}

// AC 04-02 S4: primary build threads SupportsFC from the agent's own config.
func TestBuildOneAgent_PropagatesSupportsFC(t *testing.T) {
	cfg := toolCfg()
	payloads := map[string]modePayload{"blocks": {Text: "x", FileCount: 1}}

	a, _, err := buildOneAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	assert.True(t, a.SupportsFC, "greta declares supports_function_calling=true")
}

// Epic 2.2 / TD: a fallback answers in the primary's place, so the primary's
// review constraints (min_severity, max_findings) govern the built Agent — the
// fallback's own declared constraints are intentionally ignored. Locks the
// silent-override behavior that buildFallbackAgent now surfaces with a
// load-time warning, so a future change cannot let a fallback's own
// min_severity/max_findings leak into the lane unnoticed.
func TestBuildFallbackAgent_PrimaryReviewConstraintsWin(t *testing.T) {
	cfg := toolCfg()
	// Give the fallback (kai) its own constraints that differ from the primary's.
	kai := cfg.Registry.Agents["kai"]
	kai.MinSeverity = "LOW"
	kai.MaxFindings = ptrInt(99)
	kai.Scope = []string{"performance"}
	cfg.Registry.Agents["kai"] = kai

	primary := Agent{Prompt: "p", PayloadMode: "blocks", MinSeverity: "HIGH", MaxFindings: ptrInt(3)}

	fb, _, err := buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{})
	require.NoError(t, err)
	assert.Equal(t, "HIGH", fb.MinSeverity, "primary min_severity governs, not the fallback's own LOW")
	require.NotNil(t, fb.MaxFindings)
	assert.Equal(t, 3, *fb.MaxFindings, "primary max_findings governs, not the fallback's own 99")
}
