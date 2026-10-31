package fanout

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStderr (defined in engine_degrade_test.go) redirects os.Stderr; it is
// not parallel-safe, so these tests must not call t.Parallel().

// The per-chunk LINE budget is derived from a BYTE budget at a fixed bytes/line
// estimate (payload.AvgBytesPerLine), calibrated on a ~59 B/line diff. A diff of
// longer lines — prose, YAML/JSON, generated or minified code — packs the same
// line count into far more bytes, so a chunk can ship past the byte budget it was
// sized against while every line-count check stays quiet. These tests pin the
// stderr warning that reports such a chunk, and pin that it only reports: chunk
// boundaries are not touched (chunkDiff, which owns them, splits by lines only).

// overshootMarker is the stable part of the byte-budget warning, used to count
// warnings without depending on the per-chunk figures.
const overshootMarker = "-byte chunk budget"

// wideLineSeg builds a one-file diff whose body lines are each exactly lineBytes
// bytes long, newline included.
func wideLineSeg(path string, body, lineBytes int) string {
	var b strings.Builder
	b.WriteString("diff --git a/" + path + " b/" + path + "\n")
	b.WriteString("--- a/" + path + "\n")
	b.WriteString("+++ b/" + path + "\n")
	b.WriteString("@@ -1," + itoa(body) + " +1," + itoa(body) + " @@\n")
	line := "+" + strings.Repeat("x", lineBytes-2) + "\n"
	for i := 0; i < body; i++ {
		b.WriteString(line)
	}
	return b.String()
}

// overshootRoster is greta alone on a declared 32768-token window with no
// payload or chunk ceiling, so the chunk budget is the agent's own derived
// budget: 71680 B, and 71680 / 64 = 1120 lines per chunk.
func overshootRoster(t *testing.T) (*ReviewConfig, int64) {
	t.Helper()
	cfg := declaredWindowRoster(t, 32768)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.PayloadByteBudget = 0
	cfg.Settings.ChunkByteBudget = 0
	budget := payload.EffectiveByteBudget("unlisted-small-model", ptrInt(32768), defaultMaxTokens)
	require.Equal(t, int64(71680), budget, "precondition: a 32768-token window derives a 71680-byte budget")
	require.Equal(t, 1120, payload.ChunkMaxLines("unlisted-small-model", ptrInt(32768), defaultMaxTokens),
		"precondition: that budget derives 1120 lines per chunk")
	return cfg, budget
}

func overshootLine(agent string, idx, total, bytes int, budget int64) string {
	return fmt.Sprintf("atcr: warning: agent %q: chunk %d of %d is %d bytes, over its %d-byte chunk budget; its lines are longer than the chunk sizing assumes, so it may overflow the model\n",
		agent, idx, total, bytes, budget)
}

// A diff of ~200-byte lines under a DERIVED line budget: every chunk carrying the
// wide files is over its byte budget and is reported once, with its index, its
// bytes and the budget; the short-line chunk in front of them is not.
func TestBuildSlots_ChunkByteOvershoot_WarnsOncePerOverBudgetChunk(t *testing.T) {
	cfg, budget := overshootRoster(t)

	// 600 short lines (~3.6 KB) then three 600-line files of 200-byte lines
	// (~120 KB each). 604 + 604 lines > 1120, so every file is its own chunk.
	short := fileSeg("short.go", 600)
	wide := []string{wideLineSeg("w1.go", 600, 200), wideLineSeg("w2.go", 600, 200), wideLineSeg("w3.go", 600, 200)}
	text := short + strings.Join(wide, "")
	payloads := map[string]modePayload{"blocks": {Text: text, FileCount: 4}}

	var slots []Slot
	out := captureStderr(t, func() {
		var err error
		slots, _, err = buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.NoError(t, err)
	})
	require.Len(t, slots, 4, "precondition: one chunk per file")
	require.Less(t, int64(len(short)), budget, "precondition: the short-line chunk fits its budget")

	assert.Equal(t, 3, strings.Count(out, overshootMarker), "one warning per over-budget chunk, none for the chunk that fits:\n%s", out)
	for i, w := range wide {
		assert.Contains(t, out, overshootLine("greta", i+2, 4, len(w), budget))
	}
	assert.NotContains(t, out, "chunk 1 of 4", "the short-line chunk is within its budget")
	assert.NotContains(t, out, "exceeds max_context_lines", "no chunk is over its LINE budget; only the bytes overshoot")
}

