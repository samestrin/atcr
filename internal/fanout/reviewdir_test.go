package fanout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScaffoldOutputDir_CreatesTreeWhenAbsent(t *testing.T) {
	// A non-existent --output-dir path is created (including parents) with the
	// standard review subdir trio.
	dir := filepath.Join(t.TempDir(), "nested", "ext-review")
	got, err := ScaffoldOutputDir(dir)
	require.NoError(t, err)
	require.Equal(t, dir, got)
	for _, sub := range reviewSubdirs {
		assert.DirExists(t, filepath.Join(dir, sub))
	}
}

func TestScaffoldOutputDir_AllowsEmptyExisting(t *testing.T) {
	// An existing but empty directory is a valid --output-dir target.
	dir := t.TempDir()
	got, err := ScaffoldOutputDir(dir)
	require.NoError(t, err)
	require.Equal(t, dir, got)
	assert.DirExists(t, filepath.Join(dir, "sources"))
}

func TestScaffoldOutputDir_RejectsNonEmpty(t *testing.T) {
	// A directory that already contains files is rejected (exit 2 at the CLI) so
	// --output-dir can never clobber existing content. Any entry counts,
	// including hidden files.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644))
	_, err := ScaffoldOutputDir(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not empty")
}

func TestScaffoldOutputDir_RejectsSymlinkTarget(t *testing.T) {
	// A symlink pointing at an empty directory must be rejected via os.Lstat.
	// Without the Lstat guard, os.ReadDir follows the link (finds it empty),
	// then MkdirAll writes through the symlink into the unintended target.
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	require.NoError(t, os.Mkdir(realDir, 0o755))
	symlinkPath := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(realDir, symlinkPath))

	_, err := ScaffoldOutputDir(symlinkPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink")
}

func TestScaffoldOutputDir_AllowsOutsideRepoPath(t *testing.T) {
	// --output-dir is designed for external orchestrators that own their output
	// location; arbitrary absolute paths (including locations outside the repo
	// root) are accepted by design. This test encodes the intentional trust
	// boundary: outside-repo writes are permitted, not a security gap.
	dir := filepath.Join(t.TempDir(), "atcr-outside-repo")
	got, err := ScaffoldOutputDir(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, got)
	for _, sub := range reviewSubdirs {
		assert.DirExists(t, filepath.Join(dir, sub))
	}
}

func TestScaffoldOutputDir_RejectsFileAtPath(t *testing.T) {
	// A regular file at the target path is not a usable review dir: surface a
	// clear error rather than letting MkdirAll fail opaquely.
	file := filepath.Join(t.TempDir(), "afile")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	_, err := ScaffoldOutputDir(file)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create review directory")
}

// TestScaffoldOutputDir_RejectsDanglingSymlink verifies that a dangling symlink
// at the target path is rejected with a clear error referencing "symlink" rather
// than an opaque mkdir failure.
func TestScaffoldOutputDir_RejectsDanglingSymlink(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "nonexistent")
	link := filepath.Join(tmp, "dangling-link")
	require.NoError(t, os.Symlink(target, link))
	_, err := ScaffoldOutputDir(link)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink")
}

func TestValidateOutputDirRoot_RejectsInsideReviewsRoot(t *testing.T) {
	// --output-dir inside .atcr/reviews/ writes a review tree that is invisible
	// to atcr status (WriteLatest is skipped for --output-dir), creating a
	// confusing half-state. validateOutputDirRoot must reject such paths.
	root := t.TempDir()
	insidePath := filepath.Join(ReviewsRoot(root), "my-output")
	err := validateOutputDirRoot(insidePath, root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".atcr/reviews")
}

func TestValidateOutputDirRoot_AllowsOutsidePath(t *testing.T) {
	root := t.TempDir()
	outsidePath := filepath.Join(t.TempDir(), "my-output")
	err := validateOutputDirRoot(outsidePath, root)
	require.NoError(t, err)
}

