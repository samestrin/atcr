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

// TestBuildSlots_SingleShotZeroBudgetWarningNamesNoReserve is the other arm, and
// the one that was unpinned: nothing stopped the clause being printed for an agent
// that reserves nothing. A single-shot agent is sized by its output cap alone, so a
// warning naming a replayed-reasoning reserve would send the operator to lower a
// number the run never held back.
func TestBuildSlots_SingleShotZeroBudgetWarningNamesNoReserve(t *testing.T) {
	cfg := declaredWindowRoster(t, 1)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	diff := diffOfNFiles(4, 100)
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 4}}

	var err error
	out := captureStderr(t, func() {
		_, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	})
	require.NoError(t, err)
	require.Contains(t, out, "leaves no input budget once the", "precondition: the zero-budget warning must fire")
	assert.NotContains(t, out, "replayed-reasoning reserve",
		"a single-shot agent replays nothing — naming a reserve here describes a reservation it never made")
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
	assert.Equal(t, cap, agent.ReservedOutputTokens,
		"reserved_output_tokens stays the resolved output cap, as its own test pins it")
	assert.Equal(t, cap*payload.ReasoningReplayReserveCaps, agent.ReasoningReserveTokens,
		"the EXTRA reserve a tool-loop agent holds back must be recorded, or the record understates it")
	assert.Equal(t, payload.SizingOutputTokens(true, cap), agent.ReservedOutputTokens+agent.ReasoningReserveTokens,
		"the two together must equal the reservation the payload was sized against")
}

// A single-shot agent replays nothing, so its record and its warning are unchanged.
func TestBuildOneAgent_SingleShotReservationIsTheCapAlone(t *testing.T) {
	cfg := declaredWindowRoster(t, 128000)
	agent, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	assert.Equal(t, maxTokensFor(cfg, cfg.Registry.Agents["greta"]), agent.ReservedOutputTokens)
	assert.Zero(t, agent.ReasoningReserveTokens, "a single-shot agent replays nothing, so it reserves nothing extra")
}

// The fallback agent sizes with the same reserve (a fallback takes `tools` from its
// primary), so its record must carry it too — sizing with a reservation and
// reporting none is the same understatement, one agent further down the chain.
func TestBuildFallbackAgent_RecordsTheReplayReserve(t *testing.T) {
	cfg := toolLoopGretaRoster(t, 128000)
	k := cfg.Registry.Agents["kai"]
	k.SupportsFC = true
	cfg.Registry.Agents["kai"] = k

	rng := ReviewRange{Base: "a", Head: "b"}
	primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), rng, "", "")
	require.NoError(t, err)
	require.True(t, primary.Tools, "precondition: the primary requests tools")

	fb, _, err := buildFallbackAgent(cfg, primary, "kai", false, fallbackRefit{rng: rng})
	require.NoError(t, err)
	require.Positive(t, fb.EffectiveBudget, "precondition: the fallback is funded")
	assert.Equal(t, fb.ReservedOutputTokens*payload.ReasoningReplayReserveCaps, fb.ReasoningReserveTokens,
		"the fallback inherits the tool lane, so it holds back the same replay reserve")
}

// TestBuildFallbackAgent_RefitArmRecordsTheReplayReserve covers the SECOND write
// site. buildFallbackAgent sets the reserve twice: once on the inherited-payload
// path (the test above) and again inside the re-fit arm, which re-derives the
// reservation from the budget the re-packed payload was actually sized to. Only
// the first was exercised, so a re-fit tool-loop fallback could record a wrong or
// absent reserve with nothing catching it — and the re-fit record is precisely the
// one whose arithmetic an operator cannot reconstruct from the slot's own sizing.
func TestBuildFallbackAgent_RefitArmRecordsTheReplayReserve(t *testing.T) {
	cfg := refitRoster(t, 128000, OverflowTruncate)
	// Make the pair a tool-loop pair: the primary requests tools, and the fallback's
	// own model declares function calling, which is what fbToolLoop keys on.
	g := cfg.Registry.Agents["greta"]
	g.Tools, g.SupportsFC = true, true
	cfg.Registry.Agents["greta"] = g
	k := cfg.Registry.Agents["kai"]
	k.SupportsFC = true
	// A declared window large enough that the re-fit still has a budget to fit
	// into once the tool-loop reserve is held back, but smaller than greta's, so
	// the inherited payload genuinely overflows it and the re-fit arm runs.
	kw := 64000
	k.ContextWindowTokens = &kw
	cfg.Registry.Agents["kai"] = k

	slot := buildRefitSlot(t, cfg)
	primary, fb := slot.Primary, slot.Fallbacks[0]

	require.True(t, primary.Tools, "precondition: the primary requests tools")
	require.Equal(t, degradationTruncate, fb.DegradationAction,
		"precondition: this fallback must have taken the RE-FIT arm and found a smaller framing")
	require.Positive(t, fb.EffectiveBudget, "precondition: the re-fit payload is funded")

	assert.Positive(t, fb.ReasoningReserveTokens,
		"a re-fit tool-loop fallback still holds back the replay reserve — recording none understates it")
	assert.Equal(t, fb.ReservedOutputTokens*payload.ReasoningReplayReserveCaps, fb.ReasoningReserveTokens,
		"the re-fit arm must derive the reserve from the SAME cap it recorded as reserved_output_tokens")
	assert.Equal(t, payload.SizingOutputTokens(true, fb.ReservedOutputTokens),
		fb.ReservedOutputTokens+fb.ReasoningReserveTokens,
		"the two together must equal the reservation the re-fit payload was sized against")
}
