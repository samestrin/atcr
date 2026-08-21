package doctor

import (
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A window of 0 is not a tiny window — it is an UNRESOLVED one, the state render.go
// prints as "-" rather than as a number. The verdict must stay silent for it.
//
// Left unguarded, ResolveContextWindow ignores a non-positive declaration and falls
// back to the 32768 default, so the budget computes to 0 for any cap at or above
// 28672 and the row is reported as having an input budget closed BY ITS OUTPUT CAP
// — blaming a knob that is not at fault, which is the same misattribution the
// sibling `maxTokens <= 0` guard is documented as preventing.
func TestZeroBudgetVerdict_SilentWhenTheWindowDidNotResolve(t *testing.T) {
	const cap = 32000

	// Premise, not a restatement of payload's arithmetic: without the guard this
	// input really does compute a zero budget, so the guard is what keeps the
	// verdict from firing rather than the arithmetic happening to agree.
	unresolved := 0
	require.Zero(t, payload.EffectiveByteBudget("m", &unresolved, cap),
		"precondition: an unresolved window falls back to the default and the cap closes it")

	status, hint, fired := zeroBudgetVerdict("m", 0, cap, StatusOK)

	assert.False(t, fired, "an agent whose window did not resolve has no known budget to call closed")
	assert.Empty(t, status)
	assert.Empty(t, hint)
}

// The complement: the SAME cap on a resolved window must still fire, so the guard
// above cannot be widened into a blanket suppression of the verdict.
func TestZeroBudgetVerdict_StillFiresOnAResolvedWindow(t *testing.T) {
	status, hint, fired := zeroBudgetVerdict("m", 32768, 32000, StatusOK)

	require.True(t, fired, "a resolved window whose cap closes the input budget must still be reported")
	assert.Equal(t, StatusOKWarning, status)
	assert.Contains(t, hint, zeroBudgetRemedy)
}

// The maxTokens clause standing alone. Run can no longer reach it — reviewMaxTokens
// floors at the built-in default, so the report never asks the verdict about a
// non-positive cap — which is exactly why it needs a direct case: the fixture that
// used to cover it through Run (an uncapped probe) now evaluates against review's
// default instead and asserts the opposite outcome.
func TestZeroBudgetVerdict_SilentWhenNoCapApplies(t *testing.T) {
	status, hint, fired := zeroBudgetVerdict("m", 1, 0, StatusOK)

	assert.False(t, fired, "a cap of zero is NO CAP, and no cap can have closed the budget")
	assert.Empty(t, status)
	assert.Empty(t, hint)
}

// reviewMaxTokens reproduces `atcr review`'s resolution order absent a review-side
// flag, and its floor is review's own default. A drift between the two would make
// every undeclared agent's verdict speak for a cap review does not use.
func TestReviewMaxTokens_MirrorsTheFanOutDefault(t *testing.T) {
	// The floor must BE the shared constant the fan-out caps at
	// (payload.DefaultOutputTokens, referenced by internal/fanout's
	// defaultMaxTokens and cli/doctor.go's --max-tokens flag default), not a
	// literal restatement of it: a literal on both sides cannot detect drift in
	// what it pins.
	assert.Equal(t, payload.DefaultOutputTokens, reviewDefaultMaxTokens,
		"reviewDefaultMaxTokens must equal the review fan-out's defaultMaxTokens")

	assert.Equal(t, payload.DefaultOutputTokens, reviewMaxTokens(0), "an undeclared agent is capped at review's default")
	assert.Equal(t, 32000, reviewMaxTokens(32000), "a declaring agent is capped at its own declaration")
}
