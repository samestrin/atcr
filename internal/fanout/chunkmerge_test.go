package fanout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sixtyFourFiles returns 64 100-byte files a 100-byte budget packs one per bin.
func sixtyFourFiles() []payload.FileEntry {
	var entries []payload.FileEntry
	for i := 0; i < 64; i++ {
		entries = append(entries, baselineEntry(fmt.Sprintf("f%02d.go", i), 100))
	}
	return entries
}

// failingFilesCompleter errors for every bin carrying one of failFiles and
// answers every other bin with one finding per file.
type failingFilesCompleter struct{ failFiles map[string]bool }

func (c failingFilesCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	for f := range c.failFiles {
		if strings.Contains(inv.Prompt, "// FILE:"+f+"\n") {
			return "", fmt.Errorf("simulated bin failure for %s", f)
		}
	}
	return baselineChunkFindingCompleter{}.Complete(ctx, inv)
}

func gretaBaselineSlots(t *testing.T, entries []payload.FileEntry) []Slot {
	t.Helper()
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 100
	slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", false, true)
	require.NoError(t, err)
	require.Len(t, slots, len(entries), "one bin per file")
	return slots
}

// originalRun reviews all 64 files with only the first `reviewed` bins
// succeeding, writes the pool exactly as ExecuteReview does, and returns the
// review dir.
func originalRun(t *testing.T, entries []payload.FileEntry, reviewed int) string {
	t.Helper()
	fail := map[string]bool{}
	for _, e := range entries[reviewed:] {
		fail[e.Path] = true
	}
	dir := t.TempDir()
	poolDir := filepath.Join(dir, "sources", "pool")
	results, _ := runEngine(context.Background(), failingFilesCompleter{fail}, &PreparedReview{Slots: gretaBaselineSlots(t, entries)}, poolDir)
	_, err := writePool(context.Background(), poolDir, results, nil, "")
	require.NoError(t, err)
	st := readAgentStatus(t, poolDir, "greta")
	require.Equal(t, StatusOK, st.Status)
	require.Equal(t, len(entries)-reviewed, st.UnreviewedChunks)
	return dir
}

func agentFindingFiles(t *testing.T, poolDir, agent string) []string {
	t.Helper()
	path := filepath.Join(poolDir, poolRawAgentDir, agent, findingsFile)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	pr, err := stream.ParseFindingsFile(path, data)
	require.NoError(t, err)
	var files []string
	for _, f := range pr.Findings {
		files = append(files, f.File)
	}
	return files
}

func resumeGreta(t *testing.T, dir string, slots []Slot, c Completer) {
	t.Helper()
	p := &PreparedReview{ID: "r", Dir: dir, Slots: slots, manifest: &payload.Manifest{Roster: []string{"greta"}}}
	_, err := ExecuteResume(context.Background(), c, p)
	require.NoError(t, err)
}

