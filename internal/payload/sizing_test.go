package payload

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// defaultMaxTokens mirrors internal/fanout/review.go's output cap (8192). The
// payload package does not import fanout, so the value is restated here as the
// output reservation the production caller passes into EffectiveByteBudget.
const testOutputTokens = 8192

// unknownModel resolves to defaultContextWindowTokens (32768) — the confirmed
// dax window and the conservative floor for any model absent from the table.
const unknownModel = "no-such-model/never-listed"

// TestEffectiveByteBudget_32kReservesOutput asserts a 32k-window model's input
// budget stays strictly below `window - output` in tokens (AC1) — the output cap
// is genuinely reserved, not spent on input.
func TestEffectiveByteBudget_32kReservesOutput(t *testing.T) {
	got := EffectiveByteBudget(unknownModel, testOutputTokens) // window 32768
	// tokens implied by the returned byte budget, inverting the 7/2 ratio.
	inputTokens := got * conservativeBytesPerTokenDen / conservativeBytesPerTokenNum
	assert.Greater(t, got, int64(0), "a 32k window must leave a positive input budget")
	assert.Less(t, inputTokens, int64(32768-testOutputTokens),
		"input tokens must stay below window-output (%d)", 32768-testOutputTokens)
}

// TestEffectiveByteBudget_LargeWindowScalesUp asserts a large-window model gets a
// proportionally larger budget than a 32k model, still within its own reservation
// (AC1). openai/gpt-5.5 resolves to 128000 in the F1 table.
func TestEffectiveByteBudget_LargeWindowScalesUp(t *testing.T) {
	small := EffectiveByteBudget(unknownModel, testOutputTokens)     // 32768
	large := EffectiveByteBudget("openai/gpt-5.5", testOutputTokens) // 128000
	assert.Greater(t, large, small, "a 128k window must yield a larger budget than a 32k window")

	inputTokens := large * conservativeBytesPerTokenDen / conservativeBytesPerTokenNum
	assert.Less(t, inputTokens, int64(128000-testOutputTokens),
		"large-window input tokens must stay below window-output")
}

// TestEffectiveByteBudget_DaxBoundaryRegression is the mandated regression test
// naming the confirmed dax arithmetic: 24577 input + 8192 output > 32768. It
// asserts that for a 32768-token window the reserved input tokens plus the output
// cap can never exceed the window, so the exact boundary overflow cannot recur
// (AC2). A future refactor that drops the output reservation fails here.
func TestEffectiveByteBudget_DaxBoundaryRegression(t *testing.T) {
	const window = 32768
	budget := EffectiveByteBudget(unknownModel, testOutputTokens) // window 32768
	inputTokens := budget * conservativeBytesPerTokenDen / conservativeBytesPerTokenNum

	// The dax failure was 24577 + 8192 > 32768. Assert input + output <= window,
	// and specifically input < 24576 so 24577 (the exact failing value) is
	// unreachable.
	assert.LessOrEqual(t, inputTokens+int64(testOutputTokens), int64(window),
		"input+output must fit the window; the 24577+8192>32768 overflow must not recur")
	assert.Less(t, inputTokens, int64(window-testOutputTokens),
		"input tokens must be strictly below %d so 24577 is unreachable", window-testOutputTokens)
}

// TestEffectiveByteBudget_UnknownModelUsesDefault asserts an unknown model does
// not panic and sizes against the conservative default window rather than zero.
func TestEffectiveByteBudget_UnknownModelUsesDefault(t *testing.T) {
	assert.NotPanics(t, func() { EffectiveByteBudget(unknownModel, testOutputTokens) })
	assert.Equal(t,
		EffectiveByteBudget(unknownModel, testOutputTokens),
		EffectiveByteBudget("another-unlisted/model", testOutputTokens),
		"all unknown models share the conservative default window")
}

// TestEffectiveByteBudget_DegenerateWindowReturnsZero asserts a window smaller
// than the output+overhead reservation returns 0 (no budget), never a negative
// or wrapped byte count. Reserving the full 32768 window as output leaves
// nothing for input.
func TestEffectiveByteBudget_DegenerateWindowReturnsZero(t *testing.T) {
	got := EffectiveByteBudget(unknownModel, 32768) // output >= window
	assert.Equal(t, int64(0), got, "a degenerate window must return 0, not a negative budget")
}

// TestEffectiveByteBudget_NegativeOutputTokens asserts that a negative
// outputTokens value (a plumbing bug) is clamped to zero rather than inflating
// the effective token budget beyond the window. Without this guard the formula
// `window - outputTokens - overhead` grows as outputTokens becomes more
// negative, returning an oversized byte budget that would over-fill the window.
func TestEffectiveByteBudget_NegativeOutputTokens(t *testing.T) {
	got := EffectiveByteBudget(unknownModel, -1000)
	want := EffectiveByteBudget(unknownModel, 0)
	assert.LessOrEqual(t, got, want,
		"negative outputTokens must not inflate the budget past the zero-output case")
}

// TestChunkMaxLines_SmallerForSmallWindow asserts a 32k model produces a smaller
// maxLines than a 128k model, so chunkDiff opens MORE chunks for the small window
// (AC3), from the same effective-budget source.
func TestChunkMaxLines_SmallerForSmallWindow(t *testing.T) {
	small := ChunkMaxLines(unknownModel, testOutputTokens)     // 32768
	large := ChunkMaxLines("openai/gpt-5.5", testOutputTokens) // 128000
	assert.Less(t, small, large, "a 32k window must yield fewer lines-per-chunk than a 128k window")
	assert.GreaterOrEqual(t, small, minChunkLines, "must never drop below the positive floor")
}

// TestChunkMaxLines_ClampedFloor asserts a pathologically small resolved window
// (whose EffectiveByteBudget is 0) clamps to the positive minChunkLines floor
// rather than returning 0/negative, which chunkDiff would treat as "disable
// chunking".
func TestChunkMaxLines_ClampedFloor(t *testing.T) {
	got := ChunkMaxLines(unknownModel, 32768) // degenerate: EffectiveByteBudget == 0
	assert.Equal(t, minChunkLines, got, "a degenerate window must clamp to the positive floor")
}
