package fanout

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toolCallTurns scripts n consecutive read_file tool-call turns (distinct args so
// they are not flagged as repeats).
func toolCallTurns(n int) []chatTurn {
	turns := make([]chatTurn, n)
	for i := 0; i < n; i++ {
		turns[i] = chatTurn{toolCalls: []llmclient.ToolCall{
			toolCall("c", "read_file", `{"path":"f`+string(rune('0'+i))+`.go"}`),
		}}
	}
	return turns
}

// --- AC 02-01: turn budget ---------------------------------------------------

// Model A: MaxTurns=N => N Chat-with-tools turns; the Nth turn's tool_calls are
// NOT executed (no room to feed results back). Turns=N, ToolCalls=N-1.
func TestBudget_MaxTurnsTrips(t *testing.T) {
	// Exactly MaxTurns tool turns; the unbudgeted final-answer Chat then falls
	// through to the scriptedChat default final message ("FINAL").
	cc := &scriptedChat{turns: toolCallTurns(3)}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 3, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 3, r.Turns)
	assert.Equal(t, 2, r.ToolCalls, "Model A: last (3rd) turn's tool_calls not executed")
	assert.Equal(t, 2, d.callCount())
	assert.Equal(t, []string{budgetMaxTurns}, r.TrippedBudgets)
	assert.Equal(t, "FINAL", r.Content, "final answer requested after trip")
}

func TestBudget_CompletesWithinTurnBudget(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{toolCalls: []llmclient.ToolCall{toolCall("c2", "read_file", `{"path":"b.go"}`)}},
		{content: "done"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 3, r.Turns)
	assert.Empty(t, r.TrippedBudgets)
}

// AC 02-01 EC1: MaxTurns=1 executes zero tool calls (turn-1 tool_calls not run).
func TestBudget_MaxTurnsOne(t *testing.T) {
	cc := &scriptedChat{turns: toolCallTurns(2)}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 1, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 1, r.Turns)
	assert.Equal(t, 0, r.ToolCalls)
	assert.Equal(t, 0, d.callCount())
	assert.Equal(t, []string{budgetMaxTurns}, r.TrippedBudgets)
}

// Unset MaxTurns falls back to the engine default rather than running unbounded.
func TestBudget_MaxTurnsDefaultsWhenUnset(t *testing.T) {
	cc := &scriptedChat{turns: toolCallTurns(defaultMaxTurns + 5)}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 0, 0)) // MaxTurns unset
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, defaultMaxTurns, r.Turns)
	assert.Equal(t, []string{budgetMaxTurns}, r.TrippedBudgets)
}

// --- AC 02-02: tool byte budget --------------------------------------------

func TestBudget_ByteBudgetTrips(t *testing.T) {
	// 400 + 400 + 500 = 1300 > 1000, tripped at end of turn 3.
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a"}`)}},
		{toolCalls: []llmclient.ToolCall{toolCall("c2", "grep", `{"pattern":"b"}`)}},
		{toolCalls: []llmclient.ToolCall{toolCall("c3", "list_files", `{"dir":"."}`)}},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: repeat(400)}
	d.byName["grep"] = tools.ToolResult{Content: repeat(400)}
	d.byName["list_files"] = tools.ToolResult{Content: repeat(500)}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 1000))
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 3, r.Turns)
	assert.EqualValues(t, 1300, r.ToolBytes)
	assert.Equal(t, []string{budgetToolBytes}, r.TrippedBudgets)
}

func TestBudget_ByteBudgetNotTripped(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a"}`)}},
		{content: "done"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: repeat(600)}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 5000))
	require.Equal(t, StatusOK, r.Status)
	assert.EqualValues(t, 600, r.ToolBytes)
	assert.Empty(t, r.TrippedBudgets)
}

// AC 02-02 Edge 1: cumulative == budget does not trip (strictly greater).
func TestBudget_ByteBudgetExactlyMetDoesNotTrip(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a"}`)}},
		{toolCalls: []llmclient.ToolCall{toolCall("c2", "grep", `{"pattern":"b"}`)}},
		{content: "done"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: repeat(500)}
	d.byName["grep"] = tools.ToolResult{Content: repeat(500)}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 1000))
	require.Equal(t, StatusOK, r.Status)
	assert.EqualValues(t, 1000, r.ToolBytes)
	assert.Empty(t, r.TrippedBudgets)
}