// Epic 35.16.13 T3 success criterion: a resume that re-runs 60 of a persona's 64
// bins keeps the 4 already-reviewed bins' findings, and unreviewed_chunks is
// recomputed from the union record rather than from the resumed bins alone.
func TestResumeMerge_SixtyOfSixtyFourKeepsReviewedBins(t *testing.T) {
	entries := sixtyFourFiles()

	t.Run("every resumed bin succeeds", func(t *testing.T) {
		dir := originalRun(t, entries, 4)
		poolDir := filepath.Join(dir, "sources", "pool")
		resumeGreta(t, dir, gretaBaselineSlots(t, entries[4:]), baselineChunkFindingCompleter{})

		var want []string
		for _, e := range entries {
			want = append(want, e.Path)
		}
		assert.ElementsMatch(t, want, agentFindingFiles(t, poolDir, "greta"), "the 4 reviewed bins' findings survive beside the 60 fresh ones")

		st := readAgentStatus(t, poolDir, "greta")
		assert.Equal(t, StatusOK, st.Status)
		assert.Equal(t, 0, st.UnreviewedChunks, "every bin of the union is reviewed")
		assert.Equal(t, 64, st.ChunkCount)
		assert.Equal(t, 64, st.FindingsCount)

		rec := readChunkRecord(t, poolDir, "greta")
		require.Len(t, rec.Bins, 64)
		for i, b := range rec.Bins {
			assert.Equal(t, i, b.Index)
			assert.Equal(t, StatusOK, b.Outcome, "bin %d", i)
		}
		for i := 0; i < 4; i++ {
			assert.Equal(t, []string{entries[i].Path}, rec.Bins[i].Files, "replayed bin %d leads the union", i)
		}

		sum, err := ReadPoolSummary(dir)
		require.NoError(t, err)
		assert.Equal(t, 64, sum.TotalFindings, "the rebuilt pool carries the union")

		assertCoverage(t, dir, Coverage{BinsReviewed: 64, BinsPlanned: 64, FilesCovered: 64, FilesInScope: 64})
	})

	t.Run("two resumed bins fail again", func(t *testing.T) {
		dir := originalRun(t, entries, 4)
		poolDir := filepath.Join(dir, "sources", "pool")
		resumeGreta(t, dir, gretaBaselineSlots(t, entries[4:]), failingFilesCompleter{map[string]bool{"f10.go": true, "f20.go": true}})

		files := agentFindingFiles(t, poolDir, "greta")
		assert.Len(t, files, 62)
		for i := 0; i < 4; i++ {
			assert.Contains(t, files, entries[i].Path)
		}
		st := readAgentStatus(t, poolDir, "greta")
		assert.Equal(t, StatusOK, st.Status)
		assert.Equal(t, 2, st.UnreviewedChunks, "recomputed over 64 bins, not the original 60")
		assert.Equal(t, 64, st.ChunkCount)
		assertCoverage(t, dir, Coverage{BinsReviewed: 62, BinsPlanned: 64, FilesCovered: 62, FilesInScope: 64})
	})

	t.Run("every resumed bin fails again", func(t *testing.T) {
		dir := originalRun(t, entries, 4)
		poolDir := filepath.Join(dir, "sources", "pool")
		fail := map[string]bool{}
		for _, e := range entries[4:] {
			fail[e.Path] = true
		}
		resumeGreta(t, dir, gretaBaselineSlots(t, entries[4:]), failingFilesCompleter{fail})

		assert.Len(t, agentFindingFiles(t, poolDir, "greta"), 4, "a failed resume still never erases the reviewed bins")
		st := readAgentStatus(t, poolDir, "greta")
		assert.Equal(t, StatusOK, st.Status)
		assert.Empty(t, st.Error)
		assert.Equal(t, 60, st.UnreviewedChunks)
	})
}

