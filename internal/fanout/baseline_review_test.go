package fanout

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baselineRepo builds a temp git repo committing the given path→content files
// (an --allow-empty commit when the map is empty), for the range-less --all path.
func baselineRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	run("config", "commit.gpgsign", "false")
	for p, c := range files {
		full := filepath.Join(dir, p)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(c), 0o644))
	}
	run("add", "-A")
	if len(files) == 0 {
		run("commit", "-q", "--allow-empty", "-m", "empty")
	} else {
		run("commit", "-q", "-m", "init")
	}
	return dir
}

// repoReq builds a range-less baseline ReviewRequest (Repo == Root == the repo).
// out != "" redirects to an explicit OutputDir (no .atcr tree needed).
func repoReq(dir, out string) ReviewRequest {
	return ReviewRequest{
		Repo:       dir,
		Root:       dir,
		OutputDir:  out,
		Branch:     "feature/test",
		Date:       "2026-06-10",
		TimeSuffix: "120000",
		StartedAt:  time.Unix(1000, 0).UTC(),
	}
}

// AC 01-04 Happy Path 1: a valid repo produces a non-nil PreparedReview with slots
// built from the payload and req.Range left zero-valued (manifest records no range).
func TestPrepareReviewFromRepo_ScaffoldsWithZeroRange(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	out := filepath.Join(t.TempDir(), "ext-review")
	repo := baselineRepo(t, map[string]string{
		"a.go":          "package a\n\nfunc A() {}\n",
		"internal/b.go": "package internal\n\nfunc B() {}\n",
	})
	req := repoReq(repo, out)

	prep, err := PrepareReviewFromRepo(context.Background(), cfg, req)
	require.NoError(t, err)
	require.NotNil(t, prep)
	assert.Equal(t, out, prep.Dir)
	assert.Equal(t, 2, prep.AgentCount(), "both roster agents become slots")

	// Manifest: files mode for every agent, range fields empty (no git range).
	mdata, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(mdata, &m))
	assert.Equal(t, "files", m.PayloadMode)
	assert.Equal(t, "files", m.PerAgentPayload["greta"])
	assert.Equal(t, "files", m.PerAgentPayload["kai"])
	assert.Empty(t, m.Base, "a baseline scan has no base")
	assert.Empty(t, m.Head, "a baseline scan has no head")
	assert.ElementsMatch(t, []string{"greta", "kai"}, m.Roster)
}

// AC 01-04 Happy Path 2: the shared finalizePreparedReview scaffold tail runs — the
// files payload artifact is written and (on the default path) .atcr/latest updates.
func TestPrepareReviewFromRepo_FinalizeScaffoldTail(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})
	req := repoReq(repo, "") // default path: .atcr/reviews/<id>/ under Root

	prep, err := PrepareReviewFromRepo(context.Background(), cfg, req)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(prep.Dir, "payload", "files.txt"))
	latest, err := ReadLatest(repo)
	require.NoError(t, err)
	assert.Equal(t, prep.ID, latest, ".atcr/latest repointed on the default path")
}

// AC 01-04 Happy Path 3: grounding is disabled for the range-less path via the
// existing computeGroundingData early return (rb=nil, no base/head).
func TestPrepareReviewFromRepo_GroundingDisabled(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})
	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "o")))
	require.NoError(t, err)
	assert.Nil(t, prep.Changed, "no changed-line grounding for a range-less scan")
	assert.NotEmpty(t, prep.GroundingDisabledReason, "the disabled reason must be recorded")
}

// AC 01-04 Edge Case 1: an empty roster is rejected before scaffolding.
func TestPrepareReviewFromRepo_EmptyRosterRejected(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project.Agents = nil
	out := filepath.Join(t.TempDir(), "ext-review")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})

	_, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, out))
	require.ErrorIs(t, err, ErrEmptyRoster)
	assert.NoDirExists(t, out, "no scaffold for an empty roster")
}

// AC 01-04 Edge Case 2: OutputDir and IDOverride are mutually exclusive.
func TestPrepareReviewFromRepo_OutputDirAndIDOverrideMutuallyExclusive(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})
	req := repoReq(repo, filepath.Join(t.TempDir(), "ext-review"))
	req.IDOverride = "custom-id"

	_, err := PrepareReviewFromRepo(context.Background(), cfg, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OutputDir")
	assert.Contains(t, err.Error(), "IDOverride")
}

