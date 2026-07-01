package fanout

import (
	"errors"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/require"
)

// Epic 14.3 (AC3): the chunked strategy expands one persona into one slot per
// bin-packed chunk, each carrying a different slice of the diff but the SAME
// persona name (so results merge back to a single source).

func TestBuildSlots_ChunkedExpandsPersonaIntoPerChunkSlots(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	mcl := 5 // tiny budget so a two-file diff (≈10 lines each) splits
	g := cfg.Registry.Agents["greta"]
	g.MaxContextLines = &mcl
	cfg.Registry.Agents["greta"] = g

	diff := fileSeg("a.go", 6) + fileSeg("b.go", 6)
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 2}}

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.Len(t, slots, 2, "one persona bin-packs into two chunk slots")
	require.Equal(t, "greta", slots[0].Primary.Name)
	require.Equal(t, "greta", slots[1].Primary.Name, "chunk slots keep the persona name for single-source attribution (AC4)")
	// Each chunk sees exactly one of the two files.
	require.Contains(t, slots[0].Primary.Invocation.Prompt, "a/a.go")
	require.NotContains(t, slots[0].Primary.Invocation.Prompt, "a/b.go")
	require.Contains(t, slots[1].Primary.Invocation.Prompt, "a/b.go")
	require.NotContains(t, slots[1].Primary.Invocation.Prompt, "a/a.go")
	// Distinct diffs must produce distinct cache keys so chunks are cached apart.
	require.NotEqual(t, slots[0].Primary.CacheKey, slots[1].Primary.CacheKey)
}

func TestBuildSlots_BulkEmitsOneSlotPerAgent(t *testing.T) {
	cfg := twoAgentConfig("http://unused") // ReviewStrategy "" => bulk
	diff := fileSeg("a.go", 50) + fileSeg("b.go", 50)
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 2}}

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.Len(t, slots, 2, "bulk: one slot for greta, one for kai — no chunk expansion")
	require.Equal(t, "greta", slots[0].Primary.Name)
	require.Equal(t, "kai", slots[1].Primary.Name)
}

func TestBuildSlots_ChunkedSingleChunkStaysOneSlot(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	// Default max_context_lines (1500) dwarfs this tiny diff => a single chunk.
	diff := fileSeg("a.go", 3) + fileSeg("b.go", 3)
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 2}}

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.Len(t, slots, 1, "chunked but a single chunk => one slot, nothing to merge")
	require.Equal(t, "greta", slots[0].Primary.Name)
}

// Epic 14.3 (AC4): N chunk results for one persona merge into a single source
// with every finding attributed to Reviewer=<persona> — the plain persona name
// the 14.2 consensus filter counts, NOT a per-chunk id.

func TestMergeChunkResults_SinglePersonaSingleReviewer(t *testing.T) {
	results := []Result{
		{Agent: "greta", Status: StatusOK, Content: "HIGH|a.go:1|problem A|fix A|CORRECTNESS", TokensIn: 10, TokensOut: 5},
		{Agent: "greta", Status: StatusOK, Content: "MEDIUM|b.go:2|problem B|fix B|CORRECTNESS", TokensIn: 20, TokensOut: 7},
	}
	merged := mergeChunkResults(results)
	require.Len(t, merged, 1, "two chunks of one persona collapse to a single source")
	require.Equal(t, "greta", merged[0].Agent)
	require.Equal(t, StatusOK, merged[0].Status)
	require.Equal(t, 30, merged[0].TokensIn, "token usage accumulates across chunks")
	require.Equal(t, 12, merged[0].TokensOut)

	fr := findingsFor(merged[0], nil) // nil changed => grounding disabled, keep all
	require.Len(t, fr.Findings, 2, "findings from BOTH chunks survive the merge")
	for _, f := range fr.Findings {
		require.Equal(t, "greta", f.Reviewer, "every chunk finding is attributed to the persona, not a chunk id")
	}
}

func TestMergeChunkResults_BulkIsNoOp(t *testing.T) {
	results := []Result{
		{Agent: "greta", Status: StatusOK, Content: "HIGH|a.go:1|p|f|CORRECTNESS"},
		{Agent: "kai", Status: StatusOK, Content: "LOW|b.go:2|p|f|CORRECTNESS"},
	}
	merged := mergeChunkResults(results)
	require.Len(t, merged, 2, "distinct persona names are left untouched")
	require.Equal(t, "greta", merged[0].Agent)
	require.Equal(t, "kai", merged[1].Agent)
}

func TestMergeChunkResults_PartialSuccessIsOK(t *testing.T) {
	results := []Result{
		{Agent: "greta", Status: StatusOK, Content: "HIGH|a.go:1|p|f|CORRECTNESS"},
		{Agent: "greta", Status: StatusFailed, Err: errors.New("boom")},
	}
	merged := mergeChunkResults(results)
	require.Len(t, merged, 1)
	require.Equal(t, StatusOK, merged[0].Status, "a persona that produced findings from >=1 chunk is a success")
	fr := findingsFor(merged[0], nil)
	require.Len(t, fr.Findings, 1)
}

func TestMergeChunkResults_AllFailedStaysFailed(t *testing.T) {
	results := []Result{
		{Agent: "greta", Status: StatusFailed, Err: errors.New("first")},
		{Agent: "greta", Status: StatusTimeout, Err: errors.New("second")},
	}
	merged := mergeChunkResults(results)
	require.Len(t, merged, 1)
	require.NotEqual(t, StatusOK, merged[0].Status, "no OK chunk => not a success")
	require.Error(t, merged[0].Err)
}

func TestMergeChunkResults_ParallelAgentTakesMaxDuration(t *testing.T) {
	results := []Result{
		{Agent: "kai", Status: StatusOK, Content: "HIGH|a.go:1|p|f|CORRECTNESS", DurationMS: 100},
		{Agent: "kai", Status: StatusOK, Content: "MEDIUM|b.go:2|p|f|CORRECTNESS", DurationMS: 150},
	}
	merged := mergeChunkResults(results) // no serial agents => parallel lane
	require.Len(t, merged, 1)
	require.Equal(t, int64(150), merged[0].DurationMS, "parallel-lane chunks run concurrently so wall-clock is the max")
}

func TestMergeChunkResults_SerialAgentSumsDuration(t *testing.T) {
	results := []Result{
		{Agent: "greta", Status: StatusOK, Content: "HIGH|a.go:1|p|f|CORRECTNESS", DurationMS: 100},
		{Agent: "greta", Status: StatusOK, Content: "MEDIUM|b.go:2|p|f|CORRECTNESS", DurationMS: 150},
	}
	serialAgents := map[string]bool{"greta": true}
	merged := mergeChunkResults(results, serialAgents)
	require.Len(t, merged, 1)
	require.Equal(t, int64(250), merged[0].DurationMS, "serial-lane chunks run sequentially so wall-clock is the sum")
}