// A resume killed mid-fan-out leaves a chunks.json that is already the union:
// the replayed bins are on disk from the plan write, not only after the merge.
func TestResumeMerge_PlanRecordIsTheUnion(t *testing.T) {
	entries := sixtyFourFiles()
	dir := originalRun(t, entries, 4)
	poolDir := filepath.Join(dir, "sources", "pool")

	slots := gretaBaselineSlots(t, entries[4:])
	p := &PreparedReview{Slots: slots, priorChunks: readPriorChunkRecords(poolDir, slots)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &killAfterCompleter{n: 0, reached: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runEngine(ctx, c, p, poolDir)
	}()
	<-c.reached

	rec := readChunkRecord(t, poolDir, "greta")
	require.Len(t, rec.Bins, 64)
	for i, b := range rec.Bins {
		want := chunkOutcomePending
		if i < 4 {
			want = StatusOK
		}
		assert.Equal(t, want, b.Outcome, "bin %d", i)
	}
	cancel()
	<-done
}

// Inert paths: with no readable prior chunks.json, or with a resume that
// re-runs the persona's whole payload, writeResumedAgents writes exactly what
// it wrote before T3 — the wholesale writeAgentArtifacts output.
func TestResumeMerge_InertPathsAreByteIdentical(t *testing.T) {
	entries := sixtyFourFiles()
	cases := map[string]struct {
		resumeFiles []payload.FileEntry
		prepare     func(t *testing.T, poolDir string)
	}{
		"no chunks.json": {
			resumeFiles: entries[4:],
			prepare: func(t *testing.T, poolDir string) {
				require.NoError(t, os.Remove(filepath.Join(poolDir, poolRawAgentDir, "greta", chunksFile)))
			},
		},
		"unreadable chunks.json": {
			resumeFiles: entries[4:],
			prepare: func(t *testing.T, poolDir string) {
				require.NoError(t, os.WriteFile(filepath.Join(poolDir, poolRawAgentDir, "greta", chunksFile), []byte("{not json"), 0o644))
			},
		},
		"full-persona re-run": {
			resumeFiles: entries,
			prepare:     func(*testing.T, string) {},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := originalRun(t, entries, 4)
			poolDir := filepath.Join(dir, "sources", "pool")
			tc.prepare(t, poolDir)
			legacyPool := filepath.Join(t.TempDir(), "pool")
			require.NoError(t, os.CopyFS(legacyPool, os.DirFS(poolDir)))

			slots := gretaBaselineSlots(t, tc.resumeFiles)
			p := &PreparedReview{Slots: slots, priorChunks: readPriorChunkRecords(poolDir, slots)}
			results, _ := runEngine(context.Background(), baselineChunkFindingCompleter{}, p, poolDir)
			require.Nil(t, p.chunks.carriedFiles("greta"), "nothing is replayed")
			require.NoError(t, writeResumedAgents(poolDir, results, nil, p.chunks))

			// Today's body of writeResumedAgents, on the same results.
			for _, r := range results {
				require.NoError(t, writeAgentArtifacts(legacyPool, "greta", r, findingsFor(r, nil)))
			}
			for _, f := range []string{reviewFile, findingsFile, findingsToonFile, statusFile} {
				got, err := os.ReadFile(filepath.Join(poolDir, poolRawAgentDir, "greta", f))
				require.NoError(t, err)
				want, err := os.ReadFile(filepath.Join(legacyPool, poolRawAgentDir, "greta", f))
				require.NoError(t, err)
				assert.Equal(t, string(want), string(got), f)
			}
			assert.Len(t, readChunkRecord(t, poolDir, "greta").Bins, len(tc.resumeFiles), "the record holds only this run's bins")
		})
	}
}

// rewriteChunkRecord edits greta's on-disk chunks.json in place.
func rewriteChunkRecord(t *testing.T, poolDir string, edit func(*chunkRecord)) {
	t.Helper()
	rec := readChunkRecord(t, poolDir, "greta")
	edit(&rec)
	require.NoError(t, writeJSON(filepath.Join(poolDir, poolRawAgentDir, "greta", chunksFile), rec))
}

// readPriorChunkRecords refuses a record it cannot trust, and the resume then
// takes the wholesale path: only the 60 re-run files' findings remain.
func TestReadPriorChunkRecords_RefusesUntrustedRecords(t *testing.T) {
	entries := sixtyFourFiles()
	cases := map[string]func(t *testing.T, poolDir string){
		"unknown schema version": func(t *testing.T, poolDir string) {
			rewriteChunkRecord(t, poolDir, func(r *chunkRecord) { r.SchemaVersion = chunksSchemaVersion + 1 })
		},
		"record names another agent": func(t *testing.T, poolDir string) {
			rewriteChunkRecord(t, poolDir, func(r *chunkRecord) { r.Agent = "kai" })
		},
		// A run killed after chunks.json recorded bins ok but before the persona's
		// findings were written: replaying would claim bins whose findings exist
		// nowhere.
		"no findings file": func(t *testing.T, poolDir string) {
			agentDir := filepath.Join(poolDir, poolRawAgentDir, "greta")
			require.NoError(t, os.Remove(filepath.Join(agentDir, findingsFile)))
			require.NoError(t, os.Remove(filepath.Join(agentDir, findingsToonFile)))
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			dir := originalRun(t, entries, 4)
			poolDir := filepath.Join(dir, "sources", "pool")
			slots := gretaBaselineSlots(t, entries[4:])
			require.Contains(t, readPriorChunkRecords(poolDir, slots), "greta", "the untouched record is trusted")

			corrupt(t, poolDir)
			assert.NotContains(t, readPriorChunkRecords(poolDir, slots), "greta")

			resumeGreta(t, dir, slots, baselineChunkFindingCompleter{})
			files := agentFindingFiles(t, poolDir, "greta")
			assert.Len(t, files, 60, "nothing replayed: the wholesale path wrote only the re-run bins")
			assert.NotContains(t, files, entries[0].Path)
			assert.Len(t, readChunkRecord(t, poolDir, "greta").Bins, 60)
		})
	}
}

