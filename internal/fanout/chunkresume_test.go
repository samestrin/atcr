package fanout

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// Epic 35.16.13 T4: chunk-aware resume.

// modelBinCompleter fails every bin a given model is sent unless the bin carries
// one of keep, and answers every other call with one finding per file.
type modelBinCompleter struct {
	failModel string
	keep      map[string]bool
}

func (c modelBinCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	if inv.Model == c.failModel {
		kept := false
		for f := range c.keep {
			if strings.Contains(inv.Prompt, "// FILE:"+f+"\n") {
				kept = true
			}
		}
		if !kept {
			return "", fmt.Errorf("simulated bin failure for %s", inv.Model)
		}
	}
	return baselineChunkFindingCompleter{}.Complete(ctx, inv)
}

// chunkResumeConfig is greta + kai with a byte budget that packs one file per
// bin for the files chunkResumeRepo writes.
func chunkResumeConfig() *ReviewConfig {
	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = 300
	return cfg
}

// chunkResumeRepo commits n 200-byte files f00.go.. and returns the repo and
// their paths in order.
func chunkResumeRepo(t *testing.T, n int) (string, []string) {
	t.Helper()
	files := map[string]string{}
	var paths []string
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("f%02d.go", i)
		marker := "// FILE:" + p + "\n"
		files[p] = marker + strings.Repeat("x", 200-len(marker))
		paths = append(paths, p)
	}
	return baselineRepo(t, files), paths
}

// partialBaselineReview runs a baseline review in which greta reviews only the
// first `reviewed` files and kai reviews them all, and returns the review dir.
func partialBaselineReview(t *testing.T, cfg *ReviewConfig, repo string, paths []string, reviewed int) string {
	t.Helper()
	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "review")))
	require.NoError(t, err)
	require.Len(t, prep.Slots, 2*len(paths), "one bin per file per persona")
	keep := map[string]bool{}
	for _, p := range paths[:reviewed] {
		keep[p] = true
	}
	_, err = ExecuteReview(context.Background(), modelBinCompleter{failModel: "m-greta", keep: keep}, prep)
	require.NoError(t, err)
	poolDir := filepath.Join(prep.Dir, "sources", "pool")
	greta := readAgentStatus(t, poolDir, "greta")
	require.Equal(t, StatusOK, greta.Status)
	require.Equal(t, len(paths)-reviewed, greta.UnreviewedChunks)
	kai := readAgentStatus(t, poolDir, "kai")
	require.Equal(t, StatusOK, kai.Status)
	require.Zero(t, kai.UnreviewedChunks)
	return prep.Dir
}

// slotFiles returns the files every slot of one agent carries, sorted.
func slotFiles(slots []Slot, agent string) []string {
	var files []string
	for _, s := range slots {
		if s.Primary.Name == agent {
			files = append(files, s.Primary.chunkFiles...)
		}
	}
	sort.Strings(files)
	return files
}

// T4 success criterion: brad at 4-of-64 resumes into exactly 60 dispatched bins
// for brad and zero for a fully-covered persona.
func TestChunkResume_FourOfSixtyFourDispatchesSixtyBins(t *testing.T) {
	cfg := chunkResumeConfig()
	repo, paths := chunkResumeRepo(t, 64)
	dir := partialBaselineReview(t, cfg, repo, paths, 4)

	rprep, info, err := PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
	require.NoError(t, err)
	assert.Equal(t, []string{"greta"}, info.Pending, "an ok persona with unreviewed bins is pending")
	assert.Equal(t, []string{"kai"}, info.Completed, "a fully covered persona is complete")

	require.Len(t, rprep.Slots, 60, "exactly the 60 unreviewed bins are dispatched")
	for i, s := range rprep.Slots {
		assert.Equal(t, "greta", s.Primary.Name, "slot %d", i)
		assert.Len(t, s.Primary.chunkFiles, 1, "slot %d keeps its recorded one-file bin", i)
		assert.True(t, s.resumedBin, "slot %d", i)
	}
	assert.Equal(t, paths[4:], slotFiles(rprep.Slots, "greta"), "the dispatched bins are the unreviewed ones, not the reviewed four")
	assert.Empty(t, slotFiles(rprep.Slots, "kai"))

	// The write-back records the files this resume sends to a reviewer plus the
	// files greta's four ok bins and kai's record already reviewed.
	require.NotNil(t, rprep.baseline)
	for f := range rprep.baseline.reviewed {
		assert.Contains(t, paths, f)
	}

	// End to end: the resume drains greta's 60 bins and keeps the 4 she had.
	_, err = ExecuteResume(context.Background(), baselineChunkFindingCompleter{}, rprep)
	require.NoError(t, err)
	poolDir := filepath.Join(dir, "sources", "pool")
	assert.ElementsMatch(t, paths, agentFindingFiles(t, poolDir, "greta"))
	st := readAgentStatus(t, poolDir, "greta")
	assert.Equal(t, 64, st.ChunkCount)
	assert.Zero(t, st.UnreviewedChunks)
	assertCoverage(t, dir, Coverage{BinsReviewed: 128, BinsPlanned: 128, FilesCovered: 64, FilesInScope: 64})

	// A second resume finds nothing left to dispatch.
	_, info, err = PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
	require.NoError(t, err)
	assert.True(t, info.AllComplete())
}

