package fanout

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC 05-03 Scenario 1/2: after a multi-turn run, status.json counters match the
// actual counts and are finalized (not mutated post-completion).
func TestCounters_FinalizedOnNormalCompletion(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{toolCalls: []llmclient.ToolCall{toolCall("c2", "grep", `{"pattern":"x"}`)}},
		{content: "done"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "abcde"} // 5 bytes
	d.byName["grep"] = tools.ToolResult{Content: "fg"}         // 2 bytes

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)

	st := statusFor(r, findingsResult{})
	require.NotNil(t, st.Turns)
	assert.Equal(t, 3, *st.Turns)
	assert.Equal(t, 2, *st.ToolCalls)
	assert.EqualValues(t, 7, *st.ToolBytes)
}

// AC 05-03 Scenario 3: counters are finalized at a byte-budget trip and the
// tripped budget is recorded.
func TestCounters_FinalizedOnBudgetTrip(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{toolCalls: []llmclient.ToolCall{toolCall("c2", "read_file", `{"path":"b.go"}`)}},
		{content: "should not reach"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "0123456789"} // 10 bytes each

	// Budget 15: turn 1 delivers 10 (under), turn 2 delivers 20 total (over) → trip.
	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 15))
	require.Equal(t, StatusOK, r.Status)

	st := statusFor(r, findingsResult{})
	require.NotNil(t, st.ToolBytes)
	assert.EqualValues(t, 20, *st.ToolBytes)
	assert.Equal(t, 2, *st.ToolCalls)
	assert.Contains(t, st.TrippedBudgets, budgetToolBytes)
}

// AC 05-03 Edge Case 3: tool_bytes above the int32 range serializes exactly.
func TestCounters_LargeInt64ToolBytes(t *testing.T) {
	r := Result{Agent: "a", Status: StatusOK, Tools: true, Turns: 1, ToolCalls: 1, ToolBytes: 3_000_000_000}
	data, err := json.Marshal(statusFor(r, findingsResult{}))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"tool_bytes":3000000000`)

	var got AgentStatus
	require.NoError(t, json.Unmarshal(data, &got))
	require.NotNil(t, got.ToolBytes)
	assert.EqualValues(t, 3_000_000_000, *got.ToolBytes)
}
