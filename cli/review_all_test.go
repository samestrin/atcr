package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initBaselineRepo commits a small multi-file tracked set (including a.txt, which
// the liveMockProvider's finding references so it survives path validation), for
// the range-less `atcr review --all` path.
func initBaselineRepo(t *testing.T) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	require.NoError(t, os.WriteFile("a.txt", []byte("one\n"), 0o644))
	require.NoError(t, os.WriteFile("b.go", []byte("package b\n"), 0o644))
	require.NoError(t, os.MkdirAll("internal", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("internal", "c.go"), []byte("package c\n"), 0o644))
	run("add", "-A")
	run("commit", "-q", "-m", "init")
}

func latestReviewDir(t *testing.T) string {
	t.Helper()
	latest, err := fanout.ReadLatest(".")
	require.NoError(t, err)
	return filepath.Join(fanout.ReviewsRoot("."), latest)
}

// AC 01-05 Happy Path 1 (Phase 2 single-payload scope, TD-004): `atcr review --all`
// runs to completion and writes a full review tree covering every tracked file,
// with the manifest recording no git range (proving gitrange.Resolve was skipped).
func TestReviewAll_EndToEndWholeRepo(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code := execCmd(t, "review", "--all")
	require.Equal(t, 0, code, "a completed baseline review exits 0")

	dir := latestReviewDir(t)
	for _, sub := range []string{"payload", "sources", "reconciled"} {
		assert.DirExists(t, filepath.Join(dir, sub))
	}
	assert.FileExists(t, filepath.Join(dir, "manifest.json"))

	// Whole-repo coverage: the single files-mode payload contains every tracked
	// file's content (no git ls-files omissions on the single-payload path).
	body, err := os.ReadFile(filepath.Join(dir, "payload", "files.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "package b", "b.go must be in the baseline payload")
	assert.Contains(t, string(body), "package c", "internal/c.go must be in the baseline payload")
	assert.Contains(t, string(body), "one", "a.txt must be in the baseline payload")

	// Manifest records no base/head — gitrange.Resolve was never invoked.
	mdata, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(mdata, &m))
	assert.Equal(t, "files", m.PayloadMode)
	assert.Empty(t, m.Base, "baseline scan has no base")
	assert.Empty(t, m.Head, "baseline scan has no head")

	// The reviewer produced a source under the standard pool layout.
	assert.FileExists(t, filepath.Join(dir, "sources", "pool", "raw", "agent", "bruce", "findings.txt"))
}

// AC 01-05 Happy Path 2: `--all --fail-on high` gates non-zero when a surviving
// high-or-above finding exists — the reconcile/gate chain needs no --all branching.
func TestReviewAll_FailOnGate(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t) // returns a CRITICAL finding on a.txt:1
	liveReviewConfig(t, srv.URL, "bruce")

	code := execCmd(t, "review", "--all", "--fail-on", "high")
	assert.Equal(t, 1, code, "a surviving CRITICAL must fail the high gate on the --all path")
}

// AC 01-05 Edge Case 3: an interrupted `--all` review is resumable via the existing
// resume machinery. A baseline manifest records an empty git range, so resume must
// skip range re-resolution/validation (else ErrRangeChanged, exit 2) and rebuild the
// payload from the repo walker so the pending agent reviews the same whole-repo scan
// the completed agents saw (Sprint 35.0 task 2.14.A HIGH fix).
func TestReviewAll_BaselineReviewIsResumable(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "kai")

	require.Equal(t, 0, execCmd(t, "review", "--all"))
	dir := latestReviewDir(t)

	// Simulate an interrupted run: drop one agent's completed source so resume has a
	// pending agent to finish.
	kaiDir := filepath.Join(dir, "sources", "pool", "raw", "agent", "kai")
	require.DirExists(t, kaiDir)
	require.NoError(t, os.RemoveAll(kaiDir))

	code := execCmd(t, "review", "--resume", "latest")
	require.Equal(t, 0, code, "a baseline review must be resumable (no ErrRangeChanged)")
	assert.FileExists(t, filepath.Join(kaiDir, "findings.txt"), "the pending baseline agent must be re-reviewed via the repo payload")
}