// T4 over T6: bins the ceiling deferred make an otherwise all-ok persona
// pending, the resume dispatches exactly those bins, and once they succeed the
// persona's T6 gap (unreviewed_files, deferred_files) drains to zero instead of
// counting the resumed bins twice. The write-back records the resumed files and
// the files the ok bins already reviewed.
func TestChunkResume_DeferredBinsResumeAndDrainT6Gap(t *testing.T) {
	// No global byte budget, so the write-back sheds nothing: each 40 KB file
	// fits the test model's per-agent budget alone but not two together, so
	// every persona packs one file per bin and defers the last two.
	cfg := twoAgentConfig("http://unused")
	files := map[string]string{}
	var paths []string
	for i := 0; i < maxChunksPerAgent+2; i++ {
		p := fmt.Sprintf("f%03d.go", i)
		files[p] = "// FILE:" + p + "\n" + strings.Repeat("x", 40_000)
		paths = append(paths, p)
	}
	repo := baselineRepo(t, files)
	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "review")))
	require.NoError(t, err)
	require.Len(t, prep.Slots, 2*maxChunksPerAgent, "each persona dispatches 64 bins and defers 2")
	_, err = ExecuteReview(context.Background(), baselineChunkFindingCompleter{}, prep)
	require.NoError(t, err)
	poolDir := filepath.Join(prep.Dir, "sources", "pool")
	for _, a := range []string{"greta", "kai"} {
		st := readAgentStatus(t, poolDir, a)
		require.Equal(t, StatusOK, st.Status, a)
		require.Equal(t, 2, st.DeferredFiles, a)
		require.Equal(t, 2, st.UnreviewedFiles, a)
	}

	rprep, info, err := PrepareResume(context.Background(), cfg, prep.Dir, repoReq(repo, ""))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"greta", "kai"}, info.Pending, "a deferred bin makes an ok persona pending")
	require.Len(t, rprep.Slots, 4, "only the deferred bins are dispatched")
	deferred := paths[maxChunksPerAgent:]
	assert.Equal(t, deferred, slotFiles(rprep.Slots, "greta"))
	assert.Equal(t, deferred, slotFiles(rprep.Slots, "kai"))
	assert.ElementsMatch(t, paths, sortedKeys(rprep.baseline.reviewed), "narrowReviewed keeps the resumed files and the ok bins' files")

	_, err = ExecuteResume(context.Background(), baselineChunkFindingCompleter{}, rprep)
	require.NoError(t, err)
	for _, a := range []string{"greta", "kai"} {
		st := readAgentStatus(t, poolDir, a)
		assert.Equal(t, maxChunksPerAgent+2, st.ChunkCount, a)
		assert.Zero(t, st.UnreviewedChunks, a)
		assert.Zero(t, st.UnreviewedFiles, "%s: the resumed deferred files are reviewed, not double-counted", a)
		assert.Zero(t, st.DeferredFiles, a)
		assert.ElementsMatch(t, paths, agentFindingFiles(t, poolDir, a), a)
	}
	n := 2 * (maxChunksPerAgent + 2)
	assertCoverage(t, prep.Dir, Coverage{BinsReviewed: n, BinsPlanned: n, FilesCovered: maxChunksPerAgent + 2, FilesInScope: maxChunksPerAgent + 2})

	require.NoError(t, rprep.CommitBaselineIndex("resume-1"))
	recorded, excluded := rprep.BaselineCoverage()
	assert.Equal(t, maxChunksPerAgent+2, recorded, "the two resumed files and the 64 the ok bins reviewed are recorded")
	assert.Zero(t, excluded)
}

