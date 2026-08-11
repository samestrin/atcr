package fanout

import (
	"fmt"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.5.1 (T3/AC1/AC4): an agent's context_window_tokens declaration must
// reach EVERY window/budget resolution in the fan-out — not just the agentSizing
// record that populates status.json.
//
// The two assertion families below are deliberately SEPARATE. A status.json
// assertion alone passes even when no budget changed, because the record is
// populated from its own resolver call: that is precisely the failure mode this
// task exists to prevent, so the per-chunk line budget and the byte budget are
// each asserted on their own terms.

// declaredWindowRoster returns the sizing roster with greta declaring `n` tokens
// while keeping her model absent from the static table (so any window above the
// 32768 default can ONLY have come from the declaration).
func declaredWindowRoster(t *testing.T, n int) *ReviewConfig {
	t.Helper()
	cfg := sizingRosterConfig()
	g := cfg.Registry.Agents["greta"]
	require.Equal(t, "unlisted-small-model", g.Model, "precondition: greta's model must be absent from the static table")
	g.ContextWindowTokens = &n
	cfg.Registry.Agents["greta"] = g
	return cfg
}

// diffOfNFiles builds a diff of fileCount files, each with bodyLines added lines.
func diffOfNFiles(fileCount, bodyLines int) string {
	var b strings.Builder
	for i := 0; i < fileCount; i++ {
		b.WriteString(fileSeg("f"+itoa(i)+".go", bodyLines))
	}
	return b.String()
}

func TestBuildSlots_BulkRecordsDeclaredWindow(t *testing.T) {
	// The :1756 bulk resolver — status.json's resolved_window.
	cfg := declaredWindowRoster(t, 128000)
	agent, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	assert.Equal(t, 128000, agent.ResolvedWindow,
		"the bulk path must record the agent's OWN declaration, not the 32768 default")
}

func TestBuildSlots_BulkBudgetActuallySizesAgainstDeclaredWindow(t *testing.T) {
	// The load-bearing half: the :1462 resolver feeds the applied bulk budget at
	// :1758. Asserting resolved_window alone would pass even if this stayed 32768.
	rng := ReviewRange{Base: "a", Head: "b"}

	undeclared, _, err := buildOneAgent(sizingRosterConfig(), "greta", oversizedBlocksPayload(), rng, "", "")
	require.NoError(t, err)
	declaredAgent, _, err := buildOneAgent(declaredWindowRoster(t, 128000), "greta", oversizedBlocksPayload(), rng, "", "")
	require.NoError(t, err)

	assert.Equal(t, payload.EffectiveByteBudget("unlisted-small-model", nil, defaultMaxTokens), undeclared.EffectiveBudget,
		"precondition: an undeclared agent sizes against the 32768 default")
	assert.Equal(t, payload.EffectiveByteBudget("unlisted-small-model", ptrInt(128000), defaultMaxTokens), declaredAgent.EffectiveBudget,
		"the declared agent's byte budget must derive from its declaration")
	assert.Greater(t, declaredAgent.EffectiveBudget, undeclared.EffectiveBudget)

	// The budget is not cosmetic: the same oversized payload sheds fewer files.
	assert.True(t, undeclared.Truncation.Truncated, "precondition: the 32768 agent sheds this payload")
	assert.Greater(t, len(undeclared.Truncation.FilesDropped), 0, "precondition: the undeclared agent drops at least one file")
	assert.Less(t, len(declaredAgent.Truncation.FilesDropped), len(undeclared.Truncation.FilesDropped),
		"a larger declared window must drop FEWER files from the identical payload")
}

func TestBuildSlots_ChunkedLineBudgetRisesWithDeclaration(t *testing.T) {
	// The :1658 resolver — the per-chunk line budget, the second half of AC1 and
	// the assertion status.json cannot stand in for.
	//
	//	32768 default  -> ChunkMaxLines 1493 -> a 900-line file per chunk
	//	128000 declared -> ChunkMaxLines 8437 -> the whole 3600-line diff in one
	// NOTE: these values are derived from the formula in payload.ChunkMaxLines; if the
	// formula changes (B/token ratio, rounding), these assertions must be updated.
	require.Equal(t, 1493, payload.ChunkMaxLines("unlisted-small-model", nil, defaultMaxTokens))
	require.Equal(t, 8437, payload.ChunkMaxLines("unlisted-small-model", ptrInt(128000), defaultMaxTokens))

	diff := diffOfNFiles(4, 900)
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 4}}
	rng := ReviewRange{Base: "a", Head: "b"}

	base := sizingRosterConfig()
	base.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	base.Settings.ReviewStrategy = "chunked"
	undeclaredSlots, _, err := buildSlots(base, payloads, rng, "", "", true)
	require.NoError(t, err)
	require.Greater(t, len(undeclaredSlots), 1, "precondition: at the 32768 default this diff must split")

	big := declaredWindowRoster(t, 128000)
	big.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	big.Settings.ReviewStrategy = "chunked"
	declaredSlots, _, err := buildSlots(big, payloads, rng, "", "", true)
	require.NoError(t, err)

	assert.Less(t, len(declaredSlots), len(undeclaredSlots),
		"a declared window must produce FEWER, larger chunks from the identical diff")
	assert.Len(t, declaredSlots, 1, "3600 lines fits inside the declared window's 8437-line budget")
}