// TD-011 (via CLI): a completed baseline RESUME persists the incremental
// file-hash index exactly like a fresh --all run, so the next --all/--dir can
// skip unchanged files instead of doing a full re-scan.
func TestReviewAll_BaselineResumeWritesHashIndex(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "kai")

	require.Equal(t, 0, execCmd(t, "review", "--all"))
	dir := latestReviewDir(t)

	// Simulate the interrupted-run state TD-011 covers: the original run never
	// persisted the index (the write-back fires on completion), and one agent is
	// still pending.
	require.NoError(t, os.Remove(payload.FileHashIndexPath(".")))
	kaiDir := filepath.Join(dir, "sources", "pool", "raw", "agent", "kai")
	require.NoError(t, os.RemoveAll(kaiDir))

	require.Equal(t, 0, execCmd(t, "review", "--resume", "latest"))

	idxPath := payload.FileHashIndexPath(".")
	require.FileExists(t, idxPath, "a completed baseline resume must persist the file-hash index (TD-011)")
	idx := payload.Load(idxPath, nil)
	for _, p := range []string{"a.txt", "b.go", "internal/c.go"} {
		_, _, ok := idx.Get(p)
		assert.Truef(t, ok, "index must record %s after the resumed run", p)
	}
}

// Phase-2 gate LOW: `--resume --all` is rejected up front (exit 2) rather than
// silently ignoring --all — a resume continues the original review's mode, so
// baseline-ness comes from the manifest, not this flag.
func TestReviewAll_ResumeRejectsAllFlag(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "review", "--resume", "latest", "--all")
	require.Equal(t, 2, code)
	require.Contains(t, out, "--resume does not support --all")
}

// TD (resume.go): since Sprint 35.0, --fresh has a second meaning beyond the
// --verify stage — bypassing the baseline file-hash skip (review.go). The
// resume rejection must acknowledge both meanings instead of the stale
// "only applies to --verify" reason.
func TestReviewAll_ResumeRejectsFreshFlag(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "review", "--resume", "latest", "--fresh")
	require.Equal(t, 2, code)
	require.Contains(t, out, "--resume does not support --fresh")
	require.Contains(t, out, "baseline", "the --fresh rejection must acknowledge its baseline re-scan meaning")
	require.NotContains(t, out, "only applies to --verify", "stale single-meaning rejection reason")
}

// TD (resume.go): runResume fail-closes on every fresh-only mode flag except
// --base/--head/--merge-commit. On a baseline resume the range-resolution block
// is skipped (m.Baseline), so those flags were silently ignored — the one
// mode-flag class slipping the net. A baseline resume must reject them like the
// other fresh-only flags; a non-baseline resume still consumes/validates them.
func TestReviewAll_BaselineResumeRejectsRangeFlags(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--all"))

	code, out := execCmdCapture(t, "review", "--resume", "latest", "--base", "main")
	require.Equal(t, 2, code)
	require.Contains(t, out, "--resume does not support --base")
}

// AC 01-05 Happy Path 3: `atcr reconcile <id>` and `atcr report <id>` work
// unmodified against an --all-produced review directory (provenance-agnostic).
func TestReviewAll_ReconcileAndReportWorkOnBaselineOutput(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--all"))
	latest, err := fanout.ReadLatest(".")
	require.NoError(t, err)

	assert.Equal(t, 0, execCmd(t, "reconcile", latest), "reconcile works on baseline output")
	assert.Equal(t, 0, execCmd(t, "report", latest), "report works on baseline output")
}

// baselineFilesPayload returns the whole-repo files-mode payload written for the most
// recent review (the observable "candidate list" for the skip-filter assertions).
func baselineFilesPayload(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(latestReviewDir(t), "payload", "files.txt"))
	require.NoError(t, err)
	return string(body)
}

// AC 04-01 (via CLI) / AC 04-04 Edge Case 2: a completed `--all` run writes the
// file-hash index recording every reviewed tracked file under the run id.
func TestReviewAll_WritesHashIndexOnCompletion(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--all"))

	idxPath := payload.FileHashIndexPath(".")
	require.FileExists(t, idxPath, "a completed baseline run must persist the file-hash index")
	idx := payload.Load(idxPath, nil)
	for _, p := range []string{"a.txt", "b.go", "internal/c.go"} {
		hash, runID, ok := idx.Get(p)
		require.Truef(t, ok, "index must record %s", p)
		assert.Truef(t, len(hash) == len("sha256:")+64, "%s hash must be a canonical digest", p)
		assert.NotEmpty(t, runID, "%s must carry a last-reviewed run id", p)
	}
}

