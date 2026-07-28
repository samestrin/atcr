package fanout

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.2 (TD-013) relies on results[i] describing slots[i] so a failed baseline
// chunk can be attributed to the files that chunk carried. Engine.Run documents that
// contract; this pins it as load-bearing across BOTH lanes and a mid-roster failure,
// so a future refactor that reorders or compacts the result slice fails here rather
// than silently mis-attributing coverage in the hash-index write-back.
func TestEngineRun_ResultsMatchSlotInputOrder(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.failFor["s1"] = errors.New("boom")
	slots := []Slot{
		agentSlot("p0"),
		func() Slot { s := agentSlot("s1"); s.Serial = true; return s }(),
		agentSlot("p2"),
		func() Slot { s := agentSlot("s3"); s.Serial = true; return s }(),
	}
	// maxParallel=1 forces parallel-lane queueing so completion order cannot match
	// input order by accident.
	results := NewEngine(f, WithMaxParallel(1)).Run(context.Background(), slots)

	require.Len(t, results, len(slots), "one result per slot")
	for i, s := range slots {
		assert.Equal(t, s.Primary.Name, results[i].Agent,
			"results[%d] must describe slots[%d] — baseline chunk attribution depends on it", i, i)
	}
	assert.Equal(t, StatusFailed, results[1].Status, "the failing slot's outcome lands at its own index")
	assert.Equal(t, StatusOK, results[3].Status)
}

// The index-correspondence tests above build slots with DISTINCT agent names, so a
// refactor that groups/sorts/compacts results by agent name — permuting chunk
// results WITHIN one persona — would keep them green while mis-attributing which
// files a failed chunk carried. In a real baseline fan-out every chunk slot of a
// persona carries the SAME name (that is what mergeChunkResults collapses on), so
// this pins the correspondence with same-named slots: the chunk whose payload
// carries c.go fails, and its outcome must land at ITS index.
func TestEngineRun_SamePersonaChunksKeepSlotIndex(t *testing.T) {
	t.Parallel()
	chunkSlot := func(file string) Slot {
		return Slot{Primary: Agent{
			Name:       "greta",
			Invocation: llmclient.Invocation{Model: "greta", Prompt: "// FILE:" + file + "\n"},
			chunkFiles: []string{file},
		}}
	}
	slots := []Slot{chunkSlot("a.go"), chunkSlot("b.go"), chunkSlot("c.go")}
	// maxParallel=1 forces parallel-lane queueing so completion order cannot match
	// input order by accident.
	results := NewEngine(baselinePartialFailCompleter{failMarker: "c.go"}, WithMaxParallel(1)).Run(context.Background(), slots)

	require.Len(t, results, len(slots), "one result per slot")
	for i := range slots {
		require.Equal(t, "greta", results[i].Agent, "every chunk result attributes to the persona name")
	}
	assert.Equal(t, StatusOK, results[0].Status)
	assert.Equal(t, StatusOK, results[1].Status)
	assert.Equal(t, StatusFailed, results[2].Status,
		"the failed chunk's outcome must land at ITS chunk's index — same-named slots make name-based attribution vacuous")
}

// AC1 core: the files carried by a FAILED chunk are the uncovered set; the files in
// succeeded chunks are not.
func TestUncoveredBaselineFiles_ExcludesFailedChunkFiles(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go", "b.go"}}},
		{Primary: Agent{Name: "greta", chunkFiles: []string{"c.go"}}},
	}
	results := []Result{
		{Agent: "greta", Status: StatusOK},
		{Agent: "greta", Status: StatusFailed},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2", "c.go": "h3"}

	got := uncoveredBaselineFiles(slots, results, reviewed)
	assert.Equal(t, map[string]struct{}{"c.go": {}}, got,
		"only the failed chunk's files are uncovered; the succeeded chunk's files stay recordable")
}

// Q1 (clarified 2026-07-27): UNION across personas. Each persona partitions the repo
// against its OWN model window, so the same file lands in different chunks per
// persona. A file is uncovered only when NO succeeded chunk anywhere carried it.
// Intersection semantics would regress the case that already works today (a wholly
// failed persona reports UnreviewedChunks==0 and never blocks the write-back).
func TestUncoveredBaselineFiles_UnionAcrossPersonas(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go", "b.go"}}},
		{Primary: Agent{Name: "greta", chunkFiles: []string{"c.go"}}},
		{Primary: Agent{Name: "kai", chunkFiles: []string{"a.go"}}},
		{Primary: Agent{Name: "kai", chunkFiles: []string{"b.go", "c.go"}}},
	}
	results := []Result{
		{Agent: "greta", Status: StatusOK},     // covers a, b
		{Agent: "greta", Status: StatusFailed}, // c uncovered by greta
		{Agent: "kai", Status: StatusTimeout},  // a uncovered by kai
		{Agent: "kai", Status: StatusOK},       // covers b, c
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2", "c.go": "h3"}

	got := uncoveredBaselineFiles(slots, results, reviewed)
	assert.Empty(t, got,
		"UNION: every file was carried by some succeeded chunk, so nothing is uncovered")
}

