package fanout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test doubles -----------------------------------------------------------

// chatTurn scripts one Chat response. When toolCalls is non-empty the response
// is an assistant tool-call turn (content null); otherwise it is a final message
// carrying content.
type chatTurn struct {
	toolCalls []llmclient.ToolCall
	content   string
	err       error
	delay     time.Duration
}

// scriptedChat implements both Completer (single-shot) and ChatCompleter
// (multi-turn). Each Chat call pops the next scripted turn; calls past the end
// return a default final message so a tripped loop's final-answer request always
// resolves.
type scriptedChat struct {
	mu           sync.Mutex
	completeResp string
	completeErr  error
	turns        []chatTurn
	idx          int
	chatCalls    int
	toolsSeen    []bool                // per Chat call: were tool defs supplied?
	msgsSeen     [][]llmclient.Message // per Chat call: snapshot of history
	onChat       func(call int, cancel context.CancelFunc)
	cancel       context.CancelFunc
}

func (s *scriptedChat) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	if s.completeErr != nil {
		return "", s.completeErr
	}
	return s.completeResp, nil
}

func (s *scriptedChat) Chat(ctx context.Context, _ llmclient.Invocation, messages []llmclient.Message, toolDefs []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	// Faithfully reject a malformed conversation the way an OpenAI-compatible
	// provider does: every assistant tool_call id must be answered by a later
	// role:"tool" message before the next request. This guards the loop's
	// well-formedness invariant (a dangling tool_call_id is an HTTP 400).
	if err := assertToolCallsAnswered(messages); err != nil {
		return nil, err
	}
	s.mu.Lock()
	call := s.idx
	s.idx++
	s.chatCalls++
	s.toolsSeen = append(s.toolsSeen, len(toolDefs) > 0)
	s.msgsSeen = append(s.msgsSeen, append([]llmclient.Message(nil), messages...))
	var turn chatTurn
	if call < len(s.turns) {
		turn = s.turns[call]
	} else {
		turn = chatTurn{content: "FINAL"}
	}
	onChat := s.onChat
	cancel := s.cancel
	s.mu.Unlock()

	if onChat != nil {
		onChat(call, cancel)
	}
	if turn.delay > 0 {
		select {
		case <-time.After(turn.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if turn.err != nil {
		return nil, turn.err
	}
	msg := llmclient.Message{Role: "assistant", ToolCalls: turn.toolCalls}
	fr := "stop"
	if len(turn.toolCalls) > 0 {
		fr = "tool_calls"
	} else {
		c := turn.content
		msg.Content = &c
	}
	return &llmclient.ChatResponse{Message: msg, FinishReason: fr}, nil
}

// assertToolCallsAnswered returns an error when any assistant tool_call lacks a
// matching role:"tool" answer in the conversation — the malformed shape real
// providers reject with HTTP 400.
func assertToolCallsAnswered(messages []llmclient.Message) error {
	answered := map[string]bool{}
	for _, m := range messages {
		if m.Role == "tool" {
			answered[m.ToolCallID] = true
		}
	}
	for _, m := range messages {
		for _, tc := range m.ToolCalls {
			if !answered[tc.ID] {
				return fmt.Errorf("provider 400: assistant tool_call id %q has no role:tool answer", tc.ID)
			}
		}
	}
	return nil
}

// fakeDispatcher mimics tools.Dispatcher's contract: unknown tool names yield a
// ToolError (never fatal), per-tool results/errors are configurable, and an
// optional gate lets a test hold a tool call open to exercise mid-tool timeout.
type fakeDispatcher struct {
	mu      sync.Mutex
	byName  map[string]tools.ToolResult
	errFor  map[string]error
	calls   []string
	gate    chan struct{}
	entered chan struct{} // signaled (non-blocking) when Execute is entered
	panicON map[string]bool
}

func newFakeDispatcher() *fakeDispatcher {
	return &fakeDispatcher{byName: map[string]tools.ToolResult{}, errFor: map[string]error{}, panicON: map[string]bool{}}
}

func (d *fakeDispatcher) Execute(ctx context.Context, name string, _ json.RawMessage) (tools.ToolResult, error) {
	d.mu.Lock()
	d.calls = append(d.calls, name)
	res, known := d.byName[name]
	err := d.errFor[name]
	gate := d.gate
	entered := d.entered
	doPanic := d.panicON[name]
	d.mu.Unlock()

	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return tools.ToolResult{}, ctx.Err()
		}
	}
	if doPanic {
		panic("boom")
	}
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !known {
		return tools.ToolResult{}, &tools.ToolError{Message: "unknown tool: " + name}
	}
	return res, nil
}

func (d *fakeDispatcher) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

// toolCall builds an assistant tool_call with object-form arguments.
func toolCall(id, name, args string) llmclient.ToolCall {
	return llmclient.ToolCall{ID: id, Type: "function", Function: llmclient.FunctionCall{Name: name, Arguments: json.RawMessage(args)}}
}

// toolAgent builds a tool-enabled Agent.
func toolAgent(name string, maxTurns int, byteBudget int64) Agent {
	return Agent{
		Name:            name,
		Invocation:      llmclient.Invocation{Model: name},
		PayloadMode:     "blocks",
		Tools:           true,
		MaxTurns:        maxTurns,
		ToolBudgetBytes: byteBudget,
	}
}

func toolEngine(cc ChatCompleter, d toolDispatcher) *Engine {
	return NewEngine(cc, WithDispatcher(d))
}

// --- AC 01-02: multi-turn loop ---------------------------------------------

func TestLoop_TwoTurns(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{content: "final answer"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "   1| package x"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, "final answer", r.Content)
	assert.Equal(t, 2, r.Turns)
	assert.Equal(t, 1, r.ToolCalls)
	assert.True(t, r.Tools)
	assert.Equal(t, 1, d.callCount())
}

func TestLoop_ThreeTurnsMultipleCallsPerTurn(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`), toolCall("c2", "grep", `{"pattern":"foo"}`)}},
		{toolCalls: []llmclient.ToolCall{toolCall("c3", "list_files", `{"dir":"."}`)}},
		{content: "done"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}
	d.byName["grep"] = tools.ToolResult{Content: "y"}
	d.byName["list_files"] = tools.ToolResult{Content: "z"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 3, r.Turns)
	assert.Equal(t, 3, r.ToolCalls)

	// Tool results from turn 1 are appended as role:tool before turn 2's Chat.
	require.GreaterOrEqual(t, len(cc.msgsSeen), 2)
	var toolMsgs int
	for _, m := range cc.msgsSeen[1] {
		if m.Role == "tool" {
			toolMsgs++
		}
	}
	assert.Equal(t, 2, toolMsgs, "turn 2 history should carry turn 1's two tool results")
}

func TestLoop_BranchesOnAgentToolsFalse(t *testing.T) {
	cc := &scriptedChat{completeResp: "single shot review"}
	d := newFakeDispatcher()
	a := Agent{Name: "a", Invocation: llmclient.Invocation{Model: "a"}, PayloadMode: "blocks"} // Tools:false

	r := toolEngine(cc, d).invokeAgent(context.Background(), a)
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, "single shot review", r.Content)
	assert.Equal(t, 0, r.Turns)
	assert.Equal(t, 0, r.ToolCalls)
	assert.False(t, r.Tools)
	assert.Equal(t, 0, cc.chatCalls, "single-shot path must not call Chat")
	assert.Equal(t, 0, d.callCount())
}

// AC 01-02 Edge Case 1: empty Function.Name -> unknown tool error, loop continues.
func TestLoop_EmptyToolNameContinues(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "", `{}`)}},
		{content: "recovered"},
	}}
	d := newFakeDispatcher()

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, "recovered", r.Content)
	assert.Equal(t, 2, r.Turns)
	// The unknown-tool error is delivered as a role:tool message before turn 2.
	require.GreaterOrEqual(t, len(cc.msgsSeen), 2)
	var found bool
	for _, m := range cc.msgsSeen[1] {
		if m.Role == "tool" && m.Content != nil && contains(*m.Content, "unknown tool") {
			found = true
		}
	}
	assert.True(t, found, "unknown tool error should be a role:tool result")
}

// AC 01-02 Edge Case 3: dispatcher returns empty result string -> loop continues.
func TestLoop_EmptyToolResultContinues(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{content: "ok"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: ""}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 2, r.Turns)
	assert.Equal(t, 1, r.ToolCalls)
	assert.EqualValues(t, 0, r.ToolBytes)
}

// AC 01-02 Error Scenario 1: tool panic is recovered, loop continues.
func TestLoop_ToolPanicRecoveredContinues(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{content: "ok"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}
	d.errFor["read_file"] = &tools.ToolError{Message: "tool execution failed: boom"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 2, r.Turns)
}

// AC 01-02 Error Scenario 2: Chat error mid-loop propagates; partial Turns kept.
func TestLoop_ChatErrorMidLoop(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{err: errors.New("provider exploded")},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	assert.Equal(t, StatusFailed, r.Status)
	require.Error(t, r.Err)
	assert.Equal(t, 1, r.Turns)
}

// --- AC 01-04: loop hygiene -------------------------------------------------

func TestLoop_RepeatedCallNudgedThenHalts(t *testing.T) {
	same := toolCall("c1", "read_file", `{"path":"a.go"}`)
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{same}}, // turn 1: executes
		{toolCalls: []llmclient.ToolCall{same}}, // turn 2: repeat -> nudge, not executed
		{toolCalls: []llmclient.ToolCall{same}}, // turn 3: repeat again -> halt
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 1, d.callCount(), "repeated call must not be re-executed")
	assert.Equal(t, "FINAL", r.Content, "loop halts and requests a final answer")
}

func TestLoop_DifferentCallsNotFlaggedAsRepeats(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{toolCalls: []llmclient.ToolCall{toolCall("c2", "read_file", `{"path":"b.go"}`)}},
		{content: "done"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 2, d.callCount(), "different args are not repeats")
	assert.Equal(t, 3, r.Turns)
}

// AC 01-04 Edge Case 4: one repeat among several calls; only the repeat is skipped.
func TestLoop_MixedRepeatAndFreshCalls(t *testing.T) {
	rf := toolCall("c1", "read_file", `{"path":"a.go"}`)
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{rf, toolCall("c2", "grep", `{"pattern":"foo"}`)}},
		{toolCalls: []llmclient.ToolCall{rf, toolCall("c3", "list_files", `{"dir":"."}`)}},
		{content: "done"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}
	d.byName["grep"] = tools.ToolResult{Content: "y"}
	d.byName["list_files"] = tools.ToolResult{Content: "z"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)
	// turn 1: read_file + grep (2). turn 2: list_files only (read_file skipped). = 3.
	assert.Equal(t, 3, d.callCount())
}

// AC 01-04 Scenario 3/4: malformed JSON returns a tool error then halts on repeat.
func TestLoop_MalformedJSONRetryThenHalt(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{bad`)}},
		{toolCalls: []llmclient.ToolCall{toolCall("c2", "read_file", `{worse`)}},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 0, d.callCount(), "malformed args must never reach the dispatcher")
	assert.Equal(t, "FINAL", r.Content)
}

