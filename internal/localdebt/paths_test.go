package localdebt

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestManualRunID_RejectsOutOfRangeYears pins the validated (string, error)
// contract (TD internal/localdebt/paths.go:51): years outside 1-9999 format
// through RFC3339 as "10000-01-…" or "-0001-01-…" — neither matches monthRe,
// so Append would reject the record outright — and the ZERO time formats as
// "0001-01-…", which monthRe ACCEPTS, silently creating a permanent phantom
// .atcr/debt/0001-01.jsonl shard. The helper now errors instead of formatting
// blindly, so the precondition is enforced at the boundary rather than trusted
// to every caller's reading of a doc comment.
func TestManualRunID_RejectsOutOfRangeYears(t *testing.T) {
	cases := []struct {
		name    string
		ts      time.Time
		wantErr bool
	}{
		{"zero time (phantom 0001-01 shard)", time.Time{}, true},
		{"year 0", time.Date(0, 6, 14, 10, 0, 0, 0, time.UTC), true},
		{"negative year", time.Date(-5, 6, 14, 10, 0, 0, 0, time.UTC), true},
		{"year 1 (floor, valid)", time.Date(1, 6, 14, 10, 0, 0, 0, time.UTC), false},
		{"year 9999 (ceiling, valid)", time.Date(9999, 6, 14, 10, 0, 0, 0, time.UTC), false},
		{"year 10000", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"ordinary present day", time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := ManualRunID(tc.ts)
			if tc.wantErr {
				require.Error(t, err)
				assert.Empty(t, id, "a rejected timestamp must not produce a run_id")
			} else {
				require.NoError(t, err)
				// Every accepted timestamp must resolve to a month shard.
				_, merr := monthFromRunID(id)
				require.NoError(t, merr, "an accepted run_id must carry a resolvable YYYY-MM prefix")
			}
		})
	}
}

// TestEnsureStoreDir_MissingAnchorNamesTheConfiguredStore pins row 993fffb0c24de643
// (TD internal/localdebt/paths.go:45). ensureStoreDir's existence check sits two
// levels above the store dir — the repo root ONLY for the default <root>/.atcr/debt
// layout. A configured debt.store (StoreDir) may sit at any depth: for
// docs/planning/debt the checked anchor is <root>/docs, which is not the repo root.
// When that anchor is missing, the refusal must name the CONFIGURED STORE and the
// directory that is actually missing, not label the anchor "store root" — the wrong
// diagnosis sends the operator looking for a vanished checkout instead of a missing
// parent of their configured store.
func TestEnsureStoreDir_MissingAnchorNamesTheConfiguredStore(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs", "planning", "debt")

	err := ensureStoreDir(dir)
	require.Error(t, err, "a store whose grandparent is missing must be refused, not created")
	assert.Contains(t, err.Error(), `"debt"`, "the refusal must name the configured store")
	assert.Contains(t, err.Error(), `"docs"`, "the refusal must name the directory that is actually missing")
	assert.NotContains(t, err.Error(), "store root",
		"the missing grandparent of a configured store is not the store root; calling it that misdiagnoses the cause")
	_, statErr := os.Stat(filepath.Join(root, "docs"))
	assert.True(t, os.IsNotExist(statErr), "the refusal must not create anything: %v", statErr)
}

// TestEnsureStoreDir_DefaultLayoutStillRefusesAVanishedRoot keeps the original
// stale-root stop signal intact under the new message: for the default layout the
// grandparent IS the repo root, and a root that vanished between validation and the
// write must still be an error, never a resurrection (TD internal/localdebt/root.go:123).
func TestEnsureStoreDir_DefaultLayoutStillRefusesAVanishedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "gone-repo")

	err := ensureStoreDir(DefaultDir(root))
	require.Error(t, err, "a vanished repo root must be refused, not recreated")
	assert.Contains(t, err.Error(), `"debt"`, "the refusal names the store")
	assert.Contains(t, err.Error(), `"gone-repo"`, "the refusal names the missing root")
	_, statErr := os.Stat(root)
	assert.True(t, os.IsNotExist(statErr), "the missing root must not be created: %v", statErr)
}

// TestEnsureStoreDir_ConfiguredStoreUnderExistingGrandparentIsCreated guards that
// the message fix changes no behavior: when the grandparent exists, a configured
// store at a non-default depth is still created on demand (the intermediate
// docs/planning is ordinary first-write behavior, paths.go MkdirAll).
func TestEnsureStoreDir_ConfiguredStoreUnderExistingGrandparentIsCreated(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	dir := filepath.Join(root, "docs", "planning", "debt")

	require.NoError(t, ensureStoreDir(dir))
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "the configured store must exist as a directory")
}

