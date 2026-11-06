package fanout

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readBaselineManifest reads the manifest a prepared baseline review wrote.
func readBaselineManifest(t *testing.T, dir string) payload.Manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

// A baseline scan of a clean tree records HEAD's SHA as the review head, so the
// review's tool harness and a later verify both have a commit to snapshot. The
// base stays empty, so grounding still treats the run as range-less.
func TestPrepareReviewFromRepo_CleanTreeRecordsHead(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})
	head := fanoutGit(t, repo, "rev-parse", "HEAD")

	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "out")))
	require.NoError(t, err)

	assert.Equal(t, head, prep.Head, "the tool harness snapshots HEAD")
	m := readBaselineManifest(t, prep.Dir)
	assert.Equal(t, head, m.Head, "verify reads the head from the manifest")
	assert.Empty(t, m.Base, "a baseline scan still has no base")
	assert.True(t, m.Baseline)
	assert.Contains(t, prep.GroundingDisabledReason, "range-less", "grounding stays off on a baseline scan")
}

// An edit to a tracked file outside the --scope subtree does not change what the
// scan reviews, so the head is still recorded and a tool agent may run.
func TestPrepareReviewFromRepo_DirtyOutsideScopeStillRecordsHead(t *testing.T) {
	cfg := toolAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"svc/a.go": "package svc\n", "other.go": "package other\n"})
	head := fanoutGit(t, repo, "rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "other.go"), []byte("package other // edited\n"), 0o644))

	req := repoReq(repo, filepath.Join(t.TempDir(), "out"))
	req.Dir = "svc"
	prep, err := PrepareReviewFromRepo(context.Background(), cfg, req)
	require.NoError(t, err)
	assert.Equal(t, head, readBaselineManifest(t, prep.Dir).Head)
}

// A tracked file with uncommitted edits under the scope would be reviewed as it
// is on disk while the harness snapshots HEAD, so a run that needs the harness
// (a tool-enabled reviewer) refuses before scaffolding anything.
func TestPrepareReviewFromRepo_DirtyTreeRefusedForToolAgent(t *testing.T) {
	cfg := toolAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"svc/a.go": "package svc\n"})
	require.NoError(t, os.WriteFile(filepath.Join(repo, "svc", "a.go"), []byte("package svc // edited\n"), 0o644))

	out := filepath.Join(t.TempDir(), "out")
	req := repoReq(repo, out)
	req.Dir = "svc"
	_, err := PrepareReviewFromRepo(context.Background(), cfg, req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBaselineDirtyTree), "got %v", err)
	assert.Contains(t, err.Error(), "svc/a.go", "the refusal names the uncommitted file")
	assert.Contains(t, err.Error(), "commit or stash", "the refusal names the remedy")
	assert.NoDirExists(t, out, "nothing is scaffolded for a refused run")
}

// A staged-but-uncommitted edit is as dirty as an unstaged one.
func TestPrepareReviewFromRepo_StagedEditRefusedForVerify(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a // staged\n"), 0o644))
	fanoutGit(t, repo, "add", "a.go")

	req := repoReq(repo, filepath.Join(t.TempDir(), "out"))
	req.Verify = true
	_, err := PrepareReviewFromRepo(context.Background(), cfg, req)
	assert.True(t, errors.Is(err, ErrBaselineDirtyTree), "--verify snapshots the head, so a dirty tree is refused; got %v", err)
}

// A run that needs no snapshot (no tool agent, no --verify) still reviews a dirty
// tree as before, and records no head because HEAD is not what it reviewed.
func TestPrepareReviewFromRepo_DirtyTreeWithoutHarnessReviewsWithoutHead(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a // edited\n"), 0o644))

	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "out")))
	require.NoError(t, err)
	assert.Empty(t, prep.Head)
	assert.Empty(t, readBaselineManifest(t, prep.Dir).Head)
}