// An explicit max_context_lines replaces the clamped line count verbatim, so its
// chunks can exceed the byte budget by the operator's choice. They are still
// reported: the budget is what the model can hold, whoever chose the lines.
func TestBuildSlots_ChunkByteOvershoot_WarnsUnderExplicitMaxContextLines(t *testing.T) {
	cfg, budget := overshootRoster(t)
	mcl := 2000
	g := cfg.Registry.Agents["greta"]
	g.MaxContextLines = &mcl
	cfg.Registry.Agents["greta"] = g

	// Five 600-line wide files: 3 x 604 = 1812 <= 2000, so chunk 1 holds three
	// files and chunk 2 the other two. Both are over 71680 B, neither over 2000 lines.
	var segs []string
	for i := 0; i < 5; i++ {
		segs = append(segs, wideLineSeg("w"+itoa(i)+".go", 600, 200))
	}
	payloads := map[string]modePayload{"blocks": {Text: strings.Join(segs, ""), FileCount: 5}}

	var slots []Slot
	out := captureStderr(t, func() {
		var err error
		slots, _, err = buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.NoError(t, err)
	})
	require.Len(t, slots, 2)
	require.Equal(t, 2000, slots[0].Primary.chunkMaxLines, "precondition: the explicit line budget is in force")

	assert.Equal(t, 2, strings.Count(out, overshootMarker), out)
	assert.Contains(t, out, overshootLine("greta", 1, 2, len(segs[0])+len(segs[1])+len(segs[2]), budget))
	assert.Contains(t, out, overshootLine("greta", 2, 2, len(segs[3])+len(segs[4]), budget))
}

// A budget of 0 means no budget applies (the zero-budget window is reported by
// its own warning); a budget of 1 is the floor left when the SCOPE CONSTRAINT
// block eats the whole chunk ceiling, which every chunk exceeds by construction.
// Neither is a statement about how long the diff's lines are, so neither warns.
func TestBuildSlots_ChunkByteOvershoot_SilentForBudgetZeroOrOne(t *testing.T) {
	wide := wideLineSeg("w1.go", 600, 200) + wideLineSeg("w2.go", 600, 200)
	payloads := map[string]modePayload{"blocks": {Text: wide, FileCount: 2}}

	t.Run("budget 0", func(t *testing.T) {
		cfg := declaredWindowRoster(t, 1000) // at or below output cap + overhead
		cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
		cfg.Settings.ReviewStrategy = "chunked"
		cfg.Settings.PayloadByteBudget = 0
		cfg.Settings.ChunkByteBudget = 0
		require.Equal(t, int64(0), payload.EffectiveByteBudget("unlisted-small-model", ptrInt(1000), defaultMaxTokens),
			"precondition: the window leaves no budget")

		var slots []Slot
		out := captureStderr(t, func() {
			var err error
			slots, _, err = buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
			require.NoError(t, err)
		})
		require.Greater(t, len(slots), 1, "precondition: the floor still chunks the diff")
		assert.Contains(t, out, "effective budget 0", "precondition: the zero-budget warning is what reports this window")
		assert.NotContains(t, out, overshootMarker, out)
	})

	t.Run("budget 1", func(t *testing.T) {
		const planBytes = 64 * 1024
		cfg := declaredWindowRoster(t, 128000)
		cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
		cfg.Settings.ReviewStrategy = "chunked"
		cfg.Settings.PayloadByteBudget = 512 * 1024
		cfg.Settings.ChunkByteBudget = 512 // smaller than the scope block, so the clamp floors at 1
		cfg.Settings.MaxSprintPlanBytes = planBytes

		var slots []Slot
		out := captureStderr(t, func() {
			var err error
			slots, _, err = buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", scopeBlock(t, planBytes), true)
			require.NoError(t, err)
		})
		require.Greater(t, len(slots), 1)
		require.Greater(t, len(embeddedScopeBlock(t, slots[0].Primary.Prompt)), 512,
			"precondition: the block exceeds the ceiling, so the clamp budget is the floor of 1")
		require.Equal(t, 64, slots[0].Primary.chunkMaxLines, "precondition: a budget of 1 clamps to the minChunkLines floor")
		assert.NotContains(t, out, overshootMarker, out)
	})
}