// TestEnsureStoreDir_ProjectTierDepthOneStoreRefusesAVanishedRoot pins row
// 24dcb363e7cd6d72: a project-tier debt.store one level below the repo root
// ("debt") has the root's PARENT as its grandparent, so the grandparent rule passed
// even when the root itself had vanished, and MkdirAll resurrected the root. A
// store projectStoreDir handed out is anchored to the repository root it was
// resolved against instead.
func TestEnsureStoreDir_ProjectTierDepthOneStoreRefusesAVanishedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "gone-repo")
	require.NoError(t, os.Mkdir(root, 0o755))
	dir, err := projectStoreDir(root, "debt")
	require.NoError(t, err)
	t.Cleanup(func() { forgetProjectStore(dir) })
	require.NoError(t, os.Remove(root))

	err = ensureStoreDir(dir)
	require.Error(t, err, "a vanished repo root under a depth-1 project store must be refused, not recreated")
	assert.Contains(t, err.Error(), `"debt"`, "the refusal names the store")
	assert.Contains(t, err.Error(), `"gone-repo"`, "the refusal names the missing repository root")
	// Pin the POSITIVE branch of the OpenRoot discriminator (epic 163 T2 review):
	// a truly missing root (fs.ErrNotExist) must get the missing-root refusal,
	// not the wrapped-cause message — both name the store and the root, so the
	// assertions above alone cannot tell the branches apart.
	assert.Contains(t, err.Error(), "does not exist; refusing to recreate it",
		"a missing root must be reported by the missing-root refusal")
	assert.NotContains(t, err.Error(), "opening its repository root",
		"the wrapped-cause message is only for non-ErrNotExist OpenRoot failures")
	_, statErr := os.Stat(root)
	assert.True(t, os.IsNotExist(statErr), "the missing root must not be created: %v", statErr)
}

// TestEnsureStoreDir_ProjectTierRootReplacedByAFileIsRefused pins the !info.IsDir()
// half of the project-tier root check (row e4120ba70e4c1d35): a registered root
// that now exists as a regular file is refused by the root check itself, not left
// to fail later inside MkdirAll with an error that never names the root.
func TestEnsureStoreDir_ProjectTierRootReplacedByAFileIsRefused(t *testing.T) {
	root := filepath.Join(t.TempDir(), "file-repo")
	require.NoError(t, os.Mkdir(root, 0o755))
	dir, err := projectStoreDir(root, "debt")
	require.NoError(t, err)
	t.Cleanup(func() { forgetProjectStore(dir) })
	require.NoError(t, os.Remove(root))
	require.NoError(t, os.WriteFile(root, []byte("not a repo\n"), 0o600))

	err = ensureStoreDir(dir)
	require.Error(t, err, "a registered root that is a regular file must be refused")
	assert.Contains(t, err.Error(), "repository root", "the refusal comes from the root check")
	assert.Contains(t, err.Error(), `"file-repo"`, "the refusal names the repository root")
	info, statErr := os.Stat(root)
	require.NoError(t, statErr)
	assert.False(t, info.IsDir(), "the file at the root path must be left alone")
}

