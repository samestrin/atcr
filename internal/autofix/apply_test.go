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
	"github.com/samestrin/atcr/internal/security"
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

	bm, _, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", fixtureModify)}, false)
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

	bm, _, err := ApplyPatch(root, []payload.FileEntry{fe("new.txt", fixtureCreate)}, false)
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

	bm, _, err := ApplyPatch(root, []payload.FileEntry{fe("new.txt", fixtureCreate)}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "new.txt")
	assert.Contains(t, strings.ToLower(err.Error()), "already exists")
	assert.Empty(t, bm, "refused create must not be recorded as a success")
	assert.Equal(t, "pre-existing\n", readFile(t, filepath.Join(root, "new.txt")), "existing file must be untouched")
}

func TestApplyPatch_DeleteExistingFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "del.txt"), "gone1\ngone2\n")

	bm, _, err := ApplyPatch(root, []payload.FileEntry{fe("del.txt", fixtureDelete)}, false)
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

	bm, _, err := ApplyPatch(root, []payload.FileEntry{
		fe("foo.txt", fixtureModify),
		fe("new.txt", fixtureCreate),
		fe("del.txt", fixtureDelete),
	}, false)
	require.NoError(t, err)

	assert.Equal(t, "line1\nline2-modified\nline3\n", readFile(t, filepath.Join(root, "foo.txt")))
	assert.Equal(t, "hello\nworld\n", readFile(t, filepath.Join(root, "new.txt")))
	assert.NoFileExists(t, filepath.Join(root, "del.txt"))
	assert.Len(t, bm, 3)
}

func TestApplyPatch_MultiHunkSingleFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "multi.txt"), "a\nb\nc\nd\ne\nf\ng\n")

	_, _, err := ApplyPatch(root, []payload.FileEntry{fe("multi.txt", fixtureMultiHunk)}, false)
	require.NoError(t, err)
	assert.Equal(t, "a\nB\nc\nd\ne\nF\ng\n", readFile(t, filepath.Join(root, "multi.txt")))
}

// --- AC 01-01 Edge Case 1: empty / nil entries ---------------------------

func TestApplyPatch_EmptyEntries(t *testing.T) {
	root := t.TempDir()

	for _, entries := range [][]payload.FileEntry{nil, {}} {
		bm, _, err := ApplyPatch(root, entries, false)
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

	_, _, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", fixtureModify)}, false)
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

	_, _, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", fixtureModify)}, false)
	require.NoError(t, err)
	close(stop)
	wg.Wait()
	assert.Equal(t, post, readFile(t, target))
}

func TestApplyPatch_NewFileIntoMissingDirFailsNoMkdir(t *testing.T) {
	root := t.TempDir()

	bm, _, err := ApplyPatch(root, []payload.FileEntry{fe("sub/new.txt", fixtureCreate)}, false)
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

	_, _, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", fixtureModify)}, false)
	require.NoError(t, err)
	assert.Equal(t, "line1\nline2\nline3\n", readFile(t, filepath.Join(root, "foo.txt.bak")),
		"stale .bak must be replaced with the pre-patch content")
}

// --- AC 01-01 Error Scenarios --------------------------------------------

func TestApplyPatch_MalformedDiffBody(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo.txt"), "unchanged\n")

	bm, _, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", "this is not a diff at all")}, false)
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

	bm, _, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", fixtureModify)}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "foo.txt")
	assert.Equal(t, "totally\ndifferent\ncontent\n", readFile(t, filepath.Join(root, "foo.txt")))
	assert.NoFileExists(t, filepath.Join(root, "foo.txt.bak"), "failed apply must not leave a backup")
	assert.Empty(t, bm)
}

func TestApplyPatch_ModifyMissingTarget(t *testing.T) {
	root := t.TempDir()
	// fixtureModify's old side is not /dev/null, but no file exists.
	_, _, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", fixtureModify)}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "foo.txt")
	assert.Contains(t, strings.ToLower(err.Error()), "does not exist")
}

func TestApplyPatch_PathTraversalRefused(t *testing.T) {
	root := t.TempDir()
	// A sibling directory that must never be written to.
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	_ = os.Remove(outside)

	bm, _, err := ApplyPatch(root, []payload.FileEntry{fe("../outside.txt", fixtureCreate)}, false)
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

	bm, _, err := ApplyPatch(root, []payload.FileEntry{fe("link/victim.txt", fixtureModify)}, false)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "escape")
	assert.Equal(t, "ORIGINAL\n", readFile(t, victim), "must not write through a symlinked directory component")
	assert.Empty(t, bm)
}

