package fanout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
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
