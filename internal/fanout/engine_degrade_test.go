package fanout

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what was
// written. Not parallel-safe (mutates the global os.Stderr) — callers must not
// call t.Parallel.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

// incapableToolAgent builds a tool-enabled agent whose model does NOT support
// function calling (supports_function_calling=false), so the Phase 4 capability
// gate must degrade it to single-shot even when a full harness is wired.
func incapableToolAgent(name string) Agent {
	a := toolAgent(name, 10, 0)
	a.SupportsFC = false
	return a
}

// --- AC 04-01: single-shot degradation path (capability gate) ---------------

// AC 04-01 S1 / EC1: a tools:true agent on a non-tool-capable model degrades to
// single-shot even though a ChatCompleter AND a dispatcher are both wired — the
// registry capability declaration, not the harness, governs.
func TestInvokeAgent_DegradeToSingleShot(t *testing.T) {
	cc := &scriptedChat{completeResp: "single shot answer", turns: []chatTurn{{content: "should not run"}}}
	d := newFakeDispatcher()

	r := NewEngine(cc, WithDispatcher(d)).invokeAgent(context.Background(), incapableToolAgent("m"))

	require.Equal(t, StatusOK, r.Status)
	assert.True(t, r.ToolsDegraded, "incapable model must degrade")
	assert.True(t, r.Tools, "result still marked tool-enabled")
	assert.True(t, r.ToolsRequested, "original tools:true request preserved")
	assert.Equal(t, 0, cc.chatCalls, "capability gate must skip the tool loop entirely")
	assert.Equal(t, "single shot answer", r.Content)
	assert.Equal(t, 0, r.Turns)
}

// AC 04-02 S/EC: a tools:true agent on a tool-capable model runs the multi-turn
// loop (Chat called, not Complete) and is NOT degraded.
func TestInvokeAgent_ToolCapableRunsLoop(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{{content: "final answer"}}}
	d := newFakeDispatcher()

	r := NewEngine(cc, WithDispatcher(d)).invokeAgent(context.Background(), toolAgent("m", 10, 0))

	require.Equal(t, StatusOK, r.Status)
	assert.False(t, r.ToolsDegraded, "capable model runs the loop, no degrade")
	assert.True(t, r.Tools)
	assert.True(t, r.ToolsRequested)
	assert.Equal(t, 1, cc.chatCalls, "tool-capable agent drives the Chat loop")
	assert.Equal(t, "final answer", r.Content)
}

// AC 04-01 S2 / EC2: a tools:false agent runs single-shot and never triggers any
// degrade logic; tools_requested is false.
func TestInvokeAgent_ToolsFalseNoDegradation(t *testing.T) {
	cc := &scriptedChat{completeResp: "plain"}
	a := Agent{Name: "a", Invocation: llmclient.Invocation{Model: "a"}, PayloadMode: "blocks"}

	r := NewEngine(cc, WithDispatcher(newFakeDispatcher())).invokeAgent(context.Background(), a)

	require.Equal(t, StatusOK, r.Status)
	assert.False(t, r.ToolsDegraded)
	assert.False(t, r.Tools)
	assert.False(t, r.ToolsRequested, "tools:false → tools_requested false")
	assert.Equal(t, 0, cc.chatCalls)
}

// AC 04-01 S1/S3: tools_requested survives onto the serialized AgentStatus for a
// degraded agent, while tools_degraded is true.
func TestInvokeAgent_DegradeStatusPreservesRequested(t *testing.T) {
	cc := &scriptedChat{completeResp: "x"}
	r := NewEngine(cc, WithDispatcher(newFakeDispatcher())).invokeAgent(context.Background(), incapableToolAgent("m"))

	st := statusFor(r, 0)
	data, err := json.Marshal(&st)
	require.NoError(t, err)
	s := string(data)
	assert.Contains(t, s, `"tools_degraded":true`)
	assert.Contains(t, s, `"tools_requested":true`)
}

