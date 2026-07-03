package autofix

// RED tests for Story 1 (ApplyPatch) — AC 01-01, 01-02, 01-03, 01-04.
//
// These exercise the write-path over go-gitdiff + atomicfs: parse/apply per
// diff type, atomic writes, per-file backups, per-file error isolation, and the
// defense-in-depth path-traversal re-check. Fixtures for modify/create/delete/
// drift are shared with gitdiff_contract_test.go (same package).

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Additional fixtures (modify/create/delete/drift live in gitdiff_contract_test.go).
const (
	// fixtureModifyBar patches bar.txt: "x\ny\nz\n" -> "x\ny-mod\nz\n".
	fixtureModifyBar = `diff --git a/bar.txt b/bar.txt
index 1111111..2222222 100644
--- a/bar.txt
+++ b/bar.txt
@@ -1,3 +1,3 @@
 x
-y
+y-mod
 z
`
	// fixtureMultiHunk patches multi.txt across two hunks:
	// "a\nb\nc\nd\ne\nf\ng\n" -> "a\nB\nc\nd\ne\nF\ng\n".
	fixtureMultiHunk = `diff --git a/multi.txt b/multi.txt
index 1111111..2222222 100644
--- a/multi.txt
+++ b/multi.txt
@@ -1,3 +1,3 @@
 a
-b
+B
 c
@@ -5,3 +5,3 @@
 e
-f
+F
 g
`
)

func fe(path, body string) payload.FileEntry {
	return payload.FileEntry{Path: path, Size: int64(len(body)), Body: body}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

// --- AC 01-01 Happy paths -------------------------------------------------

func TestApplyPatch_ModifyExistingFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo.txt"), "line1\nline2\nline3\n")

	bm, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", fixtureModify)})
	require.NoError(t, err)

	assert.Equal(t, "line1\nline2-modified\nline3\n", readFile(t, filepath.Join(root, "foo.txt")))
	// BackupMap keys are absolute target paths; value is the .bak path.
	bak, ok := bm[filepath.Join(root, "foo.txt")]
	require.True(t, ok, "modified file must be recorded in BackupMap")
	assert.Equal(t, filepath.Join(root, "foo.txt.bak"), bak)
	// The backup must hold the PRE-patch content (backup happened before overwrite).
	assert.Equal(t, "line1\nline2\nline3\n", readFile(t, bak))
}

func TestApplyPatch_CreateNewFile(t *testing.T) {
	root := t.TempDir()

	bm, err := ApplyPatch(root, []payload.FileEntry{fe("new.txt", fixtureCreate)})
	require.NoError(t, err)

	assert.Equal(t, "hello\nworld\n", readFile(t, filepath.Join(root, "new.txt")))
	// New file: recorded with an empty backup path (nothing to back up).
	bak, ok := bm[filepath.Join(root, "new.txt")]
	require.True(t, ok)
	assert.Equal(t, "", bak, "a created file has no backup")
	assert.NoFileExists(t, filepath.Join(root, "new.txt.bak"))
}

// A create diff whose target already exists must be refused, mirroring git apply's
// behavior and preventing a silent overwrite of existing content.
func TestApplyPatch_CreateOverExistingFileRefused(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "new.txt"), "pre-existing\n")

	bm, err := ApplyPatch(root, []payload.FileEntry{fe("new.txt", fixtureCreate)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "new.txt")
	assert.Contains(t, strings.ToLower(err.Error()), "already exists")
	assert.Empty(t, bm, "refused create must not be recorded as a success")
	assert.Equal(t, "pre-existing\n", readFile(t, filepath.Join(root, "new.txt")), "existing file must be untouched")
}

func TestApplyPatch_DeleteExistingFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "del.txt"), "gone1\ngone2\n")

	bm, err := ApplyPatch(root, []payload.FileEntry{fe("del.txt", fixtureDelete)})
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(root, "del.txt"), "delete entry must remove the target")
	bak := filepath.Join(root, "del.txt.bak")
	assert.FileExists(t, bak, "delete must back up before removing so revert can restore")
	assert.Equal(t, "gone1\ngone2\n", readFile(t, bak))
	assert.Equal(t, bak, bm[filepath.Join(root, "del.txt")])
}