// AC2: when every dispatched slot succeeded the whole payload was reviewed by
// definition, whatever shape the fan-out took, so nothing is excluded and the
// write-back stays byte-identical to pre-35.2. Full coverage is established by this
// all-succeeded rule rather than by trusting any individual slot's tag.
func TestUncoveredBaselineFiles_AllSlotsSucceededMeansFullCoverage(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{Primary: Agent{Name: "greta"}}, // untagged (e.g. a chunked-strategy slot)
		{Primary: Agent{Name: "kai", chunkFiles: []string{"a.go"}}},
	}
	results := []Result{
		{Agent: "greta", Status: StatusOK},
		{Agent: "kai", Status: StatusOK},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2"}

	got := uncoveredBaselineFiles(slots, results, reviewed)
	assert.Empty(t, got, "no failures anywhere → the whole payload was covered")
}

// SENTINEL POLARITY (independent-review HIGH): an UNTAGGED slot contributes NO
// coverage even when it succeeded. buildSlots has slot-creating paths that cannot
// attribute files (the review_strategy=chunked branch splits payload TEXT, and a
// files-mode baseline payload can reach it because isDiffFileMarker also matches
// `=== FILE:` and tracked *.patch fixtures carry real `diff --git` lines). If an
// untagged success vouched for the whole payload, a sibling chunk's failed files
// would be recorded and then silently skipped next scan.
func TestUncoveredBaselineFiles_UntaggedSlotContributesNoCoverage(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{Primary: Agent{Name: "greta"}}, // untagged, succeeds
		{Primary: Agent{Name: "greta"}}, // untagged, fails
	}
	results := []Result{
		{Agent: "greta", Status: StatusOK},
		{Agent: "greta", Status: StatusFailed},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2"}

	got := uncoveredBaselineFiles(slots, results, reviewed)
	assert.Equal(t, map[string]struct{}{"a.go": {}, "b.go": {}}, got,
		"an untagged success must never vouch for files a sibling chunk failed to review")
}

// A tagged whole-payload (bulk fall-through) slot keeps its precision: succeeding
// covers every file it names even when a sibling persona's chunk failed.
func TestUncoveredBaselineFiles_TaggedBulkSlotCoversItsPayload(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go", "b.go"}}},
		{Primary: Agent{Name: "kai", chunkFiles: []string{"a.go"}}},
	}
	results := []Result{
		{Agent: "greta", Status: StatusOK},
		{Agent: "kai", Status: StatusFailed},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2"}

	got := uncoveredBaselineFiles(slots, results, reviewed)
	assert.Empty(t, got, "the succeeded bulk slot covered the whole payload it was tagged with")
}

// No coverage evidence at all (no slot ran) is the fail-open answer: everything
// uncovered, so the caller writes nothing rather than recording a full pass.
func TestUncoveredBaselineFiles_NoResultsReportsEverythingUncovered(t *testing.T) {
	t.Parallel()
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2"}

	got := uncoveredBaselineFiles(nil, nil, reviewed)
	assert.Equal(t, map[string]struct{}{"a.go": {}, "b.go": {}}, got)
}

// AC3 at the attribution layer: when every chunk fails (by failure OR timeout) every
// reviewed file is uncovered, so the write-back has zero coverage to record.
func TestUncoveredBaselineFiles_EveryChunkFailed(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go"}}},
		{Primary: Agent{Name: "greta", chunkFiles: []string{"b.go"}}},
	}
	results := []Result{
		{Agent: "greta", Status: StatusFailed},
		{Agent: "greta", Status: StatusTimeout},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2"}

	got := uncoveredBaselineFiles(slots, results, reviewed)
	assert.Equal(t, map[string]struct{}{"a.go": {}, "b.go": {}}, got)
}