func TestScaffoldOutputDir_TwoCallsSamePathSecondFails(t *testing.T) {
	// After the first ScaffoldOutputDir call populates a dir with review subdirs,
	// a second call on the same path sees a non-empty directory and is rejected.
	// This encodes the concurrency contract: callers must use unique paths; two
	// calls on the same pre-existing empty path are not protected against each other.
	dir := t.TempDir()
	_, err := ScaffoldOutputDir(dir)
	require.NoError(t, err)
	_, err = ScaffoldOutputDir(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not empty")
}

func TestSlugifyBranch(t *testing.T) {
	cases := map[string]string{
		"feature/JIRA-123-add-auth": "JIRA-123-add-auth",
		"fix/bug":                   "bug",
		"feature/1.0_atcr_core":     "1.0_atcr_core",
		"feature/a//b  c":           "a-b-c",
		"main":                      "main",
		"!!!":                       "",
		"  feature/x  ":             "x",
	}
	for in, want := range cases {
		assert.Equal(t, want, slugifyBranch(in), "slug of %q", in)
	}
}

func TestReviewID_DefaultScheme(t *testing.T) {
	id, err := ReviewID("", "feature/add-auth", "2026-06-10", "143022", nil)
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_add-auth", id)
}

func TestReviewID_DetachedAndEmptySlugFallbacks(t *testing.T) {
	id, err := ReviewID("", "", "2026-06-10", "143022", nil)
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_detached", id, "empty branch (detached HEAD) → detached")

	id, err = ReviewID("", "feature/!!!", "2026-06-10", "143022", nil)
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_review", id, "branch that sanitizes to nothing → review")
}

func TestReviewID_CollisionAppendsSuffix(t *testing.T) {
	exists := func(id string) bool { return id == "2026-06-10_add-auth" }
	id, err := ReviewID("", "feature/add-auth", "2026-06-10", "143022", exists)
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_add-auth-143022", id)
}

func TestReviewID_OverrideWinsVerbatim(t *testing.T) {
	id, err := ReviewID("custom-review-id", "feature/ignored", "2026-06-10", "143022", nil)
	require.NoError(t, err)
	assert.Equal(t, "custom-review-id", id)
}

func TestReviewID_RejectsTraversalOverride(t *testing.T) {
	// Includes ".", leading-dash (flag injection), and Windows separators.
	for _, bad := range []string{"../escape", "a/b", "..", ".", "/abs", `a\b`, "-rf", "--id"} {
		_, err := ReviewID(bad, "feature/x", "2026-06-10", "143022", nil)
		require.Error(t, err, "override %q must be rejected", bad)
		assert.Contains(t, err.Error(), "invalid review id")
	}
}

func TestReviewID_DotSlugBranchFallsBackToReview(t *testing.T) {
	// A branch that slugifies to dots must not produce an unsafe component.
	id, err := ReviewID("", "feature/..", "2026-06-10", "143022", nil)
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_review", id)
}

func TestReviewID_CollisionReprobeAvoidsClobber(t *testing.T) {
	// Both the base id AND the first suffixed id already exist → counter appended.
	taken := map[string]bool{
		"2026-06-10_x":        true,
		"2026-06-10_x-143022": true,
	}
	id, err := ReviewID("", "feature/x", "2026-06-10", "143022", func(s string) bool { return taken[s] })
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_x-143022-2", id)
}

func TestScaffoldReviewDir_CreatesLayout(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_feature")
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(root, ".atcr", "reviews", "2026-06-10_feature"), dir)
	for _, sub := range []string{"payload", "sources", "reconciled"} {
		assert.DirExists(t, filepath.Join(dir, sub))
	}
	// Per-agent dir is NOT scaffolded here (fan-out creates it later).
	assert.NoDirExists(t, filepath.Join(dir, "sources", "pool"))
}

// TestScaffoldReviewDir_CollisionMessageNamesResumeAndForce locks AC1: an
// explicit --id whose directory already exists is rejected with a message that
// names BOTH the non-destructive resume path and the destructive --force path,
// so the user is told every way forward.
func TestScaffoldReviewDir_CollisionMessageNamesResumeAndForce(t *testing.T) {
	root := t.TempDir()
	_, err := ScaffoldReviewDir(root, "2026-06-10_dup")
	require.NoError(t, err)

	_, err = ScaffoldReviewDir(root, "2026-06-10_dup")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assert.Contains(t, err.Error(), "--resume", "AC1: must name the non-destructive resume path")
	assert.Contains(t, err.Error(), "--force", "AC1: must name the destructive overwrite path")
}

