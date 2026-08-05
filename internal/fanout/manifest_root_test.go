package fanout

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
)

// Sprint 35.13 T6 (AC7 a/b): the review path records the repo root in the manifest,
// every finalization write preserves it, and a resume backfills it when absent.
//
// The root is what lets a later `atcr reconcile` — CLI from another directory, or an
// MCP-driven one whose process CWD is unrelated to the reviewed repo — resolve the
// .atcr/debt store from the artifacts instead of guessing from its own CWD.

// TestPrepareReview_ManifestRecordsAbsoluteRoot pins the write half. The assertion
// is on ABSOLUTENESS specifically, not just non-emptiness: req.Root is "." on every
// CLI run, so recording it verbatim would produce a manifest whose root means
// something different to every reader — which is the entire failure mode the field
// exists to prevent.
func TestPrepareReview_ManifestRecordsAbsoluteRoot(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	cfg := twoAgentConfig("http://unused")

	prep, err := PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)

	m := readManifest(t, prep.Dir)
	require.NotEmpty(t, m.Root, "the review path must record the repo root")
	assert.True(t, filepath.IsAbs(m.Root), "the recorded root must be absolute, got %q", m.Root)
	// EvalSymlinks: macOS t.TempDir() hands back a /var path that is a symlink to
	// /private/var, so a raw string compare would fail on a correct value.
	wantResolved, err := filepath.EvalSymlinks(repo)
	require.NoError(t, err)
	gotResolved, err := filepath.EvalSymlinks(m.Root)
	require.NoError(t, err)
	assert.Equal(t, wantResolved, gotResolved, "the recorded root must be the review's own root")
}

// TestAbsRoot covers the helper's two branches directly, including the deliberate
// empty-on-error result: recording NO root degrades a later reconcile to its
// pre-field CWD behavior, whereas recording an unresolvable claim would send
// findings somewhere wrong on the strength of a re-validation that passed.
func TestAbsRoot(t *testing.T) {
	got := absRoot(".")
	require.True(t, filepath.IsAbs(got))
	assert.Equal(t, filepath.Clean(got), got, "the recorded root is cleaned")

	assert.Equal(t, filepath.Clean(mustAbs(t, "..")), absRoot(".."),
		"a relative parent resolves against the CWD, like every other review-time path")

	assert.Equal(t, "", absRoot(""),
		"an empty root is no claim — filepath.Abs(\"\") would fabricate a CWD claim the re-validation tier then trusts")
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	require.NoError(t, err)
	return abs
}

// TestClearInterrupted_PreservesRoot locks one of the three finalization writes.
// All three copy the loaded struct today, so preservation is structural — which is
// exactly why it needs a test: a future refactor that builds a fresh Manifest at any
// of these sites drops the root silently, and the only symptom is a reconcile that
// quietly starts writing to the CWD again.
func TestClearInterrupted_PreservesRoot(t *testing.T) {
	dir := t.TempDir()
	root := mustAbs(t, dir)
	writeManifestFixture(t, dir, payload.Manifest{
		Base: "a", Head: "b", Interrupted: true, Root: root,
	})

	require.NoError(t, ClearInterrupted(dir))

	m, err := ReadManifest(dir)
	require.NoError(t, err)
	assert.False(t, m.Interrupted)
	assert.Equal(t, root, m.Root, "clearing the interrupt marker must not drop the recorded root")
}