// A diff at the density the bytes/line estimate was calibrated on (~59 B/line,
// the T3 baseline) fills its line budget without passing its byte budget, so it
// never warns — the warning is for diffs wider than the estimate, not for every
// full chunk.
func TestBuildSlots_ChunkByteOvershoot_SilentAtFixtureDensity(t *testing.T) {
	cfg, _ := overshootRoster(t)
	var segs []string
	for i := 0; i < 8; i++ {
		segs = append(segs, wideLineSeg("f"+itoa(i)+".go", 500, 59))
	}
	payloads := map[string]modePayload{"blocks": {Text: strings.Join(segs, ""), FileCount: 8}}

	var slots []Slot
	out := captureStderr(t, func() {
		var err error
		slots, _, err = buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.NoError(t, err)
	})
	require.Greater(t, len(slots), 1, "precondition: the diff fills more than one chunk")
	assert.NotContains(t, out, overshootMarker, out)
}

// The resume rebuild path (warnOversized=false) reconstructs slots the original
// preparation already reported, so it stays silent like every sibling warning.
func TestBuildSlots_ChunkByteOvershoot_SilentOnResume(t *testing.T) {
	cfg, _ := overshootRoster(t)
	wide := wideLineSeg("w1.go", 600, 200) + wideLineSeg("w2.go", 600, 200)
	payloads := map[string]modePayload{"blocks": {Text: wide, FileCount: 2}}

	out := captureStderr(t, func() {
		_, _, err := buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", false)
		require.NoError(t, err)
	})
	assert.NotContains(t, out, overshootMarker, out)
}

// The byte check is independent of the line warning: a chunk over BOTH budgets
// gets both, and the line warning's text is unchanged byte for byte.
func TestBuildSlots_ChunkByteOvershoot_LineWarningUnchanged(t *testing.T) {
	cfg, budget := overshootRoster(t)
	mcl := 100
	g := cfg.Registry.Agents["greta"]
	g.MaxContextLines = &mcl
	cfg.Registry.Agents["greta"] = g

	w1, w2 := wideLineSeg("w1.go", 600, 200), wideLineSeg("w2.go", 600, 200)
	payloads := map[string]modePayload{"blocks": {Text: w1 + w2, FileCount: 2}}

	out := captureStderr(t, func() {
		_, _, err := buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.NoError(t, err)
	})
	assert.Equal(t, 2, strings.Count(out,
		`atcr: warning: agent "greta": a single file's diff (604 lines) exceeds max_context_lines (100); sent as its own oversized chunk`+"\n"),
		"the line warning keeps its exact text, once per chunk:\n%s", out)
	assert.Contains(t, out, overshootLine("greta", 1, 2, len(w1), budget))
	assert.Contains(t, out, overshootLine("greta", 2, 2, len(w2), budget))
}

// Warning is all the check does: the slots — and so the chunk boundaries every
// cached or resumed bin is keyed on — are identical with the warning on or off.
func TestBuildSlots_ChunkByteOvershoot_BoundariesUnchanged(t *testing.T) {
	cfg, _ := overshootRoster(t)
	text := fileSeg("short.go", 600) + wideLineSeg("w1.go", 600, 200) + wideLineSeg("w2.go", 600, 200)
	payloads := map[string]modePayload{"blocks": {Text: text, FileCount: 3}}

	var warned, quiet []Slot
	captureStderr(t, func() {
		var err error
		warned, _, err = buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.NoError(t, err)
		quiet, _, err = buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", false)
		require.NoError(t, err)
	})
	require.Len(t, warned, 3)
	require.Len(t, quiet, 3)
	for i := range warned {
		assert.Equal(t, quiet[i].Primary.Prompt, warned[i].Primary.Prompt, "slot %d", i)
	}
	assert.Equal(t, chunkDiff(text, 1120), []string{fileSeg("short.go", 600), wideLineSeg("w1.go", 600, 200), wideLineSeg("w2.go", 600, 200)},
		"the chunker still splits on lines alone")
}