// AC 04-01 EC3: a pure 1.x single-shot agent emits neither tools_requested nor
// tools_degraded (omitempty backward-compat).
func TestInvokeAgent_SingleShotStatusOmitsToolFields(t *testing.T) {
	cc := &scriptedChat{completeResp: "x"}
	a := Agent{Name: "a", Invocation: llmclient.Invocation{Model: "a"}, PayloadMode: "blocks"}
	r := NewEngine(cc).invokeAgent(context.Background(), a)

	st := statusFor(r, 0)
	data, err := json.Marshal(&st)
	require.NoError(t, err)
	s := string(data)
	assert.NotContains(t, s, "tools_requested")
	assert.NotContains(t, s, "tools_degraded")
}

// AC 04-01 Error Scenario 1: a capable-declared model that returns no tool_calls
// on its FIRST turn logs a misconfiguration warning but still succeeds (the
// warning is a hint, not a failure).
func TestLoop_WarnsOnNoToolCallsFirstTurn(t *testing.T) {
	var r Result
	out := captureStderr(t, func() {
		cc := &scriptedChat{turns: []chatTurn{{content: "answer without ever calling a tool"}}}
		r = toolEngine(cc, newFakeDispatcher()).invokeAgent(context.Background(), toolAgent("greta", 10, 0))
	})
	require.Equal(t, StatusOK, r.Status, "warning is non-fatal; loop returns the answer")
	assert.False(t, r.ToolsDegraded, "capable model running the loop is not degraded")
	assert.Contains(t, out, "no tool_calls")
	assert.Contains(t, out, "greta", "warning names the agent")
}

// A normal multi-turn run that ends with a final message on a LATER turn must NOT
// warn — the model did use tools first.
func TestLoop_NoWarnWhenToolsUsedFirst(t *testing.T) {
	out := captureStderr(t, func() {
		cc := &scriptedChat{turns: []chatTurn{
			{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)}},
			{content: "final after reading"},
		}}
		d := newFakeDispatcher()
		d.byName["read_file"] = tools.ToolResult{Content: "x"}
		r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("greta", 10, 0))
		require.Equal(t, StatusOK, r.Status)
	})
	assert.NotContains(t, out, "no tool_calls", "tools were used on turn 1; no misconfiguration warning")
}

// --- AC 04-04: mixed roster reconciler compatibility ------------------------

// AC 04-04 S/Measurable: a tool-loop agent and a single-shot agent invoked in the
// same review on the same engine both produce reconcile-ready artifacts; the
// status layer consumes both result shapes with no special-casing. The tool
// agent's status carries the tool fields; the single-shot agent's omits them.
func TestMixedRoster_BothShapesReconcileIdentically(t *testing.T) {
	cc := &scriptedChat{completeResp: "single", turns: []chatTurn{{content: "looped"}}}
	eng := NewEngine(cc, WithDispatcher(newFakeDispatcher()))

	loopRes := eng.invokeAgent(context.Background(), toolAgent("tool", 10, 0))
	plain := Agent{Name: "plain", Invocation: llmclient.Invocation{Model: "plain"}, PayloadMode: "blocks"}
	plainRes := eng.invokeAgent(context.Background(), plain)

	require.Equal(t, StatusOK, loopRes.Status)
	require.Equal(t, StatusOK, plainRes.Status)
	assert.True(t, loopRes.Tools)
	assert.False(t, loopRes.ToolsDegraded)
	assert.False(t, plainRes.Tools)

	// Both shapes serialize through the same status path without error.
	for _, r := range []Result{loopRes, plainRes} {
		st := statusFor(r, 0)
		_, err := json.Marshal(&st)
		require.NoError(t, err, "status.json must serialize for %s", r.Agent)
	}

	// Tool agent's status exposes counters; single-shot agent's does not.
	toolSt := statusFor(loopRes, 0)
	require.NotNil(t, toolSt.Turns, "tool agent reports turns")
	plainSt := statusFor(plainRes, 0)
	assert.Nil(t, plainSt.Turns, "single-shot agent omits tool counters")
}