// AC 04-02 (via CLI): a second `--all` run reviews ONLY the file whose content
// changed; the unchanged files are skipped pre-chunking and never enter the payload.
func TestReviewAll_SkipsUnchangedOnSecondRun(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--all")) // pass 1: reviews all, writes index

	// Change only a.txt (the file the mock finding references, so it stays in scope).
	require.NoError(t, os.WriteFile("a.txt", []byte("one CHANGED\n"), 0o644))

	require.Equal(t, 0, execCmd(t, "review", "--all")) // pass 2: only a.txt is a candidate
	body := baselineFilesPayload(t)
	assert.Contains(t, body, "one CHANGED", "the changed file must be re-reviewed")
	assert.NotContains(t, body, "package b", "unchanged b.go must be skipped pre-chunking")
	assert.NotContains(t, body, "package c", "unchanged internal/c.go must be skipped pre-chunking")
}

// TD-010 (via CLI): re-running `review --all` when NOTHING changed must exit 0
// with a "nothing to review" notice — the common CI re-run path — instead of
// the exit-2 usage error ("the repository contains no reviewable tracked
// files") an empty repository gets.
func TestReviewAll_NothingChangedExitsZero(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--all")) // pass 1: reviews all, writes index

	code, out := execCmdCapture(t, "review", "--all") // pass 2: every file unchanged
	assert.Equal(t, 0, code, "a no-change re-run is a successful no-op, not a usage error")
	assert.Contains(t, out, "unchanged since last review")
	assert.Contains(t, out, "--fresh", "the notice must point at the full-rescan escape hatch")
}

// AC 04-04 Scenario 1 / Edge Case 2: `--all --fresh` bypasses the skip index and
// re-reviews every file even when all recorded hashes match, and still writes the
// index on completion.
func TestReviewAllFresh_BypassesSkipIndex(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--all")) // writes an index matching every file

	// Nothing changed; without --fresh pass 2 would skip everything. --fresh forces all.
	require.Equal(t, 0, execCmd(t, "review", "--all", "--fresh"))
	body := baselineFilesPayload(t)
	assert.Contains(t, body, "one", "--fresh must re-review a.txt despite a matching hash")
	assert.Contains(t, body, "package b", "--fresh must re-review b.go despite a matching hash")
	assert.Contains(t, body, "package c", "--fresh must re-review internal/c.go despite a matching hash")
	assert.FileExists(t, payload.FileHashIndexPath("."), "--fresh still writes the index on completion")
}

// gitRun runs a git command in the CWD with a hermetic env (for tests adding files
// to the isolate() fixture after initBaselineRepo).
func gitRun(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// gitLsFiles returns the repo's tracked-file set (the coverage oracle for the
// "zero files silently omitted" bar), slash-normalized repo-root-relative paths.
func gitLsFiles(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("git", "ls-files").Output()
	require.NoError(t, err)
	var files []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			files = append(files, l)
		}
	}
	return files
}