// A file the GLOBAL byte budget dropped is absent from the write-back's reviewed set
// already, so it must never surface in the uncovered set — the uncovered set is a
// subset of what the write-back would otherwise record, never a superset.
func TestUncoveredBaselineFiles_IgnoresPathsOutsideReviewedSet(t *testing.T) {
	t.Parallel()
	slots := []Slot{{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go", "dropped.go"}}}}
	results := []Result{{Agent: "greta", Status: StatusFailed}}
	reviewed := map[string]string{"a.go": "h1"} // dropped.go was shed globally

	got := uncoveredBaselineFiles(slots, results, reviewed)
	assert.Equal(t, map[string]struct{}{"a.go": {}}, got)
}

// Defensive: a results slice shorter than the slot slice (never produced by
// Engine.Run, but the attribution must not panic or over-record if it ever were)
// leaves the unmatched slots uncovered rather than indexing out of range.
func TestUncoveredBaselineFiles_ShortResultsSliceIsSafe(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go"}}},
		{Primary: Agent{Name: "greta", chunkFiles: []string{"b.go"}}},
	}
	results := []Result{{Agent: "greta", Status: StatusOK}}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2"}

	assert.NotPanics(t, func() {
		got := uncoveredBaselineFiles(slots, results, reviewed)
		assert.Equal(t, map[string]struct{}{"b.go": {}}, got)
	})
}

// Defensive (companion to ShortResultsSliceIsSafe): a results slice LONGER than the
// slot slice must not declare full coverage either — the extra results have no
// corresponding slot, so "an outcome for EVERY slot" is not established and the
// per-slot attribution must run instead, leaving files no succeeded tagged chunk
// carried uncovered.
func TestUncoveredBaselineFiles_ExtraResultsSliceIsSafe(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go"}}},
	}
	results := []Result{
		{Agent: "greta", Status: StatusOK},
		{Agent: "greta", Status: StatusOK}, // no slot corresponds to this result
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2"}

	assert.NotPanics(t, func() {
		got := uncoveredBaselineFiles(slots, results, reviewed)
		assert.Equal(t, map[string]struct{}{"b.go": {}}, got,
			"extra results must not shortcut to full coverage — b.go has no coverage evidence")
	})
}

// buildSlots' baseline branch must tag every (persona × chunk) slot with exactly the
// files that chunk carries, so the tag and the rendered prompt cannot drift apart.
func TestBaselineSlots_TagChunkFilesPerSlot(t *testing.T) {
	t.Parallel()
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 100 // 100-byte files → one chunk each
	entries := []payload.FileEntry{
		baselineEntry("a.go", 100),
		baselineEntry("b.go", 100),
		baselineEntry("c.go", 100),
	}

	slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Len(t, slots, 3, "one persona × three chunks")

	var tagged []string
	for i, s := range slots {
		require.NotEmpty(t, s.Primary.chunkFiles, "slot %d must carry its own chunk file set", i)
		for _, p := range s.Primary.chunkFiles {
			assert.Contains(t, s.Primary.Prompt, "// FILE:"+p+"\n",
				"slot %d's tagged file %q must actually be in its rendered payload", i, p)
		}
		tagged = append(tagged, s.Primary.chunkFiles...)
	}
	assert.ElementsMatch(t, []string{"a.go", "b.go", "c.go"}, tagged,
		"the chunk tags partition the payload exactly once — no file dropped, none double-counted")
}

// A single-chunk baseline scan falls through to the bulk one-slot-per-persona path.
// That slot genuinely covers the whole payload, so it is tagged explicitly with every
// entry rather than relying on an untagged default — untagged now means "contributes
// no coverage", which would make a single-chunk scan needlessly re-review everything.
func TestBaselineSlots_BulkFallThroughTagsWholePayload(t *testing.T) {
	t.Parallel()
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	entries := []payload.FileEntry{baselineEntry("a.go", 40), baselineEntry("b.go", 40)}

	slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Len(t, slots, 1, "a small payload is one chunk → the bulk path")
	assert.ElementsMatch(t, []string{"a.go", "b.go"}, slots[0].Primary.chunkFiles,
		"the bulk baseline slot is tagged with the whole payload it covers")
	// The invariant that makes the tag trustworthy, and the one an over-tag breaks:
	// every tagged file must actually be IN the rendered payload. Coverage is a union
	// across personas, so a succeeded slot tagged with a file it never shipped would
	// vouch for a file a sibling persona's failed chunk left unreviewed.
	for _, p := range slots[0].Primary.chunkFiles {
		assert.Contains(t, slots[0].Primary.Prompt, "// FILE:"+p,
			"tagged file %q must be in the slot's rendered payload", p)
	}
}

// Every persona reaching the baseline BULK path with the whole payload gets the
// identical path list by construction, so they must share ONE slice rather than each
// retaining its own copy: p.Slots outlives the fan-out until CommitBaselineIndex, so
// an 8-persona roster over a 20k-file monorepo otherwise holds eight independent
// 20k-element string slices for the whole review.
func TestBaselineSlots_BulkTagSliceIsSharedAcrossPersonas(t *testing.T) {
	t.Parallel()
	cfg := twoAgentConfig("http://unused")
	entries := []payload.FileEntry{baselineEntry("a.go", 40), baselineEntry("b.go", 40)}

	slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Len(t, slots, 2, "two personas, one bulk slot each")
	require.NotEmpty(t, slots[0].Primary.chunkFiles)
	require.NotEmpty(t, slots[1].Primary.chunkFiles)

	assert.Equal(t, slots[0].Primary.chunkFiles, slots[1].Primary.chunkFiles,
		"both personas ship the whole payload, so both tags name the same files")
	assert.True(t, &slots[0].Primary.chunkFiles[0] == &slots[1].Primary.chunkFiles[0],
		"the identical list must be ONE shared slice, not a per-persona copy retained for the run")
}

// The per-agent byte shed must narrow the coverage tag with it. Reaching the bulk
// path currently implies no shed can occur — chunkBudget and appliedBudget are two
// independently-written copies of the same arithmetic — so this pins the tag to the
// SHIPPED entry set rather than resting on that coincidence. If the two budgets ever
// diverge, an over-tagged slot would record files it never reviewed.
func TestBaselineSlots_BulkTagNeverNamesAnUnshippedFile(t *testing.T) {
	t.Parallel()
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	entries := []payload.FileEntry{
		baselineEntry("a.go", 60),
		baselineEntry("b.go", 60),
		baselineEntry("c.go", 60),
	}

	slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)

	for i, s := range slots {
		require.NotEmpty(t, s.Primary.chunkFiles, "baseline slot %d must be tagged", i)
		for _, p := range s.Primary.chunkFiles {
			assert.Contains(t, s.Primary.Prompt, "// FILE:"+p,
				"slot %d vouches for %q, so %q must be in what it ships", i, p, p)
		}
	}
}

