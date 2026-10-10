package fanout

import (
	"context"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sizedToolAgent is a tool agent carrying the sizing record buildSlots stamps:
// a funded effective budget and a resolved output cap of maxTokens.
func sizedToolAgent(maxTokens int) Agent {
	a := toolAgent("a", 10, 0)
	a.EffectiveBudget = 1 << 20
	a.ResolvedWindow = 128000
	a.ResolvedMaxTokens = maxTokens
	return a
}

// reasoningLoop scripts one tool-call turn whose reasoning_content is n bytes on
// the wire (the JSON string, quotes included), then a turn that would call
// another tool, then a final answer.
func reasoningLoop(n int) (*scriptedChat, *fakeDispatcher) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)}, reasoning: strings.Repeat("r", n-2)},
		{toolCalls: []llmclient.ToolCall{toolCall("c2", "grep", `{"pattern":"x"}`)}},
		{content: "final answer"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "   1| package x"}
	d.byName["grep"] = tools.ToolResult{Content: "a.go:1: x"}
	return cc, d
}

// Replayed reasoning past one output cap (in conservative bytes) trips the loop
// locally, before the next request can carry it past the reserved allowance.
func TestLoop_ReplayedReasoningPastOneCapTripsLocally(t *testing.T) {
	const maxTokens = 100
	cc, d := reasoningLoop(int(payload.TokensToBytes(maxTokens)) + 1)

	r := toolEngine(cc, d).invokeAgent(context.Background(), sizedToolAgent(maxTokens))
	require.Equal(t, StatusOK, r.Status)
	assert.Contains(t, r.TrippedBudgets, budgetReasoningReplay)
	assert.Equal(t, 2, cc.chatCalls, "turn 1, then the no-tools final-answer request")
	assert.False(t, cc.toolsSeen[1], "the tripped loop asks for a final answer without tools")
	assert.Equal(t, 1, d.callCount(), "turn 1's tool results are still delivered")
}

func TestLoop_ReplayedReasoningWithinOneCapDoesNotTrip(t *testing.T) {
	const maxTokens = 100
	cc, d := reasoningLoop(int(payload.TokensToBytes(maxTokens)))

	r := toolEngine(cc, d).invokeAgent(context.Background(), sizedToolAgent(maxTokens))
	require.Equal(t, StatusOK, r.Status)
	assert.NotContains(t, r.TrippedBudgets, budgetReasoningReplay)
	assert.Equal(t, "final answer", r.Content)
	assert.Equal(t, 2, d.callCount())
}

// An unsized agent reserved nothing for reasoning, so it has nothing to trip on.
func TestLoop_UnsizedAgentNeverTripsOnReasoning(t *testing.T) {
	cc, d := reasoningLoop(1 << 20)

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.NotContains(t, r.TrippedBudgets, budgetReasoningReplay)
	assert.Equal(t, "final answer", r.Content)
}

// Epic 35.16.11.2.2.9 AC4: an agent with replay_reasoning: off re-sends none of
// its reasoning, so reasoning past the cap that would trip a default agent
// never trips it: the bytes are counted after the drop.
func TestLoop_ReplayReasoningOffNeverTripsOnReasoningItDidNotResend(t *testing.T) {
	const maxTokens = 100
	cc, d := reasoningLoop(int(payload.TokensToBytes(maxTokens)) + 1)
	a := sizedToolAgent(maxTokens)
	a.ReplayReasoningOff = true

	r := toolEngine(cc, d).invokeAgent(context.Background(), a)
	require.Equal(t, StatusOK, r.Status)
	assert.NotContains(t, r.TrippedBudgets, budgetReasoningReplay)
	assert.Equal(t, "final answer", r.Content)
	assert.Equal(t, 3, cc.chatCalls, "the loop ran every scripted turn")
	assert.Equal(t, 2, d.callCount())
}

// A sized agent whose reserve closed its budget is recorded with EffectiveBudget
// 0 but still runs the loop under chunk/truncate; the resolved window, not the
// byte budget, says it was sized, so the trip still applies.
func TestLoop_ZeroBudgetSizedAgentStillTripsOnReasoning(t *testing.T) {
	const maxTokens = 100
	cc, d := reasoningLoop(int(payload.TokensToBytes(maxTokens)) + 1)
	a := sizedToolAgent(maxTokens)
	a.EffectiveBudget = 0

	r := toolEngine(cc, d).invokeAgent(context.Background(), a)
	require.Equal(t, StatusOK, r.Status)
	assert.Contains(t, r.TrippedBudgets, budgetReasoningReplay)
}

// Each half of the sized signal is required: a resolved window with no resolved
// cap, or a cap with no resolved window, has no reserve to trip against.
func TestLoop_PartiallySizedAgentNeverTripsOnReasoning(t *testing.T) {
	for name, mutate := range map[string]func(*Agent){
		"no resolved cap":    func(a *Agent) { a.ResolvedMaxTokens = 0 },
		"no resolved window": func(a *Agent) { a.ResolvedWindow = 0; a.EffectiveBudget = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			const maxTokens = 100
			cc, d := reasoningLoop(1 << 20)
			a := sizedToolAgent(maxTokens)
			mutate(&a)

			r := toolEngine(cc, d).invokeAgent(context.Background(), a)
			require.Equal(t, StatusOK, r.Status)
			assert.NotContains(t, r.TrippedBudgets, budgetReasoningReplay)
		})
	}
}

