package fanout

import (
	"errors"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/require"
)

// captureStderr (defined in engine_degrade_test.go) redirects os.Stderr; it is
// not parallel-safe, so these tests must not call t.Parallel().

// Independent-review MEDIUM #1: a chunked run whose diff is a SINGLE oversized
// file must still emit the oversized-file warning, even though chunkDiff returns
// it as one chunk and it falls through to the one-slot path.
func TestBuildSlots_ChunkedWarnsOnLoneOversizedFile(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	mcl := 5
	g := cfg.Registry.Agents["greta"]
	g.MaxContextLines = &mcl
	cfg.Registry.Agents["greta"] = g

	diff := fileSeg("big.go", 40) // one file, ~44 lines >> 5
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 1}}

	var slots []Slot
	out := captureStderr(t, func() {
		var err error
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.NoError(t, err)
	})
	require.Len(t, slots, 1, "a lone oversized file is one slot (cannot be split)")
	require.Contains(t, out, "exceeds max_context_lines", "the oversized-file warning must fire for a lone huge file")
	require.Contains(t, out, "greta")
}

// TD 19.10 (chunker.go:130): once the maxChunksPerAgent ceiling is reached the
// final chunk absorbs EVERY remaining file and can far exceed max_context_lines —
// a MULTI-file oversized chunk. The pre-dispatch warning loop only flagged a LONE
// oversized file (fileCount == 1), so the ceiling-induced oversized chunk shipped
// silently, breaking the "each chunk fits the window" invariant with no signal.
// It must now be flagged pre-dispatch (distinct "ceiling" wording) so an operator
// knows the final chunk may overflow before the call is made.
func TestBuildSlots_ChunkedWarnsOnCeilingOversizedChunk(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	mcl := 1
	g := cfg.Registry.Agents["greta"]
	g.MaxContextLines = &mcl
	cfg.Registry.Agents["greta"] = g

	// 70 single-line files with mcl=1 forces one chunk per file until the 64-chunk
	// ceiling is hit; files 64..70 then coalesce into one oversized final chunk.
	var b strings.Builder
	for i := 0; i < 70; i++ {
		b.WriteString(fileSeg("f"+itoa(i)+".go", 1))
	}
	payloads := map[string]modePayload{"blocks": {Text: b.String(), FileCount: 70}}

	var slots []Slot
	out := captureStderr(t, func() {
		var err error
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.NoError(t, err)
	})
	require.Len(t, slots, 64, "the slot count is capped at the maxChunksPerAgent ceiling")
	require.Contains(t, out, "ceiling", "the ceiling-induced multi-file oversized chunk must be flagged pre-dispatch")
}

func TestBuildSlots_ChunkedSuppressesOversizeWarning(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	mcl := 5
	g := cfg.Registry.Agents["greta"]
	g.MaxContextLines = &mcl
	cfg.Registry.Agents["greta"] = g

	diff := fileSeg("big.go", 40) // one file, ~44 lines >> 5
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 1}}

	out := captureStderr(t, func() {
		_, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", false)
		require.NoError(t, err)
	})
	require.NotContains(t, out, "exceeds max_context_lines", "the oversized-file warning must be suppressed on the resume rebuild path")
}

// A chunked run over a NON-diff, multi-file payload (files mode: sentinel-
// delimited content with no `diff --git` markers) must not (a) mislabel the whole
// payload as "a single file's diff" when it exceeds max_context_lines, nor
// (b) silently pretend the chunked strategy applied — chunkDiff cannot split a
// payload without diff markers, so it is a no-op that the operator should see.
func TestBuildSlots_ChunkedFilesModeNoMisleadingWarning(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	mcl := 5
	g := cfg.Registry.Agents["greta"]
	g.MaxContextLines = &mcl
	cfg.Registry.Agents["greta"] = g

	// Three "files", no diff --git markers, ~15 lines total (>> mcl=5).
	var b strings.Builder
	for _, f := range []string{"a.go", "b.go", "c.go"} {
		b.WriteString("=== FILE: " + f + " ===\n")
		for i := 0; i < 4; i++ {
			b.WriteString("+content\n")
		}
	}
	payloads := map[string]modePayload{"blocks": {Text: b.String(), FileCount: 3}}

	out := captureStderr(t, func() {
		_, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
		require.NoError(t, err)
	})
	require.NotContains(t, out, "exceeds max_context_lines",
		"a multi-file non-diff payload must not be mislabeled as a single oversized file")
	require.Contains(t, out, "no effect",
		"chunked strategy on a non-diff payload must warn it had no effect")
}