// TestPrepareResume_BackfillsMissingRoot covers AC7(b)'s backfill: a review created
// before the field existed would otherwise never acquire a root, and its reconcile
// would fall back to CWD forever. This pins the SHIPPED input shape (TD
// internal/fanout/manifest_root_test.go:69): req.Root is "." on the real resume path
// (cli/resume.go), resolved from a CWD that is NOT the review's repo — so the
// backfill must name the review's OWN repo, recovered from the managed artifact
// path (<root>/.atcr/reviews/<id>), never the resuming process's CWD
// (TD internal/fanout/resume.go:376).
func TestPrepareResume_BackfillsMissingRoot(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})
	// Default path: .atcr/reviews/<id>/ under the repo root (the managed shape).
	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, ""))
	require.NoError(t, err)

	// Simulate a pre-field manifest by clearing the root the review just wrote.
	m, err := ReadManifest(prep.Dir)
	require.NoError(t, err)
	m.Root = ""
	require.NoError(t, WriteManifest(prep.Dir, m))

	// Resume with the shipped input — Root: "." — from a CWD that is not the
	// review's repo: the candidate the old absRoot(req.Root) code would stamp.
	req := repoReq(repo, "")
	req.Root = "."
	t.Chdir(t.TempDir())

	rprep, _, err := PrepareResume(context.Background(), cfg, prep.Dir, req)
	require.NoError(t, err)
	require.NotNil(t, rprep.manifest)
	require.NotEmpty(t, rprep.manifest.Root, "a resume must backfill a missing root")
	assert.True(t, filepath.IsAbs(rprep.manifest.Root))

	want, err := filepath.EvalSymlinks(repo)
	require.NoError(t, err)
	got, err := filepath.EvalSymlinks(rprep.manifest.Root)
	require.NoError(t, err)
	assert.Equal(t, want, got,
		"the backfill must name the review's own repo (derived from the artifact path), not the resuming CWD")

	// ON DISK, not just in memory. `atcr resume` on an already-complete review
	// returns after ClearInterrupted without any finalization write, so a backfill
	// that only mutates the in-memory struct never survives — and that is the very
	// path a pre-field review takes. Asserting rprep.manifest alone passes against
	// that defect.
	onDisk, err := ReadManifest(prep.Dir)
	require.NoError(t, err)
	gotDisk, err := filepath.EvalSymlinks(onDisk.Root)
	require.NoError(t, err)
	assert.Equal(t, want, gotDisk,
		"the backfilled root must be persisted, not left to ride a write that may never happen")
}

// TestPrepareResume_DoesNotBackfillUnmanagedReviewDir pins the other half of the
// derivation rule: a review written to a custom --output-dir does NOT live at
// <root>/.atcr/reviews/<id>, so the artifact path cannot vouch for any root.
// Stamping absRoot(req.Root) there trusts a claim the artifacts cannot verify —
// on the shipped input (Root: ".") that is the resuming process's CWD, silently
// attaching another repo's findings to wherever the operator stood. The backfill
// is SKIPPED: Root stays empty, degrading to the pre-field CWD behavior the
// reconcile already had for such reviews (TD internal/fanout/resume.go:376).
func TestPrepareResume_DoesNotBackfillUnmanagedReviewDir(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})
	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "review")))
	require.NoError(t, err)

	// Simulate a pre-field manifest by clearing the root the review just wrote.
	m, err := ReadManifest(prep.Dir)
	require.NoError(t, err)
	m.Root = ""
	require.NoError(t, WriteManifest(prep.Dir, m))

	rprep, _, err := PrepareResume(context.Background(), cfg, prep.Dir, repoReq(repo, ""))
	require.NoError(t, err)
	require.NotNil(t, rprep.manifest)
	assert.Empty(t, rprep.manifest.Root,
		"an unmanaged review dir yields no artifact-backed root claim — the backfill must be skipped")

	onDisk, err := ReadManifest(prep.Dir)
	require.NoError(t, err)
	assert.Empty(t, onDisk.Root, "a skipped backfill writes nothing")
}

// TestPrepareResume_DoesNotOverwriteRecordedRoot is the guard on the backfill's
// empty-only condition. A resume can run on a different machine against copied
// artifacts; overwriting the recorded root with the resuming machine's path would
// manufacture a valid-looking claim that points at the wrong repo — turning a
// detectable stale root into an undetectable wrong write, which is precisely what
// the re-validation downstream can no longer catch.
func TestPrepareResume_DoesNotOverwriteRecordedRoot(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	repo := baselineRepo(t, map[string]string{"a.go": "package a\n"})
	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, filepath.Join(t.TempDir(), "review")))
	require.NoError(t, err)

	recorded := filepath.Join(string(filepath.Separator), "recorded", "elsewhere")
	m, err := ReadManifest(prep.Dir)
	require.NoError(t, err)
	m.Root = recorded
	require.NoError(t, WriteManifest(prep.Dir, m))

	rprep, _, err := PrepareResume(context.Background(), cfg, prep.Dir, repoReq(repo, ""))
	require.NoError(t, err)
	require.NotNil(t, rprep.manifest)
	assert.Equal(t, recorded, rprep.manifest.Root,
		"a recorded root is the review's claim; the resuming machine must not replace it")
}