// TestScaffoldReviewDir_CollisionErrorDoesNotLeakPath verifies the collision
// error names only the review id, not the resolved filesystem path, so an MCP
// client (which never sees the server's .atcr/reviews/ layout) does not learn
// the absolute path of the reviews directory.
func TestScaffoldReviewDir_CollisionErrorDoesNotLeakPath(t *testing.T) {
	root := t.TempDir()
	_, err := ScaffoldReviewDir(root, "2026-06-10_leak")
	require.NoError(t, err)

	_, err = ScaffoldReviewDir(root, "2026-06-10_leak")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2026-06-10_leak", "must still name the review id")
	assert.NotContains(t, err.Error(), root, "must not leak the resolved filesystem path")
}

// TestScaffoldOutputDir_CollisionMessageNamesForce locks the AC1 parity for the
// --output-dir path: a non-empty target is rejected with a message that names
// --force as the overwrite opt-in.
func TestScaffoldOutputDir_CollisionMessageNamesForce(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "prior.txt"), []byte("x"), 0o644))
	_, err := ScaffoldOutputDir(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force", "must name --force as the overwrite opt-in")
}

// TestScaffoldOutputDir_CollisionErrorSanitizesPath verifies the non-empty
// collision error names only the sanitized leaf, not the full resolved path, so
// the error does not leak the server's filesystem layout to an MCP client.
func TestScaffoldOutputDir_CollisionErrorSanitizesPath(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "myoutput")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "prior.txt"), []byte("x"), 0o644))

	_, err := ScaffoldOutputDir(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "myoutput", "must still name the sanitized output dir")
	assert.NotContains(t, err.Error(), parent, "must not leak the full resolved parent path")
}

// TestBackupExisting_MovesAsideReplacingStaleBak verifies backupExisting renames
// a directory to <dir>.bak and replaces any pre-existing backup, so --force keeps
// exactly one prior generation and the source path is left vacant for a fresh
// scaffold.
func TestBackupExisting_MovesAsideReplacingStaleBak(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "review")
	require.NoError(t, os.MkdirAll(src, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "marker.txt"), []byte("current"), 0o644))

	// A stale backup from a previous --force must be replaced, not error out.
	stale := src + ".bak"
	require.NoError(t, os.MkdirAll(stale, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stale, "old.txt"), []byte("old"), 0o644))

	bak, err := backupExisting(src)
	require.NoError(t, err)
	assert.Equal(t, stale, bak)
	// Source is now vacant.
	_, statErr := os.Stat(src)
	assert.True(t, os.IsNotExist(statErr), "source must be moved aside, leaving the path vacant")
	// Backup holds the current generation, not the stale one.
	data, err := os.ReadFile(filepath.Join(bak, "marker.txt"))
	require.NoError(t, err)
	assert.Equal(t, "current", string(data))
	assert.NoFileExists(t, filepath.Join(bak, "old.txt"), "stale backup must be replaced, not merged")
}

// TestForceBackupOutputDir_RefusesForeignBak verifies that --force on an
// arbitrary --output-dir does NOT silently destroy a pre-existing sibling
// <dir>.bak that atcr did not create. backupExisting removes the prior .bak
// generation unconditionally; for an unmanaged output path that sibling may be
// unrelated user data, so forceBackupOutputDir must refuse instead.
func TestForceBackupOutputDir_RefusesForeignBak(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "myreview")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "report.json"), []byte("current"), 0o644))

	// A foreign sibling backup the user owns — no atcr review-tree markers.
	foreign := dir + ".bak"
	require.NoError(t, os.MkdirAll(foreign, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(foreign, "important.txt"), []byte("do not delete"), 0o644))

	_, err := forceBackupOutputDir(dir)
	require.Error(t, err, "must refuse to clobber a foreign <dir>.bak")
	assert.Contains(t, err.Error(), "atcr")
	// The foreign backup and its contents survive untouched.
	data, readErr := os.ReadFile(filepath.Join(foreign, "important.txt"))
	require.NoError(t, readErr)
	assert.Equal(t, "do not delete", string(data), "foreign backup must not be destroyed")
}

