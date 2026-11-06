package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// itemLockClones writes the sample corpus into n clones of one remote and
// returns each clone's store directory.
func itemLockClones(t *testing.T, n int) (remote string, stores []string) {
	t.Helper()
	remote = t.TempDir()
	runFixtureGit(t, remote, "init", "-q")
	for i := 0; i < n; i++ {
		store := writeLocalDebt(t)
		clone := filepath.Dir(filepath.Dir(store))
		runFixtureGit(t, clone, "init", "-q")
		runFixtureGit(t, clone, "remote", "add", "origin", remote)
		stores = append(stores, store)
	}
	return remote, stores
}

func remoteLockRefs(t *testing.T, remote string) string {
	t.Helper()
	out, err := fixtureGit(remote, "for-each-ref", "--format=%(refname)", "refs/atcr/").CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func TestDebtLock_TwoClonesOneWinnerThenUnlock(t *testing.T) {
	isolate(t)
	remote, stores := itemLockClones(t, 2)
	id := debtSampleRecords()[0].ID

	out, err := runDebt(t, "lock", id, "--store", stores[0], "--holder", "alice")
	require.NoError(t, err, out)
	require.Contains(t, out, "Locked "+id)
	require.Contains(t, out, "alice")
	require.Equal(t, localdebt.ItemLockRef(id), remoteLockRefs(t, remote))

	_, err = runDebt(t, "lock", id, "--store", stores[1], "--holder", "bob")
	require.ErrorIs(t, err, localdebt.ErrItemLocked)
	require.Contains(t, err.Error(), "alice")

	_, err = runDebt(t, "unlock", id, "--store", stores[1], "--holder", "bob")
	require.ErrorIs(t, err, localdebt.ErrItemLockHeldByOther)

	out, err = runDebt(t, "unlock", id, "--store", stores[0], "--holder", "alice")
	require.NoError(t, err, out)
	require.Contains(t, out, "Unlocked "+id)
	require.Empty(t, remoteLockRefs(t, remote))

	_, err = runDebt(t, "unlock", id, "--store", stores[0], "--holder", "alice")
	require.ErrorIs(t, err, localdebt.ErrItemNotLocked)
}

func TestDebtLock_HolderFromEnv(t *testing.T) {
	isolate(t)
	_, stores := itemLockClones(t, 1)
	t.Setenv(localdebt.ItemLockHolderEnv, "from-env")
	id := debtSampleRecords()[0].ID
	out, err := runDebt(t, "lock", id, "--store", stores[0])
	require.NoError(t, err, out)
	require.Contains(t, out, "from-env")
	out, err = runDebt(t, "unlock", id, "--store", stores[0])
	require.NoError(t, err, out)
}

func TestDebtLock_UnknownIDIsUsageError(t *testing.T) {
	isolate(t)
	remote, stores := itemLockClones(t, 1)
	id := debtSampleRecords()[0].ID
	_, err := runDebt(t, "lock", id, "no-such-id", "--store", stores[0], "--holder", "alice")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no-such-id")
	require.Empty(t, remoteLockRefs(t, remote), "an unknown id must lock nothing")
}

func TestDebtLock_NeedsAnID(t *testing.T) {
	isolate(t)
	_, stores := itemLockClones(t, 1)
	for _, sub := range []string{"lock", "unlock"} {
		_, err := runDebt(t, sub, "--store", stores[0], "--holder", "alice")
		require.Error(t, err, sub)
	}
}

func TestDebtLock_NoGitRepoIsError(t *testing.T) {
	isolate(t)
	store := writeLocalDebt(t)
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(filepath.Dir(filepath.Dir(store))))
	_, err := runDebt(t, "lock", debtSampleRecords()[0].ID, "--store", store, "--holder", "alice")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not inside a git repository")
}

// AC1: locks are advisory; debt resolve and debt add do not check them.
func TestDebtLock_ResolveAndAddIgnoreLocks(t *testing.T) {
	isolate(t)
	_, stores := itemLockClones(t, 1)
	id := debtSampleRecords()[0].ID
	_, err := runDebt(t, "lock", id, "--store", stores[0], "--holder", "alice")
	require.NoError(t, err)

	out, err := runDebt(t, "resolve", id, "--store", stores[0])
	require.NoError(t, err, out)
	require.Contains(t, out, "Marked "+id)

	out, err = runDebt(t, "add", "--store", stores[0], "--severity", "LOW", "--file", "x.go:1",
		"--problem", "p", "--fix", "f", "--category", "c", "--est", "5")
	require.NoError(t, err, out)
}

func TestDebtLock_HelpSaysHolderIsPublished(t *testing.T) {
	for _, sub := range []string{"lock", "unlock"} {
		out, err := runDebt(t, sub, "--help")
		require.NoError(t, err)
		flat := strings.Join(strings.Fields(out), " ")
		require.Contains(t, flat, "holder name is stored in a ref on the remote", sub)
		require.Contains(t, flat, "readable by anyone who can read", sub)
		require.Contains(t, flat, localdebt.ItemLockHolderEnv, sub)
	}
}