func TestBuildSlots_ChunkedRecordsDeclaredLineBudget(t *testing.T) {
	// A declared agent whose diff STILL splits records the declared-derived line
	// regime (8437), not the default-derived 1493, on every chunk slot.
	diff := diffOfNFiles(12, 900) // 10800 lines > 8437
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 12}}

	cfg := declaredWindowRoster(t, 128000)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: 10800 lines must still split at an 8437-line budget")

	for i, s := range slots {
		assert.Equal(t, 8437, s.Primary.chunkMaxLines, "chunk slot %d must carry the declared-derived line regime", i)
		assert.Equal(t, 128000, s.Primary.ResolvedWindow, "chunk slot %d must record the declaration", i)
		assert.Equal(t, payload.EffectiveByteBudget("unlisted-small-model", ptrInt(128000), defaultMaxTokens),
			s.Primary.EffectiveBudget, "chunk slot %d must size against the declaration", i)
	}
}

func TestBuildFallbackAgent_ResolvesItsOwnDeclaration(t *testing.T) {
	// AC4: a fallback must resolve its OWN window. Inheriting the primary's
	// larger declaration would overflow a smaller backup model — the one
	// direction the Conservatism NFR forbids.
	cfg := declaredWindowRoster(t, 512000) // greta (primary) declares BIG
	kai := cfg.Registry.Agents["kai"]
	kai.Model = "unlisted-backup-model" // absent from the table
	assert.Equal(t, 32768, payload.ContextWindowTokens("unlisted-backup-model", nil),
		"precondition: unlisted-backup-model has no static table entry (resolves to default)")
	small := 64000
	kai.ContextWindowTokens = &small // the fallback declares SMALL
	cfg.Registry.Agents["kai"] = kai

	primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	require.Equal(t, 512000, primary.ResolvedWindow, "precondition: the primary carries its own large declaration")

	fb, err := buildFallbackAgent(cfg, primary, "kai")
	require.NoError(t, err)

	assert.Equal(t, 64000, fb.ResolvedWindow,
		"a fallback must resolve its OWN declaration, never inherit the primary's larger window")
	assert.Equal(t, payload.EffectiveByteBudget("unlisted-backup-model", ptrInt(64000), defaultMaxTokens), fb.EffectiveBudget)
	assert.Less(t, fb.EffectiveBudget, primary.EffectiveBudget)
}

func TestBuildFallbackAgent_UndeclaredFallbackUsesResolutionChain(t *testing.T) {
	// AC4's second half: a fallback with NO declaration falls through the same
	// chain (static table, then default) rather than borrowing the primary's.
	cfg := declaredWindowRoster(t, 512000)
	kai := cfg.Registry.Agents["kai"]
	kai.Model = "unlisted-backup-model"
	kai.ContextWindowTokens = nil
	cfg.Registry.Agents["kai"] = kai

	primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	fb, err := buildFallbackAgent(cfg, primary, "kai")
	require.NoError(t, err)
	assert.Equal(t, 32768, fb.ResolvedWindow,
		"an undeclared fallback keeps the conservative default, not the primary's 512000")
}