// TestEnsureProjectStoreDir_OpenRootNonNotExistErrorKeepsCause pins rows
// 16d98ffaf8fe2b02 and 2d5085b196378c92: every os.OpenRoot failure was reported
// as "repository root does not exist", so a non-directory root, a permission
// denial or a symlink loop was misdiagnosed and the underlying cause dropped.
// Only fs.ErrNotExist may claim the root is missing; any other failure must
// wrap the cause, redacted to base names by basePathErr like every other
// surfaced path error.
func TestEnsureProjectStoreDir_OpenRootNonNotExistErrorKeepsCause(t *testing.T) {
	t.Run("root replaced by a regular file", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "file-root")
		require.NoError(t, os.Mkdir(root, 0o755))
		dir, err := projectStoreDir(root, "debt")
		require.NoError(t, err)
		t.Cleanup(func() { forgetProjectStore(dir) })
		require.NoError(t, os.Remove(root))
		require.NoError(t, os.WriteFile(root, []byte("not a repo\n"), 0o600))

		err = ensureStoreDir(dir)
		require.Error(t, err, "a root that is a regular file must be refused")
		assert.NotContains(t, err.Error(), "does not exist",
			"the root exists (as a file); only fs.ErrNotExist may report it missing")
		assert.Contains(t, err.Error(), `"debt"`, "the refusal names the store")
		assert.Contains(t, err.Error(), `"file-root"`, "the refusal names the repository root")
		var pe *os.PathError
		require.ErrorAs(t, err, &pe, "the OpenRoot cause must be wrapped, not dropped")
		require.Error(t, pe.Err, "the operational cause is preserved")
		assert.Contains(t, err.Error(), pe.Err.Error(), "the cause is readable in the message")
		assert.Equal(t, filepath.Base(pe.Path), pe.Path, "the wrapped path is redacted to its base name")
		assert.NotContains(t, err.Error(), root, "no absolute, username-bearing path reaches the error string")
	})
	t.Run("permission denied on the root", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not honor Unix permission bits")
		}
		if os.Geteuid() == 0 {
			t.Skip("root bypasses directory permission bits")
		}
		root := filepath.Join(t.TempDir(), "locked-root")
		require.NoError(t, os.Mkdir(root, 0o755))
		dir, err := projectStoreDir(root, "debt")
		require.NoError(t, err)
		t.Cleanup(func() { forgetProjectStore(dir) })
		require.NoError(t, os.Chmod(root, 0o000))
		t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

		err = ensureStoreDir(dir)
		require.Error(t, err, "an unopenable root must be refused")
		assert.NotContains(t, err.Error(), "does not exist",
			"the root exists; a permission denial must not be reported as a missing root")
		assert.ErrorIs(t, err, fs.ErrPermission, "the permission cause is preserved through the wrap")
		assert.NotContains(t, err.Error(), root, "no absolute path reaches the error string")
	})
}

// TestEnsureProjectStoreDir_MkdirAllFailureUnderLiveRootIsRedacted pins row
// ac1d185e5831d18d: the basePathErr fallback after r.MkdirAll fails while the
// repository root still exists was exercised by no test. A regular file planted
// at <root>/debt makes creation of the store at debt/sub fail inside MkdirAll
// (ENOTDIR/EEXIST) with the root alive, so the error must be the redacted
// *os.PathError itself — not the missing-root message and not the
// root-removed-during-creation message. No afterStoreRootChecked hook is used:
// it stays assigned only in the serial
// TestEnsureStoreDir_RootRemovedDuringCreationIsRefused (row 3fd4f8bde3b2ebf6).
func TestEnsureProjectStoreDir_MkdirAllFailureUnderLiveRootIsRedacted(t *testing.T) {
	root := t.TempDir()
	dir, err := projectStoreDir(root, "debt/sub")
	require.NoError(t, err)
	t.Cleanup(func() { forgetProjectStore(dir) })
	blocker := filepath.Join(root, "debt")
	require.NoError(t, os.WriteFile(blocker, []byte("a file, not a directory\n"), 0o600))

	err = ensureStoreDir(dir)
	require.Error(t, err, "a regular file blocking the store path must be refused")
	var pe *os.PathError
	require.ErrorAs(t, err, &pe, "the fallback returns the redacted *os.PathError")
	assert.Equal(t, filepath.Base(pe.Path), pe.Path, "the failing path is reduced to its base name")
	require.Error(t, pe.Err, "the operational cause is preserved")
	assert.NotContains(t, err.Error(), root, "no absolute path reaches the error string")
	assert.NotContains(t, err.Error(), "does not exist", "the root exists; this is not a missing-root refusal")
	assert.NotContains(t, err.Error(), "was removed while", "the root was never removed")
	info, statErr := os.Stat(root)
	require.NoError(t, statErr, "the live root must be left in place")
	assert.True(t, info.IsDir())
	info, statErr = os.Stat(blocker)
	require.NoError(t, statErr)
	assert.False(t, info.IsDir(), "the planted file must be left alone")
}