func TestApplyPatch_DeleteRemovalFailsIsolated(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "del.txt"), "gone1\ngone2\n")
	writeFile(t, filepath.Join(root, "bar.txt"), "x\ny\nz\n")

	// Inject a removal failure for the delete entry only; backup cleanup must still
	// succeed so the .bak is not stranded (TD-006).
	orig := removeFn
	delAbs := filepath.Join(root, "del.txt")
	removeFn = func(path string) error {
		if path == delAbs {
			return os.ErrPermission
		}
		return orig(path)
	}
	t.Cleanup(func() { removeFn = orig })

	bm, _, err := ApplyPatch(root, []payload.FileEntry{
		fe("del.txt", fixtureDelete),
		fe("bar.txt", fixtureModifyBar),
	}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "del.txt")
	// The sibling modify still succeeded (per-file isolation, AC 01-04).
	assert.Equal(t, "x\ny-mod\nz\n", readFile(t, filepath.Join(root, "bar.txt")))
	assert.Contains(t, bm, filepath.Join(root, "bar.txt"))
	assert.NotContains(t, bm, filepath.Join(root, "del.txt"), "failed delete must not be recorded as success")
	assert.NoFileExists(t, filepath.Join(root, "del.txt.bak"), "failed delete must not leave a stranded backup")
}

// A write failure after backup must clean up the staged .bak so it is not left
// untracked in the working tree (TD-006).
func TestApplyPatch_WriteFailureCleansUpBackup(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo.txt"), "line1\nline2\nline3\n")

	orig := writeFileAtomicFn
	writeFileAtomicFn = func(path string, data []byte) error { return os.ErrPermission }
	t.Cleanup(func() { writeFileAtomicFn = orig })

	bm, _, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", fixtureModify)}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "foo.txt")
	assert.Empty(t, bm, "failed write must not be recorded as a success")
	assert.NoFileExists(t, filepath.Join(root, "foo.txt.bak"), "failed write must not leave a stranded backup")
}

// --- AC 01-04: per-file error isolation & aggregation --------------------

func TestApplyPatch_FailMiddleIsolatesOtherSuccesses(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo.txt"), "line1\nline2\nline3\n")
	writeFile(t, filepath.Join(root, "bar.txt"), "x\ny\nz\n")
	// drift.txt content will not match fixtureDrift's context -> hard apply failure.
	writeFile(t, filepath.Join(root, "drift.txt"), "totally\ndifferent\ncontent\nhere\n")

	bm, _, err := ApplyPatch(root, []payload.FileEntry{
		fe("foo.txt", fixtureModify),
		fe("new.txt", fixtureCreate),
		fe("drift.txt", fixtureDrift),
		fe("bar.txt", fixtureModifyBar),
	}, false)
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

	bm, _, err := ApplyPatch(root, []payload.FileEntry{
		fe("foo.txt", "garbage one"),
		fe("bar.txt", "garbage two"),
	}, false)
	require.Error(t, err)
	// Aggregate error surfaces BOTH failing paths, not just the first.
	assert.Contains(t, err.Error(), "foo.txt")
	assert.Contains(t, err.Error(), "bar.txt")
	assert.Equal(t, "unchanged\n", readFile(t, filepath.Join(root, "foo.txt")))
	assert.Empty(t, bm)
}

// --- Task 02: workspace-integrity protected-path gate --------------------

// A patch entry whose Path targets a protected host-execution/config path is
// refused with security.ErrProtectedPath when allowConfigEdits is false, across
// all three entry kinds (create/modify/delete). The gate fires before parse/backup/
// write, so no filesystem side effect occurs and the error is distinguishable from
// a traversal/symlink/parse rejection via errors.Is.
func TestApplyPatch_ProtectedPathRefusedByDefault(t *testing.T) {
	cases := []struct {
		name string
		path string
		body string
	}{
		{"create workflow", ".github/workflows/ci.yml", fixtureCreate},
		{"modify dotenv", ".env", fixtureModify},
		{"delete git hook", ".git/hooks/pre-commit", fixtureDelete},
		{"modify vscode task", ".vscode/tasks.json", fixtureModify},
		{"modify idea run config", ".idea/workspace.xml", fixtureModify},
		{"modify gitlab ci", ".gitlab-ci.yml", fixtureModify},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()

			bm, _, err := ApplyPatch(root, []payload.FileEntry{fe(tc.path, tc.body)}, false)
			require.Error(t, err)
			assert.ErrorIs(t, err, security.ErrProtectedPath,
				"protected-path refusal must wrap security.ErrProtectedPath")
			assert.Contains(t, err.Error(), tc.path, "error must name the offending path")
			assert.Empty(t, bm, "refused entry must not be recorded as a success")
			// No filesystem side effect: the gate fires before any write/backup.
			assert.NoFileExists(t, filepath.Join(root, tc.path))
			assert.NoFileExists(t, filepath.Join(root, tc.path+".bak"))
		})
	}
}