// writeBackResumeRepo commits n 40 KB files f00.go.. and returns a greta + kai
// config with no global byte budget, so the write-back sheds nothing and each
// file fills one bin of the test model's per-agent budget.
func writeBackResumeRepo(t *testing.T, n int) (*ReviewConfig, string, []string) {
	t.Helper()
	files := map[string]string{}
	var paths []string
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("f%02d.go", i)
		files[p] = "// FILE:" + p + "\n" + strings.Repeat("x", 40_000)
		paths = append(paths, p)
	}
	return twoAgentConfig("http://unused"), baselineRepo(t, files), paths
}

// The interrupted run never wrote the file-hash index, so a chunk-aware resume
// that succeeds must record every file some persona reviewed: the resumed bins,
// the pending persona's bins that finished ok before the interruption, and every
// bin of the persona the resume skipped as done. Recording only the resumed bins
// would make the next --all scan re-review almost the whole repository.
func TestChunkResume_CommitRecordsPriorReviewedBins(t *testing.T) {
	cfg, repo, paths := writeBackResumeRepo(t, 6)
	dir := partialBaselineReview(t, cfg, repo, paths, 2)

	rprep, info, err := PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
	require.NoError(t, err)
	require.Equal(t, []string{"greta"}, info.Pending)
	require.Equal(t, paths[2:], slotFiles(rprep.Slots, "greta"), "only greta's unreviewed bins are dispatched")
	assert.ElementsMatch(t, paths, sortedKeys(rprep.baseline.reviewed),
		"the write-back keeps greta's ok bins and kai's bins, not just the resumed ones")

	_, err = ExecuteResume(context.Background(), baselineChunkFindingCompleter{}, rprep)
	require.NoError(t, err)
	require.NoError(t, rprep.CommitBaselineIndex("resume-1"))
	recorded, excluded := rprep.BaselineCoverage()
	assert.Equal(t, len(paths), recorded)
	assert.Zero(t, excluded)
	idx := payload.Load(payload.FileHashIndexPath(repo), nil)
	assert.ElementsMatch(t, paths, idx.Paths(), "the committed index holds every reviewed file")
}

// A prior bin vouches for a file only at the content it reviewed: chunks.json
// records the hash each bin shipped, and a file edited since the interrupted run
// is left out of the write-back instead of being recorded at its new hash, which
// the next --all scan would skip though no reviewer read that content.
func TestChunkResume_CommitSkipsPriorFilesChangedSinceReview(t *testing.T) {
	cfg, repo, paths := writeBackResumeRepo(t, 6)
	dir := partialBaselineReview(t, cfg, repo, paths, 2)
	edited := "// FILE:f00.go\n" + strings.Repeat("y", 40_000)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "f00.go"), []byte(edited), 0o644))

	rprep, _, err := PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
	require.NoError(t, err)
	require.Equal(t, paths[2:], slotFiles(rprep.Slots, "greta"), "f00.go sits in an ok bin, so it is not re-dispatched")
	assert.ElementsMatch(t, paths[1:], sortedKeys(rprep.baseline.reviewed),
		"the edited file is dropped; the unchanged ok-bin file is kept")

	_, err = ExecuteResume(context.Background(), baselineChunkFindingCompleter{}, rprep)
	require.NoError(t, err)
	require.NoError(t, rprep.CommitBaselineIndex("resume-1"))
	idx := payload.Load(payload.FileHashIndexPath(repo), nil)
	_, _, ok := idx.Get("f00.go")
	assert.False(t, ok, "a file no reviewer read at its current content is not recorded")
	assert.ElementsMatch(t, paths[1:], idx.Paths())
}

// A chunks.json written before bins recorded their hashes cannot show what
// content a bin reviewed, so its files are left out of a resume's write-back.
func TestReadResumeRecord_BinWithoutHashesVouchesForNothing(t *testing.T) {
	cfg := chunkResumeConfig()
	repo, paths := chunkResumeRepo(t, 4)
	dir := partialBaselineReview(t, cfg, repo, paths, 4)
	for _, name := range []string{"greta", "kai"} {
		writeCoverageRecord(t, dir, name, []chunkRecordBin{{Index: 0, Files: []string{"old.go"}, Outcome: StatusOK}})
	}
	rr, err := readResumeRecord(dir, []string{"greta", "kai"}, true)
	require.NoError(t, err)
	assert.Empty(t, rr.reviewed)
}

