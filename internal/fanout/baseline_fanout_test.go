package fanout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baselineEntry builds a FileEntry whose Size drives partitionByBudget and whose
// Body carries a unique, greppable marker so a test can prove every chunk's
// content reached a slot (no file silently omitted across the fan-out).
func baselineEntry(path string, size int) payload.FileEntry {
	marker := fmt.Sprintf("// FILE:%s\n", path)
	pad := size - len(marker)
	if pad < 0 {
		pad = 0
	}
	body := marker + strings.Repeat("x", pad)
	return payload.FileEntry{Path: path, Size: int64(len(body)), Body: body}
}

// baselinePayloads assembles the single "files"-mode modePayload a baseline
// (--all / --dir) scan hands to buildSlots: the UNBUDGETED per-file entries plus
// the whole-repo concatenation used for the audit artifact / empty-payload guard.
func baselinePayloads(entries []payload.FileEntry) map[string]modePayload {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Body)
	}
	return map[string]modePayload{
		string(payload.ModeFiles): {Entries: entries, Text: b.String(), FileCount: len(entries)},
	}
}

// slotsContainMarker reports whether any of the given slots' rendered primary
// prompt carries the file marker — used to assert coverage across the fan-out.
func slotsContainMarker(slots []Slot, path string) bool {
	marker := fmt.Sprintf("// FILE:%s\n", path)
	for _, s := range slots {
		if strings.Contains(s.Primary.Prompt, marker) {
			return true
		}
	}
	return false
}

// AC 06-01 Happy Path 1: a C-chunk, P-persona baseline scan builds exactly C×P
// slots — one per (persona × chunk) — each keeping its persona's unchanged plain
// name (the collapse key mergeChunkResults and the 14.2 consensus filter depend
// on), with every chunk's content reaching every persona.
func TestBaselineSlots_ChunkPersonaCartesian(t *testing.T) {
	cfg := twoAgentConfig("http://unused") // greta + kai
	cfg.Settings.PayloadByteBudget = 100   // small global cap forces per-file chunks
	entries := []payload.FileEntry{        // three 100-byte files → three chunks
		baselineEntry("a.go", 100),
		baselineEntry("b.go", 100),
		baselineEntry("c.go", 100),
	}
	payloads := baselinePayloads(entries)

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Len(t, slots, 6, "3 chunks × 2 personas = 6 slots (never 3+2)")

	perPersona := map[string]int{}
	for _, s := range slots {
		perPersona[s.Primary.Name]++
	}
	assert.Equal(t, 3, perPersona["greta"], "greta gets one slot per chunk")
	assert.Equal(t, 3, perPersona["kai"], "kai gets one slot per chunk")

	// Every chunk's content reached the fan-out (no file silently dropped).
	for _, p := range []string{"a.go", "b.go", "c.go"} {
		assert.True(t, slotsContainMarker(slots, p), "file %s missing from every slot", p)
	}
}

// AC 06-01 Happy Path 2: a single-chunk baseline scan (small --dir subtree)
// degrades to the existing one-slot-per-persona bulk path — no chunk fan-out
// overhead when everything fits one chunk.
func TestBaselineSlots_SingleChunkDegradesToOnePerPersona(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = 0 // unlimited → whole payload fits one chunk
	entries := []payload.FileEntry{
		baselineEntry("a.go", 20),
		baselineEntry("b.go", 20),
	}
	payloads := baselinePayloads(entries)

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Len(t, slots, 2, "one slot per persona when the scan is a single chunk")
	perPersona := map[string]int{}
	for _, s := range slots {
		perPersona[s.Primary.Name]++
	}
	assert.Equal(t, 1, perPersona["greta"])
	assert.Equal(t, 1, perPersona["kai"])
}