// A run with no chunk ledger (nothing chunked) has a nil *chunkLedger, and
// mergedEarly is nil-safe like carriedFiles: it answers false, not a panic.
func TestMergedEarly_NilLedger(t *testing.T) {
	var l *chunkLedger
	require.NotPanics(t, func() {
		assert.False(t, l.mergedEarly("greta"))
	})
}

// Epic 78 T4: an early write that fails after review.md landed (here at
// status.json) leaves the persona unmarked, so writeResumedAgents merges it
// again; that second merge must not append the fresh output to the review.md
// the first one already wrote. The result matches a resume with no failure.
func TestResumeMerge_FailedEarlyWriteDoesNotDuplicateReview(t *testing.T) {
	entries := sixtyFourFiles()
	resumed := func(t *testing.T, failEarly bool) (review string, findings []string) {
		t.Helper()
		dir := originalRun(t, entries, 4)
		poolDir := filepath.Join(dir, "sources", "pool")
		if failEarly {
			orig := writeStatusFn
			t.Cleanup(func() { writeStatusFn = orig })
			var calls int
			writeStatusFn = func(path string, s *AgentStatus) error {
				if calls++; calls == 1 {
					return fmt.Errorf("simulated status.json write failure")
				}
				return orig(path, s)
			}
		}
		resumeGreta(t, dir, gretaBaselineSlots(t, entries[4:]), baselineChunkFindingCompleter{})
		data, err := os.ReadFile(filepath.Join(poolDir, poolRawAgentDir, "greta", reviewFile))
		require.NoError(t, err)
		return string(data), agentFindingFiles(t, poolDir, "greta")
	}

	wantReview, wantFindings := resumed(t, false)
	gotReview, gotFindings := resumed(t, true)
	assert.Equal(t, strings.Count(wantReview, chunkBoundaryLine), strings.Count(gotReview, chunkBoundaryLine), "no chunk boundary is added twice")
	assert.Equal(t, wantReview, gotReview, "review.md carries the fresh output once")
	assert.ElementsMatch(t, wantFindings, gotFindings)
}

// An ok prior bin with no files is never carried: it would replay nothing and
// shift every fresh bin's index.
func TestCarryReviewed_SkipsEmptyBins(t *testing.T) {
	entries := sixtyFourFiles()
	slots := gretaBaselineSlots(t, entries[4:])
	l := newChunkLedger(log.Discard(), t.TempDir(), slots)
	require.NotNil(t, l)
	l.carryReviewed(map[string]*chunkRecord{"greta": {
		SchemaVersion: chunksSchemaVersion,
		Agent:         "greta",
		Bins: []chunkRecordBin{
			{Index: 0, Files: []string{entries[0].Path}, Outcome: StatusOK},
			{Index: 1, Files: []string{}, Outcome: StatusOK},
			{Index: 2, Files: nil, Outcome: StatusOK},
			{Index: 3, Files: []string{entries[1].Path}, Outcome: StatusOK},
		},
	}})

	bins := l.records["greta"].Bins
	require.Len(t, bins, 62, "two real replayed bins plus the 60 fresh ones")
	for i, b := range bins {
		assert.Equal(t, i, b.Index, "indices stay contiguous")
		assert.NotEmpty(t, b.Files, "bin %d", i)
	}
	assert.Equal(t, []string{entries[0].Path}, bins[0].Files)
	assert.Equal(t, []string{entries[1].Path}, bins[1].Files)
	assert.Equal(t, []string{entries[4].Path}, bins[2].Files, "the first fresh bin follows the replayed ones")
	for i := range slots {
		assert.Equal(t, i+2, l.bySlot[i].bin, "slot %d points at its shifted bin", i)
	}
}

