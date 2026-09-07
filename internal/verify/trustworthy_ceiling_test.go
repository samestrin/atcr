package verify

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/tools"
)

// TestSkepticToolBudget_UntrustworthyCeilingsTakeTheFloor pins the two halves of
// one coordinated decision (clarification Q1 + Q2, 2026-09-07).
//
// Q1 — the trust seam. A derived ceiling was classified `derived = true` at any
// positive value, so window 4097 derived THREE BYTES and still handed
// tripsVoidTheVerdict a ceiling whose trip does not void the verdict: a
// confirmed/refuted formed from a sub-kilobyte view reached reconcile's CI gate.
// The floor's `derived = false` classification now extends to any ceiling below
// minTrustworthyCeilingBytes — one real tool result — because below that the
// skeptic cannot have read enough to be trusted, whatever the number is.
//
// Q2 — the declared-budget escape hatch in the same branch. A window with no
// usable room returned the operator's FULL declaration (1<<20 → 1048576 bytes,
// `derived = false`) while the window one token larger returned 3 bytes with
// `derived = true`: one token of window swung the enforced ceiling ~350000x AND
// flipped the trip's meaning. Honouring the declaration there bought nothing —
// the window cannot hold the result either way — so the branch now floors
// regardless of what was declared, and it does so across Q1's whole widened
// gate rather than only the old `ceiling <= 0` slice.
//
// Scope check recorded with the decision: every agent in the live registry
// declares a window of 98304 tokens or more, deriving 301056 bytes — far above
// the 65536-byte threshold — so no shipped agent's classification changes.
func TestSkepticToolBudget_UntrustworthyCeilingsTakeTheFloor(t *testing.T) {
	t.Parallel()

	require.Equal(t, int64(tools.DefaultMaxResultBytes), minTrustworthyCeilingBytes,
		"the threshold is one real tool result, sourced from the dispatcher's own cap rather than restated")

	windowPtr := func(w int) *int { return &w }

	t.Run("Q1: a derived ceiling below one tool result is not trustworthy", func(t *testing.T) {
		t.Parallel()
		// 31012 derives 65534 bytes — two bytes short of one tool result — and
		// 31013 derives 65537. The pair brackets the threshold exactly, so the
		// test cannot pass against a differently-placed boundary.
		for _, w := range []int{4097, 8192, 12288, 16384, 20480, 24576, 31012} {
			sk := testSkeptic()
			sk.Config.ContextWindowTokens = windowPtr(w)
			budget, derived := skepticToolBudget(sk.Config)
			assert.Equal(t, minSkepticToolBudget, budget,
				"window %d cannot fund one tool result, so it must take the floor", w)
			assert.False(t, derived,
				"window %d: a trip on a ceiling this small must VOID the verdict, not truncate it", w)
		}
	})

	t.Run("Q1: the first window that can fund a real read still derives normally", func(t *testing.T) {
		t.Parallel()
		for _, w := range []int{31013, 32768, 40960, 98304} {
			sk := testSkeptic()
			sk.Config.ContextWindowTokens = windowPtr(w)
			reserved := min(payload.DefaultOutputTokens, payload.InputRoomTokens(sk.Config.Model, windowPtr(w))/2)
			want := payload.EffectiveByteBudget(sk.Config.Model, windowPtr(w), reserved)
			budget, derived := skepticToolBudget(sk.Config)
			assert.Equal(t, want, budget, "window %d must keep deriving its real ceiling", w)
			assert.True(t, derived,
				"window %d funds a real read, so a trip truncates rather than overrules the skeptic", w)
			assert.GreaterOrEqual(t, budget, minTrustworthyCeilingBytes,
				"precondition: window %d is above the threshold", w)
		}
	})

	t.Run("Q2: a declared budget no longer buys an untrustworthy window an unbounded read", func(t *testing.T) {
		t.Parallel()
		// The exact shape that swung ~350000x across one token of window.
		declared := int64(1 << 20)
		for _, w := range []int{4096, 4097, 12288, 24576, 31012} {
			sk := testSkeptic()
			sk.Config.ContextWindowTokens = windowPtr(w)
			sk.Config.ToolBudgetBytes = &declared
			budget, derived := skepticToolBudget(sk.Config)
			assert.Equal(t, minSkepticToolBudget, budget,
				"window %d: a declaration cannot make a window hold what it cannot hold", w)
			assert.False(t, derived, "window %d: the floor is never a derived ceiling", w)
		}
	})

	t.Run("Q2: the 4096/4097 edge no longer changes magnitude or the derived flag", func(t *testing.T) {
		t.Parallel()
		declared := int64(1 << 20)
		for _, withDeclaration := range []bool{false, true} {
			var prevBudget int64
			var prevDerived bool
			for i, w := range []int{4096, 4097} {
				sk := testSkeptic()
				sk.Config.ContextWindowTokens = windowPtr(w)
				if withDeclaration {
					sk.Config.ToolBudgetBytes = &declared
				}
				budget, derived := skepticToolBudget(sk.Config)
				if i == 1 {
					assert.Equal(t, prevBudget, budget,
						"declared=%v: one token of window must not change the enforced ceiling", withDeclaration)
					assert.Equal(t, prevDerived, derived,
						"declared=%v: one token of window must not flip the trip's meaning", withDeclaration)
				}
				prevBudget, prevDerived = budget, derived
			}
		}
	})
}

// TestMinTrustworthyCeilingMirrorsTheDispatcherCap is the anti-drift pin for a
// constant that is deliberately written twice.
//
// payload.MinUsableReadBytes must equal tools.DefaultMaxResultBytes — "one real
// tool result" is the whole justification for the threshold, so a change to the
// dispatcher's per-result cap that left the sizing constant behind would leave
// the skeptic lane refusing windows that can now fund a read, or accepting ones
// that cannot. payload cannot import tools (internal/boundaries_test.go pins the
// direction: tools sits above payload, and inverting it for one constant would
// drag the dispatcher and its sandbox backend underneath the sizing layer), so
// the literal is restated there and reconciled here — internal/verify is the
// lane that imports BOTH, which is what makes this the right home for the pin.
func TestMinTrustworthyCeilingMirrorsTheDispatcherCap(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(tools.DefaultMaxResultBytes), payload.MinUsableReadBytes,
		"payload.MinUsableReadBytes restates tools.DefaultMaxResultBytes across a layering boundary — the two must not drift")
	assert.Equal(t, payload.MinUsableReadBytes, minTrustworthyCeilingBytes,
		"the skeptic lane's gate must be the shared constant, not a second copy of the number")
}

// TestWindowFundsAUsableRead_AgreesWithTheLane pins the OTHER half of the same
// contract: `atcr doctor` warns about a starving window by asking
// payload.WindowFundsAUsableRead, while the run-time collapse is decided by
// skepticToolBudget. If those two ever disagree, doctor warns about windows that
// work or stays silent about windows that do not — and the mitigation TD row
// internal/verify/invoke.go:366 was closed on stops being true.
func TestWindowFundsAUsableRead_AgreesWithTheLane(t *testing.T) {
	t.Parallel()

	model := testSkeptic().Config.Model
	for window := 1; window <= 40960; window++ {
		w := window
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &w
		budget, derived := skepticToolBudget(sk.Config)
		floored := !derived && budget == minSkepticToolBudget

		require.Equalf(t, floored, !payload.WindowFundsAUsableRead(model, &w, nil),
			"window %d: doctor's predicate and the lane's own gate must reach the same verdict", window)
	}
}