// AC 06-01 Edge Case 1: each of a persona's chunk-slots resolves its fallback
// chain independently, and every fallback reviews the SAME chunk as the primary
// it substitutes for — never a different chunk.
func TestBaselineSlots_FallbackChainPerChunk(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}} // kai is a fallback only
	cfg.Settings.PayloadByteBudget = 100
	g := cfg.Registry.Agents["greta"]
	g.Fallback = "kai"
	cfg.Registry.Agents["greta"] = g

	entries := []payload.FileEntry{
		baselineEntry("a.go", 100),
		baselineEntry("b.go", 100),
		baselineEntry("c.go", 100),
	}
	payloads := baselinePayloads(entries)

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Len(t, slots, 3, "one persona × three chunks")
	for i, s := range slots {
		require.NotEmpty(t, s.Fallbacks, "slot %d has no fallback chain", i)
		// The fallback must review the SAME chunk as its primary: the primary and its
		// first fallback carry the identical payload text (they differ only by model),
		// so the fallback's prompt must contain the primary's chunk marker.
		assert.Equal(t, s.Primary.PayloadMode, s.Fallbacks[0].PayloadMode)
		primaryHasA := strings.Contains(s.Primary.Prompt, "// FILE:a.go\n")
		fbHasA := strings.Contains(s.Fallbacks[0].Prompt, "// FILE:a.go\n")
		assert.Equal(t, primaryHasA, fbHasA, "fallback reviews a different chunk than its primary in slot %d", i)
	}
}

// AC 06-01 Edge Case 2 (slot side): a serial-lane baseline persona's chunk-slots
// all carry Serial=true under the persona's unchanged plain name, so the caller's
// serialAgents map (keyed by plain name) records the persona once and
// mergeResultGroup applies sum-of-chunks duration semantics.
func TestBaselineSlots_SerialLanePerChunk(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	// greta runs in the serial lane only (Agents and SerialAgents are disjoint lanes).
	cfg.Project = &registry.ProjectConfig{SerialAgents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 100
	entries := []payload.FileEntry{
		baselineEntry("a.go", 100),
		baselineEntry("b.go", 100),
		baselineEntry("c.go", 100),
	}
	payloads := baselinePayloads(entries)

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Len(t, slots, 3)
	for i, s := range slots {
		assert.True(t, s.Serial, "chunk-slot %d of a serial persona must be serial", i)
		assert.Equal(t, "greta", s.Primary.Name, "serial chunk-slot keeps the plain persona name")
	}
}

// AC 06-01 Error Scenario 1: an unknown agent in the roster aborts baseline
// slot-building fail-fast, before any chunk is dispatched, with the same error
// buildSlots raises for diff-mode reviews.
func TestBaselineSlots_UnknownAgentFailFast(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"ghost"}}
	cfg.Settings.PayloadByteBudget = 100
	entries := []payload.FileEntry{baselineEntry("a.go", 100), baselineEntry("b.go", 100)}
	payloads := baselinePayloads(entries)

	_, _, err := buildSlots(cfg, payloads, ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `agent "ghost" not found in registry`)
}

// AC 06-01 Error Scenario 2: a chunk count exceeding maxChunksPerAgent is capped
// (not left to spawn an unbounded slot count) while every file is still delivered
// — mirroring chunkDiff's coalesce-into-final-chunk ceiling behavior.
func TestBaselineSlots_ChunkCapBounded(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 100
	// One file per chunk × (cap + 1) files → partitionByBudget would yield cap+1 chunks.
	n := maxChunksPerAgent + 1
	var entries []payload.FileEntry
	for i := 0; i < n; i++ {
		entries = append(entries, baselineEntry(fmt.Sprintf("f%03d.go", i), 100))
	}
	payloads := baselinePayloads(entries)

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Len(t, slots, maxChunksPerAgent, "slot count is capped at maxChunksPerAgent, not unbounded")
	// No file dropped: the final coalesced chunk still carries the overflow files.
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("f%03d.go", i)
		assert.True(t, slotsContainMarker(slots, p), "capped fan-out dropped file %s", p)
	}
}

// baselineRoutingCompleter returns a distinct finding per chunk keyed on the file
// marker the chunk prompt carries, so an end-to-end run can prove every chunk was
// actually dispatched (its marker finding present in the raw per-slot results).
type baselineRoutingCompleter struct{}

func (baselineRoutingCompleter) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	for _, p := range []string{"a.go", "b.go", "c.go"} {
		if strings.Contains(inv.Prompt, fmt.Sprintf("// FILE:%s\n", p)) {
			return fmt.Sprintf("HIGH|%s:1|problem %s|fix %s|security|10|evidence %s", p, p, p, p), nil
		}
	}
	return "", nil
}