// AC 01-04 Edge Case 3 / Error Scenario 1: a repo with zero reviewable tracked
// files surfaces ErrNoReviewableContent before any directory is scaffolded.
func TestPrepareReviewFromRepo_NoReviewableFiles(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	out := filepath.Join(t.TempDir(), "ext-review")
	repo := baselineRepo(t, nil) // empty commit, zero tracked files

	_, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, out))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoReviewableContent)
	assert.NoDirExists(t, out, "no scaffold when there is nothing to review")
}

// AC 01-04 Error Scenario 2: a payload build failure (root is not a git repo)
// propagates the fullrepo error and scaffolds nothing.
func TestPrepareReviewFromRepo_NonRepoErrorPropagates(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	out := filepath.Join(t.TempDir(), "ext-review")
	nonRepo := t.TempDir() // not a git repo
	req := repoReq(nonRepo, out)

	_, err := PrepareReviewFromRepo(context.Background(), cfg, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not enumerate tracked files")
	assert.NoDirExists(t, out, "a non-repo must not scaffold a review")
}

// The exported payload entry point BuildRepoEntries returns the enumerated,
// ignore-filtered tracked files for a repo root (AC 01-04's payload source).
func TestBuildRepoEntries_ReturnsTrackedFiles(t *testing.T) {
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n"})
	entries, err := payload.BuildRepoEntries(context.Background(), repo, log.Discard(), false, "", nil, false)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	paths := []string{entries[0].Path, entries[1].Path}
	assert.ElementsMatch(t, []string{"a.go", "b.go"}, paths)
}

// TD-008: when every in-scope tracked file is ignore-filtered, the baseline
// path must hint at --no-ignore (mirroring the diff path's AllIgnored behavior)
// instead of the generic "no reviewable tracked files" message an empty
// repository produces. The .atcrignore "*" pattern filters every candidate
// (including .atcrignore itself) while git still tracks the files.
func TestPrepareReviewFromRepo_AllIgnoreFilteredHintsNoIgnore(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	out := filepath.Join(t.TempDir(), "ext-review")
	repo := baselineRepo(t, map[string]string{
		".atcrignore": "*\n",
		"a.go":        "package a\n",
	})

	_, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, out))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoReviewableContent)
	assert.Contains(t, err.Error(), "--no-ignore",
		"an all-ignore-filtered scan must point at the recovery flag, matching the diff path")
	assert.NoDirExists(t, out, "no scaffold when there is nothing to review")
}

// TD-010 (fanout contract): a baseline re-scan whose every in-scope candidate
// is skipped by the incremental hash index (nothing changed since the last
// completed review) is reported as ErrAllFilesUnchanged — distinct from the
// ErrNoReviewableContent an empty repository gets — so the CLI can exit 0 with
// a notice instead of erroring as if the repo were empty.
func TestPrepareReviewFromRepo_AllUnchangedReportsDistinctError(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})

	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "r1")))
	require.NoError(t, err)
	require.NoError(t, prep.CommitBaselineIndex("run-1"), "simulate a completed run's write-back")

	out2 := filepath.Join(t.TempDir(), "r2")
	_, err = PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, out2))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAllFilesUnchanged)
	assert.NotErrorIs(t, err, ErrNoReviewableContent)
	assert.Contains(t, err.Error(), "1 file(s) unchanged since last review")
	assert.NoDirExists(t, out2, "no scaffold for a no-change re-scan")
}

// TD-012: the baseline truncation warning must not claim "reviewing a subset of
// the repository" — the Phase 5 baseline fan-out chunks the full pre-budget
// entry set via PartitionByBudget (which drops nothing), so the whole
// repository IS reviewed across chunks; the global byte budget only bounds the
// concatenated payload text / per-chunk sizing.
func TestPrepareReviewFromRepo_TruncationWarnDoesNotClaimSubset(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = 12 // keeps a.go (10B), sheds b.go → trunc.Truncated
	out := filepath.Join(t.TempDir(), "ext-review")
	repo := baselineRepo(t, map[string]string{
		"a.go": "package a\n",
		"b.go": "package b\n",
	})

	var buf bytes.Buffer
	capture := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := log.NewContext(context.Background(), capture)

	_, err := PrepareReviewFromRepo(ctx, cfg, repoReq(repo, out))
	require.NoError(t, err)
	got := buf.String()
	assert.Contains(t, got, "byte budget", "the global-budget truncation stays observable")
	assert.NotContains(t, got, "reviewing a subset of the repository",
		"the baseline fan-out reviews every enumerated file across chunks — the warning must not claim a subset review")
}
