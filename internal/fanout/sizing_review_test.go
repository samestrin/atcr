package fanout

import (
	"fmt"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sizingRosterConfig builds a two-agent roster whose members target different
// model windows: "greta" runs an unlisted model (conservative 32768 window) and
// "kai" runs openai/gpt-5.5 (128000 window per the F1 table). Both share the
// "blocks" payload mode so the only difference at dispatch is the per-agent
// budget derived from each model's window (Epic 19.10 F2). It reuses
// twoAgentConfig's persona wiring and only swaps the models.
func sizingRosterConfig() *ReviewConfig {
	cfg := twoAgentConfig("http://unused")
	greta := cfg.Registry.Agents["greta"]
	greta.Model = "unlisted-small-model" // absent from the table → 32768 window
	cfg.Registry.Agents["greta"] = greta
	kai := cfg.Registry.Agents["kai"]
	kai.Model = "openai/gpt-5.5" // 128000 window
	cfg.Registry.Agents["kai"] = kai
	// PayloadByteBudget 0 (unlimited) so the ONLY cap is each model's window,
	// proving the per-agent derivation rather than a shared global ceiling.
	cfg.Settings.PayloadByteBudget = 0
	return cfg
}

// oversizedBlocksPayload returns a modePayload whose raw entries total more bytes
// than a 32k model's effective budget but less than a 128k model's, so the two
// windows shed a different number of files.
func oversizedBlocksPayload() map[string]modePayload {
	const fileBytes = 50000
	const nFiles = 10
	var entries []payload.FileEntry
	var full strings.Builder
	for i := 0; i < nFiles; i++ {
		body := fmt.Sprintf("// file %d\n", i) + strings.Repeat("x", fileBytes)
		entries = append(entries, payload.FileEntry{Path: fmt.Sprintf("f%d.go", i), Size: int64(len(body)), Body: body})
		full.WriteString(body)
	}
	return map[string]modePayload{
		"blocks": {Entries: entries, Text: full.String(), FileCount: nFiles},
	}
}

// TestBuildSlots_PerAgentBudgetShedsBySmallWindow proves the per-agent effective
// budget is actually applied at dispatch (F2/AC1): against one oversized payload,
// the 32k-window agent's rendered payload is smaller AND drops MORE files than the
// 128k-window agent's — they are no longer sized identically off one global budget.
func TestBuildSlots_PerAgentBudgetShedsBySmallWindow(t *testing.T) {
	cfg := sizingRosterConfig()
	payloads := oversizedBlocksPayload()
	rng := ReviewRange{Base: "a", Head: "b"}

	small, _, err := buildOneAgent(cfg, "greta", payloads, rng, "", "")
	require.NoError(t, err)
	large, _, err := buildOneAgent(cfg, "kai", payloads, rng, "", "")
	require.NoError(t, err)

	// The small window must shed strictly more files than the large window.
	assert.Greater(t, len(small.Truncation.FilesDropped), len(large.Truncation.FilesDropped),
		"a 32k-window agent must drop more files than a 128k-window agent on the same oversized payload")
	// And its rendered prompt (which embeds the payload) must be smaller.
	assert.Less(t, len(small.Prompt), len(large.Prompt),
		"the smaller window's rendered payload must be smaller than the larger window's")
	// Sanity: the large window is not itself unbounded here — it still reflects a
	// real per-agent budget, not the whole payload passed through untouched.
	assert.True(t, small.Truncation.Truncated, "the 32k agent must record truncation")
}

// TestBuildSlots_PerAgentBudgetNeverEmptyPayload proves the AllDropped guard: when
// even a single file exceeds a small model's window, the agent must still receive
// the whole payload (Phase 3's on_overflow is the real net) rather than an EMPTY
// one, which would make it silently return a false-clean "no findings" review.
func TestBuildSlots_PerAgentBudgetNeverEmptyPayload(t *testing.T) {
	cfg := sizingRosterConfig() // greta → 32768 window (budget ≈ 71680 bytes)
	const sentinel = "SENTINEL_ONLY_FILE_TOKEN"
	// One file far larger than the 32k window budget — ApplyByteBudget would drop
	// it entirely (AllDropped).
	body := "// " + sentinel + "\n" + strings.Repeat("x", 300000)
	payloads := map[string]modePayload{
		"blocks": {
			Entries:   []payload.FileEntry{{Path: "huge.go", Size: int64(len(body)), Body: body}},
			Text:      body,
			FileCount: 1,
		},
	}
	rng := ReviewRange{Base: "a", Head: "b"}

	small, _, err := buildOneAgent(cfg, "greta", payloads, rng, "", "")
	require.NoError(t, err)
	assert.Contains(t, small.Prompt, sentinel,
		"an oversized single file must NOT be shed to an empty payload (silent false-clean)")
}

// TestBuildSlots_AllDroppedRecordsOverflowAction proves that when every file exceeds
// the agent's per-model byte budget, the forced over-window dispatch records a
// non-empty degradation_action so status.json/summary.json can distinguish it from
// a clean fit.
func TestBuildSlots_AllDroppedRecordsOverflowAction(t *testing.T) {
	cfg := sizingRosterConfig()
	const sentinel = "SENTINEL_ONLY_FILE_TOKEN"
	// One file far larger than the 32k window budget — ApplyByteBudget drops
	// it entirely (AllDropped).
	body := "// " + sentinel + "\n" + strings.Repeat("x", 300000)
	payloads := map[string]modePayload{
		"blocks": {
			Entries:   []payload.FileEntry{{Path: "huge.go", Size: int64(len(body)), Body: body}},
			Text:      body,
			FileCount: 1,
		},
	}
	rng := ReviewRange{Base: "a", Head: "b"}

	small, _, err := buildOneAgent(cfg, "greta", payloads, rng, "", "")
	require.NoError(t, err)
	assert.Equal(t, "overflow", small.DegradationAction,
		"AllDropped bulk dispatch must record degradation_action=overflow so operators can distinguish an at-risk reviewer from a clean fit")
}

// scopeBlock formats a SCOPE CONSTRAINT block wrapping a plan of planBytes bytes.
func scopeBlock(t *testing.T, planBytes int) string {
	t.Helper()
	block, _ := payload.ScopeConstraint(strings.Repeat("p", planBytes), int64(planBytes)+1)
	return block
}

// embeddedScopePlan returns the plan body a SCOPE CONSTRAINT block wraps in a prompt.
func embeddedScopePlan(t *testing.T, prompt string) string {
	t.Helper()
	const begin = "----- BEGIN SPRINT PLAN -----\n"
	const end = "\n----- END SPRINT PLAN -----"
	i := strings.Index(prompt, begin)
	require.GreaterOrEqual(t, i, 0, "prompt missing SCOPE CONSTRAINT BEGIN marker")
	rest := prompt[i+len(begin):]
	j := strings.Index(rest, end)
	require.GreaterOrEqual(t, j, 0, "prompt missing SCOPE CONSTRAINT END marker")
	return rest[:j]
}

// TestBuildSlots_ScopeConstraintFitsPerAgentWindow proves the HIGH TD fix
// (review.go:1156): the SCOPE CONSTRAINT plan block is prepended UNCOUNTED against
// the per-agent window, so a large plan on a small-window agent must (B) be capped to
// that agent's own budget and (A) have its bytes reserved from the diff budget, so
// plan + diff together fit the window instead of reintroducing the dax overflow on
// the --sprint-plan path.
func TestBuildSlots_ScopeConstraintFitsPerAgentWindow(t *testing.T) {
	cfg := sizingRosterConfig() // greta → 32768 window, PayloadByteBudget 0
	cfg.Settings.MaxSprintPlanBytes = 64 * 1024
	eff := payload.EffectiveByteBudget("unlisted-small-model", 8192) // 71680

	scope := scopeBlock(t, 64*1024) // 64 KiB plan, far larger than eff/8 (8960)

	// A diff that FITS the full budget but NOT the budget minus the reserved plan, so
	// the reservation (A) is observable as extra truncation.
	const nFiles, fileBytes = 5, 13000 // 65000 total: < eff(71680), > eff-eff/8 (62720)
	var entries []payload.FileEntry
	var full strings.Builder
	for i := 0; i < nFiles; i++ {
		body := fmt.Sprintf("// f%d\n", i) + strings.Repeat("d", fileBytes)
		entries = append(entries, payload.FileEntry{Path: fmt.Sprintf("f%d.go", i), Size: int64(len(body)), Body: body})
		full.WriteString(body)
	}
	payloads := map[string]modePayload{"blocks": {Entries: entries, Text: full.String(), FileCount: nFiles}}

	ag, _, err := buildOneAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"}, "", scope)
	require.NoError(t, err)

	// (B) the embedded plan is capped to this agent's own budget, not left at 64 KiB.
	assert.LessOrEqual(t, len(embeddedScopePlan(t, ag.Prompt)), int(eff/8),
		"the per-agent plan cap must trim a large plan to EffectiveByteBudget/8")
	// (A) the diff was shed to reserve room for the scope-constraint block.
	assert.True(t, ag.Truncation.Truncated,
		"the diff budget must be reduced by the scope-constraint bytes so plan+diff fit the window")
}

// TestBuildSlots_ChunkedReservesForScopeConstraint proves the chunked path gets the
// same treatment: the per-chunk line budget reserves headroom for the scope
// constraint prepended to every chunk, so a scoped chunked run bin-packs into more,
// smaller chunks than an unscoped one.
func TestBuildSlots_ChunkedReservesForScopeConstraint(t *testing.T) {
	newCfg := func() *ReviewConfig {
		c := sizingRosterConfig()
		c.Settings.ReviewStrategy = "chunked"
		c.Settings.MaxSprintPlanBytes = 64 * 1024
		return c
	}
	var b strings.Builder
	for i := 0; i < 400; i++ {
		b.WriteString(fileSeg(fmt.Sprintf("f%d.go", i), 40))
	}
	payloads := map[string]modePayload{"blocks": {Text: b.String(), FileCount: 400}}
	rng := ReviewRange{Base: "a", Head: "b"}

	unscoped, _, err := buildSlots(newCfg(), payloads, rng, "", "", false)
	require.NoError(t, err)
	scoped, _, err := buildSlots(newCfg(), payloads, rng, "", scopeBlock(t, 64*1024), false)
	require.NoError(t, err)

	assert.Greater(t, len(scoped), len(unscoped),
		"reserving chunk-line headroom for the scope constraint yields more, smaller chunks")
}
