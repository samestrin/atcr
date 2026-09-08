package verify

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/samestrin/atcr/internal/tools"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSkepticToolBudget_NegativeBudgetOnANonDeclarationWindowTakesTheFloor pins
// the guard that keeps an unbounded read from reaching the engine.
//
// The engine reads 0 as UNLIMITED (internal/fanout/loop.go guards on `> 0`), and
// the normalisation at the top of skepticToolBudget rewrites a negative declared
// budget to 0. On the non-declaration-window path that normalised 0 would be
// RETURNED — i.e. an AgentConfig carrying a negative tool_budget_bytes would ask
// for less than nothing and be granted no ceiling at all. The floor is what
// closes that.
//
// Mutation-verified as unpinned before this test existed: replacing
// `if incoming < 0` with `if false` left the whole suite green, and the coverage
// profile showed the branch never executing. Load-time validation rejects a
// negative budget (internal/registry/config.go), so only a programmatically
// built config reaches here — which is exactly why no other test produced one.
func TestSkepticToolBudget_NegativeBudgetOnANonDeclarationWindowTakesTheFloor(t *testing.T) {
	t.Parallel()

	for _, window := range []struct {
		name string
		val  int
	}{
		{"zero", 0},
		{"negative", -1},
		{"above the cap", 10000001},
	} {
		window := window
		for _, budget := range []int64{-1, -4096} {
			budget := budget
			t.Run(window.name, func(t *testing.T) {
				t.Parallel()
				sk := testSkeptic()
				sk.Config.ContextWindowTokens = &window.val
				sk.Config.ToolBudgetBytes = &budget

				got, derived := skepticToolBudget(sk.Config)
				assert.EqualValues(t, 1, got,
					"a negative budget must land on the 1-byte floor, never on the 0 the engine reads as UNLIMITED")
				assert.False(t, derived,
					"the floor is not a window-derived ceiling — a trip on it must void the verdict")
			})
		}
	}
}

// TestSkepticToolBudget_ZeroBudgetOnANonDeclarationWindowIsForwardedUntouched is
// the boundary the guard above must NOT swallow. A window the resolution chain
// rejects behaves like no declaration, so an absent (0) budget forwards as it
// always did — the engine's own default. Without this case the guard could be
// widened to `incoming <= 0` and nothing would notice.
func TestSkepticToolBudget_ZeroBudgetOnANonDeclarationWindowIsForwardedUntouched(t *testing.T) {
	t.Parallel()

	sk := testSkeptic()
	window := 0
	zero := int64(0)
	sk.Config.ContextWindowTokens = &window
	sk.Config.ToolBudgetBytes = &zero

	got, derived := skepticToolBudget(sk.Config)
	assert.EqualValues(t, 0, got,
		"an explicit 0 is the engine's default reading, not an operator asking for less than nothing")
	assert.False(t, derived)
}

// TestClampDispatcher_ZeroBudgetIsDeliberatelyUnwrapped states, in a test, what
// the dominant roster shape actually gets.
//
// `budget <= 0` returns the dispatcher UNWRAPPED — first-turn delivery is
// unbounded. That is the shape of every agent declaring neither
// context_window_tokens nor tool_budget_bytes (skepticToolBudget returns
// declared == 0), i.e. the agents most likely to be misconfigured, and nothing
// pinned it: narrowing the guard to `budget < 0` left the suite green.
//
// It is intended. 0 is the engine's UNLIMITED sentinel, and wrapping it would
// mean inventing a ceiling for an operator who declared none — the clamp exists
// to enforce a window's arithmetic, not to impose one where there is no window.
// The bound for that case is internal/fanout/loop.go's own budget check, which
// likewise no-ops at 0. Recording the intent here is the point: an unbounded
// path that no test mentions is indistinguishable from an oversight.
func TestClampDispatcher_ZeroBudgetIsDeliberatelyUnwrapped(t *testing.T) {
	t.Parallel()

	inner := &fakeDispatcher{result: tools.ToolResult{Content: "0123456789"}}

	for _, budget := range []int64{0, -1} {
		got := clampDispatcher(inner, budget)
		assert.Same(t, inner, got,
			"budget %d is the UNLIMITED sentinel (or below it): the dispatcher is returned as-is, deliberately unwrapped", budget)
	}

	wrapped := clampDispatcher(inner, 1)
	require.NotSame(t, inner, wrapped, "a real budget wraps")

	out, err := wrapped.Execute(context.Background(), "read", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, 2, len(out.Content),
		"budget 1 allows budget+1 bytes — the smallest overrun that still lets loop.go's strictly-greater trip fire")
	assert.True(t, out.Truncated)
}
