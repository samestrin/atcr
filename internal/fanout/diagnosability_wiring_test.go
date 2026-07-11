package fanout

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildSlots_BulkAgentCarriesSizingRecord proves the REAL source of the F8
// diagnosability fields: buildSlots' bulk path threads each agent's own per-model
// budget/window and its degradation action onto the primary Agent (via
// renderAgent). Uses the oversized payload so the 32k agent actually sheds, making
// its degradation_action "truncate".
func TestBuildSlots_BulkAgentCarriesSizingRecord(t *testing.T) {
	cfg := sizingRosterConfig() // greta → unlisted-small-model (32768 window), PayloadByteBudget 0
	payloads := oversizedBlocksPayload()
	rng := ReviewRange{Base: "a", Head: "b"}

	small, _, err := buildOneAgent(cfg, "greta", payloads, rng, "", "")
	require.NoError(t, err)

	// Window + budget are this model's own, matching the payload package's own
	// resolution (no independent recomputation, no global cap since budget is 0).
	assert.Equal(t, payload.ContextWindowTokens("unlisted-small-model"), small.ResolvedWindow)
	assert.Equal(t, payload.EffectiveByteBudget("unlisted-small-model", defaultMaxTokens), small.EffectiveBudget)
	assert.Equal(t, defaultMaxTokens, small.ReservedOutputTokens, "a sized agent reserves the output cap")
	// The oversized payload was shed to fit the 32k window → a lossy degradation.
	assert.True(t, small.Truncation.Truncated, "precondition: the 32k agent sheds this payload")
	assert.Equal(t, "truncate", small.DegradationAction, "a per-agent byte shed records degradation_action=truncate")
	assert.Zero(t, small.ChunkTotal, "the bulk path is unchunked")
}

// TestBuildSlots_ChunkedAgentCarriesChunkRecord proves the chunked path threads
// the persona's full chunk count and degradation_action="chunk" onto EVERY
// chunk-Slot's Agent, plus the chunk-line regime used later by the cache token /
// fallback (chunkMaxLines).
func TestBuildSlots_ChunkedAgentCarriesChunkRecord(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	mcl := 5 // tiny budget so a two-file diff splits into 2 chunks
	g := cfg.Registry.Agents["greta"]
	g.MaxContextLines = &mcl
	cfg.Registry.Agents["greta"] = g

	diff := fileSeg("a.go", 6) + fileSeg("b.go", 6)
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 2}}

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.Len(t, slots, 2, "precondition: the diff bin-packs into two chunk slots")

	for i, s := range slots {
		assert.Equal(t, 2, s.Primary.ChunkTotal, "chunk slot %d carries the persona's full chunk count", i)
		assert.Equal(t, "chunk", s.Primary.DegradationAction, "chunk slot %d records degradation_action=chunk", i)
		assert.Greater(t, s.Primary.ResolvedWindow, 0, "a sized chunk agent resolves a window")
		assert.Equal(t, defaultMaxTokens, s.Primary.ReservedOutputTokens)
		assert.Equal(t, 5, s.Primary.chunkMaxLines, "the chunk-line regime (operator override) is recorded for the cache token / fallback")
	}
}

// TestInvokeAgent_StampsDiagnosabilityOntoResult proves the single stamping seam:
// invokeAgent copies the serving Agent's sizing record onto the Result (whence
// statusFor surfaces it), including Agent.ChunkTotal → Result.ChunkCount.
func TestInvokeAgent_StampsDiagnosabilityOntoResult(t *testing.T) {
	r := NewEngine(newFake()).invokeAgent(context.Background(), Agent{
		Name:                 "dax",
		Invocation:           llmclient.Invocation{Model: "dax"},
		EffectiveBudget:      114688,
		ResolvedWindow:       32768,
		ReservedOutputTokens: 8192,
		ChunkTotal:           6,
		DegradationAction:    "chunk",
	})
	require.Equal(t, StatusOK, r.Status)
	assert.Equal(t, int64(114688), r.EffectiveBudget)
	assert.Equal(t, 32768, r.ResolvedWindow)
	assert.Equal(t, 8192, r.ReservedOutputTokens)
	assert.Equal(t, 6, r.ChunkCount, "Agent.ChunkTotal is stamped onto Result.ChunkCount")
	assert.Equal(t, "chunk", r.DegradationAction)
}