// A baseline persona whose per-agent byte budget yields a SINGLE chunk used to fall
// out of the baseline branch and into the review_strategy=chunked branch, which
// re-splits the payload by LINE budget on column-0 diff markers. Those slots carried
// no chunkFiles tag, and an untagged slot deliberately vouches for nothing — so any
// partial failure produced recorded == 0 and no index write, making all of Epic 35.2
// inert on that config. The baseline branch now owns baseline partitioning outright,
// so every baseline slot stays file-attributable whatever the strategy setting.
func TestBaselineSlots_ChunkedStrategyStillTagsEverySlot(t *testing.T) {
	t.Parallel()
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = reviewStrategyChunked
	// A line budget small enough that chunkDiff WOULD have split this payload.
	ac := cfg.Registry.Agents["greta"]
	ml := 4
	ac.MaxContextLines = &ml
	cfg.Registry.Agents["greta"] = ac

	// Bodies carrying real `diff --git` lines — the shape tracked content such as a
	// *.patch fixture has, and the reason chunkDiff finds markers to split on here.
	entries := []payload.FileEntry{
		patchEntry("a.patch", 3),
		patchEntry("b.patch", 3),
		patchEntry("c.patch", 3),
	}

	slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)

	for i, s := range slots {
		require.NotEmpty(t, s.Primary.chunkFiles,
			"baseline slot %d must be file-attributable under review_strategy=chunked too", i)
	}
	var tagged []string
	for _, s := range slots {
		tagged = append(tagged, s.Primary.chunkFiles...)
	}
	assert.ElementsMatch(t, []string{"a.patch", "b.patch", "c.patch"}, tagged,
		"the tags name the REVIEWED repo paths — never the inner targets of a patch fixture's own diff markers")
}

// patchEntry builds a baseline file entry whose BODY is a small unified diff, so a
// chunker splitting on column-0 `diff --git` markers sees n splittable files.
func patchEntry(path string, n int) payload.FileEntry {
	var b strings.Builder
	b.WriteString("=== FILE: " + path + "\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "diff --git a/%s-%d b/%s-%d\n--- a/%s-%d\n+++ b/%s-%d\n@@ -1 +1 @@\n-old\n+new\n", path, i, path, i, path, i, path, i)
	}
	body := b.String()
	return payload.FileEntry{Path: path, Size: int64(len(body)), Body: body}
}

// A diff-range review never partitions by file, so its slots stay untagged and the
// baseline attribution is inert for them (p.baseline is nil there anyway).
func TestDiffSlots_LeaveChunkFilesUntagged(t *testing.T) {
	t.Parallel()
	cfg := twoAgentConfig("http://unused")
	payloads := map[string]modePayload{
		"blocks": {Entries: []payload.FileEntry{baselineEntry("a.go", 40)}, Text: "a", FileCount: 1},
	}

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "b", Head: "h"}, "blocks", "", true)
	require.NoError(t, err)
	require.NotEmpty(t, slots)
	for i, s := range slots {
		assert.Nil(t, s.Primary.chunkFiles, "diff slot %d must not be chunk-file tagged", i)
	}
}

