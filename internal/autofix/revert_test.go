package autofix

// RED tests for Story 3 (Automatic Revert on Validation Failure) —
// AC 03-01 (backup-map precondition/tracking), 03-02 (restore on failure),
// 03-03 (cleanup on success), 03-04 (hard error on restore failure).
//
// These exercise revert.go's two entry points over the BackupMap that Story 1's
// ApplyPatch produces:
//   - RevertPatch(ctx, bm): failure path — restore every touched file, delete
//     patch-created files, collect ALL restore errors, return a named aggregate.
//   - CleanupBackups(ctx, bm): success path — best-effort .bak removal, tolerant
//     of already-absent, never fails the run.
//
// Shared helpers (fe/writeFile/readFile) and diff fixtures (fixtureModify,
// fixtureModifyBar, fixtureCreate, fixtureDelete, fixtureMultiHunk) live in the
// same package's apply_test.go / gitdiff_contract_test.go.
//
// TD-005 precondition: an in-tree symlink LEAF target must be refused at apply
// time so the empty-backup sentinel unambiguously means "pre-patch state absent"
// and RevertPatch can safely route an empty value to deletion.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fooPre = "line1\nline2\nline3\n"
	fooMod = "line1\nline2-modified\nline3\n"
	barPre = "x\ny\nz\n"
	barMod = "x\ny-mod\nz\n"
	// multi.txt pre/post for fixtureMultiHunk.
	multiPre = "a\nb\nc\nd\ne\nf\ng\n"
	multiMod = "a\nB\nc\nd\ne\nF\ng\n"
)

// applyModify writes pre-patch content and applies fixtureModify-style diffs,
// returning the resulting BackupMap. Fails the test on any apply error.
func applyClean(t *testing.T, root string, entries ...payload.FileEntry) BackupMap {
	t.Helper()
	bm, err := ApplyPatch(root, entries)
	require.NoError(t, err)
	return bm
}

// --- AC 03-01: backup-map precondition & tracking ------------------------

// Empty map -> RevertPatch is an immediate, error-free no-op (zero-touch patch).
func TestRevertPatch_EmptyMapIsNoOp(t *testing.T) {
	require.NoError(t, RevertPatch(context.Background(), BackupMap{}))
}

// Empty map -> CleanupBackups is a no-op that touches nothing.
func TestCleanupBackups_EmptyMapIsNoOp(t *testing.T) {
	CleanupBackups(context.Background(), BackupMap{}) // must not panic
}

// The map handed to revert covers exactly the files written — one entry per
// touched file — and each entry's value form matches its origin: a modify carries
// a non-empty .bak; a create carries the empty sentinel.
func TestRevertPatch_MapCoverageMatchesWrites(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo.txt"), fooPre)

	bm := applyClean(t, root,
		fe("foo.txt", fixtureModify),
		fe("new.txt", fixtureCreate),
	)
	require.Len(t, bm, 2)

	fooAbs := filepath.Join(root, "foo.txt")
	newAbs := filepath.Join(root, "new.txt")
	assert.Equal(t, filepath.Join(root, "foo.txt.bak"), bm[fooAbs], "modify -> non-empty .bak")
	assert.Equal(t, "", bm[newAbs], "create -> empty sentinel")
}

// --- AC 03-02: restore all touched files on validation failure -----------

func TestRevertPatch_SingleFileRestored(t *testing.T) {
	root := t.TempDir()
	fooAbs := filepath.Join(root, "foo.txt")
	writeFile(t, fooAbs, fooPre)

	bm := applyClean(t, root, fe("foo.txt", fixtureModify))
	require.Equal(t, fooMod, readFile(t, fooAbs), "precondition: patch applied")

	require.NoError(t, RevertPatch(context.Background(), bm))

	assert.Equal(t, fooPre, readFile(t, fooAbs), "file restored byte-for-byte")
	assert.NoFileExists(t, fooAbs+".bak", "backup removed after successful restore")
}

