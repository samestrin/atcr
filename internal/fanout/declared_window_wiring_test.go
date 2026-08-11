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

func TestBuildFallbackAgent_UndeclaredFallbackUsesStaticTableTier(t *testing.T) {
	// The middle tier of the same chain. The sibling test above only proves an
	// undeclared fallback reaches the DEFAULT, which an implementation that
	// ignored the static table entirely would also satisfy. Pin the table tier
	// with a model that has an entry, so "falls through the resolution chain"
	// means the whole chain and not just its last step.
	require.Equal(t, 128000, payload.ContextWindowTokens("openai/gpt-5.5", nil),
		"precondition: openai/gpt-5.5 IS in the static table, unlike unlisted-backup-model")

	cfg := declaredWindowRoster(t, 512000)
	kai := cfg.Registry.Agents["kai"]
	kai.Model = "openai/gpt-5.5"
	kai.ContextWindowTokens = nil // undeclared → must land on the TABLE, not the default
	cfg.Registry.Agents["kai"] = kai

	primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	fb, err := buildFallbackAgent(cfg, primary, "kai")
	require.NoError(t, err)
	assert.Equal(t, 128000, fb.ResolvedWindow,
		"an undeclared fallback on a table-listed model resolves the TABLE entry, not the 32768 default and not the primary's 512000")
	assert.Equal(t, payload.EffectiveByteBudget("openai/gpt-5.5", nil, defaultMaxTokens), fb.EffectiveBudget)
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

	// COUPLING: these two assertions match substrings of the warning literal in
	// buildFallbackAgent (review.go). The warning is plain stderr text, not a
	// structured log field, so there is nothing more stable to key on — a reword
	// of that message must update these strings. Kept deliberately short (the
	// agent name and "may overflow") so ordinary rewording of the surrounding
	// prose does not break them. captureStderr lives in engine_degrade_test.go.
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

func TestBuildFallbackAgent_NoWarningWhenFallbackWindowIsLarger(t *testing.T) {
	// DIRECTION. The guard is `fbBudget < primary.EffectiveBudget` — strictly
	// smaller. The two existing cases (a ~24x gap that fires, exact equality that
	// does not) leave the ordering unpinned: a mutation to `fbBudget !=
	// primary.EffectiveBudget` passes both, and under it a fallback with a LARGER
	// window than its primary — the desirable configuration — would be falsely
	// warned about and falsely stamped degradation_action "overflow".
	cfg := declaredWindowRoster(t, 64000) // primary declares SMALL
	kai := cfg.Registry.Agents["kai"]
	kai.Model = "unlisted-backup-model"
	big := 512000
	kai.ContextWindowTokens = &big // fallback declares BIG
	cfg.Registry.Agents["kai"] = kai

	primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	require.Equal(t, 64000, primary.ResolvedWindow, "precondition: the primary is the SMALLER of the pair")

	var fb Agent
	out := captureStderr(t, func() {
		fb, err = buildFallbackAgent(cfg, primary, "kai")
	})
	require.NoError(t, err)
	require.Greater(t, fb.EffectiveBudget, primary.EffectiveBudget,
		"precondition: the fallback's budget really is the larger one")

	assert.NotContains(t, out, "may overflow",
		"a fallback with a LARGER window than its primary is the desirable case and must not warn")
	assert.Equal(t, primary.DegradationAction, fb.DegradationAction,
		"a larger-windowed fallback keeps the slot's action; it must not be stamped overflow")
}

func TestBuildFallbackAgent_WarnsWhenFallbackIsOnlyMarginallySmaller(t *testing.T) {
	// BOUNDARY. The only firing case in the suite is a ~24x gap, so a mutation
	// that requires a large multiple (`fbBudget*4 < primary.EffectiveBudget`)
	// survives untouched. A fallback barely below its primary must still warn:
	// the guard is a strict `<`, not "meaningfully smaller".
	cfg := declaredWindowRoster(t, 33000)
	kai := cfg.Registry.Agents["kai"]
	kai.Model = "unlisted-backup-model"
	near := 32768
	kai.ContextWindowTokens = &near // 232 tokens below the primary
	cfg.Registry.Agents["kai"] = kai

	primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	var fb Agent
	out := captureStderr(t, func() {
		fb, err = buildFallbackAgent(cfg, primary, "kai")
	})
	require.NoError(t, err)
	require.Less(t, fb.EffectiveBudget, primary.EffectiveBudget,
		"precondition: 32768 really is below 33000 after the same reservation")

	assert.Contains(t, out, "may overflow",
		"a marginally smaller fallback must still warn — the guard is a strict <, not a multiple")
	assert.Equal(t, "overflow", fb.DegradationAction)
}

// measurablePayload returns a files-mode modePayload of nFiles entries of
// fileBytes each, written with the real "=== FILE: p ===" marker so
// EntriesFromRenderedPayload can recover the per-file bodies from the rendered
// text — i.e. so the slot's CodeContext is populated and the payload's byte
// total is measurable downstream. The other helpers in this package
// (oversizedBlocksPayload, baselinePayloads) deliberately use unmarked or
// "// FILE:"-marked bodies, which the parser does not recognize.
func measurablePayload(nFiles, fileBytes int) map[string]modePayload {
	var entries []payload.FileEntry
	var full strings.Builder
	for i := 0; i < nFiles; i++ {
		marker := fmt.Sprintf("=== FILE: f%d.go ===\n", i)
		pad := fileBytes - len(marker)
		if pad < 0 {
			pad = 0
		}
		body := marker + strings.Repeat("z", pad)
		entries = append(entries, payload.FileEntry{Path: fmt.Sprintf("f%d.go", i), Size: int64(len(body)), Body: body})
		full.WriteString(body)
	}
	return map[string]modePayload{
		string(payload.ModeFiles): {Entries: entries, Text: full.String(), FileCount: nFiles},
	}
}

func TestBuildFallbackAgent_WarningGatesOnTheInheritedPayloadNotOnBudgetsAlone(t *testing.T) {
	// The AC4 guard compared two BUDGETS and never consulted the payload it claims
	// may overflow: `primary.EffectiveBudget > 0 && fbBudget < primary.EffectiveBudget`
	// is true for ANY fallback resolving a smaller window, whether the prompt is
	// 5 KB or 500 KB. The epic's own Post-Epic Operator Step pairs declared
	// primaries with undeclared 32768 backups, so on the recommended roster an
	// ordinary small-diff review warned on every pair and stamped
	// degradation_action=overflow on fallbacks that would have held the payload
	// with room to spare — making false exactly the diagnosability record this
	// epic exists to make honest.
	//
	// The fallback's budget is fixed at 71680 B (the undeclared 32768 default) in
	// both halves below; only the payload size changes, which is the whole point.
	fbBudget := payload.EffectiveByteBudget("unlisted-backup-model", nil, defaultMaxTokens)
	require.Equal(t, int64(71680), fbBudget, "precondition: the undeclared fallback's budget")

	roster := func(t *testing.T) *ReviewConfig {
		t.Helper()
		cfg := declaredWindowRoster(t, 128000) // primary declares BIG
		kai := cfg.Registry.Agents["kai"]
		kai.Model = "unlisted-backup-model" // undeclared → 32768 default
		cfg.Registry.Agents["kai"] = kai
		return cfg
	}
	rng := ReviewRange{Base: "a", Head: "b"}

	t.Run("fits: no warning, no overflow stamp", func(t *testing.T) {
		cfg := roster(t)
		primary, _, err := buildOneAgent(cfg, "greta", measurablePayload(2, 3000), rng, string(payload.ModeFiles), "")
		require.NoError(t, err)
		require.NotEmpty(t, primary.CodeContext, "precondition: the shipped payload is measurable")
		require.Greater(t, primary.EffectiveBudget, fbBudget,
			"precondition: the budget comparison alone WOULD fire — the fallback's budget is the smaller one")

		var fb Agent
		out := captureStderr(t, func() { fb, err = buildFallbackAgent(cfg, primary, "kai") })
		require.NoError(t, err)

		assert.NotContains(t, out, "may overflow",
			"a 6 KB payload cannot overflow a 71680-byte budget, whatever the window gap")
		assert.Equal(t, primary.DegradationAction, fb.DegradationAction,
			"a fallback that comfortably holds the payload must not be stamped overflow")
	})

	t.Run("does not fit: still warns", func(t *testing.T) {
		// The complement, so the fix cannot be satisfied by suppressing the warning
		// outright: a payload genuinely larger than the fallback's budget must still
		// warn and still record the overflow.
		cfg := roster(t)
		primary, _, err := buildOneAgent(cfg, "greta", measurablePayload(20, 10000), rng, string(payload.ModeFiles), "")
		require.NoError(t, err)
		require.NotEmpty(t, primary.CodeContext)
		require.Greater(t, int64(len(primary.CodeContext[0].Body)*len(primary.CodeContext)), fbBudget,
			"precondition: the shipped payload really does exceed the fallback's budget")

		var fb Agent
		out := captureStderr(t, func() { fb, err = buildFallbackAgent(cfg, primary, "kai") })
		require.NoError(t, err)

		assert.Contains(t, out, "may overflow", "a payload past the fallback's budget must still warn")
		assert.Equal(t, "overflow", fb.DegradationAction)
	})
}

func TestReservedOutputTokens_NotRecordedWhenTheWindowCannotFundIt(t *testing.T) {
	// `if fbWindow > 0 { fbReserved = defaultMaxTokens }` is a dead branch:
	// ContextWindowTokens never returns 0 by contract (contextwindow.go:90-98), so
	// the reservation was stamped unconditionally. With a declaration as low as 1
	// token now legal, that produces a self-contradictory record — resolved_window
	// 1 with reserved_output_tokens 8192 and, via omitempty on a zero budget, no
	// effective_budget field at all, so status.json asserts the agent reserved 8192
	// output tokens out of a 1-token window. Gate on the BUDGET, the quantity that
	// actually funds the output cap. renderAgent (:2126-2129) carries the same
	// shape for the primary lane, so both are pinned here.
	cfg := declaredWindowRoster(t, 512000)
	kai := cfg.Registry.Agents["kai"]
	kai.Model = "unlisted-backup-model"
	tiny := 1
	kai.ContextWindowTokens = &tiny
	cfg.Registry.Agents["kai"] = kai

	primary, _, err := buildOneAgent(cfg, "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	require.Equal(t, defaultMaxTokens, primary.ReservedOutputTokens,
		"precondition: a primary whose budget DOES fund the cap still records it")

	var fb Agent
	_ = captureStderr(t, func() { fb, err = buildFallbackAgent(cfg, primary, "kai") })
	require.NoError(t, err)
	require.Equal(t, 1, fb.ResolvedWindow, "precondition: the fallback's declaration is honoured")
	require.Zero(t, fb.EffectiveBudget, "precondition: a 1-token window funds no input budget")
	assert.Zero(t, fb.ReservedOutputTokens,
		"a fallback whose window cannot fund the output cap must not record reserving it")

	// The primary lane's twin: sized (resolved_window is recorded) but with a
	// budget of zero, so the reservation is equally unfunded there.
	var tinyPrimary Agent
	_ = captureStderr(t, func() {
		tinyPrimary, _, err = buildOneAgent(declaredWindowRoster(t, 12288), "greta", oversizedBlocksPayload(), ReviewRange{Base: "a", Head: "b"}, "", "")
	})
	require.NoError(t, err)
	require.Equal(t, 12288, tinyPrimary.ResolvedWindow, "precondition: the primary was sized")
	require.Zero(t, tinyPrimary.EffectiveBudget, "precondition: the declaration funds no input budget")
	assert.Zero(t, tinyPrimary.ReservedOutputTokens,
		"a primary whose window cannot fund the output cap must not record reserving it either")
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
	// AC2, bulk path: an undeclared agent resolves the pre-epic window, budget,
	// and output reservation. Scoped deliberately — this covers the BULK sizing
	// scalars for one agent, not "byte-identical sizing" for the whole roster;
	// the chunked line regime and the table tier are covered by the sibling test
	// below, which is what the broader claim actually requires.
	rng := ReviewRange{Base: "a", Head: "b"}
	agent, _, err := buildOneAgent(sizingRosterConfig(), "greta", oversizedBlocksPayload(), rng, "", "")
	require.NoError(t, err)

	assert.Equal(t, 32768, agent.ResolvedWindow)
	assert.Equal(t, int64(71680), agent.EffectiveBudget)
	assert.Equal(t, defaultMaxTokens, agent.ReservedOutputTokens)
}

func TestBuildSlots_UndeclaredRosterIsUnchangedOnChunkedAndTableTiers(t *testing.T) {
	// The other half of AC2's claim, which the bulk test above cannot make. An
	// undeclared roster must be inert on the CHUNKED path too (the derived
	// per-chunk line budget, not just the byte budget), and for the roster's
	// table-listed agent (the middle resolution tier) as well as its unlisted one
	// — a regression in the declared==nil table lookup would otherwise only
	// surface in the payload package's own unit tests, never at the fan-out layer.
	cfg := sizingRosterConfig() // greta: unlisted → 32768; kai: openai/gpt-5.5 → 128000
	cfg.Settings.ReviewStrategy = "chunked"
	payloads := map[string]modePayload{"blocks": {Text: diffOfNFiles(12, 900), FileCount: 12}}
	rng := ReviewRange{Base: "a", Head: "b"}

	// Expected values are derived from the payload package rather than hardcoded:
	// what this test pins is that the fan-out layer agrees with the resolver for
	// an undeclared agent on BOTH tiers, not the arithmetic of the formula (which
	// payload's own tests own, and which hardcoding here would couple us to).
	for _, tc := range []struct {
		agent, model string
	}{
		{"greta", "unlisted-small-model"}, // default tier
		{"kai", "openai/gpt-5.5"},         // static-table tier
	} {
		wantWin := payload.ContextWindowTokens(tc.model, nil)
		wantLines := payload.ChunkMaxLines(tc.model, nil, defaultMaxTokens)
		scoped := *cfg
		proj := *cfg.Project
		proj.Agents = []string{tc.agent}
		proj.SerialAgents = nil
		scoped.Project = &proj

		slots, _, err := buildSlots(&scoped, payloads, rng, "", "", true)
		require.NoError(t, err)
		require.Greater(t, len(slots), 1, "precondition: %s must split this diff into chunks", tc.agent)

		for i, s := range slots {
			assert.Equal(t, wantWin, s.Primary.ResolvedWindow, "%s chunk %d", tc.agent, i)
			assert.Equal(t, wantLines, s.Primary.chunkMaxLines,
				"%s chunk %d must carry the undeclared-derived line regime", tc.agent, i)
		}
	}
}
