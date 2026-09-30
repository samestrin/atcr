package fanout

import (
	"context"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
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
