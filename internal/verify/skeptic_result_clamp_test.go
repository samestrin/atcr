package verify

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/tools"
)

// TestInvokeSkeptic_DerivedCeilingBoundsTheFirstTurnDelivery closes the gap
// between what skepticToolBudget's doc comment claims and what the engine
// enforces.
//
// internal/fanout/loop.go's byte-budget check is a DEFERRED end-of-turn trip:
// the current turn's tool results are delivered in full and the ceiling is
// consulted only after they are already in the message list, which
// requestFinalAnswer then re-sends to the provider. internal/tools/limits.go
// caps a single result at 64 KiB independently of any per-agent budget, so for
// a window of 12288 tokens — derived ceiling 14336 bytes, ~4096 tokens — ONE
// read_file result may legally deliver ~18700 tokens into a 12288-token window
// before anything trips. The clamp only ever stopped the SECOND turn.
//
// The assertion is on bytes the COMPLETER received, not on the tripped-budget
// slice: a trip that fires after the overflow has already been sent is exactly
// the state under test, so a test that only checked for the trip would pass
// against the defect.
func TestInvokeSkeptic_DerivedCeilingBoundsTheFirstTurnDelivery(t *testing.T) {
	t.Parallel()

	// Inside the band the half-room reservation cap governs, so the ceiling is
	// derived from the capped reservation rather than the flat default.
	window := 12288
	sk := testSkeptic()
	sk.Config.ContextWindowTokens = &window

	reserved := min(payload.DefaultOutputTokens, payload.InputRoomTokens(sk.Config.Model, &window)/2)
	ceiling := payload.EffectiveByteBudget(sk.Config.Model, &window, reserved)
	require.Equal(t, int64(14336), ceiling,
		"fixture drift: the whole test is about a single result that dwarfs THIS number")

	// One legal maximum-size result: what tools.DefaultMaxResultBytes permits a
	// single read_file to return, sized independently of the ceiling so the test
	// cannot pass by construction.
	oversized := strings.Repeat("x", tools.DefaultMaxResultBytes)
	require.Greater(t, len(oversized), int(ceiling),
		"the single result must overrun the derived ceiling, or nothing is exercised")

	disp := &fakeDispatcher{result: tools.ToolResult{
		Content:       oversized,
		OriginalBytes: len(oversized),
	}}
	cc := &fakeChatCompleter{turns: []chatTurn{
		toolCallTurn("read_file"),
		{content: `{"verdict": "refuted", "reasoning": "the cited line does not do what the finding claims"}`},
	}}

	v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", cc, disp, false)
	require.NoError(t, err)
	require.NotNil(t, v)

	// The ceiling is DERIVED, so the trip truncates rather than voids — the
	// verdict must survive, exactly as TestInvokeSkeptic_DerivedToolBudgetTripDoesNotVoidTheVerdict
	// pins. This test adds the half that one cannot see.
	assert.Equal(t, verdictRefuted, v.Verdict,
		"bounding the delivery must not change whose verdict wins")
	assert.Contains(t, tripped, "tool_budget_bytes",
		"the trip is still reported for audit — the read WAS shortened")

	// The contract: at most the ceiling plus the one byte the strictly-greater
	// trip comparison in loop.go needs in order to fire at all. Delivering
	// exactly the ceiling and no more would leave ToolBytes == ceiling, which is
	// not > ceiling, so the loop would never trip and the skeptic would spend
	// every remaining turn reading nothing.
	assert.LessOrEqual(t, cc.toolBytesDelivered(), int(ceiling)+1,
		"a single tool result must not walk a small window past the ceiling this lane derived for it")
}
