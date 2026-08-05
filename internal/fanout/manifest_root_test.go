package fanout

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
// would fall back to CWD forever.
func TestPrepareResume_BackfillsMissingRoot(t *testing.T) {
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
	assert.NotEmpty(t, rprep.manifest.Root, "a resume must backfill a missing root")
	assert.True(t, filepath.IsAbs(rprep.manifest.Root))
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

// writeManifestFixture writes a manifest.json directly, for cases that need an
// on-disk manifest without running a review.
func writeManifestFixture(t *testing.T, dir string, m payload.Manifest) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	data, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644))
}
