package payload

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// testOutputTokens is the output reservation the production caller passes into
// EffectiveByteBudget — the package's own DefaultOutputTokens, which fanout's
// defaultMaxTokens references, so the mirror cannot drift.
const testOutputTokens = DefaultOutputTokens

// unknownModel resolves to defaultContextWindowTokens (32768) — the confirmed
// dax window and the conservative floor for any model absent from the table.
const unknownModel = "no-such-model/never-listed"

// TestEffectiveByteBudget_32kReservesOutput asserts a 32k-window model's input
// budget stays strictly below `window - output` in tokens (AC1) — the output cap
// is genuinely reserved, not spent on input.
func TestEffectiveByteBudget_32kReservesOutput(t *testing.T) {
	got := EffectiveByteBudget(unknownModel, nil, testOutputTokens) // window 32768
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
	small := EffectiveByteBudget(unknownModel, nil, testOutputTokens)     // 32768
	large := EffectiveByteBudget("openai/gpt-5.5", nil, testOutputTokens) // 128000
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
	budget := EffectiveByteBudget(unknownModel, nil, testOutputTokens) // window 32768
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
	assert.NotPanics(t, func() { EffectiveByteBudget(unknownModel, nil, testOutputTokens) })
	assert.Equal(t,
		EffectiveByteBudget(unknownModel, nil, testOutputTokens),
		EffectiveByteBudget("another-unlisted/model", nil, testOutputTokens),
		"all unknown models share the conservative default window")
}

// TestEffectiveByteBudget_DegenerateWindowReturnsZero asserts a window smaller
// than the output+overhead reservation returns 0 (no budget), never a negative
// or wrapped byte count. Reserving the full 32768 window as output leaves
// nothing for input.
func TestEffectiveByteBudget_DegenerateWindowReturnsZero(t *testing.T) {
	got := EffectiveByteBudget(unknownModel, nil, 32768) // output >= window
	assert.Equal(t, int64(0), got, "a degenerate window must return 0, not a negative budget")
}

// TestEffectiveByteBudget_NegativeOutputTokens asserts that a negative
// outputTokens value (a plumbing bug) is clamped to zero rather than inflating
// the effective token budget beyond the window. Without this guard the formula
// `window - outputTokens - overhead` grows as outputTokens becomes more
// negative, returning an oversized byte budget that would over-fill the window.
func TestEffectiveByteBudget_NegativeOutputTokens(t *testing.T) {
	got := EffectiveByteBudget(unknownModel, nil, -1000)
	want := EffectiveByteBudget(unknownModel, nil, 0)
	assert.LessOrEqual(t, got, want,
		"negative outputTokens must not inflate the budget past the zero-output case")
}

// TestChunkMaxLines_SmallerForSmallWindow asserts a 32k model produces a smaller
// maxLines than a 128k model, so chunkDiff opens MORE chunks for the small window
// (AC3), from the same effective-budget source.
func TestChunkMaxLines_SmallerForSmallWindow(t *testing.T) {
	small := ChunkMaxLines(unknownModel, nil, testOutputTokens)     // 32768
	large := ChunkMaxLines("openai/gpt-5.5", nil, testOutputTokens) // 128000
	assert.Less(t, small, large, "a 32k window must yield fewer lines-per-chunk than a 128k window")
	assert.GreaterOrEqual(t, small, minChunkLines, "must never drop below the positive floor")
}

// TestChunkMaxLines_ClampedFloor asserts a pathologically small resolved window
// (whose EffectiveByteBudget is 0) clamps to the positive minChunkLines floor
// rather than returning 0/negative, which chunkDiff would treat as "disable
// chunking".
func TestChunkMaxLines_ClampedFloor(t *testing.T) {
	got := ChunkMaxLines(unknownModel, nil, 32768) // degenerate: EffectiveByteBudget == 0
	assert.Equal(t, minChunkLines, got, "a degenerate window must clamp to the positive floor")
}

// internal/fanout's chunked-diff path guards its scope-constraint reservation
// with `if chunkClampBudget > 0`, and that guard is UNOBSERVABLE today — swapping
// it for `if true` changes no output. It is not dead defensiveness by accident:
// it is unobservable because of two couplings in THIS file, and a mutation test
// over there can never pin it. Pin the couplings here instead, so a change that
// re-arms the guard fails as a payload-sizing change (where the cause is) rather
// than surfacing as a silent chunk-size regression in fanout.
//
// (1) ChunkMaxLines and EffectiveByteBudget share a zero point: ChunkMaxLines
//
//	divides that same budget, so a zero budget can only floor. The fanout guard
//	therefore only ever runs with maxLines already AT the floor.
//
// (2) ClampLinesToByteBudget returns that same floor for the byteBudget 1 the
//
//	guard's absence would produce, so the clamped and unclamped answers coincide
//	at the floor.
//
// Break either and the guard becomes load-bearing: it is then the difference
// between "no clamp" and "clamp to the floor", and fanout needs its own mutation
// -killing test at that point.
func TestZeroEffectiveBudgetFloorsChunkLinesAndSurvivesAOneByteClamp(t *testing.T) {
	// 12288 - 8192 output - 4096 fixed overhead = 0 effective tokens.
	declared := 12288
	const outputTokens = 8192

	budget := EffectiveByteBudget("unlisted-small-model", &declared, outputTokens)
	if budget != 0 {
		t.Fatalf("precondition: want a zero effective byte budget, got %d", budget)
	}

	lines := ChunkMaxLines("unlisted-small-model", &declared, outputTokens)
	if lines != minChunkLines {
		t.Fatalf("a zero effective byte budget must floor the chunk line budget at minChunkLines (%d), got %d — fanout's `chunkClampBudget > 0` guard is now load-bearing and needs its own mutation-killing test", minChunkLines, lines)
	}

	if got := ClampLinesToByteBudget(lines, 1); got != minChunkLines {
		t.Fatalf("a 1-byte budget must still return minChunkLines (%d), got %d — fanout's `chunkClampBudget > 0` guard is now load-bearing and needs its own mutation-killing test", minChunkLines, got)
	}
	if got := ClampLinesToByteBudget(lines, 0); got != lines {
		t.Fatalf("a non-positive budget must pass maxLines through unclamped, got %d want %d", got, lines)
	}
}

// docs/registry.md promises a --json consumer can re-derive the budget the
// zero-budget verdict warned about, and that derivation needs promptOverheadTokens
// as its third operand. The constant is unexported and appeared in no public doc,
// so the promise was unkeepable. Now that the doc states the number, pin it here:
// changing the reservation without updating the doc silently breaks every consumer
// that followed the published formula.
func TestPromptOverheadTokens_IsPublishedInRegistryDocs(t *testing.T) {
	b, err := os.ReadFile("../../docs/registry.md")
	if err != nil {
		t.Fatalf("read docs/registry.md: %v", err)
	}
	want := "context_window_tokens − review_max_tokens − " + strconv.Itoa(promptOverheadTokens)
	if !strings.Contains(string(b), want) {
		t.Fatalf("docs/registry.md must publish the budget formula with the current prompt overhead (%d); wanted the substring %q", promptOverheadTokens, want)
	}
}
