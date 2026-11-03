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
