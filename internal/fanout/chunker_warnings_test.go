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