// The issue's scenario end to end on the review side: a --scope scan with a
// tool-enabled reviewer wires the tool harness, and the reviewer reads a file
// the payload never held (helper.go, outside the scope) from the HEAD snapshot.
func TestExecuteReview_ScopedBaselineWiresToolHarness(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo := baselineRepo(t, map[string]string{
		".gitignore":  ".atcr/\n",
		"svc/auth.go": "package svc\n\nfunc a() { b() }\n\nfunc b() {}\n",
		"helper.go":   "package main\n\n// helper documents the contract for b.\nfunc helper() {}\n",
	})
	head := fanoutGit(t, repo, "rev-parse", "HEAD")
	srv := toolMockProvider(t)
	cfg := toolAgentConfig(srv.URL)

	req := repoReq(repo, "")
	req.Dir = "svc"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	prep, err := PrepareReviewFromRepo(ctx, cfg, req)
	require.NoError(t, err)
	res, err := ExecuteReview(ctx, llmclient.New(), prep)
	require.NoError(t, err)
	require.Equal(t, 1, res.Summary.Succeeded)

	agentDir := filepath.Join(res.Dir, "sources", "pool", "raw", "agent", "greta")
	var st AgentStatus
	sdata, err := os.ReadFile(filepath.Join(agentDir, "status.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(sdata, &st))
	require.NotNil(t, st.ToolCalls, "the reviewer ran the tool loop, not single-shot")
	assert.Equal(t, 2, *st.ToolCalls)

	tr, err := tools.ReplayTranscript(filepath.Join(agentDir, "transcript.jsonl"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(tr.Events), 2)
	assert.Contains(t, string(tr.Events[1].Raw["content"]), "helper documents the contract",
		"read_file served helper.go from the HEAD snapshot")

	var raw map[string]json.RawMessage
	mdata, err := os.ReadFile(filepath.Join(res.Dir, "manifest.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(mdata, &raw))
	var stage struct {
		HeadSHA string `json:"head_sha"`
	}
	require.NoError(t, json.Unmarshal(raw["review"], &stage))
	assert.Equal(t, head, stage.HeadSHA)
}

// A resumed baseline review snapshots the head the interrupted run recorded, so
// its pending tool agents run the tool loop like the completed ones did.
func TestPrepareResume_BaselineReusesRecordedHead(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})
	head := fanoutGit(t, repo, "rev-parse", "HEAD")
	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "review")))
	require.NoError(t, err)

	rprep, _, err := PrepareResume(context.Background(), cfg, prep.Dir, repoReq(repo, ""))
	require.NoError(t, err)
	assert.Equal(t, head, rprep.Head)
}

// A baseline review recorded at one HEAD resumes without a head after HEAD
// moved or the scope gained uncommitted edits: the rebuilt payload is read from
// disk, which the recorded head no longer holds, so pending tool agents run
// single-shot rather than read a snapshot that disagrees with their payload.
// Editing before a baseline resume stays supported (see chunkresume_test.go).
func TestPrepareResume_BaselineTreeChangedResumesHeadless(t *testing.T) {
	for name, change := range map[string]func(t *testing.T, repo string){
		"head moved": func(t *testing.T, repo string) {
			require.NoError(t, os.WriteFile(filepath.Join(repo, "b.go"), []byte("package a\n"), 0o644))
			fanoutGit(t, repo, "add", "b.go")
			fanoutGit(t, repo, "commit", "-q", "-m", "later")
		},
		"dirty tree": func(t *testing.T, repo string) {
			require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a // edited\n"), 0o644))
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := twoAgentConfig("http://unused")
			repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})
			prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "review")))
			require.NoError(t, err)
			change(t, repo)

			rprep, _, err := PrepareResume(context.Background(), cfg, prep.Dir, repoReq(repo, ""))
			require.NoError(t, err)
			assert.Empty(t, rprep.Head)
		})
	}
}

// A baseline manifest written before the head was recorded resumes as it always
// did: no head, no check against the current tree.
func TestPrepareResume_BaselineWithoutRecordedHeadResumesHeadless(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})
	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "review")))
	require.NoError(t, err)
	m, err := ReadManifest(prep.Dir)
	require.NoError(t, err)
	m.Head = ""
	require.NoError(t, WriteManifest(prep.Dir, m))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a // edited\n"), 0o644))

	rprep, _, err := PrepareResume(context.Background(), cfg, prep.Dir, repoReq(repo, ""))
	require.NoError(t, err)
	assert.Empty(t, rprep.Head)
}