// AC 06-01 (integration): every baseline chunk is actually dispatched to every
// persona — each chunk's marker finding appears in the raw per-slot results before
// the merge, proving no chunk was silently skipped.
func TestBaselineSlots_EndToEnd_EveryChunkDispatched(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = 100
	entries := []payload.FileEntry{
		baselineEntry("a.go", 100),
		baselineEntry("b.go", 100),
		baselineEntry("c.go", 100),
	}
	payloads := baselinePayloads(entries)

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Len(t, slots, 6)

	raw := NewEngine(baselineRoutingCompleter{}).Run(context.Background(), slots)
	require.Len(t, raw, 6, "one raw result per (persona × chunk) slot")

	// Each persona's three chunk findings are all present (union across its slots).
	perPersona := map[string]map[string]bool{"greta": {}, "kai": {}}
	for _, r := range raw {
		for _, p := range []string{"a.go", "b.go", "c.go"} {
			if strings.Contains(r.Content, p+":1") {
				perPersona[r.Agent][p] = true
			}
		}
	}
	for _, agent := range []string{"greta", "kai"} {
		for _, p := range []string{"a.go", "b.go", "c.go"} {
			assert.True(t, perPersona[agent][p], "%s missing chunk finding for %s (chunk not dispatched)", agent, p)
		}
	}
}

// -----------------------------------------------------------------------------
// AC 06-02: Per-persona chunk results collapse into ONE source artifact.
// These prove the baseline (persona × chunk) slots flow through the EXISTING,
// unmodified mergeChunkResults/writePool collapse — no baseline-specific merge
// logic (Story 06-02's "reuse verbatim" thesis).
// -----------------------------------------------------------------------------

// baselineChunkFindingCompleter returns one finding per file marker present in a
// chunk's prompt, so a persona fanned across N chunks yields a distinct,
// chunk-attributable finding for every file it reviewed.
type baselineChunkFindingCompleter struct{}

func (baselineChunkFindingCompleter) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	var out []string
	for _, ln := range strings.Split(inv.Prompt, "\n") {
		if p, ok := strings.CutPrefix(ln, "// FILE:"); ok {
			out = append(out, fmt.Sprintf("HIGH|%s:1|problem %s|fix %s|security|10|evidence %s", p, p, p, p))
		}
	}
	return strings.Join(out, "\n"), nil
}

// baselinePartialFailCompleter errors for the chunk whose prompt carries failMarker
// and returns a finding for every other chunk — exercising one-chunk-fails-others-
// succeed collapse semantics.
type baselinePartialFailCompleter struct{ failMarker string }

func (c baselinePartialFailCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	if strings.Contains(inv.Prompt, "// FILE:"+c.failMarker+"\n") {
		return "", fmt.Errorf("simulated chunk failure for %s", c.failMarker)
	}
	return baselineChunkFindingCompleter{}.Complete(ctx, inv)
}

// AC 06-02 Happy Path 1+2 / Edge Case 4 / Error Scenario 1: a persona fanned across
// 5 baseline chunks collapses into exactly ONE source directory whose findings.txt
// carries all 5 chunk-originated findings (each attributable to its file), attributed
// to the plain persona name. The pre-merge writePool duplicate-directory guard proves
// the merge — not writePool — is what enforces one-source-per-persona.
func TestBaselineMerge_FiveChunksCollapseToOneSource(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 100
	files := []string{"a.go", "b.go", "c.go", "d.go", "e.go"}
	var entries []payload.FileEntry
	for _, f := range files {
		entries = append(entries, baselineEntry(f, 100))
	}
	payloads := baselinePayloads(entries)

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Len(t, slots, 5, "one persona × five chunks")

	raw := NewEngine(baselineChunkFindingCompleter{}).Run(context.Background(), slots)
	require.Len(t, raw, 5, "engine returns one result per chunk slot, all named greta")

	// ES1 regression: without the merge, five same-named results collide on the
	// persona's on-disk directory — writePool's duplicate-dir guard proves it.
	_, dupErr := WritePool(filepath.Join(t.TempDir(), "pool"), raw, nil)
	require.Error(t, dupErr)
	assert.Contains(t, dupErr.Error(), "duplicate agent directory")

	merged := mergeChunkResults(raw)
	require.Len(t, merged, 1, "five chunks collapse into a single persona source")

	pool := filepath.Join(t.TempDir(), "sources", "pool")
	sum, err := WritePool(pool, merged, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, sum.Total, "one logical reviewer, not five")

	agentDirs, err := os.ReadDir(filepath.Join(pool, "raw", "agent"))
	require.NoError(t, err)
	require.Len(t, agentDirs, 1, "exactly one per-persona directory (not chunkCount × personaCount)")
	assert.Equal(t, "greta", agentDirs[0].Name())

	data, err := os.ReadFile(filepath.Join(pool, "findings.txt"))
	require.NoError(t, err)
	res, err := stream.ParseSource(data)
	require.NoError(t, err)
	require.Len(t, res.Findings, 5, "every chunk's finding survives the merge (union, none dropped)")
	gotFiles := map[string]bool{}
	for _, f := range res.Findings {
		gotFiles[f.File] = true
		assert.Equal(t, "greta", f.Reviewer, "findings attribute to the persona, never a chunk id")
	}
	for _, f := range files {
		assert.True(t, gotFiles[f], "chunk origin %s not diagnosable in the merged source", f)
	}
}