// A single surviving bin is still dispatched as a recorded bin and merged with
// the reviewed ones, rather than taken as a bulk run that overwrites them.
func TestChunkResume_OneUnreviewedBinMerges(t *testing.T) {
	cfg := chunkResumeConfig()
	repo, paths := chunkResumeRepo(t, 4)
	dir := partialBaselineReview(t, cfg, repo, paths, 3)

	rprep, info, err := PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
	require.NoError(t, err)
	assert.Equal(t, []string{"greta"}, info.Pending)
	require.Len(t, rprep.Slots, 1)
	assert.Equal(t, []string{"f03.go"}, rprep.Slots[0].Primary.chunkFiles)

	_, err = ExecuteResume(context.Background(), baselineChunkFindingCompleter{}, rprep)
	require.NoError(t, err)
	poolDir := filepath.Join(dir, "sources", "pool")
	assert.ElementsMatch(t, paths, agentFindingFiles(t, poolDir, "greta"), "the three reviewed bins survive")
	st := readAgentStatus(t, poolDir, "greta")
	assert.Equal(t, 4, st.ChunkCount)
	assert.Zero(t, st.UnreviewedChunks)
}

// Resume and coverage share one "reviewed" predicate: an ok bin whose lane
// contributed nothing, or one a fallback re-packed without serving all its
// files, is unreviewed in the coverage line and --require-coverage, so a resume
// must re-dispatch it. Otherwise a run that fails the gate could never be
// repaired.
func TestChunkResume_ReopensBinsCoverageCountsUnreviewed(t *testing.T) {
	cfg := chunkResumeConfig()
	repo, paths := chunkResumeRepo(t, 4)
	dir := partialBaselineReview(t, cfg, repo, paths, 4)
	poolDir := filepath.Join(dir, "sources", "pool")
	rewriteChunkRecord(t, poolDir, func(rec *chunkRecord) {
		rec.Bins[1].ContributedNothing = true
		rec.Bins[2].RePacked = true
		rec.Bins[2].ServedFiles = nil
	})

	rprep, info, err := PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
	require.NoError(t, err)
	assert.Equal(t, []string{"greta"}, info.Pending, "an all-ok record with unreviewed bins is pending")
	assert.Equal(t, []string{"f01.go", "f02.go"}, slotFiles(rprep.Slots, "greta"),
		"the bin that contributed nothing and the re-packed bin that served nothing are re-dispatched")

	_, err = ExecuteResume(context.Background(), baselineChunkFindingCompleter{}, rprep)
	require.NoError(t, err)
	assert.ElementsMatch(t, paths, agentFindingFiles(t, poolDir, "greta"))
	for _, b := range readChunkRecord(t, poolDir, "greta").Bins {
		assert.False(t, b.open(), "bin %d %v", b.Index, b.Files)
	}
	assertCoverage(t, dir, Coverage{BinsReviewed: 8, BinsPlanned: 8, FilesCovered: 4, FilesInScope: 4})

	_, info, err = PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
	require.NoError(t, err)
	assert.True(t, info.AllComplete())
}

