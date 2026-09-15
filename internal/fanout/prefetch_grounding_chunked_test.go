package fanout

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Under review_strategy: chunked the retrieved-span widening has TWO reachable
// outcomes, and which one a run gets is decided by the chunk count alone.
//
// chunkDiff splits payload TEXT on column-0 diff markers, and splitDiffFiles
// glues everything before the first marker — the claim ledger and the Context
// Definitions block — onto the FIRST segment only (chunker.go:137-146, the
// `started` flag and the `isDiffFileMarker(ln) && started` guard). So on a
// multi-chunk run the agents reviewing chunks 2..N genuinely never receive the
// block. scopePrefetchGrounding is review-wide and keeps the widening only when
// EVERY dispatched slot can be shown to have kept it, so it revokes — which is
// the correct answer there, not a defect.
//
// A chunked run that produces ONE chunk is a different path entirely: the
// chunk-slot loop is guarded by `if len(chunks) > 1` (review.go:2357), so a
// single-chunk run falls through to the bulk path (review.go:2515), which builds
// its slot with entries: bulkEntries (review.go:2754). Those entries include the
// Context Definitions entry, so the guard sees it and the widening SURVIVES.
//
// Neither outcome was pinned. These two tests fix that, and they are the
// behavioural warrant for the carve-out documented in docs/payload-modes.md and
// docs/registry.md — without them the docs would be asserting a control-flow
// reading rather than a measured fact.

// prefetchOnlyPaths returns the grounding-map keys that are groundable ONLY
// because pre-fetching retrieved them, i.e. the widening scopePrefetchGrounding
// either keeps or revokes.
func prefetchOnlyPaths(prep *PreparedReview) []string {
	var out []string
	for p, fc := range prep.Changed {
		if fc.PrefetchOnly {
			out = append(out, p)
		}
	}
	return out
}

func TestPrepareReview_ChunkedSingleChunkKeepsPrefetchGrounding(t *testing.T) {
	// ONE changed file → splitDiffFiles yields one segment → chunkDiff returns a
	// single chunk regardless of max_context_lines, so `len(chunks) > 1` is false
	// and the run takes the bulk path. max_context_lines is set deliberately low
	// to prove the fall-through is decided by the SEGMENT count and not by the
	// line budget being generous.
	repo, base, head := prefetchFanoutRepo(t)

	cfg := twoAgentConfig("http://unused")
	cfg.Project.Agents = []string{"kai"}
	cfg.Settings.ReviewStrategy = reviewStrategyChunked
	ml := 40
	kai := cfg.Registry.Agents["kai"]
	kai.MaxContextLines = &ml
	cfg.Registry.Agents["kai"] = kai

	req := reviewReq(repo, repo, base, head)
	req.OutputDir = filepath.Join(t.TempDir(), "review")

	var prep *PreparedReview
	var err error
	captureStderr(t, func() {
		prep, err = PrepareReview(context.Background(), cfg, req)
	})
	require.NoError(t, err)
	require.NotNil(t, prep)

	require.Len(t, prep.Slots, 1,
		"PRECONDITION: a single-chunk chunked run must fall through to the bulk path's one slot per agent; more than one means it took the chunk-slot loop and this asserts the wrong path")
	require.NotEmpty(t, prep.Slots[0].entries,
		"PRECONDITION: the bulk path carries entries — an empty list here means the chunked-diff path built this slot and the fall-through did not happen")

	assert.NotEmpty(t, prefetchOnlyPaths(prep),
		"a single-chunk chunked run delivers the Context Definitions block in its one slot, so the retrieved-span widening must survive; revoking here would make the feature inert on a shape that genuinely received the block")

	pf, ok := readRevokedManifest(t, prep.Dir)["prefetch"].(map[string]any)
	require.True(t, ok, "a git-range review records the pre-fetch outcome")
	assert.Equal(t, true, pf["present"])
	assert.Nil(t, pf["grounding_revoked"],
		"nothing was revoked on this run, so status.json must not claim it was")
}

func TestPrepareReview_ChunkedMultiChunkRevokesPrefetchGrounding(t *testing.T) {
	// THREE changed files, each padded well past the line budget, so the diff
	// bin-packs into more than one chunk. Only chunk 1 carries the preamble, so
	// the agents on chunks 2..N never receive the block and the review-wide
	// widening is correctly withdrawn.
	var consumer strings.Builder
	consumer.WriteString("package p\n\nfunc Reconcile() string {\n")
	for i := 0; i < 34; i++ {
		consumer.WriteString("\t_ = " + itoa(i) + " // " + strings.Repeat("z", 30) + "\n")
	}
	consumer.WriteString("\treturn ReadStore() + WriteStore() + CloseStore()\n}\n")

	repo, base := seedClaimHeavyRepo(t,
		fixtureFile{"a.go", prefetchBandFile("ReadStore", "int", 76)},
		fixtureFile{"b.go", prefetchBandFile("WriteStore", "int", 76)},
		fixtureFile{"c.go", prefetchBandFile("CloseStore", "int", 76)},
		fixtureFile{"consumer.go", []byte(consumer.String())},
	)
	head := commitClaimHeavyHead(t, repo,
		fixtureFile{"a.go", prefetchBandFile("ReadStore", "string", 76)},
		fixtureFile{"b.go", prefetchBandFile("WriteStore", "string", 76)},
		fixtureFile{"c.go", prefetchBandFile("CloseStore", "string", 76)},
	)

	cfg := twoAgentConfig("http://unused")
	cfg.Project.Agents = []string{"kai"}
	cfg.Settings.ReviewStrategy = reviewStrategyChunked
	ml := 40
	kai := cfg.Registry.Agents["kai"]
	kai.MaxContextLines = &ml
	cfg.Registry.Agents["kai"] = kai

	req := reviewReq(repo, repo, base, head)
	req.OutputDir = filepath.Join(t.TempDir(), "review")

	var prep *PreparedReview
	var err error
	captureStderr(t, func() {
		prep, err = PrepareReview(context.Background(), cfg, req)
	})
	require.NoError(t, err)
	require.NotNil(t, prep)

	require.Greater(t, len(prep.Slots), 1,
		"PRECONDITION: the diff must bin-pack into multiple chunk slots, or this exercises the single-chunk fall-through instead and proves the opposite of what it claims")
	for i, s := range prep.Slots {
		require.Empty(t, s.entries,
			"PRECONDITION: slot %d must be a chunked-diff slot, which carries no FileEntry list", i)
	}

	assert.Empty(t, prefetchOnlyPaths(prep),
		"chunks 2..N never receive the Context Definitions block, so the review-wide widening must be withdrawn; keeping it would ground findings on files those agents were never shown")

	pf, ok := readRevokedManifest(t, prep.Dir)["prefetch"].(map[string]any)
	require.True(t, ok, "a git-range review records the pre-fetch outcome")
	assert.Equal(t, true, pf["present"],
		"the block WAS delivered to chunk 1 — revocation is about groundability, not delivery")
	assert.Equal(t, true, pf["grounding_revoked"],
		"the revocation must be observable in status.json; it is the operator's only durable signal that retrieved context was paid for and then made ungroundable")
}