// AC 06-02 Edge Case 1: one chunk fails while the others succeed for the same
// persona — the merged Result is StatusOK (any-chunk-succeeded), the successful
// chunks' findings are preserved, and the failure is surfaced via UnreviewedChunks
// rather than silently sinking the persona's whole contribution.
func TestBaselineMerge_OneChunkFailsOthersSucceed(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 100
	files := []string{"a.go", "b.go", "c.go", "d.go", "e.go"}
	var entries []payload.FileEntry
	for _, f := range files {
		entries = append(entries, baselineEntry(f, 100))
	}
	payloads := baselinePayloads(entries)

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Len(t, slots, 5)

	raw := NewEngine(baselinePartialFailCompleter{failMarker: "c.go"}).Run(context.Background(), slots)
	merged := mergeChunkResults(raw)
	require.Len(t, merged, 1)
	assert.Equal(t, StatusOK, merged[0].Status, "any-chunk-succeeded keeps the persona OK")
	assert.Equal(t, 1, merged[0].UnreviewedChunks, "the one failed chunk is surfaced, not swallowed")

	// merged Content is the newline-joined union of the succeeded chunks' outputs
	// (the failed chunk contributes empty content, dropped by mergeResultGroup).
	var findingLines int
	for _, ln := range strings.Split(strings.TrimSpace(merged[0].Content), "\n") {
		if strings.Contains(ln, "|") {
			findingLines++
		}
	}
	assert.Equal(t, 4, findingLines, "the four succeeded chunks' findings are preserved")
}

// AC 06-02 Edge Case 2+3: partial fallback across a persona's chunks unions
// FallbackUsed/FallbackFrom and picks the modal FallbackModel, and token/telemetry
// accumulate across every chunk — the existing mergeResultGroup contract, unchanged
// for baseline provenance. Uses hand-built (persona × chunk) results to pin the
// merge semantics directly.
func TestBaselineMerge_FallbackUnionAndTelemetryAccumulate(t *testing.T) {
	g := []Result{
		{Agent: "greta", Status: StatusOK, Content: "HIGH|a.go:1|p|f|security|10|e", TokensIn: 100, TokensOut: 10, Turns: 1, ToolCalls: 2, ToolBytes: 50},
		{Agent: "greta", Status: StatusOK, Content: "HIGH|b.go:1|p|f|security|10|e", TokensIn: 200, TokensOut: 20, Turns: 1, ToolCalls: 3, ToolBytes: 70, FallbackUsed: true, FallbackFrom: "greta", FallbackModel: "m-fb1"},
		{Agent: "greta", Status: StatusOK, Content: "HIGH|c.go:1|p|f|security|10|e", TokensIn: 300, TokensOut: 30, Turns: 1, ToolCalls: 1, ToolBytes: 30, FallbackUsed: true, FallbackFrom: "greta", FallbackModel: "m-fb1"},
	}
	merged := mergeChunkResults(g)
	require.Len(t, merged, 1)
	m := merged[0]
	assert.True(t, m.FallbackUsed, "partial fallback across chunks records one fallback slot")
	assert.Equal(t, "m-fb1", m.FallbackModel, "modal fallback model wins across chunks")
	assert.Equal(t, 600, m.TokensIn, "tokens in accumulate across all chunks")
	assert.Equal(t, 60, m.TokensOut, "tokens out accumulate across all chunks")
	assert.Equal(t, 6, m.ToolCalls, "tool calls accumulate across all chunks")
	assert.Equal(t, int64(150), m.ToolBytes, "tool bytes accumulate across all chunks")
}