// A persona whose open bins name only files the fresh payload no longer carries
// is complete, and its record must say so: the stale bins leave chunks.json, so
// coverage stops counting the vanished files and can reach 100%.
func TestChunkResume_VanishedOpenBinsLeaveTheRecord(t *testing.T) {
	cfg := chunkResumeConfig()
	repo, paths := chunkResumeRepo(t, 4)
	dir := partialBaselineReview(t, cfg, repo, paths, 4)
	poolDir := filepath.Join(dir, "sources", "pool")
	rewriteChunkRecord(t, poolDir, func(rec *chunkRecord) {
		rec.Bins = append(rec.Bins,
			chunkRecordBin{Index: 4, Files: []string{"gone0.go"}, Outcome: chunkOutcomePending},
			chunkRecordBin{Index: 5, Files: []string{"gone1.go"}, Outcome: chunkOutcomeDeferred},
			chunkRecordBin{Index: 6, Files: []string{"gone2.go"}, Outcome: StatusOK, ContributedNothing: true},
			chunkRecordBin{Index: 7, Files: []string{"gone3.go", "gone4.go"}, Outcome: StatusFailed},
		)
	})

	rprep, info, err := PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
	require.NoError(t, err)
	assert.True(t, info.AllComplete())
	assert.Empty(t, rprep.Slots)

	rec := readChunkRecord(t, poolDir, "greta")
	require.Len(t, rec.Bins, 4, "the four stale bins are resolved out of the record")
	for i, b := range rec.Bins {
		assert.Equal(t, i, b.Index)
		assert.Equal(t, []string{paths[i]}, b.Files)
		assert.Equal(t, StatusOK, b.Outcome)
	}
	assertCoverage(t, dir, Coverage{BinsReviewed: 8, BinsPlanned: 8, FilesCovered: 4, FilesInScope: 4})

	_, info, err = PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
	require.NoError(t, err)
	assert.True(t, info.AllComplete())
}

// T4 success criterion: a path in chunks.json outside the in-scope payload
// (`..`, absolute) is never dispatched.
func TestChunkResume_RecordedPathsOutsidePayloadAreNeverDispatched(t *testing.T) {
	cfg := chunkResumeConfig()
	repo, paths := chunkResumeRepo(t, 4)
	// A real file beside the repo, so a `..` path would resolve if it were
	// ever opened.
	outside := filepath.Join(filepath.Dir(repo), "outside.go")
	require.NoError(t, os.WriteFile(outside, []byte("// FILE:../outside.go\nOUTSIDE_SENTINEL\n"), 0o644))
	dir := partialBaselineReview(t, cfg, repo, paths, 1)
	poolDir := filepath.Join(dir, "sources", "pool")

	hostile := []string{"../outside.go", outside, "/etc/passwd", "./f01.go", "f01.go/../f02.go", ""}
	rewriteChunkRecord(t, poolDir, func(rec *chunkRecord) {
		rec.Bins = []chunkRecordBin{
			{Index: 0, Files: []string{"f00.go"}, Outcome: StatusOK},
			{Index: 1, Files: hostile, Outcome: chunkOutcomePending},
			{Index: 2, Files: []string{"../outside.go", "f02.go"}, Outcome: StatusFailed},
			{Index: 3, Files: []string{outside}, Outcome: StatusTimeout},
			{Index: 4, Files: []string{"f03.go", "f02.go"}, Outcome: chunkOutcomeDeferred},
		}
	})

	rprep, info, err := PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
	require.NoError(t, err)
	assert.Equal(t, []string{"greta"}, info.Pending)
	require.Len(t, rprep.Slots, 2, "only the bins with an in-payload file survive")
	assert.Equal(t, []string{"f02.go"}, rprep.Slots[0].Primary.chunkFiles)
	assert.Equal(t, []string{"f03.go"}, rprep.Slots[1].Primary.chunkFiles, "a file two bins name goes to the first")
	for _, s := range rprep.Slots {
		assert.NotContains(t, s.Primary.Prompt, "OUTSIDE_SENTINEL")
		assert.NotContains(t, s.Primary.Prompt, "outside.go")
		assert.NotContains(t, s.Primary.Prompt, "/etc/passwd")
	}

	t.Run("a persona whose unreviewed bins are all outside the payload is complete", func(t *testing.T) {
		rewriteChunkRecord(t, poolDir, func(rec *chunkRecord) {
			rec.Bins = []chunkRecordBin{
				{Index: 0, Files: []string{"f00.go"}, Outcome: StatusOK},
				{Index: 1, Files: hostile, Outcome: chunkOutcomePending},
			}
		})
		rprep, info, err := PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
		require.NoError(t, err)
		assert.True(t, info.AllComplete())
		assert.Empty(t, rprep.Slots)
	})
}