// TestProjectStores_KeyedByAbsolutePath pins row 7a03d6351e9f221a: projectStores
// was keyed by filepath.Clean(dir), so a store resolved against a relative root
// and later looked up by its absolute spelling (or the reverse) missed the entry
// and fell back to the grandparent rule, which resurrected a vanished depth-1
// root. Both spellings of one store now hit one entry.
func TestProjectStores_KeyedByAbsolutePath(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	require.NoError(t, os.Mkdir("rel-repo", 0o755))
	dir, err := projectStoreDir("rel-repo", "debt")
	require.NoError(t, err)
	require.False(t, filepath.IsAbs(dir), "precondition: the store was resolved against a relative root")
	t.Cleanup(func() { forgetProjectStore(dir) })

	absDir, err := filepath.Abs(dir)
	require.NoError(t, err)
	for _, spelling := range []string{dir, absDir, "./" + dir} {
		_, ok := projectStores.Load(projectStoreKey(spelling))
		assert.True(t, ok, "%q must find the project-store entry", spelling)
	}

	require.NoError(t, os.Remove("rel-repo"))
	err = ensureStoreDir(absDir)
	require.Error(t, err, "the absolute spelling of a project store must keep the root anchor")
	assert.Contains(t, err.Error(), `"rel-repo"`, "the refusal names the missing repository root")
	_, statErr := os.Stat(filepath.Join(parent, "rel-repo"))
	assert.True(t, os.IsNotExist(statErr), "the missing root must not be created: %v", statErr)
}

// TestEnsureStoreDir_ProjectTierStoreUnderLiveRootIsCreated guards the other side
// of row 24dcb363e7cd6d72: with the root present, a project-tier store is created
// on demand at depth 1, and at depth 3 with its intermediate directories missing.
func TestEnsureStoreDir_ProjectTierStoreUnderLiveRootIsCreated(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{"debt", "docs/planning/debt"} {
		dir, err := projectStoreDir(root, v)
		require.NoError(t, err)
		t.Cleanup(func() { forgetProjectStore(dir) })

		require.NoError(t, ensureStoreDir(dir), v)
		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.True(t, info.IsDir(), "%s must exist as a directory", v)
	}
}