// TestForceBackupOutputDir_RefusesFileBak verifies that a regular file at the
// backup path is rejected with a specific, actionable error rather than the
// generic "not created by atcr" message.
func TestForceBackupOutputDir_RefusesFileBak(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "myreview")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "report.json"), []byte("current"), 0o644))

	// A regular file where the backup directory should go.
	require.NoError(t, os.WriteFile(dir+".bak", []byte("not a dir"), 0o644))

	_, err := forceBackupOutputDir(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "regular file")
	assert.Contains(t, err.Error(), "not a directory")
}

// TestForceBackupOutputDir_ReplacesPriorAtcrBak verifies that a genuine prior
// atcr backup (one carrying the scaffolded review-tree markers) is still
// replaced, preserving the one-generation --force contract for managed trees.
func TestForceBackupOutputDir_ReplacesPriorAtcrBak(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "myreview")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "report.json"), []byte("current"), 0o644))

	// A prior atcr backup carries the review subdirs AND a manifest.json
	// provenance marker — safe to replace.
	prior := dir + ".bak"
	for _, sub := range reviewSubdirs {
		require.NoError(t, os.MkdirAll(filepath.Join(prior, sub), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(prior, "manifest.json"), []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(prior, "old.txt"), []byte("old"), 0o644))

	_, err := forceBackupOutputDir(dir)
	require.NoError(t, err)
	// dir was moved aside to .bak; the prior generation is gone.
	data, err := os.ReadFile(filepath.Join(prior, "report.json"))
	require.NoError(t, err)
	assert.Equal(t, "current", string(data))
	assert.NoFileExists(t, filepath.Join(prior, "old.txt"), "prior atcr backup must be replaced")
}

// TestForceBackupOutputDir_RefusesStructuralLookalike verifies that a sibling
// <dir>.bak which merely mirrors the review subdir layout (payload/, sources/,
// reconciled/) but carries no atcr provenance (no manifest.json) is NOT
// classified as an atcr backup and is therefore refused, not silently
// destroyed. Structural directory names alone are too weak a signal — the
// never-destroy-user-data guard must require a manifest.json provenance marker.
func TestForceBackupOutputDir_RefusesStructuralLookalike(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "myreview")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "report.json"), []byte("current"), 0o644))

	// A foreign sibling that happens to contain the review subdir names but is
	// user data, not an atcr backup (no manifest.json at its root).
	lookalike := dir + ".bak"
	for _, sub := range reviewSubdirs {
		require.NoError(t, os.MkdirAll(filepath.Join(lookalike, sub), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(lookalike, "important.txt"), []byte("do not delete"), 0o644))

	_, err := forceBackupOutputDir(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not look like an atcr backup")
	// The foreign data must survive — the guard refused rather than destroying it.
	data, readErr := os.ReadFile(filepath.Join(lookalike, "important.txt"))
	require.NoError(t, readErr)
	assert.Equal(t, "do not delete", string(data))
}

func TestReviewExists_AndCollisionProbe(t *testing.T) {
	root := t.TempDir()
	assert.False(t, ReviewExists(root, "2026-06-10_x"))
	_, err := ScaffoldReviewDir(root, "2026-06-10_x")
	require.NoError(t, err)
	assert.True(t, ReviewExists(root, "2026-06-10_x"))
}

// Two concurrent claims of the same derived id must land in distinct
// directories: directory creation itself is the atomic claim, so the
// Stat-probe TOCTOU window (both probes pass, both scaffold one dir) is gone.
func TestClaimReviewDir_ConcurrentSameIDGetDistinctDirs(t *testing.T) {
	root := t.TempDir()
	const id = "2026-06-10_race"

	type claim struct {
		id  string
		dir string
		err error
	}
	results := make(chan claim, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			cid, dir, err := claimReviewDir(root, id, "143022")
			results <- claim{cid, dir, err}
		}()
	}
	close(start)
	a, b := <-results, <-results
	require.NoError(t, a.err)
	require.NoError(t, b.err)
	assert.NotEqual(t, a.dir, b.dir, "concurrent claims of one id must yield distinct review dirs")
	for _, c := range []claim{a, b} {
		for _, sub := range []string{"payload", "sources", "reconciled"} {
			assert.DirExists(t, filepath.Join(c.dir, sub))
		}
	}
}

