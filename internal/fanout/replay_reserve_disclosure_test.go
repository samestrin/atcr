package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toolLoopGretaRoster makes greta a tool-loop agent (tools + function calling), which
// is what puts the replayed-reasoning reserve on top of her output cap.
func toolLoopGretaRoster(t *testing.T, window int) *ReviewConfig {
	t.Helper()
	cfg := declaredWindowRoster(t, window)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	g := cfg.Registry.Agents["greta"]
	g.Tools, g.SupportsFC = true, true
	cfg.Registry.Agents["greta"] = g
	return cfg
}

// TD internal/fanout/review.go:3103 — review's zero-budget warning named only the
// "%d-token output cap", with the UNRESERVED cap, so for a tool-loop agent the
// window-vs-cap arithmetic on screen looked like it fit while the real reservation
// was three caps. doctor got a reserveClause for exactly this; review did not.
func TestBuildSlots_ZeroBudgetWarningNamesTheReplayReserve(t *testing.T) {
	cfg := toolLoopGretaRoster(t, 1)
	cfg.Settings.ReviewStrategy = "chunked"
	diff := diffOfNFiles(4, 100)
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 4}}

	var err error
	out := captureStderr(t, func() {
		_, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	})
	require.NoError(t, err)
	require.Contains(t, out, "leaves no input budget once the", "precondition: the zero-budget warning must fire")
	assert.Contains(t, out, "replayed-reasoning reserve its tool loop holds back",
		"a tool-loop agent's warning must name the reserve, or the window-vs-cap sum reads as if it fits")
}

// And the sizing record must report the reservation actually held back, not the bare
// output cap: an operator reconciling resolved_window against reserved_output_tokens
// otherwise computes a budget the run never had.
func TestBuildOneAgent_ReservedOutputTokensIncludesTheReplayReserve(t *testing.T) {
	cfg := toolLoopGretaRoster(t, 128000)
	agent, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	require.Positive(t, agent.EffectiveBudget, "precondition: a funded agent records its reservation")

	cap := maxTokensFor(cfg, cfg.Registry.Agents["greta"])
	assert.Equal(t, payload.SizingOutputTokens(true, cap), agent.ReservedOutputTokens,
		"a tool-loop agent holds back the cap PLUS the replay reserve")
	assert.Greater(t, agent.ReservedOutputTokens, cap, "precondition: the reserve is more than the cap alone")
}

// A single-shot agent replays nothing, so its record and its warning are unchanged.
func TestBuildOneAgent_SingleShotReservationIsTheCapAlone(t *testing.T) {
	cfg := declaredWindowRoster(t, 128000)
	agent, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	assert.Equal(t, maxTokensFor(cfg, cfg.Registry.Agents["greta"]), agent.ReservedOutputTokens)
}
