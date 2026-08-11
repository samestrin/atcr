package payload

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.5.1 (T2/AC1/AC2): the resolver accepts a caller-supplied declared
// window and resolves it AHEAD of the static table:
//
//	agent declaration -> static table -> defaultContextWindowTokens
//
// The declaration exists because a litellm-backed roster names models by bare
// proxy alias, which no provider-qualified table key matches, so the whole
// roster silently resolves the 32,768-token default.

func declared(n int) *int { return &n }

func TestContextWindowTokens_DeclarationBeatsStaticTable(t *testing.T) {
	// A model that IS in the static table still yields to an explicit
	// declaration: the operator knows how their proxy serves this deployment,
	// and the table's entry is only a conservative floor for the OpenRouter-style
	// id. This is the tier order AC2 fixes, not merely a fallback.
	assert.Equal(t, 128000, ContextWindowTokens("z-ai/glm-5.2", nil),
		"precondition: the static table resolves this id to 128000")
	assert.Equal(t, 262144, ContextWindowTokens("z-ai/glm-5.2", declared(262144)),
		"an explicit declaration must win over the static table")
}

func TestContextWindowTokens_DeclarationBeatsDefaultForProxyAlias(t *testing.T) {
	// The motivating case: `glm-5.2` is the proxy-local alias, absent from the
	// table by design (AC3), so it resolves the conservative default today.
	for _, alias := range []string{"glm-5.2", "kimi-k3", "qwen3.8-max", "deepseek-v4-pro"} {
		assert.Equal(t, defaultContextWindowTokens, ContextWindowTokens(alias, nil),
			"precondition: proxy-local alias %q must stay absent from the static table", alias)
		assert.Equal(t, 128000, ContextWindowTokens(alias, declared(128000)),
			"a declaration must lift proxy-local alias %q off the 32768 default", alias)
	}
}

func TestContextWindowTokens_StaticTablePrecedesDefault(t *testing.T) {
	// Pin the static table's MEMBERSHIP as literals so adding a stray bare proxy
	// alias (an AC3 violation) fails immediately rather than silently raising
	// every undeclared window that matches it. The existing
	// NilDeclarationIsByteIdenticalToPreEpic iterates the production map, which
	// moves with the map and pins nothing.
	literals := map[string]int{
		"anthropic/claude-opus-4.8": 200000,
		"anthropic/claude-sonnet-5": 200000,
		"google/gemini-2.5-pro":     1000000,
		"google/gemini-2.5-flash":   1000000,
		"openai/gpt-5.5":            128000,
		"openai/gpt-5.4-mini":       128000,
		"deepseek/deepseek-v4-pro":  128000,
		"moonshotai/kimi-k2.7-code": 128000,
		"qwen/qwen3-coder-plus":     128000,
		"z-ai/glm-5.2":              128000,
	}
	assert.Len(t, contextWindowTokens, len(literals),
		"static table has gained or lost entries — update the literal golden if intentional")
	for model, want := range literals {
		assert.Contains(t, model, "/",
			"every static-table key must be provider-qualified (AC3: no bare proxy alias)")
		assert.Equal(t, want, ContextWindowTokens(model, nil),
			"nil declaration must resolve the static table entry for %q, not the default", model)
	}
	assert.Equal(t, defaultContextWindowTokens, ContextWindowTokens("no-such-model", nil),
		"an unknown model with no declaration must fall through to the conservative default")
}

func TestContextWindowTokens_NonPositiveDeclarationIgnored(t *testing.T) {
	// Defense-in-depth for a directly-constructed AgentConfig that bypasses
	// validateAgent (which already rejects <= 0 at load), mirroring how
	// EffectiveMaxContextLines clamps a non-positive value to the default rather
	// than propagating it. A declared 0 must NOT resolve as a 0-token window:
	// that drives EffectiveByteBudget to 0, which the bulk path reads as the
	// "window too small to reserve output headroom" overflow degradation.
	assert.Equal(t, 128000, ContextWindowTokens("z-ai/glm-5.2", declared(0)),
		"a non-positive declaration must fall through to the static table")
	assert.Equal(t, defaultContextWindowTokens, ContextWindowTokens("glm-5.2", declared(-1)),
		"a negative declaration must fall through to the default")
}