// Sequential claims follow the same candidate sequence the probe-based
// resolver used: base id, then id-suffix, then id-suffix-2.
func TestClaimReviewDir_SecondClaimAppendsSuffix(t *testing.T) {
	root := t.TempDir()
	id1, _, err := claimReviewDir(root, "2026-06-10_x", "143022")
	require.NoError(t, err)
	id2, _, err := claimReviewDir(root, "2026-06-10_x", "143022")
	require.NoError(t, err)
	id3, _, err := claimReviewDir(root, "2026-06-10_x", "143022")
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_x", id1)
	assert.Equal(t, "2026-06-10_x-143022", id2)
	assert.Equal(t, "2026-06-10_x-143022-2", id3)
}

func TestWriteAndReadLatest(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, WriteLatest(root, "2026-06-10_feature"))

	data, err := os.ReadFile(filepath.Join(root, ".atcr", "latest"))
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_feature\n", string(data))

	got, err := ReadLatest(root)
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_feature", got)
}

// A WriteManifest failure after WritePool leaves manifest.json saying
// partial:false while summary.json (the completion signal) says true.
// ReadManifestPartial must agree with the summary — the same source of truth
// ReadReviewStatus uses — not the stale manifest.
func TestReadManifestPartial_SummaryIsSourceOfTruth(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_partial")
	require.NoError(t, err)

	// Stale manifest: finalization failed, Partial still false.
	require.NoError(t, WriteManifest(dir, &payload.Manifest{Partial: false}))

	// Completed summary: the run was partial.
	poolDir := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	sum, err := json.Marshal(PoolSummary{Total: 2, Succeeded: 1, Failed: 1, Partial: true})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "summary.json"), sum, 0o644))

	assert.True(t, ReadManifestPartial(dir), "summary.json says partial:true; stale manifest must not override it")
}

// A write-phase failure (WritePool aborted mid-flush after agents ran) leaves a
// failure-marker summary that can say Partial=false (no agent FAILED) even
// though only a subset of per-agent artifacts reached disk. ReadManifestPartial
// must force partial so a reconcile over the surviving artifacts never emits a
// non-partial verdict from an incomplete roster.
func TestReadManifestPartial_FailureMarkerForcesPartial(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_marker")
	require.NoError(t, err)

	poolDir := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	// Every agent ran (Succeeded=2, Failed=0 -> Partial=false), but the write
	// aborted mid-flush, so the marker is set and the on-disk set may be a subset.
	sum, err := json.Marshal(PoolSummary{Total: 2, Succeeded: 2, Failed: 0, Partial: false, FailureMarker: true})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "summary.json"), sum, 0o644))

	assert.True(t, ReadManifestPartial(dir), "a failure-marker summary with surviving successes must read as partial")
}

// A failure-marker summary where NO agent succeeded is a genuine total failure;
// the marker alone must not fabricate partial=true (there is no surviving
// subset to protect).
// A failure-marker summary where every agent timed out (Succeeded=0, Failed=2)
// must still be treated as partial. summarize() buckets StatusTimeout as
// Failed (outcome.go:55), so a roster of timed-out agents that produced content
// before a WritePool fault appears as all-Failed in the summary — but the
// per-agent artifacts may still be on disk. Gating on Succeeded>0 misses this
// class of write-aborted runs; gating on Total>0 covers all failure-path
// writes conservatively.
func TestReadManifestPartial_FailureMarkerAllAgents_IsPartial(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_marker_allfailed")
	require.NoError(t, err)

	poolDir := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	sum, err := json.Marshal(PoolSummary{Total: 2, Succeeded: 0, Failed: 2, Partial: false, FailureMarker: true})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "summary.json"), sum, 0o644))

	assert.True(t, ReadManifestPartial(dir), "a failure-marker run with agents is always partial regardless of success count")
}

