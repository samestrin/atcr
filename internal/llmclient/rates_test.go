package llmclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestComputeCostUSD_SingleDivisionIsExact pins the cost formula to a single
// trailing division. Dividing each token count by 1e6 before multiplying rounds
// each term first, so the sum accumulates IEEE-754 error in the least-significant
// digit (e.g. 1.49999999999999986859e-05 instead of 1.5e-05). Folding the lone
// division to the end — (tokensIn*rate.input + tokensOut*rate.output)/1e6 — keeps
// the result the correctly-rounded value, which keeps the per-reviewer summation
// on the scorecard deterministic. Each case below is an input where the
// two-division form diverges from the exact single-division reference.
func TestComputeCostUSD_SingleDivisionIsExact(t *testing.T) {
	// claude-sonnet-4-6 is priced $3/M in, $15/M out.
	const (
		rateIn  = 3.0
		rateOut = 15.0
	)
	cases := []struct{ tokensIn, tokensOut int }{
		{0, 1},
		{0, 2},
		{0, 5},
		{0, 10},
		{1, 0},
		{5, 0},
		{10, 5},
		{100, 200},
	}
	for _, c := range cases {
		want := (float64(c.tokensIn)*rateIn + float64(c.tokensOut)*rateOut) / 1_000_000.0
		got := ComputeCostUSD("claude-sonnet-4-6", c.tokensIn, c.tokensOut)
		assert.Equal(t, want, got, "tokensIn=%d tokensOut=%d must equal single-division reference", c.tokensIn, c.tokensOut)
	}
}