func TestContextWindowTokens_DeclarationTrimsNothingFromModel(t *testing.T) {
	// The declaration short-circuits before the table lookup, so a whitespace-y
	// model id is irrelevant when a declaration is present.
	assert.Equal(t, 500000, ContextWindowTokens("  kimi-k3  ", declared(500000)))
}

func TestEffectiveByteBudget_HonorsDeclaration(t *testing.T) {
	// AC1: the declared window must flow into the payload byte budget, not just
	// into the diagnosability record.
	//
	//	(128000 - 8192 output - 4096 overhead) * 7/2 = 115712 * 3.5 = 404992
	got := EffectiveByteBudget("glm-5.2", declared(128000), testOutputTokens)
	assert.Equal(t, int64(404992), got)

	base := EffectiveByteBudget("glm-5.2", nil, testOutputTokens)
	assert.Equal(t, int64(71680), base, "undeclared stays at the 32768-default budget")
	assert.Greater(t, got, base, "a declaration must raise the byte budget")
}

func TestChunkMaxLines_HonorsDeclaration(t *testing.T) {
	// AC1's second, SEPARATE assertion: the chunked per-chunk line budget must
	// rise too. The status.json record alone can carry a declared window while
	// every budget still sizes against 32768 — that is the failure mode this
	// epic exists to prevent, so the line budget is asserted on its own.
	//
	//	404992 / 48 = 8437 lines (vs 71680 / 48 = 1493 today)
	base := ChunkMaxLines("glm-5.2", nil, testOutputTokens)
	require.Equal(t, 1493, base, "the pre-epic derived value at the 32768 default window")

	got := ChunkMaxLines("glm-5.2", declared(128000), testOutputTokens)
	assert.Equal(t, 8437, got)

	// Monotonic sweep: a larger declared window must yield a larger per-chunk
	// line budget. With both endpoints pinned by Equal above, a simple Greater
	// would be arithmetic over two constants (zero detection power); a sweep
	// catches a ChunkMaxLines that ignores the declaration for some subrange.
	for _, tc := range []struct {
		small, large int
	}{
		{32768, 65536},
		{65536, 128000},
		{128000, 200000},
	} {
		assert.Greater(t,
			ChunkMaxLines("glm-5.2", declared(tc.large), testOutputTokens),
			ChunkMaxLines("glm-5.2", declared(tc.small), testOutputTokens),
			"declared %d must yield a larger chunk line budget than declared %d", tc.large, tc.small)
	}
}

func TestChunkMaxLines_DeclarationStillClampsToFloor(t *testing.T) {
	// A declaration small enough that the output reservation eats the whole
	// window still clamps to the positive floor, never to the <= 0 value
	// chunkDiff reads as "disable chunking".
	assert.Equal(t, minChunkLines, ChunkMaxLines("glm-5.2", declared(1), testOutputTokens))
}

func TestEffectiveByteBudget_DeclarationBelowOverheadYieldsZero(t *testing.T) {
	// registry validation permits any value in 1..ContextWindowTokensCap, so a
	// declaration at or below (output + overhead) is now REACHABLE where the
	// 32768 floor previously made it impossible. It must return the documented
	// "no budget available" 0 rather than a negative byte count.
	assert.Equal(t, int64(0), EffectiveByteBudget("glm-5.2", declared(12288), testOutputTokens))
	assert.Equal(t, int64(0), EffectiveByteBudget("glm-5.2", declared(1), testOutputTokens))
	// One token above the (output + overhead) floor must yield a positive
	// budget, not 0. Without this, a mutation widening the zero band to
	// `effectiveTokens <= promptOverheadTokens` — silently zeroing every
	// window under about 16k — survives the suite because every existing
	// case is either far above the band or already returns 0.
	assert.Equal(t, int64(3), EffectiveByteBudget("glm-5.2", declared(12289), testOutputTokens))
}