func TestApplyPatch_MultiEntryBatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo.txt"), "line1\nline2\nline3\n")
	writeFile(t, filepath.Join(root, "del.txt"), "gone1\ngone2\n")

	bm, err := ApplyPatch(root, []payload.FileEntry{
		fe("foo.txt", fixtureModify),
		fe("new.txt", fixtureCreate),
		fe("del.txt", fixtureDelete),
	})
	require.NoError(t, err)

	assert.Equal(t, "line1\nline2-modified\nline3\n", readFile(t, filepath.Join(root, "foo.txt")))
	assert.Equal(t, "hello\nworld\n", readFile(t, filepath.Join(root, "new.txt")))
	assert.NoFileExists(t, filepath.Join(root, "del.txt"))
	assert.Len(t, bm, 3)
}

func TestApplyPatch_MultiHunkSingleFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "multi.txt"), "a\nb\nc\nd\ne\nf\ng\n")

	_, err := ApplyPatch(root, []payload.FileEntry{fe("multi.txt", fixtureMultiHunk)})
	require.NoError(t, err)
	assert.Equal(t, "a\nB\nc\nd\ne\nF\ng\n", readFile(t, filepath.Join(root, "multi.txt")))
}

// --- AC 01-01 Edge Case 1: empty / nil entries ---------------------------

func TestApplyPatch_EmptyEntries(t *testing.T) {
	root := t.TempDir()

	for _, entries := range [][]payload.FileEntry{nil, {}} {
		bm, err := ApplyPatch(root, entries)
		require.NoError(t, err)
		assert.Empty(t, bm)
	}
	// No filesystem side effects.
	got, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, got, "empty batch must not touch the working tree")
}

// --- AC 01-02: atomic write, no temp residue, no implicit mkdir ----------

func TestApplyPatch_NoTempResidueAfterWrite(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo.txt"), "line1\nline2\nline3\n")

	_, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", fixtureModify)})
	require.NoError(t, err)

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp-", "atomic write must leave no temp sibling behind")
	}
}

func TestApplyPatch_ConcurrentReaderNeverSeesPartial(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "foo.txt")
	pre := "line1\nline2\nline3\n"
	post := "line1\nline2-modified\nline3\n"
	writeFile(t, target, pre)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if b, err := os.ReadFile(target); err == nil {
					s := string(b)
					if s != pre && s != post {
						t.Errorf("observed partial/torn content: %q", s)
						return
					}
				}
			}
		}
	}()

	_, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", fixtureModify)})
	require.NoError(t, err)
	close(stop)
	wg.Wait()
	assert.Equal(t, post, readFile(t, target))
}

func TestApplyPatch_NewFileIntoMissingDirFailsNoMkdir(t *testing.T) {
	root := t.TempDir()

	bm, err := ApplyPatch(root, []payload.FileEntry{fe("sub/new.txt", fixtureCreate)})
	require.Error(t, err, "WriteFileAtomic does not create parent dirs; this must fail, not silently mkdir")
	assert.Contains(t, err.Error(), "sub/new.txt")
	assert.NoDirExists(t, filepath.Join(root, "sub"), "no implicit mkdir")
	assert.Empty(t, bm)
}

// --- AC 01-03: backup semantics ------------------------------------------

func TestApplyPatch_PreExistingBakReplaced(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo.txt"), "line1\nline2\nline3\n")
	// A stale .bak from a prior run.
	writeFile(t, filepath.Join(root, "foo.txt.bak"), "STALE\n")

	_, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", fixtureModify)})
	require.NoError(t, err)
	assert.Equal(t, "line1\nline2\nline3\n", readFile(t, filepath.Join(root, "foo.txt.bak")),
		"stale .bak must be replaced with the pre-patch content")
}

// --- AC 01-01 Error Scenarios --------------------------------------------

func TestApplyPatch_MalformedDiffBody(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo.txt"), "unchanged\n")

	bm, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", "this is not a diff at all")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "foo.txt")
	assert.Equal(t, "unchanged\n", readFile(t, filepath.Join(root, "foo.txt")), "no disk touch on parse failure")
	assert.NoFileExists(t, filepath.Join(root, "foo.txt.bak"))
	assert.Empty(t, bm)
}

func TestApplyPatch_DriftIsHardFailure(t *testing.T) {
	root := t.TempDir()
	// foo.txt content does not match fixtureModify's expected old-side context.
	writeFile(t, filepath.Join(root, "foo.txt"), "totally\ndifferent\ncontent\n")

	bm, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", fixtureModify)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "foo.txt")
	assert.Equal(t, "totally\ndifferent\ncontent\n", readFile(t, filepath.Join(root, "foo.txt")))
	assert.NoFileExists(t, filepath.Join(root, "foo.txt.bak"), "failed apply must not leave a backup")
	assert.Empty(t, bm)
}