// AC 02-02 Edge 2: a single oversize result is delivered in full, then trips.
func TestBudget_SingleOversizeResultDeliveredThenTrips(t *testing.T) {
	cc := &scriptedChat{turns: toolCallTurns(2)}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: repeat(500)}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 100))
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 1, d.callCount(), "the oversize result is delivered in full")
	assert.EqualValues(t, 500, r.ToolBytes)
	assert.Equal(t, []string{budgetToolBytes}, r.TrippedBudgets)
}

// AC 02-02 S3: ToolBudgetBytes=0 is unlimited.
func TestBudget_ByteBudgetUnlimited(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a"}`)}},
		{content: "done"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: repeat(100000)}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.Empty(t, r.TrippedBudgets)
}

// AC 02-02 Error 1: a tool error adds no bytes to the counter.
func TestBudget_ToolErrorAddsNoBytes(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a"}`)}},
		{content: "done"},
	}}
	d := newFakeDispatcher()
	d.errFor["read_file"] = &tools.ToolError{Message: "file not found"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 1000))
	require.Equal(t, StatusOK, r.Status)
	assert.EqualValues(t, 0, r.ToolBytes)
	assert.Empty(t, r.TrippedBudgets)
}

// --- AC 02-03: timeout ------------------------------------------------------

// AC 02-03 EC1 / 02-04 EC1: the first Chat is cancelled -> turns=0, timeout.
func TestBudget_TimeoutOnFirstChat(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cc := &scriptedChat{turns: toolCallTurns(2), cancel: cancel}
	cc.onChat = func(call int, c context.CancelFunc) {
		if call == 0 {
			c()
		}
	}
	d := newFakeDispatcher()
	r := toolEngine(cc, d).invokeAgent(ctx, toolAgent("a", 10, 0))
	assert.Equal(t, StatusTimeout, r.Status)
	assert.Equal(t, 0, r.Turns)
	assert.Equal(t, []string{budgetTimeout}, r.TrippedBudgets)
}

// AC 02-03 S2: timeout mid-loop -> partial result with completed turns.
func TestBudget_TimeoutMidLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cc := &scriptedChat{turns: toolCallTurns(3), cancel: cancel}
	cc.onChat = func(call int, c context.CancelFunc) {
		if call == 1 {
			c()
		}
	}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}

	r := toolEngine(cc, d).invokeAgent(ctx, toolAgent("a", 10, 0))
	assert.Equal(t, StatusTimeout, r.Status)
	assert.Equal(t, 1, r.Turns, "turn 1 completed before the cancel on turn 2")
	assert.Equal(t, []string{budgetTimeout}, r.TrippedBudgets)
}

// AC 02-03 S3: cancellation during tool execution -> partial result preserved.
func TestBudget_TimeoutDuringToolExecution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cc := &scriptedChat{turns: toolCallTurns(2)}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}
	d.gate = make(chan struct{}) // never released
	d.entered = make(chan struct{}, 1)
	go func() {
		<-d.entered // tool call is in flight
		cancel()
	}()

	r := toolEngine(cc, d).invokeAgent(ctx, toolAgent("a", 10, 0))
	assert.Equal(t, StatusTimeout, r.Status)
	assert.Equal(t, 1, r.Turns)
	assert.Equal(t, []string{budgetTimeout}, r.TrippedBudgets)
}

// AC 02-02 Error 2: when the deadline fires while cumulative bytes are still
// under budget, only timeout_secs is recorded (the byte check is never reached).
func TestBudget_TimeoutPrecedesByteBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cc := &scriptedChat{turns: toolCallTurns(4), cancel: cancel}
	cc.onChat = func(call int, c context.CancelFunc) {
		if call == 2 { // cancel during turn 3's Chat
			c()
		}
	}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: repeat(1000)} // 2 turns = 2000 < 5000 budget

	r := toolEngine(cc, d).invokeAgent(ctx, toolAgent("a", 10, 5000))
	assert.Equal(t, StatusTimeout, r.Status)
	assert.Equal(t, 2, r.Turns)
	assert.EqualValues(t, 2000, r.ToolBytes)
	assert.Equal(t, []string{budgetTimeout}, r.TrippedBudgets)
}

// repeat returns a string of n 'x' bytes.
func repeat(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}