// End-to-end AC: ExecuteReview's WritePool-failure branch calls
// writeFailureSummary with the real results, then a later `atcr reconcile`
// reads the partial flag via ReadManifestPartial. When every agent ran
// (Succeeded=2, Failed=0 -> Partial=false) but the write aborted mid-flush, the
// failure marker must carry through so the reconcile caller receives partial.
func TestReadManifestPartial_WriteFailureAfterSuccessReadsPartial(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_writefail")
	require.NoError(t, err)
	poolDir := filepath.Join(dir, "sources", "pool")

	// Exactly what ExecuteReview does on a WritePool error: the agents ran, the
	// persistence faulted, so writeFailureSummary records the real (all-OK) tally.
	results := []Result{
		{Agent: "greta", Status: StatusOK, PayloadMode: "blocks"},
		{Agent: "kai", Status: StatusOK, PayloadMode: "blocks"},
	}
	writeFailureSummary(poolDir, results)

	// The on-disk summary is a best-effort failure marker, not a real run record.
	data, err := os.ReadFile(filepath.Join(poolDir, summaryFile))
	require.NoError(t, err)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(data, &ps))
	assert.True(t, ps.FailureMarker, "failure summary must be marked")
	assert.False(t, ps.Partial, "no agent FAILED, so the raw partial flag is false — the marker is what saves us")

	// The reconcile caller threads Partial: ReadManifestPartial(dir); it must be true.
	assert.True(t, ReadManifestPartial(dir),
		"reconcile over a write-aborted review must run partial, not drop the unflushed agent silently")
}

// Without a summary (fan-out still running, or a hand-assembled review) the
// manifest remains the only available signal and is still honored.
func TestReadManifestPartial_FallsBackToManifestWhenNoSummary(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_fallback")
	require.NoError(t, err)
	require.NoError(t, WriteManifest(dir, &payload.Manifest{Partial: true}))
	assert.True(t, ReadManifestPartial(dir))
}

func TestWriteManifest_AtReviewRootWithFields(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_feature")
	require.NoError(t, err)

	m := &payload.Manifest{
		Base:            "abc123",
		Head:            "def456",
		DetectionMode:   "explicit",
		CommitCount:     3,
		PayloadMode:     "blocks",
		PerAgentPayload: map[string]string{"greta": "blocks", "kai": "diff"},
		Roster:          []string{"greta", "kai"},
		StartedAt:       time.Unix(1000, 0).UTC(),
		Partial:         false,
	}
	require.NoError(t, WriteManifest(dir, m))

	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var got payload.Manifest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "abc123", got.Base)
	assert.Equal(t, "def456", got.Head)
	assert.Equal(t, "explicit", got.DetectionMode)
	assert.Equal(t, "blocks", got.PerAgentPayload["greta"])
	assert.Equal(t, []string{"greta", "kai"}, got.Roster)
	assert.False(t, got.Partial)
}

// --- Epic 1.9: FailureMarker contract across both summary readers ---

// ReadReviewStatus must apply the same FailureMarker-aware partial derivation
// as ReadManifestPartial. A failure-marker summary with Succeeded>0 and
// Partial=false must set ReviewStatus.Partial=true so `atcr status` and the
// atcr_status MCP handler agree with the reconcile path on what is partial.
func TestReadReviewStatus_FailureMarkerForcesPartial(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_status_marker")
	require.NoError(t, err)
	require.NoError(t, WriteManifest(dir, &payload.Manifest{
		Base: "a", Head: "b", Roster: []string{"greta", "kai"},
	}))
	poolDir := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	// Every agent ran (Succeeded=2) but WritePool aborted mid-flush.
	sum, err := json.Marshal(PoolSummary{Total: 2, Succeeded: 2, Failed: 0, Partial: false, FailureMarker: true})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, summaryFile), sum, 0o644))

	st, readErr := ReadReviewStatus(dir, "2026-06-10_status_marker")
	require.NoError(t, readErr)
	assert.True(t, st.Partial, "ReadReviewStatus must report partial:true for a failure-marker run with surviving successes")
	assert.Equal(t, RunCompleted, st.Status, "status must be RunCompleted when Succeeded>0")
}