// TestReviewAll_FullBaselineChain_MultiChunkMultiPersona is Sprint 35.0's Phase 6
// capstone fixture (task 6.1). It drives the ENTIRE baseline pipeline end-to-end in a
// single run — `review --all` (sized to force 2+ chunks, rostered with 2+ personas) ->
// ExecuteReview -> RunReconcile -> `debt add` (filing a real reconciled baseline
// finding) -> `report` — asserting exit 0 at every stage and zero tracked files
// silently omitted across chunks (AC 01-05's coverage bar at combined-chain scale).
//
// Fixture shape: initBaselineRepo (a.txt, b.go, internal/c.go) + two 80 KB files.
// Each 80 KB file exceeds the per-agent effective budget yet the total stays under the
// 512 KiB global budget, so each lands in its own baseline chunk (3+ chunks total);
// the roster (bruce, kai) fans every chunk out across 2 personas.
//
// Gap this closes: every other --all E2E test in this file exercises only a SEGMENT of
// the chain — multi-chunk index write-back (TestReviewAll_MultiChunkRecordsAllReviewedFiles,
// single persona), multi-persona resume (TestReviewAll_BaselineReviewIsResumable, not
// multi-chunk, no debt add/report), reconcile+report (single persona), standalone flag-mode
// debt add. None drives the combined chain with 2+ chunks AND 2+ personas through debt add
// and report together. Co-located untagged (not //go:build integration) so Phase 6.2's
// `go test ./...` DoD sweep exercises the capstone, matching its sibling --all E2E tests;
// the mock provider is in-process (httptest, no docker/network), so it belongs in the fast
// suite exactly like those siblings.
func TestReviewAll_FullBaselineChain_MultiChunkMultiPersona(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	// Two oversized files each exceed the per-agent budget (~71680 bytes) but their
	// combined size stays under the 512 KiB global budget, so each becomes its own
	// baseline chunk — the scan fans out across 3+ chunks.
	big := make([]byte, 80_000)
	for i := range big {
		big[i] = 'x'
	}
	require.NoError(t, os.WriteFile("big1.txt", big, 0o644))
	require.NoError(t, os.WriteFile("big2.txt", big, 0o644))
	gitRun(t, "add", "-A")
	gitRun(t, "commit", "-qm", "add big files")

	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "kai") // 2+ personas

	// Stage 1: review --all -> ExecuteReview.
	require.Equal(t, 0, execCmd(t, "review", "--all"), "baseline review exits 0")
	dir := latestReviewDir(t)

	// Zero omissions: every git ls-files tracked file is recorded in the hash index.
	// The index write-back spans all chunks, so a missing entry means an unreviewed
	// chunk — the exact multi-chunk coverage regression Phase 5 (task 5.2.A) guarded.
	// This index-presence proxy implies "reviewed" because the write-back is gated to
	// skip entirely on any unreviewed chunk (task 5.6, pinned by
	// TestReviewAll_PartialChunkFailureSkipsIndexWrite) — a recorded entry cannot exist
	// for a file whose chunk was not reviewed.
	idx := payload.Load(payload.FileHashIndexPath("."), nil)
	for _, p := range gitLsFiles(t) {
		_, _, ok := idx.Get(p)
		assert.Truef(t, ok, "tracked file %s must be reviewed and recorded across all chunks (no silent omission)", p)
	}

	// 2+ personas fanned out: each rostered persona produced a pool source.
	for _, persona := range []string{"bruce", "kai"} {
		assert.FileExistsf(t, filepath.Join(dir, "sources", "pool", "raw", "agent", persona, "findings.txt"),
			"persona %s must produce a baseline source (multi-persona fan-out)", persona)
	}

	// Stage 2: RunReconcile — collapses the multi-chunk × multi-persona sources into a
	// single reconciled report (the two personas corroborate the same finding).
	require.Equal(t, 0, execCmd(t, "reconcile"), "reconcile over baseline output exits 0")
	findings, err := reconcile.ReadReconciledFindings(dir)
	require.NoError(t, err)
	require.NotEmpty(t, findings, "the baseline chain produces at least one reconciled finding")

	// Stage 3: debt add — file the FIRST reconciled baseline finding into a TD store,
	// proving debt add consumes baseline-sourced findings identically to diff-sourced.
	readme, items := emptyTDRepo(t)
	f := findings[0]
	code := execCmd(t, "debt", "add",
		"--readme", readme, "--items", items,
		"--severity", f.Severity, "--file", fmt.Sprintf("%s:%d", f.File, f.Line),
		"--problem", f.Problem, "--fix", f.Fix, "--category", f.Category,
		"--est", "15", "--date", "2026-07-27")
	require.Equal(t, 0, code, "debt add of a reconciled baseline finding exits 0")
	body, err := os.ReadFile(readme)
	require.NoError(t, err)
	assert.Contains(t, string(body), f.File, "the baseline finding is filed into the debt store")

	// Stage 4: report — md and json both render the baseline review at exit 0, and the
	// reconciled finding's location surfaces in each rendered body (not just an exit code),
	// proving report reads the baseline-derived findings through the same path as diff mode.
	mdCode, mdOut := execCmdCapture(t, "report", "--format", "md")
	require.Equal(t, 0, mdCode, "report md over the baseline chain exits 0")
	assert.Contains(t, mdOut, f.File, "the md report renders the baseline finding's location")
	jsonCode, jsonOut := execCmdCapture(t, "report", "--format", "json")
	require.Equal(t, 0, jsonCode, "report json over the baseline chain exits 0")
	assert.Contains(t, jsonOut, f.File, "the json report renders the baseline finding's location")
}