func TestRevertPatch_MultiFileAllRestored(t *testing.T) {
	root := t.TempDir()
	fooAbs := filepath.Join(root, "foo.txt")
	barAbs := filepath.Join(root, "bar.txt")
	multiAbs := filepath.Join(root, "multi.txt")
	writeFile(t, fooAbs, fooPre)
	writeFile(t, barAbs, barPre)
	writeFile(t, multiAbs, multiPre)

	bm := applyClean(t, root,
		fe("foo.txt", fixtureModify),
		fe("bar.txt", fixtureModifyBar),
		fe("multi.txt", fixtureMultiHunk),
	)

	require.NoError(t, RevertPatch(context.Background(), bm))

	assert.Equal(t, fooPre, readFile(t, fooAbs))
	assert.Equal(t, barPre, readFile(t, barAbs))
	assert.Equal(t, multiPre, readFile(t, multiAbs))
}

// Partial apply: only files actually backed up appear in the map, so revert
// touches exactly those and leaves the never-written file alone.
func TestRevertPatch_PartialApplyRestoresOnlyBackedUp(t *testing.T) {
	root := t.TempDir()
	fooAbs := filepath.Join(root, "foo.txt")
	barAbs := filepath.Join(root, "bar.txt")
	driftAbs := filepath.Join(root, "drift.txt")
	writeFile(t, fooAbs, fooPre)
	writeFile(t, barAbs, barPre)
	writeFile(t, driftAbs, "totally\ndifferent\ncontent\nhere\n")

	// drift entry fails to apply -> never backed up, never in the map.
	bm, err := ApplyPatch(root, []payload.FileEntry{
		fe("foo.txt", fixtureModify),
		fe("drift.txt", fixtureDrift),
		fe("bar.txt", fixtureModifyBar),
	})
	require.Error(t, err)
	require.Len(t, bm, 2)

	require.NoError(t, RevertPatch(context.Background(), bm))

	assert.Equal(t, fooPre, readFile(t, fooAbs), "backed-up file restored")
	assert.Equal(t, barPre, readFile(t, barAbs), "backed-up file restored")
	assert.Equal(t, "totally\ndifferent\ncontent\nhere\n", readFile(t, driftAbs),
		"never-written file left exactly as-is")
}

// Edge Case 3: a restore-of-CREATE (empty sentinel) DELETES the created file,
// while a restore-of-MODIFY entry in the SAME map is copied back from its .bak.
func TestRevertPatch_RestoreOfCreateDeletesModifyCopiesBack(t *testing.T) {
	root := t.TempDir()
	fooAbs := filepath.Join(root, "foo.txt")
	newAbs := filepath.Join(root, "new.txt")
	writeFile(t, fooAbs, fooPre)

	bm := applyClean(t, root,
		fe("foo.txt", fixtureModify), // modify -> copy-back
		fe("new.txt", fixtureCreate), // create -> delete on revert
	)
	require.FileExists(t, newAbs)
	require.Equal(t, fooMod, readFile(t, fooAbs))

	require.NoError(t, RevertPatch(context.Background(), bm))

	assert.Equal(t, fooPre, readFile(t, fooAbs), "modify entry copied back from .bak")
	assert.NoFileExists(t, newAbs, "create entry deleted -> tree as if patch never applied")
	assert.NoFileExists(t, fooAbs+".bak")
}

// A delete-entry revert restores the removed file from its .bak (non-empty value).
func TestRevertPatch_DeleteEntryRestoresRemovedFile(t *testing.T) {
	root := t.TempDir()
	delAbs := filepath.Join(root, "del.txt")
	writeFile(t, delAbs, "gone1\ngone2\n")

	bm := applyClean(t, root, fe("del.txt", fixtureDelete))
	require.NoFileExists(t, delAbs, "precondition: delete applied")

	require.NoError(t, RevertPatch(context.Background(), bm))

	assert.Equal(t, "gone1\ngone2\n", readFile(t, delAbs), "deleted file restored from backup")
}

// Edge Case 2 (03-02) + Edge Case 1 (03-04): one file's restore fails, the loop
// still attempts every other file rather than stopping at the first failure.
func TestRevertPatch_OneRestoreFailsOthersAttempted(t *testing.T) {
	root := t.TempDir()
	fooAbs := filepath.Join(root, "foo.txt")
	barAbs := filepath.Join(root, "bar.txt")
	multiAbs := filepath.Join(root, "multi.txt")
	writeFile(t, fooAbs, fooPre)
	writeFile(t, barAbs, barPre)
	writeFile(t, multiAbs, multiPre)

	bm := applyClean(t, root,
		fe("foo.txt", fixtureModify),
		fe("bar.txt", fixtureModifyBar),
		fe("multi.txt", fixtureMultiHunk),
	)

	// Fault-inject: only bar.txt's restore fails.
	orig := copyPathFn
	copyPathFn = func(src, dst string) error {
		if dst == barAbs {
			return os.ErrPermission
		}
		return orig(src, dst)
	}
	t.Cleanup(func() { copyPathFn = orig })

	err := RevertPatch(context.Background(), bm)
	require.Error(t, err)

	assert.Equal(t, fooPre, readFile(t, fooAbs), "foo still restored despite bar's failure")
	assert.Equal(t, multiPre, readFile(t, multiAbs), "multi still restored despite bar's failure")
	assert.Equal(t, barMod, readFile(t, barAbs), "bar left in patched state (restore failed)")
	assert.FileExists(t, barAbs+".bak", "failed restore leaves .bak for manual recovery")
}

