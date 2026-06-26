package fanout

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gateStubResolver is a minimal jail: exec tools take no path args, so Resolve is
// never exercised; Root reports a fixed snapshot dir.
type gateStubResolver struct{ root string }

func (s gateStubResolver) Resolve(string) (string, error) { return s.root, nil }
func (s gateStubResolver) Root() string                   { return s.root }

// gateStubBackend records whether the sandbox was actually asked to run anything,
// so a test can prove a refused exec call never reached the backend.
type gateStubBackend struct {
	ran    bool
	result sandbox.RunResult
}

func (b *gateStubBackend) Name() string                    { return "gate-stub" }
func (b *gateStubBackend) Preflight(context.Context) error { return nil }
func (b *gateStubBackend) Run(context.Context, sandbox.RunSpec) (sandbox.RunResult, error) {
	b.ran = true
	return b.result, nil
}

func newRealExecDispatcher(b sandbox.Backend) *tools.Dispatcher {
	d := tools.NewDispatcher(gateStubResolver{root: "/snap"}, tools.DefaultLimits())
	d.EnableExecution(b, []string{"go", "test", "./..."}, 30*time.Second)
	return d
}

// TestLoop_ThreadsExecEligibility_AllowsExecAgent proves the fanout loop carries
// the agent's Exec flag into the dispatch context, so an execution-enabled agent
// reaches the run_tests handler on a real exec-wired dispatcher (AC2 — verify
// --exec keeps executing for exec agents).
func TestLoop_ThreadsExecEligibility_AllowsExecAgent(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "run_tests", `{"target":"./..."}`)}},
		{content: "done"},
	}}
	b := &gateStubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
	d := newRealExecDispatcher(b)

	a := toolAgent("exec-agent", 10, 0)
	a.Exec = true
	r := toolEngine(cc, d).invokeAgent(context.Background(), a)

	require.Equal(t, StatusOK, r.Status)
	assert.True(t, b.ran, "an exec-eligible agent must reach the sandbox backend through the loop")
}

// TestLoop_ThreadsExecEligibility_RefusesNonExecAgent proves the structural gate
// (Epic 11.1) holds end-to-end: a NON-exec agent that names run_script on the SAME
// shared exec-wired dispatcher is refused at dispatch — the call never reaches the
// backend and the model receives the refusal as a tool result (AC1, integration).
func TestLoop_ThreadsExecEligibility_RefusesNonExecAgent(t *testing.T) {
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "run_script", `{"content":"echo test\n"}`)}},
		{content: "done"},
	}}
	b := &gateStubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
	d := newRealExecDispatcher(b)

	a := toolAgent("readonly-agent", 10, 0) // Exec defaults to false
	r := toolEngine(cc, d).invokeAgent(context.Background(), a)

	require.Equal(t, StatusOK, r.Status)
	assert.False(t, b.ran, "a non-exec agent must NOT reach the backend even on a shared exec dispatcher")

	// The refusal is relayed to the model as a role:"tool" result on the next turn.
	require.GreaterOrEqual(t, len(cc.msgsSeen), 2)
	var sawRefusal bool
	for _, m := range cc.msgsSeen[1] {
		if m.Role == "tool" && m.Content != nil && strings.Contains(*m.Content, tools.ExecEligibilityRequiredMsg) {
			sawRefusal = true
		}
	}
	assert.True(t, sawRefusal, "the refusal must be relayed to the model as a tool result")
}