// TestExecuteReview_FinalizationPreservesRoot locks the second of the three
// finalization writes the sprint design's risk list names (TD
// internal/fanout/manifest_root_test.go:69): today every site copies the loaded
// struct, so preservation is structural — exactly why it needs a test. A future
// refactor that builds a fresh payload.Manifest literal at the success-path
// stamp drops the root silently, and the only symptom is a reconcile that
// quietly starts writing to the CWD again.
func TestExecuteReview_FinalizationPreservesRoot(t *testing.T) {
	dir := t.TempDir()
	root := mustAbs(t, t.TempDir())
	names := []string{"greta", "kai"}
	m := &payload.Manifest{
		Base: "a", Head: "b", Roster: names, Root: root,
		StartedAt: time.Now().UTC(), TimeoutSecs: 600, PayloadMode: "blocks",
		PerAgentPayload: map[string]string{}, Stages: []string{"review"},
	}
	require.NoError(t, WriteManifest(dir, m))

	var slots []Slot
	for _, n := range names {
		slots = append(slots, Slot{Primary: Agent{
			Name: n, Invocation: llmclient.Invocation{Model: n}, PayloadMode: "blocks",
		}})
	}
	prep := &PreparedReview{ID: "2026-06-18_root", Dir: dir, Slots: slots, MaxParallel: 1, manifest: m}

	_, err := ExecuteReview(context.Background(), okCompleter{}, prep)
	require.NoError(t, err)

	got, err := ReadManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, root, got.Root, "ExecuteReview's finalization must preserve the recorded root")
}

// TestExecuteResume_FinalizationPreservesRoot locks the third finalization
// write — the resumed run's completion stamp — under the same structural-risk
// argument as the review path above.
func TestExecuteResume_FinalizationPreservesRoot(t *testing.T) {
	dir := t.TempDir()
	root := mustAbs(t, t.TempDir())
	names := []string{"greta", "kai", "mira", "otto"}
	m := &payload.Manifest{
		Base: "a", Head: "b", Roster: names, Root: root,
		StartedAt: time.Now().UTC(), TimeoutSecs: 600, PayloadMode: "blocks",
		PerAgentPayload: map[string]string{}, Stages: []string{"review"},
	}
	require.NoError(t, WriteManifest(dir, m))

	// Pre-populate greta + kai as already completed on disk; resume runs only
	// the pending slots (mira, otto) and then stamps finalization.
	poolDir := filepath.Join(dir, "sources", "pool")
	require.NoError(t, writeResumedAgents(poolDir, []Result{
		{Agent: "greta", Status: StatusOK, Content: "CRITICAL|auth.go:3|x|y|security|15|ev"},
		{Agent: "kai", Status: StatusOK, Content: ""},
	}, nil))

	var slots []Slot
	for _, n := range []string{"mira", "otto"} {
		slots = append(slots, Slot{Primary: Agent{
			Name: n, Invocation: llmclient.Invocation{Model: n}, PayloadMode: "blocks",
		}})
	}
	prep := &PreparedReview{ID: "2026-06-18_x", Dir: dir, Slots: slots, MaxParallel: 1, manifest: m}

	_, err := ExecuteResume(context.Background(), okCompleter{}, prep)
	require.NoError(t, err)

	got, err := ReadManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, root, got.Root, "ExecuteResume's finalization must preserve the recorded root")
}

// writeManifestFixture writes a manifest.json directly, for cases that need an
// on-disk manifest without running a review.
func writeManifestFixture(t *testing.T, dir string, m payload.Manifest) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	data, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644))
}

// TestBackfillReviewRoot_PreservesOnDiskFieldsAgainstStaleSnapshot pins the
// narrow-write shape (TD internal/fanout/resume.go:271): PrepareResume reads the
// manifest at the top of a long validation run and the backfill writes at the
// end, so a finalization landing in between (Partial, Interrupted, CompletedAt,
// Review) must survive. The backfill re-reads immediately before writing and
// sets ONLY Root — the read-modify-write shape ClearInterrupted already uses —
// rather than writing its stale snapshot back over the concurrent write.
func TestBackfillReviewRoot_PreservesOnDiskFieldsAgainstStaleSnapshot(t *testing.T) {
	repo := t.TempDir()
	reviewDir := filepath.Join(repo, ".atcr", "reviews", "r1")

	// On-disk state as a CONCURRENT finalization left it: Root still empty
	// (pre-field review), but the interrupt marker already cleared.
	writeManifestFixture(t, reviewDir, payload.Manifest{Base: "a", Head: "b", Interrupted: false})

	// The backfill's caller holds a STALE snapshot from before that finalization.
	stale := payload.Manifest{Base: "a", Head: "b", Interrupted: true}
	backfillReviewRoot(reviewDir, &stale)

	onDisk, err := ReadManifest(reviewDir)
	require.NoError(t, err)
	assert.False(t, onDisk.Interrupted,
		"the backfill must not revert a concurrent finalization's fields")
	assert.NotEmpty(t, onDisk.Root, "the backfill still stamps the derived root")
	assert.Equal(t, onDisk.Root, stale.Root, "the caller's in-memory manifest tracks the stamped root")
}