// The engine-rendered preamble (on a range payload, the claim ledger that
// splitDiffFiles glues onto chunk 1) is dispatched, so its bytes count toward
// the budget gate, but they are not the diff's own: the warning names them
// apart, as the line warning names its preamble lines, instead of charging them
// to lines that "are longer than the chunk sizing assumes".
func TestBuildSlots_ChunkByteOvershoot_NamesPreambleBytesApart(t *testing.T) {
	cfg, budget := overshootRoster(t)
	preamble := strings.Repeat("ledger line\n", 30)
	w1 := wideLineSeg("w1.go", 600, 200)
	payloads := map[string]modePayload{"blocks": {Text: preamble + w1, FileCount: 1}}

	out := captureStderr(t, func() {
		_, _, err := buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.NoError(t, err)
	})
	assert.Equal(t, 1, strings.Count(out, overshootMarker), out)
	assert.Contains(t, out, fmt.Sprintf("atcr: warning: agent %q: chunk 1 of 1 is %d bytes plus %d engine-rendered preamble byte(s), over its %d-byte chunk budget;",
		"greta", len(w1), len(preamble), budget), out)
	assert.NotContains(t, out, fmt.Sprintf("is %d bytes", len(preamble)+len(w1)),
		"the delivered total is not the diff's own byte count")
}

// The preamble alone never makes the diff's lines look wide: a chunk whose own
// diff fits the budget still warns when the preamble pushes the delivered total
// over it (the gate runs on what is dispatched), and says which part is which.
func TestBuildSlots_ChunkByteOvershoot_PreamblePushesTotalOver(t *testing.T) {
	cfg, budget := overshootRoster(t)
	w1 := wideLineSeg("w1.go", 300, 200)
	require.LessOrEqual(t, int64(len(w1)), budget, "precondition: the diff's own bytes fit")
	preamble := strings.Repeat(strings.Repeat("l", 99)+"\n", 200)
	require.Greater(t, int64(len(preamble)+len(w1)), budget, "precondition: the delivered total does not")
	payloads := map[string]modePayload{"blocks": {Text: preamble + w1, FileCount: 1}}

	out := captureStderr(t, func() {
		_, _, err := buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.NoError(t, err)
	})
	assert.Contains(t, out, fmt.Sprintf("chunk 1 of 1 is %d bytes plus %d engine-rendered preamble byte(s), over its %d-byte chunk budget", len(w1), len(preamble), budget), out)
}

// overshootFleet is overshootRoster with greta cloned into n agents of one
// model and window, so every agent derives the same chunks and the same budget.
func overshootFleet(t *testing.T, n int) (*ReviewConfig, int64, []string) {
	t.Helper()
	cfg, budget := overshootRoster(t)
	names := []string{"greta"}
	for i := 2; i <= n; i++ {
		name := "greta" + itoa(i)
		ac := cfg.Registry.Agents["greta"]
		ac.Persona = name // an explicit ref must resolve to a file; the agent's own name falls back to the base
		cfg.Registry.Agents[name] = ac
		names = append(names, name)
	}
	cfg.Project = &registry.ProjectConfig{Agents: names}
	return cfg, budget, names
}

// Every agent of one budget chunks the diff identically, so per-agent warnings
// were the same line N times. A 13-agent roster prints each over-budget chunk
// once, naming every agent it applies to.
func TestBuildSlots_ChunkByteOvershoot_DedupedAcrossAgents(t *testing.T) {
	cfg, budget, names := overshootFleet(t, 13)
	w1, w2 := wideLineSeg("w1.go", 600, 200), wideLineSeg("w2.go", 600, 200)
	payloads := map[string]modePayload{"blocks": {Text: w1 + w2, FileCount: 2}}

	var slots []Slot
	out := captureStderr(t, func() {
		var err error
		slots, _, err = buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.NoError(t, err)
	})
	require.Len(t, slots, 26, "precondition: 13 agents x 2 chunks")

	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	agents := "agents " + strings.Join(quoted, ", ")
	assert.Equal(t, 2, strings.Count(out, overshootMarker), "one line per distinct (chunk, bytes, budget), not per agent:\n%s", out)
	assert.Contains(t, out, fmt.Sprintf("atcr: warning: %s: chunk 1 of 2 is %d bytes, over its %d-byte chunk budget;", agents, len(w1), budget), out)
	assert.Contains(t, out, fmt.Sprintf("atcr: warning: %s: chunk 2 of 2 is %d bytes, over its %d-byte chunk budget;", agents, len(w2), budget), out)
}

