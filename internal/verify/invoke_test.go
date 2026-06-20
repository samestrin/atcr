package verify

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/log"
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
// forced into the tool loop. The fake's Complete returns a real verdict so the
// single-shot path produces a confirmed outcome — proving the degrade happened
// and the tool loop was skipped (dispatcher call count == 0).
func TestInvokeSkeptic_DegradesWhenNotFC(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.SupportsFC = false
	disp := okDispatcher()
	v, _, err := invokeSkeptic(context.Background(), sk, "prompt", finalChat(`{"verdict":"confirmed"}`), disp)
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, "confirmed", v.Verdict, "degrade path should return the single-shot verdict, not unverifiable")
	assert.Equal(t, "skeptic-1", v.Skeptic)
	assert.Equal(t, 0, disp.count(), "tool loop must not be entered on degrade — dispatcher never called")
}

// TestBuildSkepticAgent_ForwardsProviderAndBudgets locks the provider routing and
// budget forwarding onto the constructed Agent/Invocation (the HIGH fix from 2.2.A).
func TestBuildSkepticAgent_ForwardsProviderAndBudgets(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.MaxTurns = intPtr(7)
	sk.Config.ToolBudgetBytes = int64Ptr(4096)
	sk.Config.TimeoutSecs = intPtr(30)
	sk.Config.MaxRetries = intPtr(4)
	sk.Config.InitialBackoffMs = intPtr(200)
	a := buildSkepticAgent(sk, "the prompt")
	assert.True(t, a.Tools)
	assert.True(t, a.SupportsFC)
	assert.Equal(t, 7, a.MaxTurns)
	assert.Equal(t, int64(4096), a.ToolBudgetBytes)
	assert.Equal(t, 30, a.TimeoutSecs)
	assert.Equal(t, 4, a.MaxRetries, "skeptic max_retries forwarded (Epic 4.6)")
	assert.Equal(t, 200, a.InitialBackoffMs, "skeptic initial_backoff_ms forwarded (Epic 4.6)")
	assert.Equal(t, "http://localhost/v1", a.Invocation.BaseURL)
	assert.Equal(t, "TEST_KEY", a.Invocation.APIKeyEnv)
	assert.Equal(t, "skeptic-model", a.Invocation.Model)
	assert.Equal(t, "the prompt", a.Invocation.Prompt)
}

func TestInvokeSkeptic_Confirms(t *testing.T) {
	t.Parallel()
	cc := finalChat(`{"verdict": "confirmed", "reasoning": "evidence valid"}`)
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher())
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, verdictConfirmed, v.Verdict)
	assert.Equal(t, "evidence valid", v.Notes)
	assert.Equal(t, "skeptic-1", v.Skeptic)
}

func TestInvokeSkeptic_Refutes(t *testing.T) {
	t.Parallel()
	cc := finalChat(`{"verdict": "refuted", "reasoning": "false positive"}`)
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher())
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
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, disp)
	require.NoError(t, err)
	assert.Equal(t, verdictConfirmed, v.Verdict)
	assert.Equal(t, "verified via file read", v.Notes)
	assert.GreaterOrEqual(t, disp.count(), 1, "tool loop should have dispatched at least once")
}

func TestInvokeSkeptic_ProviderError(t *testing.T) {
	t.Parallel()
	cc := &fakeChatCompleter{turns: []chatTurn{{err: errors.New("rate limit exceeded")}}}
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher())
	require.NoError(t, err) // runtime failure NOT propagated
	require.NotNil(t, v)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Equal(t, "skeptic-1", v.Skeptic)
	assert.Contains(t, v.Notes, "rate limit exceeded")
}