func TestApplyPatch_ModifyMissingTarget(t *testing.T) {
	root := t.TempDir()
	// fixtureModify's old side is not /dev/null, but no file exists.
	_, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", fixtureModify)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "foo.txt")
	assert.Contains(t, strings.ToLower(err.Error()), "does not exist")
}

func TestApplyPatch_PathTraversalRefused(t *testing.T) {
	root := t.TempDir()
	// A sibling directory that must never be written to.
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	_ = os.Remove(outside)

	bm, err := ApplyPatch(root, []payload.FileEntry{fe("../outside.txt", fixtureCreate)})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "escapes")
	assert.NoFileExists(t, outside, "must never write outside the working-tree root")
	assert.Empty(t, bm)
}

// TestApplyPatch_SymlinkedDirComponentRefused guards the symlink-escape defense:
// a symlinked directory component inside root that points outside must not let a
// write follow it out of the tree (a purely lexical containment check misses this).
func TestApplyPatch_SymlinkedDirComponentRefused(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // a sibling dir that must never be written to
	victim := filepath.Join(outside, "victim.txt")
	writeFile(t, victim, "ORIGINAL\n")
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "link")))

	bm, err := ApplyPatch(root, []payload.FileEntry{fe("link/victim.txt", fixtureModify)})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "escape")
	assert.Equal(t, "ORIGINAL\n", readFile(t, victim), "must not write through a symlinked directory component")
	assert.Empty(t, bm)
}

func TestApplyPatch_DeleteRemovalFailsIsolated(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "del.txt"), "gone1\ngone2\n")
	writeFile(t, filepath.Join(root, "bar.txt"), "x\ny\nz\n")

	// Inject a removal failure for the delete entry only.
	orig := removeFn
	removeFn = func(path string) error { return os.ErrPermission }
	t.Cleanup(func() { removeFn = orig })

	bm, err := ApplyPatch(root, []payload.FileEntry{
		fe("del.txt", fixtureDelete),
		fe("bar.txt", fixtureModifyBar),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "del.txt")
	// The sibling modify still succeeded (per-file isolation, AC 01-04).
	assert.Equal(t, "x\ny-mod\nz\n", readFile(t, filepath.Join(root, "bar.txt")))
	assert.Contains(t, bm, filepath.Join(root, "bar.txt"))
	assert.NotContains(t, bm, filepath.Join(root, "del.txt"), "failed delete must not be recorded as success")
}

// --- AC 01-04: per-file error isolation & aggregation --------------------

func TestApplyPatch_FailMiddleIsolatesOtherSuccesses(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo.txt"), "line1\nline2\nline3\n")
	writeFile(t, filepath.Join(root, "bar.txt"), "x\ny\nz\n")
	// drift.txt content will not match fixtureDrift's context -> hard apply failure.
	writeFile(t, filepath.Join(root, "drift.txt"), "totally\ndifferent\ncontent\nhere\n")

	bm, err := ApplyPatch(root, []payload.FileEntry{
		fe("foo.txt", fixtureModify),
		fe("new.txt", fixtureCreate),
		fe("drift.txt", fixtureDrift),
		fe("bar.txt", fixtureModifyBar),
	})
	require.Error(t, err)
	// Successful entries applied on disk.
	assert.Equal(t, "line1\nline2-modified\nline3\n", readFile(t, filepath.Join(root, "foo.txt")))
	assert.Equal(t, "hello\nworld\n", readFile(t, filepath.Join(root, "new.txt")))
	assert.Equal(t, "x\ny-mod\nz\n", readFile(t, filepath.Join(root, "bar.txt")))
	// Failed entry untouched, no backup.
	assert.Equal(t, "totally\ndifferent\ncontent\nhere\n", readFile(t, filepath.Join(root, "drift.txt")))
	assert.NoFileExists(t, filepath.Join(root, "drift.txt.bak"))
	// Error names the failing entry; BackupMap has only successes.
	assert.Contains(t, err.Error(), "drift.txt")
	assert.Len(t, bm, 3)
	assert.NotContains(t, bm, filepath.Join(root, "drift.txt"))
}

func TestApplyPatch_AllFailNoFilesModified(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo.txt"), "unchanged\n")

	bm, err := ApplyPatch(root, []payload.FileEntry{
		fe("foo.txt", "garbage one"),
		fe("bar.txt", "garbage two"),
	})
	require.Error(t, err)
	// Aggregate error surfaces BOTH failing paths, not just the first.
	assert.Contains(t, err.Error(), "foo.txt")
	assert.Contains(t, err.Error(), "bar.txt")
	assert.Equal(t, "unchanged\n", readFile(t, filepath.Join(root, "foo.txt")))
	assert.Empty(t, bm)
}