// TestEnsureStoreDir_UserTierStoreOutsideRepoKeepsGrandparentRule: a user-tier
// store is not in projectStores, so it keeps the grandparent rule: refused when
// its grandparent is missing, created when it exists (row 24dcb363e7cd6d72).
func TestEnsureStoreDir_UserTierStoreOutsideRepoKeepsGrandparentRule(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	missing, err := userStoreDir(root, filepath.Join(outside, "planning", "notes", "debt"))
	require.NoError(t, err)
	err = ensureStoreDir(missing)
	require.Error(t, err, "a user-tier store whose grandparent is missing must be refused")
	assert.Contains(t, err.Error(), `"planning"`, "the refusal names the missing grandparent")
	_, statErr := os.Stat(filepath.Join(outside, "planning"))
	assert.True(t, os.IsNotExist(statErr), "the refusal must not create anything: %v", statErr)

	present, err := userStoreDir(root, filepath.Join(outside, "notes", "debt"))
	require.NoError(t, err)
	require.NoError(t, ensureStoreDir(present), "a user-tier store under an existing grandparent is created")
	info, err := os.Stat(present)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

// TestRedactPathErr pins the exported path-reduction contract (TD
// internal/localdebt/paths.go:51 — the row's FIX cell was a reconcile
// copy-paste of the ManualRunID item; this is the clarified fix): a surfaced
// *os.PathError is reduced to its base name so an absolute, username-bearing
// store path never reaches a diagnostic sink or an error string, while the
// operational Op and underlying Err are preserved; a non-PathError passes
// through unchanged.
func TestRedactPathErr(t *testing.T) {
	abs := filepath.Join(string(filepath.Separator), "home", "somebody", "repo", ".atcr", "debt", "2026-08.jsonl")
	pathErr := &os.PathError{Op: "open", Path: abs, Err: os.ErrPermission}

	redacted := RedactPathErr(pathErr)
	var pe *os.PathError
	require.ErrorAs(t, redacted, &pe, "a *os.PathError stays a *os.PathError")
	assert.Equal(t, filepath.Base(abs), pe.Path, "the absolute path is reduced to its base name")
	assert.Equal(t, "open", pe.Op, "the operational Op is preserved")
	assert.ErrorIs(t, redacted, os.ErrPermission, "the wrapped Err is preserved")
	assert.NotContains(t, redacted.Error(), "somebody", "no username-bearing path segment reaches the error string")

	other := errors.New("boom")
	assert.Same(t, other, RedactPathErr(other), "a non-PathError passes through unchanged")
}

// TestProjectStores_KeyedThroughSymlinks pins row 573b54a6bb1024d7: the
// projectStores key was absolute but not symlink-resolved, so a store
// registered through a linked parent and later reached by its real spelling
// (or the reverse: /var vs /private/var on macOS, a bind mount) missed the
// entry and fell to the grandparent rule, whose anchor at depth 1 is the
// root's parent; MkdirAll then resurrected the vanished root.
func TestProjectStores_KeyedThroughSymlinks(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	require.NoError(t, os.Mkdir(real, 0o755))
	link := filepath.Join(base, "link")
	if err := os.Symlink("real", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	require.NoError(t, os.Mkdir(filepath.Join(real, "repo"), 0o755))
	dir, err := projectStoreDir(filepath.Join(link, "repo"), "debt")
	require.NoError(t, err)
	t.Cleanup(func() { forgetProjectStore(dir) })

	realDir := filepath.Join(real, "repo", "debt")
	_, ok, err := projectStoreFor(realDir)
	require.NoError(t, err)
	assert.True(t, ok, "the real spelling must find the entry registered through the link")

	require.NoError(t, os.Remove(filepath.Join(real, "repo")))
	err = ensureStoreDir(realDir)
	require.Error(t, err, "the real spelling of a project store must keep the root anchor")
	assert.Contains(t, err.Error(), `"repo"`, "the refusal names the missing repository root")
	_, statErr := os.Stat(filepath.Join(real, "repo"))
	assert.True(t, os.IsNotExist(statErr), "the missing root must not be created: %v", statErr)
}

// TestForgetProjectStore pins the registry's eviction API (row
// 573b54a6bb1024d7): after forgetProjectStore no spelling of the store finds
// an entry, so a test or long-lived process is not left with call-order state.
func TestForgetProjectStore(t *testing.T) {
	root := t.TempDir()
	dir, err := projectStoreDir(root, "debt")
	require.NoError(t, err)
	_, ok, err := projectStoreFor(dir)
	require.NoError(t, err)
	require.True(t, ok)
	forgetProjectStore(dir)
	_, ok, err = projectStoreFor(dir)
	require.NoError(t, err)
	assert.False(t, ok, "a forgotten store must not be found")
}

// TestEnsureStoreDir_GrandparentRefusalNamesTheRule pins the observability half
// of row 573b54a6bb1024d7: a store that is not in the project registry is
// refused under the grandparent rule, and the error says so, so a registry miss
// is distinguishable from a project-tier root refusal.
func TestEnsureStoreDir_GrandparentRefusalNamesTheRule(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing", "notes", "debt")
	err := ensureStoreDir(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a project-tier store", "the refusal must name the rule it applied")
}

// TestEnsureStoreDir_NonStringRegistryValueIsAnError pins row
// 9ffc484fcafdf7c1: the registry value was read with a bare root.(string), so a
// value of any other type panicked on the write path of every debt command.
func TestEnsureStoreDir_NonStringRegistryValueIsAnError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo", "debt")
	projectStores.Store(projectStoreKey(dir), 42)
	t.Cleanup(func() { projectStores.Delete(projectStoreKey(dir)) })

	var ensureErr, containErr error
	require.NotPanics(t, func() { ensureErr = ensureStoreDir(dir) })
	require.NotPanics(t, func() { containErr = checkStoreContainment(dir) })
	assert.Error(t, ensureErr, "a malformed registry entry must be refused")
	assert.Error(t, containErr, "a malformed registry entry must be refused")
}

// TestEnsureStoreDir_RootRemovedDuringCreationIsRefused pins the fix row
// 9ffc484fcafdf7c1 asks for: the root was checked by path and the store then
// created by path with MkdirAll, so a root removed between the two was
// recreated. Creation is anchored to an opened root handle; a root that
// vanishes after it is opened makes creation fail instead.
func TestEnsureStoreDir_RootRemovedDuringCreationIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not remove a directory that is open")
	}
	root := filepath.Join(t.TempDir(), "racy-repo")
	require.NoError(t, os.Mkdir(root, 0o755))
	dir, err := projectStoreDir(root, "debt")
	require.NoError(t, err)
	t.Cleanup(func() { forgetProjectStore(dir) })
	afterStoreRootChecked = func() { require.NoError(t, os.Remove(root)) }
	t.Cleanup(func() { afterStoreRootChecked = func() {} })

	err = ensureStoreDir(dir)
	require.Error(t, err, "a root removed during creation must be refused, not recreated")
	assert.Contains(t, err.Error(), `"racy-repo"`, "the refusal names the repository root")
	_, statErr := os.Stat(root)
	assert.True(t, os.IsNotExist(statErr), "the removed root must not be created: %v", statErr)
}