// With allowConfigEdits=true the gate is bypassed and the entry falls through to
// the normal apply logic unchanged, for each of create/modify/delete.
func TestApplyPatch_ProtectedPathAllowedWithBypass(t *testing.T) {
	t.Run("create workflow", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755))

		bm, _, err := ApplyPatch(root, []payload.FileEntry{fe(".github/workflows/ci.yml", fixtureCreate)}, true)
		require.NoError(t, err)
		assert.Equal(t, "hello\nworld\n", readFile(t, filepath.Join(root, ".github/workflows/ci.yml")))
		assert.Contains(t, bm, filepath.Join(root, ".github/workflows/ci.yml"))
	})
	t.Run("modify dotenv", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".env"), "line1\nline2\nline3\n")

		bm, _, err := ApplyPatch(root, []payload.FileEntry{fe(".env", fixtureModify)}, true)
		require.NoError(t, err)
		assert.Equal(t, "line1\nline2-modified\nline3\n", readFile(t, filepath.Join(root, ".env")))
		assert.Contains(t, bm, filepath.Join(root, ".env"))
	})
	t.Run("delete git hook", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".git", "hooks", "del.txt"), "gone1\ngone2\n")

		bm, _, err := ApplyPatch(root, []payload.FileEntry{fe(".git/hooks/del.txt", fixtureDelete)}, true)
		require.NoError(t, err)
		assert.NoFileExists(t, filepath.Join(root, ".git/hooks/del.txt"))
		assert.Contains(t, bm, filepath.Join(root, ".git/hooks/del.txt"))
	})
}

// A non-protected path is completely unaffected by the new gate regardless of the
// allowConfigEdits value — a regression guard against over-broad matching.
func TestApplyPatch_NonProtectedPathUnaffectedByGate(t *testing.T) {
	for _, allow := range []bool{false, true} {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "src", "main.go"), "line1\nline2\nline3\n")

		bm, _, err := ApplyPatch(root, []payload.FileEntry{fe("src/main.go", fixtureModify)}, allow)
		require.NoErrorf(t, err, "non-protected path must apply cleanly (allowConfigEdits=%v)", allow)
		assert.Equal(t, "line1\nline2-modified\nline3\n", readFile(t, filepath.Join(root, "src/main.go")))
		assert.Contains(t, bm, filepath.Join(root, "src/main.go"))
	}
}

// The per-entry isolation contract holds for a protected-path rejection: a clean
// sibling entry in the same batch still applies, and only the protected entry is
// reported in the aggregated error.
func TestApplyPatch_ProtectedPathBatchIsolation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo.txt"), "line1\nline2\nline3\n")

	bm, _, err := ApplyPatch(root, []payload.FileEntry{
		fe(".github/workflows/ci.yml", fixtureCreate),
		fe("foo.txt", fixtureModify),
	}, false)
	require.Error(t, err)
	assert.ErrorIs(t, err, security.ErrProtectedPath)
	assert.Contains(t, err.Error(), ".github/workflows/ci.yml")
	// The clean sibling still landed (per-file isolation, AC 01-04).
	assert.Equal(t, "line1\nline2-modified\nline3\n", readFile(t, filepath.Join(root, "foo.txt")))
	assert.Contains(t, bm, filepath.Join(root, "foo.txt"))
	assert.NotContains(t, bm, filepath.Join(root, ".github/workflows/ci.yml"))
}

// The protected-path check fires BEFORE gitdiff.Parse: a protected entry with a
// deliberately malformed diff body returns the protected-path error, not a parse
// error, proving the gate's choke-point placement.
func TestApplyPatch_ProtectedPathCheckedBeforeParse(t *testing.T) {
	root := t.TempDir()

	bm, _, err := ApplyPatch(root, []payload.FileEntry{
		fe(".git/config", "this is not a diff at all"),
	}, false)
	require.Error(t, err)
	assert.ErrorIs(t, err, security.ErrProtectedPath,
		"gate must fire before parse; error must be the protected-path error, not a parse error")
	assert.NotContains(t, strings.ToLower(err.Error()), "parsing diff")
	assert.Empty(t, bm)
}

// --- Task 06: non-blocking review flags ----------------------------------

// fixtureModeChangeExec modifies bin/tool AND flips its mode 100644 -> 100755
// (an executable-bit change). Source content is the shared "line1\nline2\nline3\n".
// The index line carries NO trailing mode, exactly as real git emits for a
// combined mode+content change (a trailing mode on the index line would be parsed
// by go-gitdiff into OldMode, masking the change — this mirrors real output).
const fixtureModeChangeExec = `diff --git a/bin/tool b/bin/tool
old mode 100644
new mode 100755
index 1111111..2222222
--- a/bin/tool
+++ b/bin/tool
@@ -1,3 +1,3 @@
 line1
-line2
+line2-modified
 line3
`

