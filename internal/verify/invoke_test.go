package verify

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/tools"
)

func intPtr(i int) *int       { return &i }
func int64Ptr(i int64) *int64 { return &i }

func testSkeptic() Skeptic {
	return Skeptic{
		Name:     "skeptic-1",
		Config:   registry.AgentConfig{Provider: "p", Model: "skeptic-model", Role: registry.RoleSkeptic, SupportsFC: true},
		Provider: registry.Provider{APIKeyEnv: "TEST_KEY", BaseURL: "http://localhost/v1"},
	}
}

// TestInvokeSkeptic_DegradesWhenNotFC verifies SupportsFC is forwarded: a skeptic
// whose model lacks function calling degrades to single-shot rather than being
// forced into the tool loop. The fake's single-shot Complete returns empty, so the
// verdict is unverifiable (empty_response) — but no error and a populated Skeptic.
func TestInvokeSkeptic_DegradesWhenNotFC(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.SupportsFC = false
	v, err := invokeSkeptic(context.Background(), sk, "prompt", finalChat(`{"verdict":"confirmed"}`), okDispatcher())
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Equal(t, "skeptic-1", v.Skeptic)
}

// TestBuildSkepticAgent_ForwardsProviderAndBudgets locks the provider routing and
// budget forwarding onto the constructed Agent/Invocation (the HIGH fix from 2.2.A).
func TestBuildSkepticAgent_ForwardsProviderAndBudgets(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.MaxTurns = intPtr(7)
	sk.Config.ToolBudgetBytes = int64Ptr(4096)
	sk.Config.TimeoutSecs = intPtr(30)
	a := buildSkepticAgent(sk, "the prompt")
	assert.True(t, a.Tools)
	assert.True(t, a.SupportsFC)
	assert.Equal(t, 7, a.MaxTurns)
	assert.Equal(t, int64(4096), a.ToolBudgetBytes)
	assert.Equal(t, 30, a.TimeoutSecs)
	assert.Equal(t, "http://localhost/v1", a.Invocation.BaseURL)
	assert.Equal(t, "TEST_KEY", a.Invocation.APIKeyEnv)
	assert.Equal(t, "skeptic-model", a.Invocation.Model)
	assert.Equal(t, "the prompt", a.Invocation.Prompt)
}

func TestInvokeSkeptic_Confirms(t *testing.T) {
	t.Parallel()
	cc := finalChat(`{"verdict": "confirmed", "reasoning": "evidence valid"}`)
	v, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher())
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, verdictConfirmed, v.Verdict)
	assert.Equal(t, "evidence valid", v.Notes)
	assert.Equal(t, "skeptic-1", v.Skeptic)
}

func TestInvokeSkeptic_Refutes(t *testing.T) {
	t.Parallel()
	cc := finalChat(`{"verdict": "refuted", "reasoning": "false positive"}`)
	v, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher())
	require.NoError(t, err)
	assert.Equal(t, verdictRefuted, v.Verdict)
	assert.Equal(t, "skeptic-1", v.Skeptic)
}

func TestInvokeSkeptic_UsesToolsThenConcludes(t *testing.T) {
	t.Parallel()
	cc := &fakeChatCompleter{turns: []chatTurn{
		toolCallTurn("read_file"),
		{content: `{"verdict": "confirmed", "reasoning": "verified via file read"}`},
	}}
	disp := okDispatcher()
	v, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, disp)
	require.NoError(t, err)
	assert.Equal(t, verdictConfirmed, v.Verdict)
	assert.Equal(t, "verified via file read", v.Notes)
	assert.GreaterOrEqual(t, disp.count(), 1, "tool loop should have dispatched at least once")
}

func TestInvokeSkeptic_ProviderError(t *testing.T) {
	t.Parallel()
	cc := &fakeChatCompleter{turns: []chatTurn{{err: errors.New("rate limit exceeded")}}}
	v, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher())
	require.NoError(t, err) // runtime failure NOT propagated
	require.NotNil(t, v)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Equal(t, "skeptic-1", v.Skeptic)
	assert.Contains(t, v.Notes, "rate limit exceeded")
}

func TestInvokeSkeptic_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the first turn
	cc := &fakeChatCompleter{turns: []chatTurn{{content: `{"verdict":"confirmed"}`}}}
	v, err := invokeSkeptic(ctx, testSkeptic(), "prompt", cc, okDispatcher())
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Equal(t, "skeptic-1", v.Skeptic)
}

func TestInvokeSkeptic_MalformedOutput(t *testing.T) {
	t.Parallel()
	cc := finalChat("I don't know")
	v, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher())
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "malformed_output")
	assert.Equal(t, "skeptic-1", v.Skeptic)
}

func TestInvokeSkeptic_EmptyResponse(t *testing.T) {
	t.Parallel()
	cc := finalChat("")
	v, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher())
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "empty_response")
}

func TestInvokeSkeptic_BudgetTripMaxTurns(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.MaxTurns = intPtr(2)
	cc := &fakeChatCompleter{turns: []chatTurn{toolCallTurn("read_file"), toolCallTurn("read_file")}}
	v, err := invokeSkeptic(context.Background(), sk, "prompt", cc, okDispatcher())
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "max_turns")
}

func TestInvokeSkeptic_BudgetTripToolBytes(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.ToolBudgetBytes = int64Ptr(10)
	cc := &fakeChatCompleter{turns: []chatTurn{toolCallTurn("read_file")}}
	disp := &fakeDispatcher{result: tools.ToolResult{Content: "this content is definitely more than ten bytes", OriginalBytes: 46}}
	v, err := invokeSkeptic(context.Background(), sk, "prompt", cc, disp)
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "tool_budget_bytes")
}

func TestInvokeSkeptic_NilContext(t *testing.T) {
	t.Parallel()
	_, err := invokeSkeptic(nil, testSkeptic(), "prompt", finalChat("{}"), okDispatcher()) //nolint:staticcheck // intentional nil-ctx guard test
	require.Error(t, err)
}

func TestInvokeSkeptic_NilChatCompleter(t *testing.T) {
	t.Parallel()
	_, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", nil, okDispatcher())
	require.Error(t, err)
}

func TestInvokeSkeptic_NilDispatcher(t *testing.T) {
	t.Parallel()
	_, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", finalChat("{}"), nil)
	require.Error(t, err)
}