// The dedupe key includes the budget: agents whose windows differ get their own
// line, so one line per distinct budget, not one line overall.
func TestBuildSlots_ChunkByteOvershoot_DistinctBudgetsWarnSeparately(t *testing.T) {
	cfg, budget, _ := overshootFleet(t, 3)
	n := 16384
	other := cfg.Registry.Agents["greta"]
	other.ContextWindowTokens = &n
	other.Persona = "narrow"
	cfg.Registry.Agents["narrow"] = other
	cfg.Project.Agents = append(cfg.Project.Agents, "narrow")
	otherBudget := payload.EffectiveByteBudget("unlisted-small-model", &n, defaultMaxTokens)
	require.NotEqual(t, budget, otherBudget, "precondition: the two windows derive different budgets")

	w1 := wideLineSeg("w1.go", 600, 200)
	require.Greater(t, int64(len(w1)), otherBudget, "precondition: the chunk is over both budgets")
	payloads := map[string]modePayload{"blocks": {Text: w1, FileCount: 1}}

	out := captureStderr(t, func() {
		_, _, err := buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.NoError(t, err)
	})
	assert.Equal(t, 2, strings.Count(out, overshootMarker), out)
	assert.Contains(t, out, fmt.Sprintf(`atcr: warning: agents "greta", "greta2", "greta3": chunk 1 of 1 is %d bytes, over its %d-byte chunk budget;`, len(w1), budget), out)
	assert.Contains(t, out, overshootLine("narrow", 1, 1, len(w1), otherBudget), out)
}

func TestChunkExceedsByteBudget(t *testing.T) {
	assert.True(t, payload.ChunkExceedsByteBudget(101, 100))
	assert.False(t, payload.ChunkExceedsByteBudget(100, 100), "a chunk exactly at its budget fits")
	assert.False(t, payload.ChunkExceedsByteBudget(1<<20, 0), "0 means no budget applies")
	assert.False(t, payload.ChunkExceedsByteBudget(1<<20, 1), "1 is the scope-block floor, not a real budget")
	assert.True(t, payload.ChunkExceedsByteBudget(3, 2), "2 is the smallest budget that is checked")
}

// A preparation that fails must not print the overshoot warnings it collected
// before the failure: no slot it described is ever dispatched.
func TestBuildSlots_ChunkByteOvershoot_NotPrintedWhenPrepareFails(t *testing.T) {
	cfg, _, _ := overshootFleet(t, 2)
	cfg.Project.Agents = append(cfg.Project.Agents, "ghost") // not in the registry
	w1 := wideLineSeg("w1.go", 600, 200)
	payloads := map[string]modePayload{"blocks": {Text: w1, FileCount: 1}}

	out := captureStderr(t, func() {
		_, _, err := buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.ErrorContains(t, err, `agent "ghost" not found`)
	})
	assert.NotContains(t, out, overshootMarker, "a failed prepare must not warn about chunks it never dispatches:\n%s", out)
}

// chunkPrefixBytes sums the first prefixLines lines of a chunk and clamps a
// prefixLines past the chunk's last line instead of slicing out of range.
func TestChunkPrefixBytes(t *testing.T) {
	cases := []struct {
		name        string
		chunk       string
		prefixLines int
		want        int
	}{
		{"no preamble", "diff --git a/x b/x\n+x\n", 0, 0},
		{"two preamble lines", "p1\np2\ndiff --git a/x b/x\n", 2, len("p1\np2\n")},
		{"preamble only", "p1\np2", 2, len("p1\np2")},
		{"prefixLines past the last line", "p1\np2\n", 5, len("p1\np2\n")},
		{"empty chunk", "", 3, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, chunkPrefixBytes(tc.chunk, tc.prefixLines))
		})
	}
}

// warn with no agents prints nothing rather than indexing agents[0].
func TestByteOvershootWarn_NoAgentsPrintsNothing(t *testing.T) {
	out := captureStderr(t, func() {
		byteOvershoot{chunk: 1, chunks: 1, diffBytes: 10, budget: 5}.warn(nil)
	})
	assert.Empty(t, out)
}