// After the AC5 tech-debt resolution (option A), an executable-bit change is NO LONGER
// flagged: the apply pipeline forces mode 0644 (atomicfs) and the commit forces 100644,
// so an exec-bit change never lands and flagging it would warn about a non-change. The
// content edit still applies on a non-build path with no flag emitted.
func TestApplyPatch_ReviewFlags_ExecBitNotReported(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "bin", "tool"), "line1\nline2\nline3\n")
	writeFile(t, filepath.Join(root, "foo.txt"), "line1\nline2\nline3\n")

	bm, flags, err := ApplyPatch(root, []payload.FileEntry{
		fe("bin/tool", fixtureModeChangeExec),
		fe("foo.txt", fixtureModify),
	}, false)
	require.NoError(t, err)
	assert.Len(t, bm, 2)
	assert.Empty(t, flags, "exec-bit changes are no longer flagged (mode is not consulted)")
	// The content change still landed.
	assert.Equal(t, "line1\nline2-modified\nline3\n", readFile(t, filepath.Join(root, "bin", "tool")))
}

// A build-script path touch (deploy.sh) with no mode change is flagged as a
// build-script path.
func TestApplyPatch_ReviewFlags_BuildScriptReported(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "deploy.sh"), "line1\nline2\nline3\n")

	_, flags, err := ApplyPatch(root, []payload.FileEntry{fe("deploy.sh", fixtureModify)}, false)
	require.NoError(t, err)
	require.Len(t, flags, 1)
	assert.Equal(t, "deploy.sh", flags[0].Path)
	assert.Contains(t, flags[0].Reason, "build-script path")
}

// A flagged entry whose apply FAILS mid-write must not appear in the returned
// []ReviewFlag — only successfully-applied entries are reported. A clean flagged
// sibling still lands and is reported (per-entry isolation).
func TestApplyPatch_ReviewFlags_FailedEntryNotReported(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "deploy.sh"), "line1\nline2\nline3\n")
	writeFile(t, filepath.Join(root, "build.sh"), "line1\nline2\nline3\n")

	failAbs := filepath.Join(root, "deploy.sh")
	orig := writeFileAtomicFn
	writeFileAtomicFn = func(path string, data []byte) error {
		if path == failAbs {
			return os.ErrPermission
		}
		return orig(path, data)
	}
	t.Cleanup(func() { writeFileAtomicFn = orig })

	_, flags, err := ApplyPatch(root, []payload.FileEntry{
		fe("deploy.sh", fixtureModify), // flagged build-script, write fails
		fe("build.sh", fixtureModify),  // flagged build-script, write succeeds
	}, false)
	require.Error(t, err)
	require.Len(t, flags, 1, "the failed entry must not be reported; only the succeeding one")
	assert.Equal(t, "build.sh", flags[0].Path)
}

// fixtureModifyExecContentOnly is a content-only edit of an already-executable file
// (mode 100755 rides the index line, no "new mode" header). It is retained as a guard
// that an ordinary edit of an executable, non-build file emits no advisory flag.
const fixtureModifyExecContentOnly = `diff --git a/bin/tool b/bin/tool
index 83db48f..12940e8 100755
--- a/bin/tool
+++ b/bin/tool
@@ -1,3 +1,3 @@
 line1
-line2
+line2-modified
 line3
`

// A content-only edit of an already-executable, non-build file must not be flagged —
// FlagsForReview consults the path only, and bin/tool is neither a build script nor a
// protected path.
func TestApplyPatch_ReviewFlags_ContentEditOfExecutableNotFlagged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "bin", "tool"), "line1\nline2\nline3\n")

	_, flags, err := ApplyPatch(root, []payload.FileEntry{fe("bin/tool", fixtureModifyExecContentOnly)}, false)
	require.NoError(t, err)
	assert.Empty(t, flags, "a content-only edit of an executable file is not an exec-bit change")
	assert.Equal(t, "line1\nline2-modified\nline3\n", readFile(t, filepath.Join(root, "bin", "tool")))
}

// A run with no flagged entries returns an empty []ReviewFlag, so the caller's PR
// body stays byte-identical to today.
func TestApplyPatch_ReviewFlags_NoneWhenUnflagged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo.txt"), "line1\nline2\nline3\n")

	_, flags, err := ApplyPatch(root, []payload.FileEntry{fe("foo.txt", fixtureModify)}, false)
	require.NoError(t, err)
	assert.Empty(t, flags)
}