// Independent-review MEDIUM #2: a persona whose chunks partially failed reports
// OK (it contributed findings) but must surface the unreviewed portion.
func TestMergeChunkResults_PartialFailureWarns(t *testing.T) {
	results := []Result{
		{Agent: "greta", Status: StatusOK, Content: "HIGH|a.go:1|p|f|CORRECTNESS"},
		{Agent: "greta", Status: StatusFailed, Err: errors.New("boom")},
	}
	var merged []Result
	out := captureStderr(t, func() {
		merged = mergeChunkResults(results)
	})
	require.Len(t, merged, 1)
	require.Equal(t, StatusOK, merged[0].Status)
	require.Contains(t, out, "chunk(s) failed", "partial chunk failure must be surfaced")
	require.Contains(t, out, "not reviewed")
}

// A fully-successful chunked persona must NOT emit the partial-failure warning.
func TestMergeChunkResults_AllSuccessNoWarning(t *testing.T) {
	results := []Result{
		{Agent: "greta", Status: StatusOK, Content: "HIGH|a.go:1|p|f|CORRECTNESS"},
		{Agent: "greta", Status: StatusOK, Content: "LOW|b.go:2|p|f|CORRECTNESS"},
	}
	out := captureStderr(t, func() {
		_ = mergeChunkResults(results)
	})
	require.NotContains(t, out, "not reviewed", "no warning when every chunk succeeded")
}

// TD 14.3: a partial-coverage persona reports StatusOK but must record a
// MACHINE-READABLE signal of the unreviewed portion, so a CI gate reading
// summary.json can react instead of trusting the green status. The stderr warning
// alone is not enough (nothing downstream parses stderr).
func TestMergeChunkResults_PartialFailureRecordsUnreviewedChunks(t *testing.T) {
	results := []Result{
		{Agent: "greta", Status: StatusOK, Content: "HIGH|a.go:1|p|f|CORRECTNESS"},
		{Agent: "greta", Status: StatusFailed, Err: errors.New("boom")},
		{Agent: "greta", Status: StatusFailed, Err: errors.New("boom2")},
	}
	merged := mergeChunkResults(results)
	require.Len(t, merged, 1)
	require.Equal(t, StatusOK, merged[0].Status, "a partial persona still reports OK (it contributed findings)")
	require.Equal(t, 2, merged[0].UnreviewedChunks, "the two failed chunks must be recorded as unreviewed")
}

// A fully-successful chunked persona records zero unreviewed chunks so omitempty
// keeps summary.json byte-identical to the pre-field shape.
func TestMergeChunkResults_FullSuccessNoUnreviewedChunks(t *testing.T) {
	results := []Result{
		{Agent: "greta", Status: StatusOK, Content: "HIGH|a.go:1|p|f|CORRECTNESS"},
		{Agent: "greta", Status: StatusOK, Content: "LOW|b.go:2|p|f|CORRECTNESS"},
	}
	merged := mergeChunkResults(results)
	require.Equal(t, 0, merged[0].UnreviewedChunks, "full coverage records zero unreviewed chunks")
}

// statusFor must thread UnreviewedChunks from the merged Result onto AgentStatus
// so it lands in summary.json (PoolSummary.Agents) and per-agent status.json.
func TestStatusFor_SurfacesUnreviewedChunks(t *testing.T) {
	st := statusFor(Result{Agent: "greta", Status: StatusOK, UnreviewedChunks: 2}, findingsResult{})
	require.Equal(t, 2, st.UnreviewedChunks, "partial-coverage count must surface in AgentStatus")
}