// --- AC 03-03: cleanup backups on validation success ---------------------

func TestCleanupBackups_AllBackupsRemovedLiveUntouched(t *testing.T) {
	root := t.TempDir()
	fooAbs := filepath.Join(root, "foo.txt")
	barAbs := filepath.Join(root, "bar.txt")
	writeFile(t, fooAbs, fooPre)
	writeFile(t, barAbs, barPre)

	bm := applyClean(t, root,
		fe("foo.txt", fixtureModify),
		fe("bar.txt", fixtureModifyBar),
	)
	require.FileExists(t, fooAbs+".bak")
	require.FileExists(t, barAbs+".bak")

	CleanupBackups(context.Background(), bm)

	assert.NoFileExists(t, fooAbs+".bak", "backup removed on success")
	assert.NoFileExists(t, barAbs+".bak", "backup removed on success")
	assert.Equal(t, fooMod, readFile(t, fooAbs), "live patched file untouched")
	assert.Equal(t, barMod, readFile(t, barAbs), "live patched file untouched")
}

func TestCleanupBackups_AlreadyAbsentTolerated(t *testing.T) {
	root := t.TempDir()
	fooAbs := filepath.Join(root, "foo.txt")
	barAbs := filepath.Join(root, "bar.txt")
	writeFile(t, fooAbs, fooPre)
	writeFile(t, barAbs, barPre)

	bm := applyClean(t, root,
		fe("foo.txt", fixtureModify),
		fe("bar.txt", fixtureModifyBar),
	)
	// Remove one .bak out-of-band before cleanup.
	require.NoError(t, os.Remove(fooAbs+".bak"))

	CleanupBackups(context.Background(), bm) // must not fail on the absent .bak

	assert.NoFileExists(t, barAbs+".bak", "remaining backup still removed")
}

// A non-ErrNotExist removal failure is best-effort: it is NOT fatal, and cleanup
// still processes every remaining entry rather than stopping at the failure.
func TestCleanupBackups_RemovalFailureIsBestEffort(t *testing.T) {
	root := t.TempDir()
	fooAbs := filepath.Join(root, "foo.txt")
	barAbs := filepath.Join(root, "bar.txt")
	writeFile(t, fooAbs, fooPre)
	writeFile(t, barAbs, barPre)

	bm := applyClean(t, root,
		fe("foo.txt", fixtureModify),
		fe("bar.txt", fixtureModifyBar),
	)

	orig := removeFn
	removeFn = func(path string) error {
		if path == fooAbs+".bak" {
			return os.ErrPermission
		}
		return orig(path)
	}
	t.Cleanup(func() { removeFn = orig })

	CleanupBackups(context.Background(), bm) // does not fail the run

	assert.FileExists(t, fooAbs+".bak", "the un-removable backup is left in place")
	assert.NoFileExists(t, barAbs+".bak", "the other backup is still removed (loop did not stop)")
}

// A created-file entry (empty sentinel) has no .bak; cleanup skips it and never
// deletes the live created file (that is revert's job, not cleanup's).
func TestCleanupBackups_CreatedEntrySkipped(t *testing.T) {
	root := t.TempDir()
	fooAbs := filepath.Join(root, "foo.txt")
	newAbs := filepath.Join(root, "new.txt")
	writeFile(t, fooAbs, fooPre)

	bm := applyClean(t, root,
		fe("foo.txt", fixtureModify),
		fe("new.txt", fixtureCreate),
	)

	CleanupBackups(context.Background(), bm)

	assert.NoFileExists(t, fooAbs+".bak", "modify backup removed")
	assert.FileExists(t, newAbs, "created live file NOT deleted by cleanup")
}

