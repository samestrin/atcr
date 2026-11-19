package localdebt

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storeDirEnv gives a test its own repo root and HOME, so the user tier is
// read from a file the test controls and never from the developer's own.
func storeDirEnv(t *testing.T) (root, home string) {
	t.Helper()
	root = t.TempDir()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return root, home
}

func setProjectStore(t *testing.T, root, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "config.yaml"), []byte(body), 0o644))
}

func setUserStore(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "registry.yaml"), []byte(body), 0o644))
}

func TestStoreDir_UnsetIsTheDefault(t *testing.T) {
	root, _ := storeDirEnv(t)
	got, err := StoreDir(root)
	require.NoError(t, err)
	assert.Equal(t, DefaultDir(root), got)

	// A config file without a debt block is unset too.
	setProjectStore(t, root, "agents: [a]\n")
	got, err = StoreDir(root)
	require.NoError(t, err)
	assert.Equal(t, DefaultDir(root), got)
}

func TestStoreDir_ProjectRelativePath(t *testing.T) {
	root, _ := storeDirEnv(t)
	setProjectStore(t, root, "debt:\n  store: planning/debt\n")
	got, err := StoreDir(root)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "planning", "debt"), got)
}

func TestStoreDir_ProjectBeatsUser(t *testing.T) {
	root, home := storeDirEnv(t)
	setProjectStore(t, root, "debt:\n  store: planning/debt\n")
	setUserStore(t, home, "debt:\n  store: "+filepath.Join(home, "elsewhere")+"\n")
	got, err := StoreDir(root)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "planning", "debt"), got)
}

func TestStoreDir_UserAbsoluteAndParentPaths(t *testing.T) {
	root, home := storeDirEnv(t)
	ext := filepath.Join(home, "planning", "debt")
	setUserStore(t, home, "debt:\n  store: "+ext+"\n")
	got, err := StoreDir(root)
	require.NoError(t, err)
	assert.Equal(t, ext, got)

	// A relative user path resolves against the repo root, so a sibling
	// planning repo is "../planning/debt".
	setUserStore(t, home, "debt:\n  store: ../planning/debt\n")
	got, err = StoreDir(root)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(filepath.Dir(root), "planning", "debt"), got)
}

func TestStoreDir_UserSystemDirRejected(t *testing.T) {
	root, home := storeDirEnv(t)
	setUserStore(t, home, "debt:\n  store: /etc/atcr-debt\n")
	_, err := StoreDir(root)
	require.Error(t, err)
}

func TestStoreDir_ProjectRejectsUntrustedPaths(t *testing.T) {
	for _, v := range []string{
		"/srv/planning/debt", // absolute
		"../planning/debt",   // parent traversal
		"a/../../b",          // traversal that Clean would collapse
		"a/../b",             // not clean
		"./debt",             // not clean
		"debt/",              // not clean
		".",                  // the repository itself
	} {
		t.Run(v, func(t *testing.T) {
			root, home := storeDirEnv(t)
			// A valid user tier must NOT rescue an invalid project value: an
			// invalid claim stops, it never falls through to the next tier.
			setUserStore(t, home, "debt:\n  store: "+filepath.Join(home, "ok")+"\n")
			setProjectStore(t, root, "debt:\n  store: '"+v+"'\n")
			_, err := StoreDir(root)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "debt.store")
			assert.NotContains(t, err.Error(), root, "errors carry no absolute repo path")
		})
	}
}

func TestStoreDir_ProjectSymlinkEscapeRejected(t *testing.T) {
	root, _ := storeDirEnv(t)
	outside := t.TempDir()

	// An intermediate directory that links outside the repository.
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "planning")))
	setProjectStore(t, root, "debt:\n  store: planning/debt\n")
	_, err := StoreDir(root)
	require.Error(t, err, "a link out of the repo must not carry the store with it")

	// The store directory itself as a link out of the repository.
	require.NoError(t, os.Remove(filepath.Join(root, "planning")))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "debtlink")))
	setProjectStore(t, root, "debt:\n  store: debtlink\n")
	_, err = StoreDir(root)
	require.Error(t, err)
}