// A resume over a record that holds T2's deferred bins. The original run of 70
// one-file bins dispatched 64 (the first 4 ok, the other 60 failed) and deferred
// 6. A deferred bin is never replayed as reviewed: it has no findings, and it
// counts toward the recomputed unreviewed_chunks.
func TestResumeMerge_DeferredBinsStayUnreviewed(t *testing.T) {
	var entries []payload.FileEntry
	for i := 0; i < 70; i++ {
		entries = append(entries, baselineEntry(fmt.Sprintf("f%02d.go", i), 100))
	}
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 100
	slotsFor := func(t *testing.T, es []payload.FileEntry) []Slot {
		t.Helper()
		slots, _, err := buildSlots(cfg, baselinePayloads(es), ReviewRange{}, string(payload.ModeFiles), "", false, true)
		require.NoError(t, err)
		return slots
	}
	originalRun := func(t *testing.T) string {
		t.Helper()
		fail := map[string]bool{}
		for _, e := range entries[4:] {
			fail[e.Path] = true
		}
		dir := t.TempDir()
		poolDir := filepath.Join(dir, "sources", "pool")
		results, _ := runEngine(context.Background(), failingFilesCompleter{fail}, &PreparedReview{Slots: withLegacyDeferral(slotsFor(t, entries))}, poolDir)
		_, err := writePool(context.Background(), poolDir, results, nil, "")
		require.NoError(t, err)
		rec := readChunkRecord(t, poolDir, "greta")
		require.Len(t, rec.Bins, 70)
		for _, b := range rec.Bins[64:] {
			require.Equal(t, chunkOutcomeDeferred, b.Outcome)
		}
		return dir
	}
	outcomes := func(rec chunkRecord) map[string]string {
		out := map[string]string{}
		for i, b := range rec.Bins {
			assert.Equal(t, i, b.Index)
			require.Len(t, b.Files, 1)
			_, dup := out[b.Files[0]]
			assert.False(t, dup, "%s recorded twice", b.Files[0])
			out[b.Files[0]] = b.Outcome
		}
		return out
	}

	t.Run("resume leaves the deferred bins undispatched", func(t *testing.T) {
		dir := originalRun(t)
		poolDir := filepath.Join(dir, "sources", "pool")
		slots := slotsFor(t, entries[4:64])
		require.Len(t, slots, 60)
		resumeGreta(t, dir, slots, baselineChunkFindingCompleter{})

		files := agentFindingFiles(t, poolDir, "greta")
		assert.Len(t, files, 64, "4 replayed + 60 fresh; deferred bins replay nothing")
		for _, e := range entries[64:] {
			assert.NotContains(t, files, e.Path)
		}

		got := outcomes(readChunkRecord(t, poolDir, "greta"))
		require.Len(t, got, 70, "the prior deferred bins stay in the union")
		for i, e := range entries {
			want := StatusOK
			if i >= 64 {
				want = chunkOutcomeDeferred
			}
			assert.Equal(t, want, got[e.Path], e.Path)
		}

		st := readAgentStatus(t, poolDir, "greta")
		assert.Equal(t, StatusOK, st.Status)
		assert.Equal(t, 6, st.UnreviewedChunks, "the 6 deferred bins are unreviewed")
		assert.Equal(t, 70, st.ChunkCount)
		// T6 over the union: the 6 carried deferred bins are this persona's
		// unreviewed files though the resume dispatched none of them.
		assert.Equal(t, 6, st.UnreviewedFiles, "carried deferred files stay unreviewed")
		assert.Equal(t, 6, st.DeferredFiles)
		sum, err := ReadPoolSummary(dir)
		require.NoError(t, err)
		require.Len(t, sum.Agents, 1)
		assert.Equal(t, 6, sum.Agents[0].UnreviewedFiles, "summary.json carries the union's gap too")
		assert.Equal(t, 6, sum.Agents[0].DeferredFiles)
		assertCoverage(t, dir, Coverage{BinsReviewed: 64, BinsPlanned: 70, FilesCovered: 64, FilesInScope: 70})
	})

	t.Run("resume dispatches some deferred bins and defers the rest again", func(t *testing.T) {
		dir := originalRun(t)
		poolDir := filepath.Join(dir, "sources", "pool")
		slots := withLegacyDeferral(slotsFor(t, entries[4:]))
		require.Len(t, slots, legacyCeiling, "66 bins: 64 dispatched, 2 deferred")
		resumeGreta(t, dir, slots, baselineChunkFindingCompleter{})

		files := agentFindingFiles(t, poolDir, "greta")
		assert.Len(t, files, 68, "4 replayed + 64 fresh")

		got := outcomes(readChunkRecord(t, poolDir, "greta"))
		require.Len(t, got, 70, "4 replayed + 64 fresh + 2 deferred again, none duplicated")
		for i, e := range entries {
			want := StatusOK
			if i >= 68 {
				want = chunkOutcomeDeferred
			}
			assert.Equal(t, want, got[e.Path], e.Path)
		}

		st := readAgentStatus(t, poolDir, "greta")
		assert.Equal(t, 2, st.UnreviewedChunks)
		assert.Equal(t, 70, st.ChunkCount)
		assert.Equal(t, 2, st.UnreviewedFiles, "deferred again by this run, counted once")
		assert.Equal(t, 2, st.DeferredFiles)
		assertCoverage(t, dir, Coverage{BinsReviewed: 68, BinsPlanned: 70, FilesCovered: 68, FilesInScope: 70})
	})
}