// RESUME INVARIANT (Epic 35.2, adversarial): the resume path dispatches only the
// PENDING personas, so attribution sees a partial slot set. A file an already-completed
// persona reviewed is still reported uncovered when the pending chunk carrying it
// fails. That is deliberate fail-open behavior — the file is re-reviewed next scan,
// never silently skipped. This test pins it so a future "relax the uncovered set using
// the on-disk full-coverage statuses" optimization fails here: it would be unsound,
// because a resume rebuilds the FULL superset payload while a completed persona only
// ever saw the original hash-skipped subset.
func TestUncoveredBaselineFiles_ResumePartialSlotSetStaysFailOpen(t *testing.T) {
	t.Parallel()
	// greta completed in the original run and is absent from the resumed slot set;
	// only kai is pending, and its chunk carrying c.go fails.
	slots := []Slot{
		{Primary: Agent{Name: "kai", chunkFiles: []string{"a.go", "b.go"}}},
		{Primary: Agent{Name: "kai", chunkFiles: []string{"c.go"}}},
	}
	results := []Result{
		{Agent: "kai", Status: StatusOK},
		{Agent: "kai", Status: StatusFailed},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2", "c.go": "h3"}

	got := uncoveredBaselineFiles(slots, results, reviewed)
	assert.Equal(t, map[string]struct{}{"c.go": {}}, got,
		"a resume attributes coverage ONLY from the slots it dispatched — fail-open toward re-review")
}

// The fallback chain reviews the SAME chunk as the primary it substitutes for, so
// attribution reads the slot's Primary tag and a fallback-served success still counts
// that chunk's files as covered. This drives the REAL engine — a primary that fails
// and a fallback that succeeds — and feeds the actual returned results into
// uncoveredBaselineFiles, so a fallback-served result emitted under the fallback's
// name or appended as an extra result fails here instead of silently breaking
// coverage attribution.
func TestUncoveredBaselineFiles_FallbackServedSlotCountsAsCovered(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.failFor["m-greta"] = errors.New("boom")
	slots := []Slot{{
		Primary:   Agent{Name: "greta", Invocation: llmclient.Invocation{Model: "m-greta"}, chunkFiles: []string{"a.go"}},
		Fallbacks: []Agent{{Name: "kai", Invocation: llmclient.Invocation{Model: "m-kai"}}},
	}}

	results := NewEngine(f).Run(context.Background(), slots)
	require.Len(t, results, 1, "a fallback-served slot yields exactly one result, never an extra")
	require.Equal(t, StatusOK, results[0].Status, "the fallback served the slot")
	require.True(t, results[0].FallbackUsed, "the success came from the fallback chain")
	require.Equal(t, "greta", results[0].Agent,
		"attribution follows the slot, not the substitute — coverage reads the primary's tag")

	got := uncoveredBaselineFiles(slots, results, map[string]string{"a.go": "h1"})
	assert.Empty(t, got, "a fallback reviewed the same chunk, so its files are covered")
}

// AC1 at the write-back layer: the files in SUCCEEDED chunks are persisted and the
// uncovered ones are left out, so the next scan re-reviews only what went unreviewed
// instead of the whole repository.
func TestCommitBaselineIndex_ExcludesUncoveredFiles(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{
		"a.go": "package a\n",
		"b.go": "package b\n",
		"c.go": "package c\n",
	})

	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "r1")))
	require.NoError(t, err)
	require.NotNil(t, prep.baseline)
	// c.go's chunk failed; a.go and b.go were covered by succeeded chunks.
	prep.baseline.uncovered = map[string]struct{}{"c.go": {}}

	require.NoError(t, prep.CommitBaselineIndex("run-1"))

	idx := payload.Load(payload.FileHashIndexPath(repo), nil)
	_, _, okA := idx.Get("a.go")
	_, _, okB := idx.Get("b.go")
	_, _, okC := idx.Get("c.go")
	assert.True(t, okA, "a covered file is recorded")
	assert.True(t, okB, "a covered file is recorded")
	assert.False(t, okC, "an uncovered file must NOT be recorded, or the next scan would skip it though unreviewed")
}

// AC1 end-to-end at the engine layer: after a partial write-back the next baseline
// prepare re-reviews EXACTLY the uncovered file — not the whole repository, and not
// nothing.
func TestCommitBaselineIndex_NextScanReReviewsOnlyUncovered(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{
		"a.go": "package a\n",
		"b.go": "package b\n",
		"c.go": "package c\n",
	})

	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "r1")))
	require.NoError(t, err)
	prep.baseline.uncovered = map[string]struct{}{"c.go": {}}
	require.NoError(t, prep.CommitBaselineIndex("run-1"))

	next, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "r2")))
	require.NoError(t, err, "the uncovered file is still reviewable, so the re-scan is not ErrAllFilesUnchanged")
	mp := next.baseline.reviewed
	assert.Equal(t, []string{"c.go"}, sortedKeys(mp),
		"only the previously-uncovered file is re-reviewed; the covered ones are hash-skipped")
}