// A tool-loop agent's payload is sized to leave room for the output cap plus
// payload.ReasoningReplayReserveCaps caps of replayed reasoning; a non-tool agent keeps
// the plain output-cap reservation.
func TestBuildSlots_ToolLoopAgentReservesReplayedReasoning(t *testing.T) {
	cfg := sizingRosterConfig()
	greta := cfg.Registry.Agents["greta"]
	greta.Tools = true
	greta.SupportsFC = true
	cfg.Registry.Agents["greta"] = greta

	a, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	want := payload.EffectiveByteBudget("unlisted-small-model", nil, defaultMaxTokens*(1+payload.ReasoningReplayReserveCaps))
	require.Positive(t, want, "precondition: the 32k window still funds a payload after the reserve")
	assert.Equal(t, want, a.EffectiveBudget)
	assert.Less(t, a.EffectiveBudget, payload.EffectiveByteBudget("unlisted-small-model", nil, defaultMaxTokens),
		"the reserve must actually shrink the budget below the plain output-cap reservation")
	assert.Equal(t, defaultMaxTokens, a.ReservedOutputTokens, "the recorded output cap is unchanged")
}

// A range-less review (baseline --all/--dir, diff ingestion) has no head to
// snapshot, so the tool harness is never wired and a tool+FC agent degrades to
// single-shot: it replays nothing, so it keeps the plain output-cap reservation.
func TestBuildSlots_RangeLessToolAgentKeepsPlainReserve(t *testing.T) {
	cfg := sizingRosterConfig()
	greta := cfg.Registry.Agents["greta"]
	greta.Tools = true
	greta.SupportsFC = true
	cfg.Registry.Agents["greta"] = greta

	a, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{}, "", "")
	require.NoError(t, err)
	assert.Equal(t, payload.EffectiveByteBudget("unlisted-small-model", nil, defaultMaxTokens), a.EffectiveBudget,
		"no range head means no tool loop, so no replayed-reasoning reserve")
}

func TestBuildSlots_ToolsWithoutFunctionCallingKeepsPlainReserve(t *testing.T) {
	cfg := sizingRosterConfig()
	greta := cfg.Registry.Agents["greta"]
	greta.Tools = true
	cfg.Registry.Agents["greta"] = greta

	a, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	assert.Equal(t, payload.EffectiveByteBudget("unlisted-small-model", nil, defaultMaxTokens), a.EffectiveBudget,
		"a degraded single-shot agent never enters the loop, so it replays nothing")
}

// When the replayed-reasoning reserve alone closes a chunked tool-loop agent's
// budget, its chunks must sit at the minChunkLines floor like any other
// zero-budget agent — not at the line count the unreserved cap would fund.
func TestBuildSlots_ChunkedToolLoopReserveClosingTheBudgetFloorsChunkLines(t *testing.T) {
	const capTokens = 10000
	const wantLines = 64 // payload.minChunkLines
	cfg := sizingRosterConfig()
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.OnOverflow = OverflowTruncate
	greta := cfg.Registry.Agents["greta"]
	greta.Tools = true
	greta.SupportsFC = true
	greta.MaxTokens = ptrInt(capTokens)
	cfg.Registry.Agents["greta"] = greta

	require.Zero(t, payload.EffectiveByteBudget("unlisted-small-model", nil, payload.SizingOutputTokens(true, capTokens)),
		"precondition: the reserve closes the budget")
	require.Positive(t, payload.EffectiveByteBudget("unlisted-small-model", nil, capTokens),
		"precondition: the cap alone still funds a budget")

	slots, _, err := buildSlots(cfg, chunkedDiffPayload(12, 900), ReviewRange{Base: "a", Head: "b"}, "", "", false)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: the diff must split into chunks")
	for i, s := range slots {
		assert.Equal(t, wantLines, s.Primary.chunkMaxLines,
			"chunk slot %d: a budget the reserve closes must floor the chunk lines", i)
	}
}

// A fallback runs the loop when its PRIMARY's lane requests tools and its OWN
// model declares function calling, on a review with a range head; only then is
// its budget sized with the replayed-reasoning reserve.
func TestBuildFallbackAgent_ReplayReserveFollowsTheLane(t *testing.T) {
	ranged := ReviewRange{Base: "a", Head: "b"}
	cases := []struct {
		name               string
		primaryTools, fbFC bool
		fbOwnTools         bool
		rng                ReviewRange
		wantReserved       bool
	}{
		{"tools lane, FC fallback", true, true, false, ranged, true},
		{"tools lane, non-FC fallback", true, false, false, ranged, false},
		{"non-tools lane, FC fallback declaring its own tools", false, true, true, ranged, false},
		{"tools lane, FC fallback, range-less review", true, true, false, ReviewRange{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := sizingRosterConfig()
			kai := cfg.Registry.Agents["kai"]
			kai.Model = "unlisted-backup-model"
			kai.SupportsFC = tc.fbFC
			kai.Tools = tc.fbOwnTools
			cfg.Registry.Agents["kai"] = kai
			primary := Agent{Name: "greta", Tools: tc.primaryTools, SupportsFC: true}

			fb, _, err := buildFallbackAgent(cfg, primary, "kai", false, fallbackRefit{rng: tc.rng})
			require.NoError(t, err)
			want := payload.EffectiveByteBudget("unlisted-backup-model", nil, payload.SizingOutputTokens(tc.wantReserved, defaultMaxTokens))
			require.NotEqual(t, payload.EffectiveByteBudget("unlisted-backup-model", nil, defaultMaxTokens),
				payload.EffectiveByteBudget("unlisted-backup-model", nil, payload.SizingOutputTokens(true, defaultMaxTokens)),
				"precondition: the reserve changes the budget")
			assert.Equal(t, want, fb.EffectiveBudget)
		})
	}
}
