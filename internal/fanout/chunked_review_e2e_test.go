package fanout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// promptRoutingCompleter returns a distinct finding per chunk, keyed on which
// file's diff the chunk prompt carries — so each of a persona's chunk calls
// yields its own finding and the end-to-end merge can be checked for
// completeness (no chunk's findings are lost) and attribution.
type promptRoutingCompleter struct{}

func (promptRoutingCompleter) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	switch {
	case strings.Contains(inv.Prompt, "a/a.go"):
		return "HIGH|a.go:1|problem A|fix A|security|10|evidence A", nil
	case strings.Contains(inv.Prompt, "a/b.go"):
		return "MEDIUM|b.go:2|problem B|fix B|security|10|evidence B", nil
	default:
		return "", nil
	}
}

// Epic 14.3 (AC4): a persona fanned out into multiple chunks must reach the
// reconciler as ONE source — a single raw/agent/<persona>/ directory whose every
// finding is attributed to Reviewer=<persona> (the plain name the merged 14.2
// consensus filter counts), with no finding lost across chunk boundaries.
func TestChunkedReview_EndToEnd_SingleSourcePerPersona(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	mcl := 5 // tiny budget so the two-file diff splits into two chunks
	g := cfg.Registry.Agents["greta"]
	g.MaxContextLines = &mcl
	cfg.Registry.Agents["greta"] = g

	diff := fileSeg("a.go", 6) + fileSeg("b.go", 6)
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 2}}

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.Len(t, slots, 2, "one persona bin-packs into two chunk slots")

	raw := NewEngine(promptRoutingCompleter{}).Run(context.Background(), slots)
	require.Len(t, raw, 2, "engine returns one result per chunk slot, both named greta")

	// The merge is load-bearing: without it the two same-named results collide on
	// the persona's on-disk dir — WritePool's duplicate-dir guard proves it.
	_, dupErr := WritePool(filepath.Join(t.TempDir(), "pool"), raw, nil)
	require.Error(t, dupErr)
	require.Contains(t, dupErr.Error(), "duplicate agent directory")

	merged := mergeChunkResults(raw)
	require.Len(t, merged, 1, "chunks collapse to a single persona source")

	pool := filepath.Join(t.TempDir(), "sources", "pool")
	sum, err := WritePool(pool, merged, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, sum.Total, "one logical reviewer, not two")
	assert.Equal(t, 1, sum.Succeeded)

	// Exactly one agent directory — no per-chunk dirs leaked.
	entries, err := os.ReadDir(filepath.Join(pool, "raw", "agent"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "greta", entries[0].Name())

	// Both chunks' findings survive and are attributed to the plain persona name.
	data, err := os.ReadFile(filepath.Join(pool, "findings.txt"))
	require.NoError(t, err)
	res, err := stream.ParseSource(data)
	require.NoError(t, err)
	require.Len(t, res.Findings, 2, "no chunk's findings are lost in the merge")
	for _, f := range res.Findings {
		assert.Equal(t, "greta", f.Reviewer, "chunk findings attribute to the persona, never a chunk id")
	}
	locs := []string{
		fmt.Sprintf("%s:%d", res.Findings[0].File, res.Findings[0].Line),
		fmt.Sprintf("%s:%d", res.Findings[1].File, res.Findings[1].Line),
	}
	assert.ElementsMatch(t, []string{"a.go:1", "b.go:2"}, locs)
}
