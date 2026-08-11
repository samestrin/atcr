package fanout

import (
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.5.1 TD: ContextWindowTokensCap permits a 10,000,000-token
// declaration, but the chunked path derived its per-chunk LINE budget from the
// window alone — ~728,000 lines, ~34.9 MB per chunk, far past any real proxy
// request-body limit. The global payload_byte_budget is the operator's stated
// ceiling on how many bytes may ride one call, so the derived line budget must
// respect it too. An explicit max_context_lines still wins (least surprise).
//
// The wiring assertions below use a 128,000-token declaration rather than the
// 10M cap: 10M derives a line budget so large the whole test diff fits in ONE
// chunk, which falls through to the bulk path where chunkMaxLines is 0 and the
// clamp is unobservable. The cap-sized case is covered directly against the
// clamp itself in TestClampLinesToByteBudget_CapSizedDeclaration.

// clampRoster is a chunked-strategy roster where greta declares a window on a
// model absent from the static table, with a global byte budget configured.
func clampRoster(t *testing.T, declared int, globalBudget int64) *ReviewConfig {
	t.Helper()
	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.PayloadByteBudget = globalBudget
	return cfg
}

// clampPayloads is a diff large enough to split under both the clamped and the
// unclamped line budget, so each case stays on the chunked path.
func clampPayloads() map[string]modePayload {
	return map[string]modePayload{"blocks": {Text: diffOfNFiles(12, 900), FileCount: 12}}
}

func TestChunkMaxLines_ClampedByGlobalByteBudget(t *testing.T) {
	const declared = 128000
	const globalBudget = int64(64 * 1024)
	// 65536 bytes / 48 bytes-per-line = 1365 lines. Stated as a literal, not
	// recomputed from the code under test, so a change to either the ratio or the
	// clamp has to be acknowledged here.
	const wantLines = 1365

	unclamped := payload.ChunkMaxLines("unlisted-small-model", ptrInt(declared), defaultMaxTokens)
	require.Equal(t, 8437, unclamped, "precondition: the declaration alone derives 8437 lines")

	slots, _, err := buildSlots(clampRoster(t, declared, globalBudget), clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: the clamped budget must still split this diff")
	assert.Equal(t, wantLines, slots[0].Primary.chunkMaxLines,
		"the declaration-derived line budget must be clamped to the configured payload_byte_budget")
}

func TestChunkMaxLines_UnclampedWhenNoGlobalBudget(t *testing.T) {
	// payload_byte_budget: 0 means "no global cap configured" everywhere else in
	// the settings tier; the clamp must not invent one.
	const declared = 128000
	slots, _, err := buildSlots(clampRoster(t, declared, 0), clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1)
	assert.Equal(t, 8437, slots[0].Primary.chunkMaxLines,
		"an unset payload_byte_budget must leave the derived line budget alone")
}

func TestChunkMaxLines_ExplicitOverrideBeatsTheClamp(t *testing.T) {
	// The clamp must not become a back-door ceiling on an operator's explicit
	// max_context_lines — that precedence is pinned elsewhere and stays.
	const explicit = 700
	cfg := clampRoster(t, 128000, 4096)
	g := cfg.Registry.Agents["greta"]
	lines := explicit
	g.MaxContextLines = &lines
	cfg.Registry.Agents["greta"] = g

	slots, _, err := buildSlots(cfg, clampPayloads(), ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1)
	assert.Equal(t, explicit, slots[0].Primary.chunkMaxLines,
		"an explicit max_context_lines is used verbatim, clamp or no clamp")
}

func TestClampLinesToByteBudget_CapSizedDeclaration(t *testing.T) {
	// The motivating case: a legal declaration at ContextWindowTokensCap.
	unclamped := payload.ChunkMaxLines("unlisted-small-model", ptrInt(registry.ContextWindowTokensCap), defaultMaxTokens)
	require.Greater(t, unclamped, 700000,
		"precondition: the cap-sized declaration derives a line budget no proxy body limit can carry")

	// 512 KiB / 48 bytes-per-line = 10922 lines.
	assert.Equal(t, 10922, payload.ClampLinesToByteBudget(unclamped, 512*1024),
		"a cap-sized declaration must be cut to the operator's byte ceiling")
}

func TestClampLinesToByteBudget_FloorAndPassthrough(t *testing.T) {
	// The clamp must never return a non-positive value: chunkDiff reads <= 0 as
	// "disable chunking", the exact opposite of what a tight budget needs.
	assert.Positive(t, payload.ClampLinesToByteBudget(100000, 1),
		"a pathologically small budget must still leave a positive line budget")
	assert.Equal(t, 500, payload.ClampLinesToByteBudget(500, 0),
		"an unset (0) budget is 'no cap', not a cap of zero")
	assert.Equal(t, 500, payload.ClampLinesToByteBudget(500, 1<<40),
		"a budget larger than the line budget needs leaves it untouched")
}