// TestInvokeSkeptic_UsesLogger verifies invokeSkeptic routes a skeptic failure
// through the injected context logger (not os.Stderr): the failure summary is
// emitted at Warn with the skeptic name and class, observable in the buffer.
func TestInvokeSkeptic_UsesLogger(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	ctx := log.NewContext(context.Background(), logger)

	cc := &fakeChatCompleter{turns: []chatTurn{{err: errors.New("rate limit exceeded")}}}
	v, _, err := invokeSkeptic(ctx, testSkeptic(), "prompt", cc, okDispatcher())
	require.NoError(t, err)
	require.Equal(t, verdictUnverifiable, v.Verdict)

	out := buf.String()
	assert.Contains(t, out, "skeptic failed",
		"a skeptic failure must be logged through the injected context logger")
	assert.Contains(t, out, "skeptic=skeptic-1")
	assert.Contains(t, out, "class=provider_error")
}

func TestInvokeSkeptic_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the first turn
	cc := &fakeChatCompleter{turns: []chatTurn{{content: `{"verdict":"confirmed"}`}}}
	v, _, err := invokeSkeptic(ctx, testSkeptic(), "prompt", cc, okDispatcher())
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Equal(t, "skeptic-1", v.Skeptic)
}

func TestInvokeSkeptic_MalformedOutput(t *testing.T) {
	t.Parallel()
	cc := finalChat("I don't know")
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher())
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "malformed_output")
	assert.Equal(t, "skeptic-1", v.Skeptic)
}

func TestInvokeSkeptic_EmptyResponse(t *testing.T) {
	t.Parallel()
	cc := finalChat("")
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher())
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "empty_response")
}

func TestInvokeSkeptic_BudgetTripMaxTurns(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.MaxTurns = intPtr(2)
	cc := &fakeChatCompleter{turns: []chatTurn{toolCallTurn("read_file"), toolCallTurn("read_file")}}
	v, _, err := invokeSkeptic(context.Background(), sk, "prompt", cc, okDispatcher())
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
	v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", cc, disp)
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "tool_budget_bytes")
	assert.Contains(t, tripped, "tool_budget_bytes", "tripped budgets must be surfaced separately from Notes")
}

func TestInvokeSkeptic_BudgetTripTimeout(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.TimeoutSecs = intPtr(1)
	cc := &fakeChatCompleter{turns: []chatTurn{toolCallTurn("read_file"), {delay: 2 * time.Second, content: `{"verdict":"confirmed"}`}}}
	v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", cc, okDispatcher())
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, tripped, "timeout_secs", "timeout budget trip must be surfaced structurally")
}

// TestInvokeSkeptic_SurfacesTrippedBudgets locks AC1: a budget trip is returned
// as a separate []string, not only folded into the free-text Notes, so the caller
// can populate VerificationResult.TrippedBudgets structurally.
func TestInvokeSkeptic_SurfacesTrippedBudgets(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.MaxTurns = intPtr(2)
	cc := &fakeChatCompleter{turns: []chatTurn{toolCallTurn("read_file"), toolCallTurn("read_file")}}
	v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", cc, okDispatcher())
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, tripped, "max_turns", "tripped budgets must be surfaced separately from Notes")
}

// TestInvokeSkeptic_NoTrippedBudgetsOnCleanVerdict: a verdict reached without a
// trip returns an empty tripped-budgets slice (the field never carries noise).
func TestInvokeSkeptic_NoTrippedBudgetsOnCleanVerdict(t *testing.T) {
	t.Parallel()
	v, tripped, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt",
		finalChat(`{"verdict":"confirmed","reasoning":"ok"}`), okDispatcher())
	require.NoError(t, err)
	assert.Equal(t, verdictConfirmed, v.Verdict)
	assert.Empty(t, tripped, "a clean verdict trips no budgets")
}

func TestInvokeSkeptic_NilContext(t *testing.T) {
	t.Parallel()
	_, _, err := invokeSkeptic(nil, testSkeptic(), "prompt", finalChat("{}"), okDispatcher()) //nolint:staticcheck // intentional nil-ctx guard test
	require.Error(t, err)
}

func TestInvokeSkeptic_NilChatCompleter(t *testing.T) {
	t.Parallel()
	_, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", nil, okDispatcher())
	require.Error(t, err)
}

func TestInvokeSkeptic_NilDispatcher(t *testing.T) {
	t.Parallel()
	_, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", finalChat("{}"), nil)
	require.Error(t, err)
}
