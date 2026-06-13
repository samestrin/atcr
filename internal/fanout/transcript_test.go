package fanout

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// transcriptEngine wires a per-agent transcript writer rooted at dir so the test
// can replay <dir>/<agent>.jsonl after the run.
func transcriptEngine(cc ChatCompleter, d toolDispatcher, dir string) *Engine {
	return NewEngine(cc, WithDispatcher(d), WithTranscript(func(agent string) *tools.Transcript {
		return tools.OpenTranscript(filepath.Join(dir, agent+".jsonl"), agent)
	}))
}

// AC 05-01 Scenario 5 + AC 05-02 Scenario 3: a multi-turn run emits a transcript
// whose replayed sequence matches the engine's Chat/tool exchange exactly.
func TestTranscript_LoopRecordsFullSequence(t *testing.T) {
	dir := t.TempDir()
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{toolCalls: []llmclient.ToolCall{toolCall("c2", "grep", `{"pattern":"foo"}`)}},
		{content: "final answer"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "   1| pkg", OriginalBytes: 9}
	d.byName["grep"] = tools.ToolResult{Content: "match", OriginalBytes: 5}

	r := transcriptEngine(cc, d, dir).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)

	res, err := tools.ReplayTranscript(filepath.Join(dir, "a.jsonl"))
	require.NoError(t, err)
	// tool_calls(1), tool_result(1), tool_calls(2), tool_result(2), final(3)
	require.Len(t, res.Events, 5)
	assert.Equal(t, "tool_calls", res.Events[0].Event)
	assert.Equal(t, "tool_result", res.Events[1].Event)
	assert.Equal(t, "tool_calls", res.Events[2].Event)
	assert.Equal(t, "tool_result", res.Events[3].Event)
	assert.Equal(t, "final", res.Events[4].Event)
	assert.Equal(t, 3, res.Events[4].Turn)
}

// AC 05-01 Edge Case 1: an immediate final answer (no tool calls) emits exactly
// one final event.
func TestTranscript_LoopImmediateFinal(t *testing.T) {
	dir := t.TempDir()
	cc := &scriptedChat{turns: []chatTurn{{content: "no tools needed"}}}
	d := newFakeDispatcher()

	r := transcriptEngine(cc, d, dir).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)

	res, err := tools.ReplayTranscript(filepath.Join(dir, "a.jsonl"))
	require.NoError(t, err)
	require.Len(t, res.Events, 1)
	assert.Equal(t, "final", res.Events[0].Event)
	assert.Equal(t, 1, res.Events[0].Turn)
}

// AC 05-02 Scenario 2: a Chat error mid-loop leaves a valid partial transcript
// (turn-1 events present, replayable) and no final event.
func TestTranscript_LoopPartialOnError(t *testing.T) {
	dir := t.TempDir()
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{err: errors.New("provider exploded")},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x", OriginalBytes: 1}

	r := transcriptEngine(cc, d, dir).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusFailed, r.Status)

	res, err := tools.ReplayTranscript(filepath.Join(dir, "a.jsonl"))
	require.NoError(t, err)
	// turn 1: tool_calls + tool_result. No final (loop errored before final).
	require.Len(t, res.Events, 2)
	assert.Equal(t, "tool_calls", res.Events[0].Event)
	assert.Equal(t, "tool_result", res.Events[1].Event)
}

// AC 05-01 Scenario 3: a truncated tool result is recorded with truncated:true
// and original_bytes in the transcript.
func TestTranscript_LoopRecordsTruncation(t *testing.T) {
	dir := t.TempDir()
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"big.go"}`)}},
		{content: "done"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "capped", Truncated: true, OriginalBytes: 50000}

	r := transcriptEngine(cc, d, dir).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusOK, r.Status)

	res, err := tools.ReplayTranscript(filepath.Join(dir, "a.jsonl"))
	require.NoError(t, err)
	require.Len(t, res.Events, 3)
	tr := res.Events[1]
	require.Equal(t, "tool_result", tr.Event)
	assert.JSONEq(t, `true`, string(tr.Raw["truncated"]))
	assert.JSONEq(t, `50000`, string(tr.Raw["original_bytes"]))
}

// A nil transcript factory (recording disabled — the default) does not affect
// the loop result.
func TestTranscript_DisabledByDefault(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{content: "done"},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x", OriginalBytes: 1}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0)) // no WithTranscript
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, "done", r.Content)
}
