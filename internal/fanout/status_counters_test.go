package fanout

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
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

// The grounding gate's per-agent drop count reaches status.json.
//
// It was stderr-only through epic 14.1. findings_count is the SURVIVING count, so
// a reviewer that raised three findings and had all three dropped as ungrounded is
// byte-identical on disk to one that genuinely found nothing — and the
// repo-state-v1 benchmark tier publishes exactly that pair as the same "clean"
// outcome. Persisting the count is what makes them distinguishable at all.
func TestStatusFor_PersistsTheGroundingDropCount(t *testing.T) {
	r := Result{Agent: "a", Status: StatusOK}

	st := statusFor(r, findingsResult{Ungrounded: 3})

	assert.Equal(t, 3, st.DroppedByGrounding,
		"the ungrounded count must reach status.json, not stderr alone")
}

// A present zero is a real claim — "the gate ran and dropped nothing" — so the
// field must serialize even when empty, exactly as the two sibling counters do.
func TestStatusFor_GroundingDropCountSerializesAtZero(t *testing.T) {
	st := statusFor(Result{Agent: "a", Status: StatusOK}, findingsResult{})

	b, err := json.Marshal(st)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"dropped_by_grounding":0`,
		"a zero must be present, not omitted — absence means a status.json predating the field")
}

// findingsFor is where the count originates: groundFindings already returns it and
// the result was discarded. Pinned end to end so the wiring cannot regress to a
// stderr-only warning.
func TestFindingsFor_RecordsTheUngroundedCount(t *testing.T) {
	r := Result{
		Agent:   "a",
		Status:  StatusOK,
		Content: "HIGH|nope.go:1|not in the patch|fix|correctness|5|ctx\n",
	}
	changed := payload.ChangedLines{"real.go": {Ranges: []payload.LineRange{{Start: 1, End: 1}}}}

	fr := findingsFor(r, changed)

	assert.Empty(t, fr.Findings, "a finding citing an untouched file is dropped")
	assert.Equal(t, 1, fr.Ungrounded, "and the drop is counted, not just warned about")
}