// assertCoverage checks the coverage T5 reads back from the resumed review's
// chunks.json: the figure behind the summary's coverage line and the
// --require-coverage gate.
func assertCoverage(t *testing.T, dir string, want Coverage) {
	t.Helper()
	cov, ok, err := ReadCoverage(dir)
	require.NoError(t, err)
	require.True(t, ok, "the resumed review is measured")
	assert.Equal(t, want, cov)
}

// A replayed bin keeps what the interrupted run recorded about it. An ok bin
// whose lane refused its content, or one a fallback re-packed to a subset, must
// not count as fully reviewed in T5's coverage just because a resume carried it.
func TestCarryReviewed_KeepsCoverageMarkers(t *testing.T) {
	entries := sixtyFourFiles()
	dir := originalRun(t, entries, 4)
	poolDir := filepath.Join(dir, "sources", "pool")
	rewriteChunkRecord(t, poolDir, func(r *chunkRecord) {
		r.Bins[0].ContributedNothing = true
		r.Bins[1].RePacked = true
		r.Bins[1].ServedFiles = nil
	})
	resumeGreta(t, dir, gretaBaselineSlots(t, entries[4:]), baselineChunkFindingCompleter{})

	rec := readChunkRecord(t, poolDir, "greta")
	require.Len(t, rec.Bins, 64)
	assert.True(t, rec.Bins[0].ContributedNothing)
	assert.True(t, rec.Bins[1].RePacked)
	assert.Empty(t, rec.Bins[1].ServedFiles)
	assert.False(t, rec.Bins[2].ContributedNothing || rec.Bins[2].RePacked)

	assertCoverage(t, dir, Coverage{BinsReviewed: 63, BinsPlanned: 64, FilesCovered: 62, FilesInScope: 64})

	// T6 reads the same markers: the refused bin and the re-packed bin that served
	// nothing leave 2 files unreviewed, none of them deferred.
	st := readAgentStatus(t, poolDir, "greta")
	assert.Equal(t, 2, st.UnreviewedFiles)
	assert.Zero(t, st.DeferredFiles)
}