// AC3: a run whose every chunk failed has zero coverage and must write NOTHING —
// fail-open toward re-review, never toward skip. Not even the self-trim may run, or
// the next scan would inherit a half-written index.
func TestCommitBaselineIndex_ZeroCoverageWritesNothing(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n"})

	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "r1")))
	require.NoError(t, err)
	prep.baseline.uncovered = map[string]struct{}{"a.go": {}, "b.go": {}}

	require.NoError(t, prep.CommitBaselineIndex("run-1"), "zero coverage is not an error")
	assert.NoFileExists(t, payload.FileHashIndexPath(repo),
		"a run that covered nothing must not persist an index at all")
}

// The zero-coverage guard's second sub-condition (&& len(b.reviewed) > 0) exists so
// an EMPTY-reviewed baseline run still performs the self-trim and save — only a run
// that reviewed files but covered NONE may skip the write. Pin the empty-reviewed
// arm: with nothing reviewed and a stale pre-index entry, CommitBaselineIndex must
// still trim the stale entry AND persist the (now empty) index.
func TestCommitBaselineIndex_EmptyReviewedStillTrimsAndSaves(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})

	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "r1")))
	require.NoError(t, err)
	prep.baseline.reviewed = map[string]string{} // nothing reviewed this run
	prep.baseline.preIndex.Record("gone.go", "deadbeef", "old-run")

	require.NoError(t, prep.CommitBaselineIndex("run-1"))

	assert.FileExists(t, payload.FileHashIndexPath(repo),
		"an empty-reviewed run must still SAVE — skipping the write is the zero-coverage arm, not this one")
	idx := payload.Load(payload.FileHashIndexPath(repo), nil)
	assert.Empty(t, idx.Paths(), "the stale entry is trimmed, not preserved")
}

// AC2: with nothing uncovered the write-back behaves exactly as before 35.2 — every
// reviewed file recorded, and the whole-repo self-trim still drops paths that are no
// longer tracked.
func TestCommitBaselineIndex_NoUncoveredRecordsEverythingAndStillTrims(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n"})

	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "r1")))
	require.NoError(t, err)
	require.Nil(t, prep.baseline.uncovered, "full coverage is the nil default")
	// A stale entry for a path that is no longer tracked must be self-trimmed.
	prep.baseline.preIndex.Record("gone.go", "deadbeef", "old-run")

	require.NoError(t, prep.CommitBaselineIndex("run-1"))

	idx := payload.Load(payload.FileHashIndexPath(repo), nil)
	assert.ElementsMatch(t, []string{"a.go", "b.go"}, idx.Paths(),
		"every reviewed file recorded and the untracked path trimmed, unchanged from pre-35.2")
}

// BaselineCoverage must report the write-back's ACTUAL outcome, so the CLI can log
// what was recorded instead of inferring coverage from Summary.UnreviewedChunks.
// Partial, full, and zero coverage are all distinguishable from the counts alone.
func TestBaselineCoverage_ReportsRecordedAndExcluded(t *testing.T) {
	cfg := twoAgentConfig("http://unused")

	t.Run("partial coverage", func(t *testing.T) {
		repo := baselineRepo(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n", "c.go": "package c\n"})
		prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "r1")))
		require.NoError(t, err)
		prep.baseline.uncovered = map[string]struct{}{"c.go": {}}

		require.NoError(t, prep.CommitBaselineIndex("run-1"))

		recorded, excluded := prep.BaselineCoverage()
		assert.Equal(t, 2, recorded, "the two covered files were recorded")
		assert.Equal(t, 1, excluded, "the failed chunk's file was excluded")
	})

	t.Run("full coverage", func(t *testing.T) {
		repo := baselineRepo(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n"})
		prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "r1")))
		require.NoError(t, err)
		require.Nil(t, prep.baseline.uncovered, "full coverage is the nil default")

		require.NoError(t, prep.CommitBaselineIndex("run-1"))

		recorded, excluded := prep.BaselineCoverage()
		assert.Equal(t, 2, recorded)
		assert.Zero(t, excluded, "nothing was excluded on a fully covered run")
	})

	// The case the operator can currently not see at all: nothing was recorded and
	// the index was left untouched, yet Summary.UnreviewedChunks may read 0.
	t.Run("zero coverage", func(t *testing.T) {
		repo := baselineRepo(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n"})
		prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "r1")))
		require.NoError(t, err)
		prep.baseline.uncovered = map[string]struct{}{"a.go": {}, "b.go": {}}

		require.NoError(t, prep.CommitBaselineIndex("run-1"))

		recorded, excluded := prep.BaselineCoverage()
		assert.Zero(t, recorded, "nothing was covered, so nothing was recorded")
		assert.Equal(t, 2, excluded, "every reviewed file was excluded — the signal a zero-coverage run must expose")
	})

	// No baseline state at all (diff-range review): the accessor is a safe zero.
	t.Run("no baseline state", func(t *testing.T) {
		var prep *PreparedReview
		recorded, excluded := prep.BaselineCoverage()
		assert.Zero(t, recorded)
		assert.Zero(t, excluded)
	})
}

