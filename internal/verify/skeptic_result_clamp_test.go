package verify

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/tools"
)

// twoCallTurn builds an assistant turn requesting read_file TWICE with distinct
// arguments. Distinct arguments matter: the loop treats an identical repeat
// within a turn as a nudge and does not re-dispatch it, so a single-turn
// multi-result overrun needs two genuinely different calls.
func twoCallTurn() chatTurn {
	return chatTurn{toolCalls: []llmclient.ToolCall{
		{ID: "call_1", Type: "function", Function: llmclient.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}},
		{ID: "call_2", Type: "function", Function: llmclient.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"b.go"}`)}},
	}}
}

// TestInvokeSkeptic_CeilingBoundsTheFirstTurnDelivery closes the gap between
// what skepticToolBudget's doc comment claims and what the engine enforces.
//
// internal/fanout/loop.go's byte-budget check is a DEFERRED end-of-turn trip:
// the current turn's tool results are delivered in full and the ceiling is
// consulted only after they are already in the message list, which
// requestFinalAnswer then re-sends to the provider. internal/tools/limits.go
// caps a single result at 64 KiB independently of any per-agent budget, so the
// clamp only ever stopped the SECOND turn.
//
// Two paths still reach that state, and both are covered below. (A third —
// a single 64 KiB result against a small DERIVED ceiling — is no longer
// reachable: clarification Q1 made the lane refuse any derived ceiling under
// one tool result, so a window that small never reaches the engine at all.)
//
// The assertion is on bytes the COMPLETER received, not on the tripped-budget
// slice: a trip that fires after the overflow has already been sent is exactly
// the state under test, so a test that only checked for the trip would pass
// against the defect.
func TestInvokeSkeptic_CeilingBoundsTheFirstTurnDelivery(t *testing.T) {
	t.Parallel()

	// Above minTrustworthyCeilingBytes so the lane installs a real ceiling
	// instead of flooring and short-circuiting before the engine.
	window := 32768
	sk := testSkeptic()
	sk.Config.ContextWindowTokens = &window

	reserved := min(payload.DefaultOutputTokens, payload.InputRoomTokens(sk.Config.Model, &window)/2)
	ceiling := payload.EffectiveByteBudget(sk.Config.Model, &window, reserved)
	require.Equal(t, int64(71680), ceiling,
		"fixture drift: the sizes below are chosen against THIS number")
	require.GreaterOrEqual(t, ceiling, minTrustworthyCeilingBytes,
		"precondition: a sub-threshold window never reaches the engine")

	// One legal maximum-size result: what tools.DefaultMaxResultBytes permits a
	// single read_file to return, sized independently of any ceiling so neither
	// subtest can pass by construction.
	oneMaxResult := strings.Repeat("x", tools.DefaultMaxResultBytes)

	t.Run("a derived ceiling bounds a multi-result first turn", func(t *testing.T) {
		t.Parallel()
		// Two maximum-size results in ONE turn: 131072 bytes against a
		// 71680-byte ceiling, all delivered before the deferred trip can fire.
		require.Greater(t, int64(2*len(oneMaxResult)), ceiling,
			"the turn must overrun the ceiling, or nothing is exercised")

		disp := &fakeDispatcher{result: tools.ToolResult{
			Content:       oneMaxResult,
			OriginalBytes: len(oneMaxResult),
		}}
		cc := &fakeChatCompleter{turns: []chatTurn{
			twoCallTurn(),
			{content: `{"verdict": "refuted", "reasoning": "the cited line does not do what the finding claims"}`},
		}}

		v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", cc, disp, false)
		require.NoError(t, err)
		require.NotNil(t, v)

		// The ceiling is DERIVED, so the trip truncates rather than voids — the
		// verdict must survive, exactly as
		// TestInvokeSkeptic_DerivedToolBudgetTripDoesNotVoidTheVerdict pins.
		assert.Equal(t, verdictRefuted, v.Verdict,
			"bounding the delivery must not change whose verdict wins")
		assert.Contains(t, tripped, "tool_budget_bytes",
			"the trip is still reported for audit — the read WAS shortened")
		assert.LessOrEqual(t, cc.toolBytesDelivered(), int(ceiling)+1,
			"one turn's results must not walk the window past the ceiling this lane derived for it")
		assert.Empty(t, cc.appendOnlyViolation(),
			"toolBytesDelivered dedupes by index; if the engine's message list stopped being append-only the assertion above would pass vacuously")
	})

	t.Run("a declared ceiling below one tool result bounds a single-result first turn", func(t *testing.T) {
		t.Parallel()
		// The surviving single-result path: an operator declaring less than one
		// tool result. The declaration is below the derived ceiling, so it is the
		// number enforced and a trip on it VOIDS the verdict.
		declared := int64(20000)
		require.Less(t, declared, ceiling, "precondition: the declaration must be the number enforced")
		require.Greater(t, int64(len(oneMaxResult)), declared,
			"precondition: one legal result must overrun the declaration")

		skDeclared := testSkeptic()
		skDeclared.Config.ContextWindowTokens = &window
		skDeclared.Config.ToolBudgetBytes = &declared

		disp := &fakeDispatcher{result: tools.ToolResult{
			Content:       oneMaxResult,
			OriginalBytes: len(oneMaxResult),
		}}
		cc := &fakeChatCompleter{turns: []chatTurn{
			toolCallTurn("read_file"),
			{content: `{"verdict": "refuted", "reasoning": "answered from an oversized read"}`},
		}}

		v, tripped, err := invokeSkeptic(context.Background(), skDeclared, "prompt", cc, disp, false)
		require.NoError(t, err)
		require.NotNil(t, v)
		assert.Equal(t, verdictUnverifiable, v.Verdict,
			"a declared ceiling keeps its enforcement semantics — the trip voids the verdict")
		assert.Contains(t, tripped, "tool_budget_bytes")
		assert.LessOrEqual(t, cc.toolBytesDelivered(), int(declared)+1,
			"a single tool result must not walk the window past the ceiling the operator declared")
		assert.Empty(t, cc.appendOnlyViolation(),
			"toolBytesDelivered dedupes by index; if the engine's message list stopped being append-only the assertion above would pass vacuously")
	})
}