// A zero-value PoolSummary produced by unmarshalling {} or null must not be
// trusted as a valid completion signal — ReadManifestPartial must fall through
// to the manifest fallback and not silently return partial:false.
func TestReadManifestPartial_EmptyJSONFallsBackToManifest(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_emptyjson")
	require.NoError(t, err)
	poolDir := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	// An empty JSON object unmarshals to a zero-value PoolSummary (Total=0,
	// Partial=false, FailureMarker=false). The sanity check (Total>0) must
	// reject this and fall through to the manifest.
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, summaryFile), []byte("{}"), 0o644))
	require.NoError(t, WriteManifest(dir, &payload.Manifest{Partial: true}))

	assert.True(t, ReadManifestPartial(dir), "empty-JSON summary must fall through to manifest")
}

// Integration: a WritePool I/O fault writes a failure-marker summary via
// writeFailureSummary; EnsureReviewComplete must pass (the review is terminal)
// and BOTH readers must agree the run is partial so a subsequent reconcile
// cannot emit a non-partial verdict from an incomplete agent set.
func TestIntegration_WritePoolFault_BothReadersAgreePartial(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-13_fault-integration")
	require.NoError(t, err)
	require.NoError(t, WriteManifest(dir, &payload.Manifest{
		Base: "a", Head: "b", Roster: []string{"greta", "kai"},
	}))

	// Simulate the WritePool-failure path: two agents ran OK, then persistence
	// faulted. writeFailureSummary records the real tallies with FailureMarker=true.
	results := []Result{
		{Agent: "greta", Status: StatusOK, PayloadMode: "blocks"},
		{Agent: "kai", Status: StatusOK, PayloadMode: "blocks"},
	}
	writeFailureSummary(filepath.Join(dir, "sources", "pool"), results)

	// EnsureReviewComplete must pass: the marker summary makes the review terminal.
	require.NoError(t, EnsureReviewComplete(dir, "2026-06-13_fault-integration"),
		"EnsureReviewComplete must not block a reconcile on a failure-marked review")

	// ReadReviewStatus must report partial:true (currently FAILS — bug in item 1).
	st, readErr := ReadReviewStatus(dir, "2026-06-13_fault-integration")
	require.NoError(t, readErr)
	assert.True(t, st.Partial,
		"ReadReviewStatus must report partial:true for a write-aborted run so atcr status and reconcile agree")

	// ReadManifestPartial must also report true (reconcile path).
	assert.True(t, ReadManifestPartial(dir),
		"ReadManifestPartial must report partial:true for the same failure-marker run")
}

// ReadManifestPartial must not read an unbounded summary.json. A file larger
// than the read cap must be skipped (falling back to manifest) rather than
// allocating an unbounded heap slice before json.Unmarshal.
func TestReadManifestPartial_OversizedSummaryFallsBack(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_oversize")
	require.NoError(t, err)
	poolDir := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))

	// Build valid JSON larger than the 1 MiB cap. The summary says partial:false;
	// the manifest says partial:true. Without the cap, the function trusts the
	// summary and returns false. With the cap, the oversized file is rejected and
	// the function falls back to the manifest, returning true.
	const cap = 1 << 20
	prefix := `{"total":2,"succeeded":2,"failed":0,"partial":false,"total_findings":0,"note":"`
	suffix := `"}`
	note := strings.Repeat("x", cap+10-len(prefix)-len(suffix))
	summaryContent := prefix + note + suffix
	require.Greater(t, len(summaryContent), cap, "test precondition: summary must exceed the cap")
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, summaryFile), []byte(summaryContent), 0o644))
	require.NoError(t, WriteManifest(dir, &payload.Manifest{Partial: true}))

	// Without the cap: reads valid JSON with partial=false -> returns false -> FAILS.
	// With the cap: oversized -> falls through -> manifest -> returns true -> PASSES.
	assert.True(t, ReadManifestPartial(dir), "oversized summary must fall back to manifest")
}