// AC 05-02 Scenario 1: `--dir <subtree> --fresh` forces a full re-scan of the subtree
// (identical bypass to `--all --fresh`, scoped) — every in-scope file is a candidate
// even when its recorded hash matches.
func TestReviewDirFresh_ForcesSubtreeRescan(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--dir", "internal")) // records internal/c.go
	// No change; without --fresh a second --dir run would be zero-candidate. --fresh forces it.
	require.Equal(t, 0, execCmd(t, "review", "--dir", "internal", "--fresh"))
	assert.Contains(t, baselineFilesPayload(t), "package c",
		"--dir --fresh must re-review the subtree despite a matching hash")
}

// AC 05-02 Scenario 2 / Edge Case 2: `--dir --fresh` only refreshes the subtree's
// index entries; entries for files outside the subtree keep their prior run id.
func TestReviewDirFresh_LeavesOutOfScopeEntriesUntouched(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--all")) // records a.txt/b.go/internal/c.go under run R1
	_, aRun1, ok := payload.Load(payload.FileHashIndexPath("."), nil).Get("a.txt")
	require.True(t, ok)

	require.Equal(t, 0, execCmd(t, "review", "--dir", "internal", "--fresh"))
	after := payload.Load(payload.FileHashIndexPath("."), nil)
	_, aRun2, okA := after.Get("a.txt")
	require.True(t, okA, "out-of-scope a.txt entry must survive")
	assert.Equal(t, aRun1, aRun2, "--dir --fresh must NOT re-stamp out-of-scope a.txt's run id")
}

// AC 05-02 Scenario 3 (regression guard): `--dir` WITHOUT `--fresh` retains Story 4's
// default skip — only the changed in-scope file is reviewed. Two in-scope files keep
// the candidate set non-empty so the run does not hit the all-skipped no-content case.
func TestReviewDir_WithoutFreshSkipsUnchanged(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	// Add a second in-scope file so a one-file change still leaves a candidate.
	require.NoError(t, os.WriteFile(filepath.Join("internal", "d.go"), []byte("package d\n"), 0o644))
	gitRun(t, "add", "-A")
	gitRun(t, "commit", "-qm", "add internal/d.go")
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--dir", "internal")) // records c.go + d.go
	require.NoError(t, os.WriteFile(filepath.Join("internal", "c.go"), []byte("package c // edited\n"), 0o644))
	require.Equal(t, 0, execCmd(t, "review", "--dir", "internal")) // only c.go changed
	body := baselineFilesPayload(t)
	assert.Contains(t, body, "package c // edited", "changed in-scope file re-reviewed")
	assert.NotContains(t, body, "package d", "unchanged in-scope file skipped (default incremental behavior)")
}

// 4.11.A HIGH #2 regression: a scoped `--dir` run must NOT self-trim (wipe) index
// entries for out-of-scope files recorded by a prior `--all` run.
func TestReviewDir_PreservesOutOfScopeIndexEntries(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--all")) // records a.txt, b.go, internal/c.go
	idx := payload.Load(payload.FileHashIndexPath("."), nil)
	for _, p := range []string{"a.txt", "b.go", "internal/c.go"} {
		_, _, ok := idx.Get(p)
		require.Truef(t, ok, "--all must record %s", p)
	}

	// Change internal/c.go so the scoped run has a candidate, then scope to internal/.
	require.NoError(t, os.WriteFile(filepath.Join("internal", "c.go"), []byte("package c // edited\n"), 0o644))
	require.Equal(t, 0, execCmd(t, "review", "--dir", "internal"))

	after := payload.Load(payload.FileHashIndexPath("."), nil)
	// Out-of-scope entries survive the scoped run.
	_, _, okA := after.Get("a.txt")
	_, _, okB := after.Get("b.go")
	assert.True(t, okA, "--dir must not prune out-of-scope a.txt from the index")
	assert.True(t, okB, "--dir must not prune out-of-scope b.go from the index")
	// The in-scope file was re-recorded.
	_, _, okC := after.Get("internal/c.go")
	assert.True(t, okC, "--dir must record its in-scope reviewed file")
}

