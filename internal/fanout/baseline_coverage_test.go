package fanout

import (
	"context"
	"errors"
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

// The bulk fall-through (single chunk, or a non-positive per-agent chunk budget)
// produces one slot covering the WHOLE payload; nil chunkFiles is that sentinel. A
// succeeded whole-payload slot covers everything, so nothing is excluded — this is
// what keeps the pre-35.2 single-chunk behavior byte-identical.
func TestUncoveredBaselineFiles_WholePayloadSlotCoversEverything(t *testing.T) {
	t.Parallel()
	slots := []Slot{
		{Primary: Agent{Name: "greta"}}, // nil chunkFiles → whole payload
		{Primary: Agent{Name: "kai", chunkFiles: []string{"a.go"}}},
	}
	results := []Result{
		{Agent: "greta", Status: StatusOK},
		{Agent: "kai", Status: StatusFailed},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2"}

	got := uncoveredBaselineFiles(slots, results, reviewed)
	assert.Empty(t, got, "a succeeded whole-payload slot covers every reviewed file")
}

// A FAILED whole-payload slot covers nothing — the nil sentinel must not be read as
// "covers everything" regardless of outcome.
func TestUncoveredBaselineFiles_FailedWholePayloadSlotCoversNothing(t *testing.T) {
	t.Parallel()
	slots := []Slot{{Primary: Agent{Name: "greta"}}}
	results := []Result{{Agent: "greta", Status: StatusFailed}}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2"}

	got := uncoveredBaselineFiles(slots, results, reviewed)
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

// A single-chunk baseline scan falls through to the bulk one-slot-per-persona path,
// which leaves chunkFiles nil (the whole-payload sentinel). AC2 depends on this: a
// run with nothing to exclude must behave exactly as before 35.2.
func TestBaselineSlots_SingleChunkLeavesChunkFilesUntagged(t *testing.T) {
	t.Parallel()
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	entries := []payload.FileEntry{baselineEntry("a.go", 40)}

	slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", true, true)
	require.NoError(t, err)
	require.Len(t, slots, 1)
	assert.Nil(t, slots[0].Primary.chunkFiles,
		"a single-chunk baseline slot covers the whole payload — nil is that sentinel")
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
// that chunk's files as covered.
func TestUncoveredBaselineFiles_FallbackServedSlotCountsAsCovered(t *testing.T) {
	t.Parallel()
	slots := []Slot{{
		Primary:   Agent{Name: "greta", chunkFiles: []string{"a.go"}},
		Fallbacks: []Agent{{Name: "kai", Invocation: llmclient.Invocation{Model: "m-kai"}}},
	}}
	// invokeSlot attributes a fallback-served result to the primary's slot.
	results := []Result{{Agent: "greta", Status: StatusOK, FallbackUsed: true, FallbackFrom: "greta"}}
	reviewed := map[string]string{"a.go": "h1"}

	got := uncoveredBaselineFiles(slots, results, reviewed)
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
