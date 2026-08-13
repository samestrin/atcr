package fanout

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.5.4 T1: a Slot carries the FileEntry list its Primary actually
// shipped, so a fallback can re-pack that payload against its OWN budget instead
// of inheriting a prompt sized for the primary's window.
//
// The entries are CARRIED from the construction site rather than recovered from
// Agent.CodeContext: recovery runs through EntriesFromRenderedPayload, which does
// not recognize every payload shape and returns nothing for the ones it misses,
// so a re-fit driven off it would silently shed everything for exactly the
// payloads nothing else can vouch for. These tests pin the carry — and pin the
// empty case as EMPTY rather than wrong, which is what makes an entry-less slot
// decline to re-fit instead of re-packing against nothing.

// entryPathSet is the path set of an entry list, for order-insensitive comparison
// against a coverage tag.
func entryPathSet(entries []payload.FileEntry) map[string]struct{} {
	out := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		out[e.Path] = struct{}{}
	}
	return out
}

func pathSet(paths []string) map[string]struct{} {
	out := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		out[p] = struct{}{}
	}
	return out
}

func TestBuildSlots_BulkSlotCarriesTheEntriesItShipped(t *testing.T) {
	// greta's 32k window sheds files from this payload, so the shipped set is a
	// strict subset of mp.Entries. The carried list must be the SHIPPED subset —
	// carrying the pre-shed set would hand a fallback files the primary's prompt
	// never contained, and a re-fit over them would review text nothing sent.
	cfg := sizingRosterConfig()
	cfg.Project.Agents = []string{"greta"}
	payloads := oversizedBlocksPayload()

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", false)
	require.NoError(t, err)
	require.Len(t, slots, 1)

	require.True(t, slots[0].Primary.Truncation.Truncated,
		"precondition: the 32k window must shed at least one file, or this asserts nothing")
	dropped := pathSet(slots[0].Primary.Truncation.FilesDropped)

	got := entryPathSet(slots[0].entries)
	require.NotEmpty(t, got, "a bulk slot built from a decomposed payload must carry its entries")
	assert.Len(t, got, len(payloads["blocks"].Entries)-len(dropped),
		"the carried set must be exactly the kept subset (whole payload minus the shed files)")
	for p := range dropped {
		assert.NotContains(t, got, p, "a shed file must NOT appear in the carried entry list")
	}
	// And every carried entry's body must really be in the rendered payload.
	for _, e := range slots[0].entries {
		assert.Contains(t, slots[0].Primary.Prompt, e.Body,
			"carried entry %q must be present in the prompt the primary actually shipped", e.Path)
	}
}

func TestBuildSlots_BaselineChunkSlotCarriesItsChunkEntries(t *testing.T) {
	// The baseline partition path: each slot's carried entries must match that
	// slot's own coverage tag exactly. Those two are the same fact recorded twice
	// (what this chunk shipped), and T3 makes coverage attribution read a re-packed
	// fallback's tag — so a carry that disagreed with the tag would put the re-fit
	// and the coverage record on different file sets.
	cfg := sizingRosterConfig()
	cfg.Project.Agents = []string{"greta"}
	payloads := oversizedBlocksPayload()

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "blocks", "", false, true)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1,
		"precondition: this payload must partition into multiple baseline chunks for greta's window")

	for i, s := range slots {
		require.NotEmpty(t, s.Primary.chunkFiles, "slot %d: precondition, a baseline chunk slot is tagged", i)
		assert.Equal(t, pathSet(s.Primary.chunkFiles), entryPathSet(s.entries),
			"slot %d: the carried entries must be exactly the files its coverage tag names", i)
		assert.Len(t, s.entries, len(s.Primary.chunkFiles),
			"slot %d: carried entries and coverage tag must have the same length (no duplicates, no omissions)", i)
	}
}

func TestBuildSlots_ChunkedDiffSlotCarriesNoEntries(t *testing.T) {
	// The review_strategy=chunked diff path splits TEXT on diff markers (chunkDiff)
	// and has no FileEntry list in scope. It must carry EMPTY rather than a
	// reconstructed-and-possibly-wrong list: empty is the signal buildFallbackAgent
	// reads to decline the re-fit and keep the honest warn-and-ship, whereas a
	// recovered list would authorize a re-pack over entries the chunk may not hold.
	cfg := sizingRosterConfig()
	cfg.Project.Agents = []string{"greta"}
	cfg.Settings.ReviewStrategy = reviewStrategyChunked

	diff := diffOfNFiles(40, 200)
	payloads := map[string]modePayload{
		"blocks": {Text: diff, FileCount: 40, Entries: []payload.FileEntry{{Path: "f0.go", Size: int64(len(diff)), Body: diff}}},
	}

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "blocks", "", false)
	require.NoError(t, err)
	require.Greater(t, len(slots), 1,
		"precondition: the diff must bin-pack into multiple chunk slots, or this exercises the bulk path instead")

	for i, s := range slots {
		assert.Empty(t, s.entries,
			"slot %d: a chunked-diff slot has no FileEntry source and must carry an empty list, not a reconstructed one", i)
		require.NotEmpty(t, strings.TrimSpace(s.Primary.Prompt), "slot %d: precondition, the slot still ships a payload", i)
	}
}
