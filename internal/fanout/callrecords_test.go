package fanout

import (
	"context"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/metrics"
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

// TestToolLoop_MidFlightTimeoutCountsWireAttempt verifies the tool-loop path does
// not reproduce the AC1 undercount: a Chat() that reached the wire and then timed
// out surfaces its CallRecords even though it returns an error, so the wire
// attempt is counted and timed (independent-review MEDIUM finding).
func TestToolLoop_MidFlightTimeoutCountsWireAttempt(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)

	cc := &scriptedChat{turns: []chatTurn{
		{
			err:         context.DeadlineExceeded,
			callRecords: []llmclient.CallRecord{{ReachedWire: true, Duration: 7 * time.Millisecond}},
		},
	}}
	r := toolEngine(cc, newFakeDispatcher()).invokeAgent(context.Background(), toolAgent("a", 10, 0))

	require.Equal(t, StatusTimeout, r.Status)
	require.Len(t, r.CallRecords, 1, "the errored turn's wire attempt must be recorded")
	assert.True(t, r.CallRecords[0].ReachedWire)
	if got := metrics.Counter(metrics.NameAPICallsTotal).Value(); got != 1 {
		t.Errorf("atcr_api_calls_total = %d, want 1 (mid-flight tool-loop timeout counts its wire attempt, not 0)", got)
	}
	if got := metrics.Histogram(metrics.NameAPICallDurationSeconds).Count(); got != 1 {
		t.Errorf("histogram count = %d, want 1 (the wire attempt is timed)", got)
	}
}
