package verify

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/tools"
	reclib "github.com/samestrin/atcr/reconcile"
)

// TestInvokeSkeptic_MarksASurvivingVerdictTruncated puts the truncation fact on
// the object that actually travels to the readers.
//
// invokeSkeptic already returned the tripped-budget slice on the
// surviving-verdict path, but that slice reaches only
// reconciled/verification.json. The reclib.Verification is what rides the
// finding into findings.json and report.md, and it carried nothing — so a
// confirmed reached a human with no sign it came from a shortened read.
func TestInvokeSkeptic_MarksASurvivingVerdictTruncated(t *testing.T) {
	t.Parallel()

	window := 32768 // above minTrustworthyCeilingBytes, so a derived ceiling is enforced
	sk := testSkeptic()
	sk.Config.ContextWindowTokens = &window
	reserved := min(payload.DefaultOutputTokens, payload.InputRoomTokens(sk.Config.Model, &window)/2)
	ceiling := payload.EffectiveByteBudget(sk.Config.Model, &window, reserved)
	require.GreaterOrEqual(t, ceiling, minTrustworthyCeilingBytes, "precondition")

	t.Run("a derived trip marks the verdict truncated", func(t *testing.T) {
		t.Parallel()
		disp := &fakeDispatcher{result: tools.ToolResult{
			Content:       strings.Repeat("x", int(ceiling)+1),
			OriginalBytes: int(ceiling) + 1,
		}}
		cc := &fakeChatCompleter{turns: []chatTurn{
			toolCallTurn("read_file"),
			{content: `{"verdict": "refuted", "reasoning": "the cited line does not do what the finding claims"}`},
		}}
		v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", cc, disp, false)
		require.NoError(t, err)
		require.Equal(t, verdictRefuted, v.Verdict, "precondition: the surviving-verdict path")
		require.Contains(t, tripped, budgetToolBytes)
		assert.True(t, v.Truncated,
			"the verdict stands but the read was cut short — the object that reaches the report must say so")
	})

	t.Run("a clean run marks nothing", func(t *testing.T) {
		t.Parallel()
		cc := &fakeChatCompleter{turns: []chatTurn{
			toolCallTurn("read_file"),
			{content: `{"verdict": "confirmed", "reasoning": "read the whole file"}`},
		}}
		v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", cc, okDispatcher(), false)
		require.NoError(t, err)
		require.Empty(t, tripped, "precondition: nothing tripped")
		assert.False(t, v.Truncated, "an untruncated read must not be flagged, or the flag says nothing")
	})
}

// TestAggregateVerdicts_CarriesTruncationFromTheVotersThatCount folds the
// per-skeptic marker into the finding-level verdict the report renders, using
// the SAME rule winningAttribution applies to tripped budgets: credit the
// skeptics whose verdict the record actually reports.
func TestAggregateVerdicts_CarriesTruncationFromTheVotersThatCount(t *testing.T) {
	t.Parallel()

	t.Run("a decisive winner that was truncated marks the aggregate", func(t *testing.T) {
		t.Parallel()
		got := aggregateVerdicts([]*reclib.Verification{
			{Verdict: verdictConfirmed, Skeptic: "s1", Truncated: true},
			{Verdict: verdictConfirmed, Skeptic: "s2"},
			{Verdict: verdictRefuted, Skeptic: "s3"},
		})
		require.Equal(t, verdictConfirmed, got.Verdict)
		assert.True(t, got.Truncated, "a winner answered from a shortened read — the finding-level record owes that caveat")
	})

	t.Run("a truncated LOSER does not mark a decisive aggregate", func(t *testing.T) {
		t.Parallel()
		got := aggregateVerdicts([]*reclib.Verification{
			{Verdict: verdictConfirmed, Skeptic: "s1"},
			{Verdict: verdictConfirmed, Skeptic: "s2"},
			{Verdict: verdictRefuted, Skeptic: "s3", Truncated: true},
		})
		require.Equal(t, verdictConfirmed, got.Verdict)
		assert.False(t, got.Truncated, "the reported verdict was not the truncated one — the caveat would be false")
	})

	t.Run("a single pass-through verdict keeps its own marker", func(t *testing.T) {
		t.Parallel()
		got := aggregateVerdicts([]*reclib.Verification{
			{Verdict: verdictRefuted, Skeptic: "s1", Truncated: true},
		})
		assert.True(t, got.Truncated)
	})
}
