package fanout

import (
	"context"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSingleShot_ThreadsCallRecordsOntoResult verifies the single-shot path
// carries the client's per-call telemetry from CompleteWithUsage onto the Result.
func TestSingleShot_ThreadsCallRecordsOntoResult(t *testing.T) {
	t.Parallel()
	recs := []llmclient.CallRecord{{ReachedWire: true, Duration: 5 * time.Millisecond}}
	e := NewEngine(&usageCompleter{records: recs})
	r := e.invokeSingleShot(context.Background(), Agent{Name: "x", Invocation: llmclient.Invocation{Model: "m"}})
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, recs, r.CallRecords, "single-shot must surface the client's CallRecords")
}

// TestToolLoop_AccumulatesCallRecordsAcrossTurns verifies the tool loop sums the
// per-turn CallRecords from every Chat() call (turn 1's tool call + turn 2's
// final message) onto the Result, mirroring how token usage is accumulated.
func TestToolLoop_AccumulatesCallRecordsAcrossTurns(t *testing.T) {
	t.Parallel()
	cc := &scriptedChat{turns: []chatTurn{
		{
			toolCalls:   []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)},
			callRecords: []llmclient.CallRecord{{ReachedWire: true, Duration: time.Millisecond}},
		},
		{
			content:     "final answer",
			callRecords: []llmclient.CallRecord{{ReachedWire: true, Duration: 2 * time.Millisecond}},
		},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}

	e := toolEngine(cc, d)
	r := e.invokeToolLoop(context.Background(), toolAgent("a", 10, 0), cc, d)
	require.Equal(t, StatusOK, r.Status)
	require.Len(t, r.CallRecords, 2, "both turns' CallRecords must accumulate onto the Result")
	assert.Equal(t, time.Millisecond, r.CallRecords[0].Duration)
	assert.Equal(t, 2*time.Millisecond, r.CallRecords[1].Duration)
}