// --- AC 01-05: degrade path -------------------------------------------------

func TestDegrade_CompleterWithoutChatCompleter(t *testing.T) {
	// fakeCompleter (engine_test.go) implements only Complete, not ChatCompleter.
	fc := newFake()
	d := newFakeDispatcher()
	r := NewEngine(fc, WithDispatcher(d)).invokeAgent(context.Background(), toolAgent("m", 10, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.True(t, r.ToolsDegraded)
	assert.True(t, r.Tools)
	assert.Equal(t, 0, r.Turns)
	assert.Equal(t, "review by m", r.Content)
}

func TestDegrade_NoDispatcher(t *testing.T) {
	cc := &scriptedChat{completeResp: "degraded answer"}
	r := NewEngine(cc).invokeAgent(context.Background(), toolAgent("a", 10, 0)) // no WithDispatcher
	require.Equal(t, StatusOK, r.Status)
	assert.True(t, r.ToolsDegraded)
	assert.Equal(t, "degraded answer", r.Content)
	assert.Equal(t, 0, cc.chatCalls)
}

func TestDegrade_NonToolAgentNotDegraded(t *testing.T) {
	cc := &scriptedChat{completeResp: "x"}
	a := Agent{Name: "a", Invocation: llmclient.Invocation{Model: "a"}, PayloadMode: "blocks"}
	r := NewEngine(cc, WithDispatcher(newFakeDispatcher())).invokeAgent(context.Background(), a)
	require.Equal(t, StatusOK, r.Status)
	assert.False(t, r.ToolsDegraded)
	assert.False(t, r.Tools)
}

// --- AC 01-06: result accounting -------------------------------------------

func TestAccounting_NonToolResultZeroAndPointersNil(t *testing.T) {
	cc := &scriptedChat{completeResp: "x"}
	a := Agent{Name: "a", Invocation: llmclient.Invocation{Model: "a"}, PayloadMode: "blocks"}
	r := NewEngine(cc, WithDispatcher(newFakeDispatcher())).invokeAgent(context.Background(), a)

	assert.Equal(t, 0, r.Turns)
	assert.Equal(t, 0, r.ToolCalls)
	assert.EqualValues(t, 0, r.ToolBytes)

	st := statusFor(r, 0)
	assert.Nil(t, st.Turns)
	assert.Nil(t, st.ToolCalls)
	assert.Nil(t, st.ToolBytes)
}

func TestAccounting_ToolResultPropagatesToStatus(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{content: "done"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "abcd"} // 4 bytes

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)
	assert.EqualValues(t, 4, r.ToolBytes)

	st := statusFor(r, 0)
	require.NotNil(t, st.Turns)
	require.NotNil(t, st.ToolCalls)
	require.NotNil(t, st.ToolBytes)
	assert.Equal(t, 2, *st.Turns)
	assert.Equal(t, 1, *st.ToolCalls)
	assert.EqualValues(t, 4, *st.ToolBytes)
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