// 4.23 gate CRITICAL regression: a file that survives the GLOBAL byte budget but is
// shed from every agent's per-model window (Epic 19.10 F2) must NOT be recorded as
// reviewed — else the next run would skip a file no agent ever saw. The test model
// (m-bruce, unknown → 32768-token window → ~71680-byte effective budget) sheds an
// ~80 KB file per-agent while the 512 KiB global budget keeps it.
// Sprint 35.0 Phase 5 (task 5.3, 5.2.A HIGH): under the baseline (persona × chunk)
// fan-out, a file larger than a single per-agent budget is NOT shed — it becomes its
// OWN oversized chunk (PartitionByBudget never drops), so it IS reviewed and therefore
// MUST be recorded in the incremental index. This supersedes the Phase 4 bulk-path
// behavior (a per-agent-shed file was correctly not recorded) because Phase 5 no longer
// sheds per agent; recording the full reviewed set is what makes the Story 4/5 skip work
// on multi-chunk repos.
func TestReviewAll_OversizedFileReviewedInOwnChunkIsRecorded(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	// A file larger than the per-agent effective budget (~71680 bytes) but well under
	// the 512 KiB global budget — so it lands in its own oversized baseline chunk.
	big := make([]byte, 80_000)
	for i := range big {
		big[i] = 'x'
	}
	require.NoError(t, os.WriteFile("big.txt", big, 0o644))
	gitRun(t, "add", "-A")
	gitRun(t, "commit", "-qm", "add big.txt")
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--all"))
	idx := payload.Load(payload.FileHashIndexPath("."), nil)
	// Both the small file and the oversized (own-chunk, reviewed) file are recorded.
	_, _, okA := idx.Get("a.txt")
	assert.True(t, okA, "a small reviewed file is recorded")
	_, _, okBig := idx.Get("big.txt")
	assert.True(t, okBig, "an oversized file reviewed in its own baseline chunk must be recorded, so the incremental skip covers it next run")
}

// Sprint 35.0 Phase 5 (task 5.3, 5.2.A HIGH regression): a baseline scan that fans
// out across MULTIPLE chunks records EVERY reviewed file in the incremental index —
// not just one chunk's worth. Prior to the 5.2.A fix the write-back bounded the
// recorded set to a single per-agent budget (drop-to-fit), so on a multi-chunk repo
// most reviewed files were left unrecorded and re-reviewed every run, defeating the
// Story 4/5 incremental skip.
func TestReviewAll_MultiChunkRecordsAllReviewedFiles(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	// Three files each larger than the per-agent budget (~71680 bytes) but whose total
	// (~240 KB) is under the 512 KiB global budget: each becomes its own baseline chunk,
	// so the scan fans out across 3+ chunks and every file is reviewed.
	big := make([]byte, 80_000)
	for i := range big {
		big[i] = 'x'
	}
	for _, name := range []string{"big1.txt", "big2.txt", "big3.txt"} {
		require.NoError(t, os.WriteFile(name, big, 0o644))
	}
	gitRun(t, "add", "-A")
	gitRun(t, "commit", "-qm", "add big files")
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--all"))
	idx := payload.Load(payload.FileHashIndexPath("."), nil)
	for _, name := range []string{"a.txt", "big1.txt", "big2.txt", "big3.txt"} {
		_, _, ok := idx.Get(name)
		assert.True(t, ok, "multi-chunk baseline must record every reviewed file; %s missing", name)
	}
}

// AC 04-04 Scenario 2: `--fresh` on a diff-range review is a no-op for the hash
// index — no baseline index is read or written, and the run is unaffected.
func TestReviewFresh_WithoutBaselineWritesNoIndex(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	// A second commit so --base HEAD^ resolves a real range.
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	require.NoError(t, os.WriteFile("a.txt", []byte("one\ntwo\n"), 0o644))
	run("commit", "-aqm", "edit a.txt")
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^", "--head", "HEAD", "--fresh"))
	assert.NoFileExists(t, payload.FileHashIndexPath("."), "a diff-range --fresh review must not touch the baseline hash index")
}