// A missing or unreadable chunks.json keeps today's agent-level behaviour: an
// ok persona is skipped and a failed one re-runs its whole payload.
func TestChunkResume_UnreadableRecordFallsBackToAgentLevel(t *testing.T) {
	cases := map[string]func(t *testing.T, path string){
		"missing": func(t *testing.T, path string) { require.NoError(t, os.Remove(path)) },
		"corrupt": func(t *testing.T, path string) { require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o644)) },
		"unknown schema": func(t *testing.T, path string) {
			require.NoError(t, os.WriteFile(path, []byte(`{"schema_version":99,"agent":"greta","bins":[{"index":0,"files":["f01.go"],"outcome":"pending"}]}`), 0o644))
		},
		"another agent": func(t *testing.T, path string) {
			require.NoError(t, os.WriteFile(path, []byte(`{"schema_version":1,"agent":"kai","bins":[{"index":0,"files":["f01.go"],"outcome":"pending"}]}`), 0o644))
		},
		"symlink out of the review": func(t *testing.T, path string) {
			target := filepath.Join(t.TempDir(), "chunks.json")
			require.NoError(t, os.WriteFile(target, []byte(`{"schema_version":1,"agent":"greta","bins":[{"index":0,"files":["f01.go"],"outcome":"pending"}]}`), 0o644))
			require.NoError(t, os.Remove(path))
			require.NoError(t, os.Symlink(target, path))
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := chunkResumeConfig()
			repo, paths := chunkResumeRepo(t, 4)
			dir := partialBaselineReview(t, cfg, repo, paths, 1)
			spoil(t, filepath.Join(dir, "sources", "pool", poolRawAgentDir, "greta", chunksFile))

			rprep, info, err := PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
			require.NoError(t, err)
			assert.True(t, info.AllComplete(), "greta's status is ok, so agent-level resume skips her")
			assert.Empty(t, rprep.Slots)
		})
	}

	t.Run("a failed persona with no record re-runs whole", func(t *testing.T) {
		cfg := chunkResumeConfig()
		repo, paths := chunkResumeRepo(t, 4)
		dir := partialBaselineReview(t, cfg, repo, paths, 1)
		agentDir := filepath.Join(dir, "sources", "pool", poolRawAgentDir, "greta")
		require.NoError(t, os.Remove(filepath.Join(agentDir, chunksFile)))
		require.NoError(t, os.Remove(filepath.Join(agentDir, statusFile)))

		rprep, info, err := PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
		require.NoError(t, err)
		assert.Equal(t, []string{"greta"}, info.Pending)
		assert.Equal(t, paths, slotFiles(rprep.Slots, "greta"), "the whole payload is re-dispatched")
		for _, s := range rprep.Slots {
			assert.False(t, s.resumedBin)
		}
	})

	t.Run("an all-ok record with a non-ok status re-runs whole", func(t *testing.T) {
		cfg := chunkResumeConfig()
		repo, paths := chunkResumeRepo(t, 4)
		dir := partialBaselineReview(t, cfg, repo, paths, 1)
		poolDir := filepath.Join(dir, "sources", "pool")
		rewriteChunkRecord(t, poolDir, func(rec *chunkRecord) {
			for i := range rec.Bins {
				rec.Bins[i].Outcome = StatusOK
			}
		})
		require.NoError(t, os.Remove(filepath.Join(poolDir, poolRawAgentDir, "greta", statusFile)))

		rprep, info, err := PrepareResume(context.Background(), cfg, dir, repoReq(repo, ""))
		require.NoError(t, err)
		assert.Equal(t, []string{"greta"}, info.Pending)
		assert.Equal(t, paths, slotFiles(rprep.Slots, "greta"))
	})
}

// T4 success criterion: the range and roster guards still fail closed, before
// any chunk record is consulted.
func TestChunkResume_GuardsStillFailClosed(t *testing.T) {
	cfg := chunkResumeConfig()
	repo, paths := chunkResumeRepo(t, 4)
	dir := partialBaselineReview(t, cfg, repo, paths, 1)

	// Epic 35.16.14 T1: the work ledger is guarded by the same identity. A
	// drifted roster fails closed before the ledger is read or rewritten.
	t.Run("roster", func(t *testing.T) {
		ledger := filepath.Join(dir, "sources", "pool", poolRawAgentDir, "greta", chunksFile)
		before, err := os.ReadFile(ledger)
		require.NoError(t, err)
		require.Positive(t, readChunkRecord(t, filepath.Join(dir, "sources", "pool"), "greta").Bins[0].Attempts,
			"the ledger carries the attempts of the bins it resolved")

		changed := chunkResumeConfig()
		changed.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
		_, _, err = PrepareResume(context.Background(), changed, dir, repoReq(repo, ""))
		assert.True(t, errors.Is(err, ErrRosterChanged), "got %v", err)

		after, err := os.ReadFile(ledger)
		require.NoError(t, err)
		assert.Equal(t, before, after, "a refused resume leaves the ledger untouched")
	})

	t.Run("range", func(t *testing.T) {
		m, err := ReadManifest(dir)
		require.NoError(t, err)
		m.Baseline = false
		m.Base, m.Head = "aaa111", "bbb222"
		require.NoError(t, WriteManifest(dir, m))
		req := repoReq(repo, "")
		req.Range = ReviewRange{Base: "aaa111", Head: "ccc333"}
		_, _, err = PrepareResume(context.Background(), cfg, dir, req)
		assert.True(t, errors.Is(err, ErrRangeChanged), "got %v", err)
	})
}