func TestStoreDir_ProjectSymlinkInsideRepoAllowed(t *testing.T) {
	root, _ := storeDirEnv(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "real"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "planning")))
	setProjectStore(t, root, "debt:\n  store: planning/debt\n")
	got, err := StoreDir(root)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "planning", "debt"), got)
}

// The repo root itself may sit behind a link (macOS /tmp, a symlinked
// checkout); containment compares resolved paths, so that is not an escape.
func TestStoreDir_SymlinkedRootAllowed(t *testing.T) {
	_, _ = storeDirEnv(t)
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "repo")
	require.NoError(t, os.Symlink(real, link))
	setProjectStore(t, real, "debt:\n  store: planning/debt\n")
	got, err := StoreDir(link)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(link, "planning", "debt"), got)
}

func TestStoreDir_MalformedConfigIsAnError(t *testing.T) {
	root, home := storeDirEnv(t)
	setProjectStore(t, root, "debt:\n  store: [a]\n")
	_, err := StoreDir(root)
	require.Error(t, err)

	setProjectStore(t, root, "agents: [a]\n")
	setUserStore(t, home, "debt:\n  store: 3\n")
	_, err = StoreDir(root)
	require.Error(t, err)
}

// PersistForReconcile resolves its store through StoreDir, so reconcile writes
// land in the configured store and the default .atcr/debt is never created.
func TestPersistForReconcile_UsesConfiguredStore(t *testing.T) {
	root, home := storeDirEnv(t)
	ext := filepath.Join(home, "planning", "debt")
	setUserStore(t, home, "debt:\n  store: "+ext+"\n")

	var diag bytes.Buffer
	PersistForReconcile("review", oneFindingResult("a.go", 1, "leaks a handle"), PersistOpts{Root: root, Diag: &diag})

	recs, err := ReadAll(ext, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 1, "diag: %s", diag.String())
	_, err = os.Stat(DefaultDir(root))
	assert.True(t, os.IsNotExist(err), "the default store must not be written: %v", err)
}

func TestPersistForReconcile_InvalidStoreConfigIsNoPersist(t *testing.T) {
	root, _ := storeDirEnv(t)
	setProjectStore(t, root, "debt:\n  store: ../escape\n")

	var diag bytes.Buffer
	PersistForReconcile("review", oneFindingResult("a.go", 1, "leaks a handle"), PersistOpts{Root: root, Diag: &diag})

	assert.Contains(t, diag.String(), "skipping local debt persistence")
	_, err := os.Stat(DefaultDir(root))
	assert.True(t, os.IsNotExist(err), "an invalid store claim must not fall back to the default: %v", err)
	_, err = os.Stat(filepath.Join(filepath.Dir(root), "escape"))
	assert.True(t, os.IsNotExist(err))
}

// A dangling link cannot be followed, so containment cannot be shown: refused.
func TestStoreDir_ProjectDanglingSymlinkRejected(t *testing.T) {
	root, _ := storeDirEnv(t)
	require.NoError(t, os.Symlink(filepath.Join(root, "gone"), filepath.Join(root, "debtlink")))
	setProjectStore(t, root, "debt:\n  store: debtlink\n")
	_, err := StoreDir(root)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), root)
}

// A whitespace-only value is blank, and a blank value is unset in either tier:
// the project tier falls through to the user tier, the user tier to the default.
func TestStoreDir_WhitespaceOnlyValueIsUnset(t *testing.T) {
	root, home := storeDirEnv(t)
	ext := filepath.Join(home, "planning", "debt")
	setProjectStore(t, root, "debt:\n  store: \" \"\n")
	setUserStore(t, home, "debt:\n  store: "+ext+"\n")
	got, err := StoreDir(root)
	require.NoError(t, err)
	assert.Equal(t, ext, got, "a blank project value is unset, so the user tier applies")

	setUserStore(t, home, "debt:\n  store: \" \"\n")
	got, err = StoreDir(root)
	require.NoError(t, err)
	assert.Equal(t, DefaultDir(root), got, "a blank user value is unset, so the default applies")
}