func TestBuildFallbackAgent_WarnsWhenInheritingAnOversizedPrompt(t *testing.T) {
	// AC4's other half. Resolving the fallback's OWN window is only half the
	// guarantee: the prompt it inherits was sized to the PRIMARY's window, so a
	// declared primary hands its undeclared backup a payload that backup cannot
	// hold. AC4 forbids doing that SILENTLY, so the condition must be surfaced —
	// a warning pre-dispatch and the honest overflow degradation on the fallback,
	// rather than copying the primary's action.
	cfg := declaredWindowRoster(t, 512000)
	kai := cfg.Registry.Agents["kai"]
	kai.Model = "unlisted-backup-model" // undeclared → 32768 default
	cfg.Registry.Agents["kai"] = kai

	primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	var fb Agent
	out := captureStderr(t, func() {
		fb, err = buildFallbackAgent(cfg, primary, "kai")
	})
	require.NoError(t, err)

	assert.Contains(t, out, `fallback agent "kai"`, "the mismatch must be surfaced pre-dispatch")
	assert.Contains(t, out, "may overflow")
	assert.Equal(t, "overflow", fb.DegradationAction,
		"a fallback whose own budget is smaller than the payload's sizing must record overflow, not copy the primary's action")
}

func TestBuildFallbackAgent_NoWarningWhenBudgetsMatch(t *testing.T) {
	// The guard must not fire in the ordinary case where primary and fallback
	// resolve the same window — otherwise every truncated bulk review would warn.
	cfg := sizingRosterConfig()
	kai := cfg.Registry.Agents["kai"]
	kai.Model = "unlisted-small-model" // same model/window as greta
	cfg.Registry.Agents["kai"] = kai

	primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	require.True(t, primary.Truncation.Truncated, "precondition: the primary's payload was shed to its budget")

	var fb Agent
	out := captureStderr(t, func() {
		fb, err = buildFallbackAgent(cfg, primary, "kai")
	})
	require.NoError(t, err)

	assert.NotContains(t, out, "may overflow", "equal windows must not warn")
	assert.Equal(t, primary.DegradationAction, fb.DegradationAction,
		"an equally-sized fallback keeps the slot's degradation action")
}

func TestBuildSlots_TinyDeclarationRecordsOverflowDegradation(t *testing.T) {
	// A declaration is valid down to 1 token, so the zero-effective-budget arm in
	// the bulk path — defense-in-depth while ContextWindowTokens floored at 32768
	// — is now REACHABLE. It must record the honest "overflow" degradation rather
	// than silently shipping an over-window payload with the action unmarked.
	//
	// 12288 = defaultMaxTokens (8192) + promptOverheadTokens (4096).
	cfg := declaredWindowRoster(t, 12288)
	agent, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	assert.Equal(t, 12288, agent.ResolvedWindow, "the declaration is recorded even when it buys no budget")
	assert.Zero(t, agent.EffectiveBudget, "output cap + prompt overhead consume the whole declared window")
	assert.Equal(t, "overflow", agent.DegradationAction,
		"a declaration too small to reserve output headroom must be marked, not silently shipped")
}

func TestBuildSlots_BaselineChunkedRecordsDeclaredWindow(t *testing.T) {
	// The BASELINE chunked sizing record at review.go:1575-1576 is never reached
	// by the existing tests because they all call the 6-arg buildSlots form
	// (baseline defaults to false). This test exercises that path by calling
	// buildSlots with baseline=true and enough entries to force multiple chunks.
	declared := 128000
	entries := make([]payload.FileEntry, 0, 50)
	for i := 0; i < 50; i++ {
		// Each entry is 10000 bytes; 50 entries = 500000 bytes total, which exceeds
		// the effective budget of 404992 for a 128000-token declaration, forcing
		// multiple chunks.
		entries = append(entries, baselineEntry(fmt.Sprintf("f%03d.go", i), 10000))
	}
	payloads := baselinePayloads(entries)

	cfg := declaredWindowRoster(t, declared)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: 500000 bytes must split at a 404992-byte budget")

	expectedBudget := payload.EffectiveByteBudget("unlisted-small-model", ptrInt(declared), defaultMaxTokens)
	for i, s := range slots {
		assert.Equal(t, declared, s.Primary.ResolvedWindow,
			"baseline chunk slot %d must record the declaration", i)
		assert.Equal(t, expectedBudget, s.Primary.EffectiveBudget,
			"baseline chunk slot %d must size against the declaration", i)
	}
}

func TestBuildSlots_UndeclaredRosterIsUnchanged(t *testing.T) {
	// AC2: a roster declaring nothing must produce byte-identical sizing to the
	// pre-epic build. The declaration is additive and inert when absent.
	rng := ReviewRange{Base: "a", Head: "b"}
	agent, _, err := buildOneAgent(sizingRosterConfig(), "greta", oversizedBlocksPayload(), rng, "", "")
	require.NoError(t, err)

	assert.Equal(t, 32768, agent.ResolvedWindow)
	assert.Equal(t, int64(71680), agent.EffectiveBudget)
	assert.Equal(t, defaultMaxTokens, agent.ReservedOutputTokens)
}
