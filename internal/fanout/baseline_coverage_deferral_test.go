package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.13 T6: a deferred bin is dispatched to nobody, so a run where every
// DISPATCHED slot succeeded must still report the deferred files uncovered. Before
// T6 the all-OK shortcut returned nil here and the deferred files were recorded as
// reviewed, then skipped by the next scan.
func TestUncoveredBaselineFiles_DeferralDisablesAllOKShortcut(t *testing.T) {
	slots := []Slot{
		{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go"}}},
		{Primary: Agent{Name: "greta", chunkFiles: []string{"b.go"}}, deferredBins: [][]string{{"c.go"}, {"d.go"}}},
	}
	results := []Result{
		{Agent: "greta", Status: StatusOK, servedChunkFiles: []string{"a.go"}},
		{Agent: "greta", Status: StatusOK, servedChunkFiles: []string{"b.go"}},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2", "c.go": "h3", "d.go": "h4"}

	got := uncoveredBaselineFiles(context.Background(), slots, results, reviewed)
	assert.Equal(t, map[string]struct{}{"c.go": {}, "d.go": {}}, got,
		"every dispatched slot succeeded, but the deferred files were reviewed by nobody")
}

// Coverage stays a union across personas: a file one persona deferred is covered
// when another persona dispatched and reviewed it.
func TestUncoveredBaselineFiles_DeferredFileCoveredByAnotherPersona(t *testing.T) {
	slots := []Slot{
		{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go"}}, deferredBins: [][]string{{"b.go"}}},
		{Primary: Agent{Name: "kai", chunkFiles: []string{"a.go", "b.go"}}},
	}
	results := []Result{
		{Agent: "greta", Status: StatusOK, servedChunkFiles: []string{"a.go"}},
		{Agent: "kai", Status: StatusOK, servedChunkFiles: []string{"a.go", "b.go"}},
	}
	reviewed := map[string]string{"a.go": "h1", "b.go": "h2"}

	assert.Nil(t, uncoveredBaselineFiles(context.Background(), slots, results, reviewed),
		"kai reviewed the file greta deferred, so nothing is uncovered")
}

// The per-persona counts behind status.json's unreviewed_files / deferred_files:
// a failed bin's files and the deferred files are both unreviewed; only the
// deferred ones are deferred.
func TestUnreviewedFileCounts(t *testing.T) {
	slots := []Slot{
		{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go", "b.go"}}},
		{Primary: Agent{Name: "greta", chunkFiles: []string{"c.go"}}, deferredBins: [][]string{{"d.go", "e.go"}}},
		{Primary: Agent{Name: "kai", chunkFiles: []string{"a.go", "b.go", "c.go", "d.go", "e.go"}}},
	}
	results := []Result{
		{Agent: "greta", Status: StatusOK, servedChunkFiles: []string{"a.go", "b.go"}},
		{Agent: "greta", Status: StatusFailed},
		{Agent: "kai", Status: StatusOK, servedChunkFiles: []string{"a.go", "b.go", "c.go", "d.go", "e.go"}},
	}

	got := unreviewedFileCounts(context.Background(), slots, results)
	assert.Equal(t, fileGap{unreviewed: 3, deferred: 2}, got["greta"],
		"c.go's bin failed and d.go/e.go were deferred")
	_, ok := got["kai"]
	assert.False(t, ok, "a persona that reviewed every planned file has no entry")
}

// An untagged slot (the chunked diff path) plans no attributable files, so the
// counts stay zero and status.json stays byte-identical.
func TestUnreviewedFileCounts_UntaggedSlotsCountNothing(t *testing.T) {
	slots := []Slot{{Primary: Agent{Name: "greta"}}, {Primary: Agent{Name: "greta"}}}
	results := []Result{{Agent: "greta", Status: StatusOK}, {Agent: "greta", Status: StatusFailed}}
	assert.Empty(t, unreviewedFileCounts(context.Background(), slots, results))
}

// The T6 success criterion, end to end through the real prepare, slot builder,
// engine, write-back and re-scan: a persona whose files need more than
// maxChunksPerAgent bins has every DISPATCHED bin succeed, yet the file-hash index
// records none of the deferred files, status.json counts them, and the next scan
// re-reviews exactly the deferred set.
func TestBaselineDeferral_AllDispatchedOKLeavesDeferredFilesOutOfIndex(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	// No global byte budget, so nothing is shed. Each 40 KB file fits the unknown
	// test model's per-agent budget (~71680 bytes) alone but not two together, so
	// PartitionByBudget packs one file per bin and two files land past the ceiling.
	n := legacyCeiling + 2
	files := make(map[string]string, n)
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("f%03d.go", i)
		files[p] = "// FILE:" + p + "\n" + strings.Repeat("x", 40_000)
	}
	repo := baselineRepo(t, files)

	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "r1")))
	require.NoError(t, err)
	prep.Slots = withLegacyDeferral(prep.Slots)
	require.Len(t, prep.Slots, legacyCeiling, "the ceiling bounds the dispatched bins")
	var deferred []string
	for _, s := range prep.Slots {
		for _, bin := range s.deferredBins {
			deferred = append(deferred, bin...)
		}
	}
	sort.Strings(deferred)
	require.Len(t, deferred, 2, "two files are past the ceiling")

	poolDir := filepath.Join(t.TempDir(), "pool")
	results, _ := runEngine(context.Background(), baselineChunkFindingCompleter{}, prep, poolDir)
	require.Len(t, results, 1, "the persona's bins merge into one result")
	require.Equal(t, StatusOK, results[0].Status, "every dispatched bin succeeded")
	assert.Zero(t, results[0].UnreviewedChunks, "no dispatched chunk failed, so unreviewed_chunks keeps its meaning")
	assert.Equal(t, 2, results[0].UnreviewedFiles, "the deferred files went unreviewed")
	assert.Equal(t, 2, results[0].DeferredFiles)

	sum, err := WritePool(poolDir, results, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, sum.UnreviewedFiles, "the run-level tally sums the persona's count")
	assert.Equal(t, 2, sum.DeferredFiles)
	var pool PoolSummary
	rawSum, err := os.ReadFile(filepath.Join(poolDir, "summary.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(rawSum, &pool))
	require.Len(t, pool.Agents, 1)
	assert.Equal(t, 2, pool.Agents[0].UnreviewedFiles, "summary.json carries the count")
	raw, err := os.ReadFile(filepath.Join(poolDir, "raw", "agent", "greta", "status.json"))
	require.NoError(t, err)
	var st map[string]any
	require.NoError(t, json.Unmarshal(raw, &st))
	assert.EqualValues(t, 2, st["unreviewed_files"], "status.json carries the count")
	assert.EqualValues(t, 2, st["deferred_files"])

	require.NoError(t, prep.CommitBaselineIndex("run-1"))
	recorded, excluded := prep.BaselineCoverage()
	assert.Equal(t, legacyCeiling, recorded, "every dispatched file was recorded")
	assert.Equal(t, 2, excluded, "the deferred files were withheld from the index")

	next, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "r2")))
	require.NoError(t, err)
	assert.Equal(t, deferred, sortedKeys(next.baseline.reviewed),
		"the re-run rescans exactly the deferred files, no more and no fewer")
}