// recordedChunks re-partitions a recorded bin that no longer fits the budget
// and keeps the fresh payload's entries, never the recorded strings.
func TestRecordedChunks_SplitsOversizeBinsAndKeepsFreshEntries(t *testing.T) {
	entries := []payload.FileEntry{baselineEntry("a.go", 100), baselineEntry("b.go", 100), baselineEntry("c.go", 100)}
	chunks, err := recordedChunks(entries, [][]string{{"c.go", "a.go", "../a.go"}, {}, {"b.go"}}, 150)
	require.NoError(t, err)
	require.Len(t, chunks, 3, "the two-file bin no longer fits 150 bytes and splits")
	var got [][]string
	for _, ck := range chunks {
		got = append(got, entryPaths(ck))
		for _, e := range ck {
			assert.NotEmpty(t, e.Body)
		}
	}
	assert.ElementsMatch(t, [][]string{{"c.go"}, {"a.go"}, {"b.go"}}, got)
}

// narrowReviewed keeps the files a dispatched slot carries and the files a
// recorded bin reviewed at their current hash, and drops every other file.
func TestNarrowReviewed_DropsFilesNothingReviewed(t *testing.T) {
	b := &baselineWriteback{reviewed: map[string]string{"a.go": "h-a", "b.go": "h-b", "done.go": "h-d", "new.go": "h-n"}}
	narrowReviewed(b, []Slot{
		{Primary: Agent{Name: "greta", chunkFiles: []string{"a.go"}}},
		{Primary: Agent{Name: "kai", chunkFiles: []string{"b.go", "gone.go"}}},
	}, map[string]map[string]bool{"done.go": {"h-old": true, "h-d": true}, "new.go": {"h-old": true}, "gone-too.go": {"h-g": true}})
	assert.Equal(t, map[string]string{"a.go": "h-a", "b.go": "h-b", "done.go": "h-d"}, b.reviewed,
		"done.go was reviewed at its current hash; new.go only at another content")
}

// A recorded bin counts only the files it reviewed: none for a failed bin or one
// whose lane contributed nothing, and only ServedFiles for a re-packed bin.
func TestReadResumeRecord_ReviewedFollowsTheBinPredicate(t *testing.T) {
	cfg := chunkResumeConfig()
	repo, paths := chunkResumeRepo(t, 4)
	dir := partialBaselineReview(t, cfg, repo, paths, 4)
	for _, name := range []string{"greta", "kai"} {
		writeCoverageRecord(t, dir, name, []chunkRecordBin{
			{Index: 0, Files: []string{"ok.go"}, Outcome: StatusOK, Hashes: map[string]string{"ok.go": "h-ok-" + name}},
			{Index: 1, Files: []string{"failed.go"}, Outcome: StatusFailed, Hashes: map[string]string{"failed.go": "h-f"}},
			{Index: 2, Files: []string{"refused.go"}, Outcome: StatusOK, ContributedNothing: true, Hashes: map[string]string{"refused.go": "h-r"}},
			{Index: 3, Files: []string{"served.go", "shed.go"}, Outcome: StatusOK, RePacked: true, ServedFiles: []string{"served.go"},
				Hashes: map[string]string{"served.go": "h-s", "shed.go": "h-x"}},
		})
	}
	rr, err := readResumeRecord(dir, []string{"greta", "kai"}, true)
	require.NoError(t, err)
	assert.Equal(t, map[string]map[string]bool{
		"ok.go":     {"h-ok-greta": true, "h-ok-kai": true},
		"served.go": {"h-s": true},
	}, rr.reviewed, "each reviewed file keeps every hash a bin reviewed it at")
}
