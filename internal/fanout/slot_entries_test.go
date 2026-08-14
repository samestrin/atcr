package fanout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	// Exact-set equality, matching T1's success criterion: cardinality plus
	// absence would also pass a set that omitted one kept file and included one
	// file that was never in the payload at all.
	want := entryPathSet(payloads["blocks"].Entries)
	for p := range dropped {
		delete(want, p)
	}
	assert.Equal(t, want, got,
		"the carried set must be exactly the kept subset (whole payload minus the shed files)")
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

// TD (review.go:1852): both nil-Kept production builders apply a GLOBAL byte-budget
// pass, so on the arms where bulkEntries survives initialization (the AllDropped
// whole-payload dispatch) a nil Kept makes the carried set the PRE-budget mp.Entries
// — naming files the rendered prompt does not contain. Populating Kept at the two
// builders makes the carried set the global-kept subset on every arm.
func TestPrepareReviewFromDiff_SlotEntriesNeverNameGloballyDroppedFiles(t *testing.T) {
	// greta declares 12289 tokens → a 3-byte effective budget: positive, so the
	// per-agent shed runs, but no file fits it (AllDropped) and the whole
	// global-kept payload is dispatched under the overflow record. The global
	// budget (200 bytes) keeps small.go and drops big.go from the payload text.
	cfg := declaredWindowRoster(t, 12289)
	cfg.Settings.PayloadByteBudget = 200
	cfg.Project.Agents = []string{"greta"}

	diff := "diff --git a/small.go b/small.go\n--- a/small.go\n+++ b/small.go\n@@ -1,1 +1,1 @@\n+tiny\n" +
		"diff --git a/big.go b/big.go\n--- a/big.go\n+++ b/big.go\n@@ -1,5000 +1,5000 @@\n" +
		strings.Repeat("+big line of content here\n", 5000)

	prep, err := PrepareReviewFromDiff(context.Background(), cfg, diffReq(t.TempDir(), filepath.Join(t.TempDir(), "review")), diff)
	require.NoError(t, err)
	require.Len(t, prep.Slots, 1)
	slot := prep.Slots[0]

	require.True(t, slot.Primary.Truncation.Truncated || slot.Primary.DegradationAction != "",
		"precondition: the global budget must have shed big.go from the payload")
	got := entryPathSet(slot.entries)
	require.NotEmpty(t, got, "a bulk slot must carry its entries")
	assert.NotContains(t, got, "big.go",
		"a file the GLOBAL budget dropped is not in the prompt, so the carried set must not name it")
	for _, e := range slot.entries {
		assert.Contains(t, slot.Primary.Prompt, e.Body,
			"carried entry %q must be present in the prompt the primary actually shipped", e.Path)
	}
}

// Baseline companion (review.go:834 buildRepoPayloads): same subset invariant on
// the over-window bulk fall-through of a baseline run — small payload_byte_budget
// plus an UNCAPPED scope-constraint wrapper, the configuration
// TestBaselineWriteback_ExcludesGloballyDroppedFiles proves reachable.
func TestPrepareReviewFromRepo_BaselineSlotEntriesSubsetOfPrompt(t *testing.T) {
	cfg := sizingRosterConfig()
	cfg.Project.Agents = []string{"greta"}
	cfg.Settings.PayloadByteBudget = 100
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	require.NoError(t, os.WriteFile(planPath, []byte("# Plan\n\nreview the auth code carefully\n"), 0o644))
	repo := baselineRepo(t, map[string]string{
		"small.go": "package a\n",
		"big.go":   "package b\n" + strings.Repeat("x", 5000),
	})
	req := repoReq(repo, filepath.Join(t.TempDir(), "review"))
	req.SprintPlanPath = planPath

	prep, err := PrepareReviewFromRepo(context.Background(), cfg, req)
	require.NoError(t, err)
	require.NotEmpty(t, prep.Slots)

	for i, slot := range prep.Slots {
		got := entryPathSet(slot.entries)
		require.NotEmpty(t, got, "slot %d: a bulk slot must carry its entries", i)
		assert.NotContains(t, got, "big.go",
			"slot %d: a file the GLOBAL budget dropped is not in the prompt, so the carried set must not name it", i)
		for _, e := range slot.entries {
			assert.Contains(t, slot.Primary.Prompt, e.Body,
				"slot %d: carried entry %q must be present in the prompt the primary actually shipped", i, e.Path)
		}
		require.NotEmpty(t, slot.Primary.chunkFiles, "slot %d: precondition, a baseline bulk slot is coverage-tagged", i)
		assert.NotContains(t, pathSet(slot.Primary.chunkFiles), "big.go",
			"slot %d: the coverage tag must not vouch for a file the GLOBAL budget dropped from the prompt", i)
	}
}

// TD (slot_entries_test.go:41, clarified): the Kept-preference at
// review.go:1895-1897 (bulkEntries := mp.Kept, falling back to mp.Entries) is
// only observable on the arms where bulkEntries survives initialization — the
// normal per-agent shed overwrites it from mp.Entries unconditionally. This
// case forces the AllDropped arm (a positive-but-tiny agent budget drops every
// entry), so a hand-set Kept strict subset is the carried set. Reverting the
// preference to mp.Entries fails this test.
func TestBuildSlots_BulkSlotCarriesKeptSubsetOnAllDroppedArm(t *testing.T) {
	// greta declares 12289 tokens → a 3-byte effective budget: positive, so the
	// per-agent shed runs, but no 100-byte entry fits it (AllDropped) and the
	// whole global-kept payload is dispatched under the overflow record.
	cfg := declaredWindowRoster(t, 12289)
	cfg.Project.Agents = []string{"greta"}

	mk := func(path string) payload.FileEntry {
		body := fmt.Sprintf("=== FILE: %s ===\n", path) + strings.Repeat("x", 100) + "\n"
		return payload.FileEntry{Path: path, Size: int64(len(body)), Body: body}
	}
	kept := []payload.FileEntry{mk("a.go"), mk("b.go")}
	entries := append([]payload.FileEntry{}, kept...)
	entries = append(entries, mk("c.go")) // "dropped by the global budget": absent from Kept and Text
	var full strings.Builder
	for _, e := range kept {
		full.WriteString(e.Body)
	}
	payloads := map[string]modePayload{
		"blocks": {Entries: entries, Kept: kept, Text: full.String(), FileCount: 2},
	}

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", false)
	require.NoError(t, err)
	require.Len(t, slots, 1)

	require.Equal(t, degradationOverflow, slots[0].Primary.DegradationAction,
		"precondition: the AllDropped arm must be the one taken, or the hand-set Kept is overwritten from mp.Entries")
	assert.Equal(t, entryPathSet(kept), entryPathSet(slots[0].entries),
		"the carried entries must be drawn from Kept, never from the Entries-only files")
	assert.NotContains(t, entryPathSet(slots[0].entries), "c.go")
}