// The scenario Summary.UnreviewedChunks structurally cannot report: one persona whose
// chunks ALL fail alongside a persona that succeeds. mergeResultGroup sets
// UnreviewedChunks only when okCount > 0 && okCount < len(g), so the wholly-failed
// persona contributes 0 — the run looks fully covered from the summary alone. The
// coverage counts must expose the exclusion that the summary hides.
func TestBaselineCoverage_WhollyFailedPersonaIsVisibleWhenSummaryIsNot(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n"})

	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "r1")))
	require.NoError(t, err)

	// One slot per persona over disjoint chunks; greta's every request fails, kai's
	// succeeds. Driven through the REAL engine so the result/slot correspondence the
	// attribution depends on is exercised rather than hand-built.
	f := newFake()
	f.failFor["m-greta"] = errors.New("boom")
	slots := []Slot{
		{Primary: Agent{Name: "greta", Invocation: llmclient.Invocation{Model: "m-greta"}, chunkFiles: []string{"a.go"}}},
		{Primary: Agent{Name: "kai", Invocation: llmclient.Invocation{Model: "m-kai"}, chunkFiles: []string{"b.go"}}},
	}
	results := NewEngine(f).Run(context.Background(), slots)
	require.Len(t, results, 2)

	// The summary signal the CLI currently keys its warning off: greta failed
	// WHOLLY, so mergeResultGroup records no unreviewed chunks for it.
	merged := mergeResultGroup(results[:1], nil)
	require.NotEqual(t, StatusOK, merged.Status, "a wholly failed persona is not OK")
	require.Zero(t, merged.UnreviewedChunks,
		"the bug's root: a wholly failed persona contributes 0 unreviewed chunks")

	prep.baseline.reviewed = map[string]string{"a.go": "h-a", "b.go": "h-b"}
	prep.baseline.uncovered = uncoveredBaselineFiles(slots, results, prep.baseline.reviewed)
	require.NoError(t, prep.CommitBaselineIndex("run-1"))

	recorded, excluded := prep.BaselineCoverage()
	assert.Equal(t, 1, recorded, "only the succeeded persona's file was recorded")
	assert.Equal(t, 1, excluded,
		"the wholly-failed persona's file was excluded — invisible in UnreviewedChunks, visible here")
}

// sortedKeys returns m's keys in ascending order for stable assertions.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Epic 35.2 AC4: the resume path honors the SAME uncovered-set semantics as the fresh
// path. Both drive the identical runEngine seam, so a resumed baseline run whose chunk
// fails stamps the uncovered set rather than discarding the whole write-back.
func TestResumeBaseline_HonorsUncoveredSetSemantics(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	// No global byte budget: each file exceeds the per-agent effective budget for the
	// unknown test model (~71680 bytes), so PartitionByBudget gives each its own chunk.
	repo := baselineRepo(t, map[string]string{
		"a.go": "// FILE:a.go\n" + strings.Repeat("a", 80_000),
		"b.go": "// FILE:b.go\n" + strings.Repeat("b", 80_000),
	})
	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "review")))
	require.NoError(t, err)

	rprep, _, err := PrepareResume(context.Background(), cfg, prep.Dir, repoReq(repo, ""))
	require.NoError(t, err)
	require.NotNil(t, rprep.baseline, "a resumed baseline run captures the write-back state (TD-011)")
	require.Greater(t, len(rprep.Slots), 1, "the baseline resume must fan out across chunks for this test to mean anything")

	// Fail only the chunk carrying b.go; a.go's chunk succeeds.
	runEngine(context.Background(), baselinePartialFailCompleter{failMarker: "b.go"}, rprep, t.TempDir())

	require.NotNil(t, rprep.baseline.uncovered, "the resume path must stamp the uncovered set (AC4)")
	assert.Contains(t, rprep.baseline.uncovered, "b.go", "the failed chunk's file is uncovered")
	assert.NotContains(t, rprep.baseline.uncovered, "a.go", "the succeeded chunk's file stays covered")
}