// A ".." segment is refused with its own message. containedIn and the clean-path
// check would refuse most of these too, but with a reason that does not name
// the ".." the operator wrote.
func TestStoreDir_ProjectDotDotSegmentMessage(t *testing.T) {
	for _, v := range []string{"..", "../planning/debt", "a/../../b", "a/../b"} {
		t.Run(v, func(t *testing.T) {
			root, _ := storeDirEnv(t)
			setProjectStore(t, root, "debt:\n  store: '"+v+"'\n")
			_, err := StoreDir(root)
			require.Error(t, err)
			assert.Contains(t, err.Error(), `must not contain ".."`)
		})
	}
}

// An unreadable user registry is an error, never a fall-through to the default
// store: writing to .atcr/debt would hide that the configured store was missed.
func TestStoreDir_UnreadableUserRegistryIsAnError(t *testing.T) {
	root, home := storeDirEnv(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "atcr", "registry.yaml"), 0o755))
	_, err := StoreDir(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "registry.yaml")
	assert.NotContains(t, err.Error(), home)
}

// A project store is checked for containment when it is resolved AND again
// under the store lock before the first write: a link swapped in between (here
// the intermediate "planning" directory replaced by a link out of the repo)
// must not carry the write out of the repository.
func TestStoreDir_ProjectSymlinkSwappedAfterResolveRefusedAtWrite(t *testing.T) {
	root, _ := storeDirEnv(t)
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "planning"), 0o755))
	setProjectStore(t, root, "debt:\n  store: planning/debt\n")
	dir, err := StoreDir(root)
	require.NoError(t, err)

	require.NoError(t, os.Remove(filepath.Join(root, "planning")))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "planning")))

	err = Append(dir, sampleRecord("2026-06-14T10:00:00Z-abc"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the repository")
	assert.NotContains(t, err.Error(), root)
	entries, rerr := os.ReadDir(outside)
	require.NoError(t, rerr)
	assert.Empty(t, entries, "nothing may be written through the swapped link")
}

// The same swap made while the writer waits for the lock is caught by the
// re-check made once the lock is held.
func TestStoreDir_ProjectSymlinkSwappedWhileLockedRefused(t *testing.T) {
	root, _ := storeDirEnv(t)
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "planning", "debt"), 0o755))
	setProjectStore(t, root, "debt:\n  store: planning/debt\n")
	dir, err := StoreDir(root)
	require.NoError(t, err)

	// Mirror the store into outside, so the path stays valid after the swap.
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "debt"), 0o755))
	ran := false
	err = withLock(dir, "test", func() error {
		ran = true
		return nil
	})
	require.NoError(t, err)
	require.True(t, ran, "an unswapped store is written as before")

	swapped := false
	afterContainmentCheck = func() {
		if swapped {
			return
		}
		swapped = true
		require.NoError(t, os.RemoveAll(filepath.Join(root, "planning")))
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "planning")))
	}
	t.Cleanup(func() { afterContainmentCheck = func() {} })
	ran = false
	err = withLock(dir, "test", func() error {
		ran = true
		return nil
	})
	require.Error(t, err)
	assert.False(t, ran, "fn must not run on a store that left the repository")
	assert.Contains(t, err.Error(), "outside the repository")
}

// A user-tier store may sit anywhere, including behind a link out of the
// repository; the write-time re-check applies to project stores only.
func TestStoreDir_UserStoreBehindLinkStillWritable(t *testing.T) {
	root, home := storeDirEnv(t)
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "planning")))
	setUserStore(t, home, "debt:\n  store: planning/debt\n")
	dir, err := StoreDir(root)
	require.NoError(t, err)
	require.NoError(t, Append(dir, sampleRecord("2026-06-14T10:00:00Z-abc")))
	_, err = os.Stat(filepath.Join(outside, "debt", "2026-06.jsonl"))
	require.NoError(t, err)
}