// --- AC 03-04: hard error surfacing on restore failure -------------------

func TestRevertPatch_SingleRestoreFailureNamedError(t *testing.T) {
	root := t.TempDir()
	fooAbs := filepath.Join(root, "foo.txt")
	writeFile(t, fooAbs, fooPre)

	bm := applyClean(t, root, fe("foo.txt", fixtureModify))

	orig := copyPathFn
	copyPathFn = func(_, _ string) error { return os.ErrPermission }
	t.Cleanup(func() { copyPathFn = orig })

	err := RevertPatch(context.Background(), bm)
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, fooAbs, "error names the diverged original path")
	assert.Contains(t, msg, fooAbs+".bak", "error names the expected backup path")
}

func TestRevertPatch_MultiRestoreFailureNamesEvery(t *testing.T) {
	root := t.TempDir()
	fooAbs := filepath.Join(root, "foo.txt")
	barAbs := filepath.Join(root, "bar.txt")
	multiAbs := filepath.Join(root, "multi.txt")
	writeFile(t, fooAbs, fooPre)
	writeFile(t, barAbs, barPre)
	writeFile(t, multiAbs, multiPre)

	bm := applyClean(t, root,
		fe("foo.txt", fixtureModify),
		fe("bar.txt", fixtureModifyBar),
		fe("multi.txt", fixtureMultiHunk),
	)

	// foo and multi fail; bar succeeds.
	orig := copyPathFn
	copyPathFn = func(src, dst string) error {
		if dst == fooAbs || dst == multiAbs {
			return os.ErrPermission
		}
		return orig(src, dst)
	}
	t.Cleanup(func() { copyPathFn = orig })

	err := RevertPatch(context.Background(), bm)
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, fooAbs, "aggregate names foo")
	assert.Contains(t, msg, multiAbs, "aggregate names multi")
	assert.Equal(t, barPre, readFile(t, barAbs), "the restorable file still restored")
}

// --- TD-005 precondition: in-tree symlink leaf refused at apply time -----

// A modify targeting an in-tree symlink leaf must be REFUSED, so the empty-backup
// sentinel never conflates "symlinked target" with "created file". The symlink and
// its real target are left untouched, and nothing is recorded in the map.
func TestApplyPatch_InTreeSymlinkLeafModifyRefused(t *testing.T) {
	root := t.TempDir()
	realAbs := filepath.Join(root, "real.txt")
	linkAbs := filepath.Join(root, "link.txt")
	writeFile(t, realAbs, fooPre)
	require.NoError(t, os.Symlink(realAbs, linkAbs))

	bm, err := ApplyPatch(root, []payload.FileEntry{fe("link.txt", fixtureModify)})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "symlink")
	assert.Empty(t, bm, "refused symlink-leaf entry is not recorded as a success")
	assert.Equal(t, fooPre, readFile(t, realAbs), "real target untouched")

	fi, lerr := os.Lstat(linkAbs)
	require.NoError(t, lerr)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "link.txt is still a symlink, not clobbered")
}

// Round-trip regression for TD-005: with the symlink leaf refused at apply time, a
// batch mixing a refused symlink target and a real create yields a map whose only
// empty-sentinel entry is the genuine create — so RevertPatch deletes the created
// file and never mistakes the symlink for one.
func TestRevertPatch_SymlinkLeafNeverAmbiguousWithCreate(t *testing.T) {
	root := t.TempDir()
	realAbs := filepath.Join(root, "real.txt")
	linkAbs := filepath.Join(root, "link.txt")
	newAbs := filepath.Join(root, "new.txt")
	writeFile(t, realAbs, fooPre)
	require.NoError(t, os.Symlink(realAbs, linkAbs))

	bm, err := ApplyPatch(root, []payload.FileEntry{
		fe("link.txt", fixtureModify), // refused
		fe("new.txt", fixtureCreate),  // genuine create
	})
	require.Error(t, err) // the symlink entry failed
	require.Len(t, bm, 1, "only the genuine create is recorded")
	require.Equal(t, "", bm[newAbs])

	require.NoError(t, RevertPatch(context.Background(), bm))

	assert.NoFileExists(t, newAbs, "created file removed on revert")
	assert.Equal(t, fooPre, readFile(t, realAbs), "symlink target never touched")
	fi, lerr := os.Lstat(linkAbs)
	require.NoError(t, lerr)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "symlink intact")
}