// Independent-review HIGH regression (Epic 35.2), now closed: a baseline scan under
// review_strategy=chunked whose PartitionByBudget yields a single chunk used to fall
// through to the chunkDiff branch, which DOES split a files-mode payload when the
// tracked content carries column-0 diff markers (atcr's own repo has *.patch
// fixtures). Those slots could not be file-attributed, so ANY failure among them left
// the whole payload uncovered and the write-back recorded nothing at all — Epic 35.2
// silently inert on that configuration.
//
// The chunked branch is now skipped for baseline runs: the baseline branch owns the
// partitioning, so the slots stay attributable and a partial failure records the
// covered files instead of discarding everything. The invariant that must NOT change
// is the fail-open direction — a file no succeeded slot vouched for is never recorded.
func TestBaselineChunkedStrategy_PartialFailureStillRecordsCoveredFiles(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = reviewStrategyChunked
	// A small max_context_lines is what used to make chunkDiff split this payload.
	small := 50
	greta := cfg.Registry.Agents["greta"]
	greta.MaxContextLines = &small
	cfg.Registry.Agents["greta"] = greta
	// No global byte budget: each file exceeds the per-agent effective budget for the
	// unknown test model (~71680 bytes), so the BASELINE branch gives each its own
	// chunk — the split that keeps them file-attributable. Tracked content carrying
	// real diff markers is the shape that used to leak into chunkDiff instead.
	repo := baselineRepo(t, map[string]string{
		"one.patch": "diff --git a/x b/x\n// FILE:one\n" + strings.Repeat("+line\n", 20_000),
		"two.patch": "diff --git a/y b/y\n// FILE:two\n" + strings.Repeat("+line\n", 20_000),
	})
	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "review")))
	require.NoError(t, err)
	require.NotNil(t, prep.baseline)
	require.Greater(t, len(prep.Slots), 1, "the baseline branch must fan out for this test to exercise a partial failure")
	for i, s := range prep.Slots {
		require.NotEmpty(t, s.Primary.chunkFiles,
			"baseline slot %d must stay file-attributable under review_strategy=chunked", i)
		assert.NotContains(t, s.Primary.chunkFiles, "x",
			"a tag must never name a patch fixture's inner diff target — that path was not reviewed")
		assert.NotContains(t, s.Primary.chunkFiles, "y",
			"a tag must never name a patch fixture's inner diff target — that path was not reviewed")
	}

	// Fail the chunk carrying two.patch; the other succeeds.
	runEngine(context.Background(), baselinePartialFailCompleter{failMarker: "two"}, prep, t.TempDir())

	require.NoError(t, prep.CommitBaselineIndex("run-1"))
	idx := payload.Load(payload.FileHashIndexPath(repo), nil)
	_, _, okOne := idx.Get("one.patch")
	_, _, okTwo := idx.Get("two.patch")
	assert.True(t, okOne, "the succeeded chunk's file is recorded instead of being discarded wholesale")
	assert.False(t, okTwo, "the failed chunk's file must NOT be recorded — fail open toward re-review")
}

// The index-correspondence contract must also hold on the two result-assignment
// paths TestEngineRun_ResultsMatchSlotInputOrder does not reach: the deferred
// panic-recovery handlers in BOTH lanes, and the semaphore-acquire cancellation
// branch. The baseline attribution reads results[i] for every slot regardless of how
// that result was produced, so a misaligned panic/cancel result would mis-attribute
// coverage.
func TestEngineRun_PanicAndCancelResultsKeepSlotIndex(t *testing.T) {
	t.Parallel()
	pc := &panicCompleter{f: newFake(), panicFor: map[string]bool{"p1": true, "s3": true}}
	slots := []Slot{
		agentSlot("p0"),
		agentSlot("p1"), // parallel lane, panics
		func() Slot { s := agentSlot("s2"); s.Serial = true; return s }(),
		func() Slot { s := agentSlot("s3"); s.Serial = true; return s }(), // serial lane, panics
	}
	results := NewEngine(pc, WithMaxParallel(1)).Run(context.Background(), slots)

	require.Len(t, results, len(slots))
	for i, s := range slots {
		assert.Equal(t, s.Primary.Name, results[i].Agent,
			"a recovered panic must still land at its own slot index (results[%d])", i)
	}
	assert.Equal(t, StatusFailed, results[1].Status, "the panicking parallel slot failed in place")
	assert.Equal(t, StatusFailed, results[3].Status, "the panicking serial slot failed in place")

	// Cancelled before any semaphore token is available: every slot must still
	// occupy its own index, and none may read as StatusOK (which would wrongly
	// vouch for its chunk's files).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := NewEngine(newFake(), WithMaxParallel(1)).Run(ctx, slots)
	require.Len(t, cancelled, len(slots))
	for i := range slots {
		assert.NotEqual(t, StatusOK, cancelled[i].Status,
			"a cancelled slot must never read as covered (results[%d])", i)
	}
}