// AC 05-03 Edge Case 1: `--force` combined with `--all` is NEVER read as a --fresh
// alias — it does not bypass the hash-skip. A changed file is reviewed; unchanged
// files stay skipped despite --force.
func TestReviewForce_DoesNotBypassHashSkip(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--all")) // records all
	require.NoError(t, os.WriteFile("a.txt", []byte("one CHANGED\n"), 0o644))

	// --force (no --fresh): overwrite semantics only, never a skip bypass.
	require.Equal(t, 0, execCmd(t, "review", "--all", "--force"))
	body := baselineFilesPayload(t)
	assert.Contains(t, body, "one CHANGED", "the changed file is reviewed")
	assert.NotContains(t, body, "package b", "--force must NOT bypass the skip for unchanged b.go")
	assert.NotContains(t, body, "package c", "--force must NOT bypass the skip for unchanged internal/c.go")
}

// AC 05-03 Scenario 1: `--all --fresh --force` combines both flags' orthogonal
// effects (fresh bypasses the skip; force is a no-op on a derived id) and exits 0.
func TestReviewAllFreshForce_CombineFreely(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--all")) // populate the index
	require.Equal(t, 0, execCmd(t, "review", "--all", "--fresh", "--force"))
	body := baselineFilesPayload(t)
	assert.Contains(t, body, "package b", "--fresh forces a full re-scan even alongside --force")
	assert.Contains(t, body, "package c")
}

// AC 05-03 Edge Case 2 / Error Scenario 2: `--resume --force` still raises the
// pre-existing mutual-exclusion usage error (exit 2), unchanged by this story.
func TestReviewResumeForce_MutualExclusionUnchanged(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "review", "--resume", "latest", "--force")
	assert.Equal(t, 2, code)
	assert.Contains(t, out, "--resume and --force are mutually exclusive")
}

// AC 05-03 Scenario 2 / Edge Case 4: `--fresh --force` on a diff-range review is a
// no-op for the hash index (no baseline index read or written) and exits 0.
func TestReviewFreshForce_DiffModeNoOp(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	require.NoError(t, os.WriteFile("a.txt", []byte("one\ntwo\n"), 0o644))
	gitRun(t, "commit", "-aqm", "edit a.txt")
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^", "--head", "HEAD", "--fresh", "--force"))
	assert.NoFileExists(t, payload.FileHashIndexPath("."), "a diff-range --fresh --force review must not touch the baseline index")
}

// failOnMarkerProvider returns 500 for any request whose body contains marker and a
// finding otherwise — so exactly one baseline chunk (the one carrying marker) fails
// while the rest succeed.
func failOnMarkerProvider(t *testing.T, marker string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), marker) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		content := "CRITICAL|a.txt:1|Unchecked call|Guard it|security|15|evidence"
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Sprint 35.0 Phase 5 (task 5.6, 5.5.A MEDIUM): when a baseline scan fans out into
// multiple chunks and one chunk FAILS (the persona still reports StatusOK via
// any-chunk-succeeded), the incremental hash-index write-back is SKIPPED entirely —
// recording the reviewed set would stamp the unreviewed (failed-chunk) files too, so
// they'd be silently skipped next run. Fail-open: no index write → next run re-scans.
func TestReviewAll_PartialChunkFailureSkipsIndexWrite(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	// An oversized file (own chunk) carrying a marker the provider fails on; the small
	// files (a.txt/b.go/internal/c.go) share a separate chunk that succeeds. One persona
	// → one failed chunk under an OK persona → Summary.UnreviewedChunks > 0.
	big := []byte("FAILME\n")
	big = append(big, make([]byte, 80_000)...)
	require.NoError(t, os.WriteFile("fail.txt", big, 0o644))
	gitRun(t, "add", "-A")
	gitRun(t, "commit", "-qm", "add fail.txt")
	srv := failOnMarkerProvider(t, "FAILME")
	liveReviewConfig(t, srv.URL, "bruce")

	// The run still exits 0 (the persona succeeded on at least one chunk).
	require.Equal(t, 0, execCmd(t, "review", "--all"))
	// But because a chunk went unreviewed, the write-back was skipped: no index on disk.
	assert.NoFileExists(t, payload.FileHashIndexPath("."),
		"a baseline run with an unreviewed chunk must NOT persist the hash index, or the unreviewed files would be skipped-though-unreviewed next run")
}
