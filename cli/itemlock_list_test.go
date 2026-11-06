package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// debtLocksJSON runs `atcr debt locks --json` and returns its rows by id.
func debtLocksJSON(t *testing.T, args ...string) (debtLocksReport, map[string]debtLockRow) {
	t.Helper()
	out, err := runDebt(t, append([]string{"locks", "--json"}, args...)...)
	require.NoError(t, err, out)
	var rep debtLocksReport
	require.NoError(t, json.Unmarshal([]byte(out), &rep), out)
	rows := map[string]debtLockRow{}
	for _, r := range rep.Locks {
		rows[r.ID] = r
	}
	return rep, rows
}

// The task's success criterion: a >12h lock and a lock on a resolved item are
// flagged, a fresh lock is not.
func TestDebtLocks_FlagsOldAndResolvedNotFresh(t *testing.T) {
	isolate(t)
	remote, stores := itemLockClones(t, 1)
	recs := debtSampleRecords()
	fresh, resolved, old := recs[0].ID, recs[1].ID, recs[2].ID
	require.Equal(t, localdebt.StatusResolved, recs[1].Status)

	_, err := runDebt(t, "lock", fresh, resolved, "--store", stores[0], "--holder", "alice")
	require.NoError(t, err)
	_, err = localdebt.ItemLocker{Dir: stores[0], Holder: "bob", Now: func() time.Time {
		return time.Now().Add(-13 * time.Hour)
	}}.Lock(context.Background(), []string{old})
	require.NoError(t, err)
	before := remoteLockRefs(t, remote)

	rep, rows := debtLocksJSON(t, "--store", stores[0])
	require.Equal(t, "12h0m0s", rep.OlderThan)
	require.Len(t, rows, 3)
	require.Empty(t, rows[fresh].Flags, "a fresh lock on an open item is not flagged")
	require.Equal(t, []string{debtLockFlagResolved}, rows[resolved].Flags)
	require.Equal(t, localdebt.StatusResolved, rows[resolved].Status)
	require.Equal(t, []string{debtLockFlagOld}, rows[old].Flags)
	require.Equal(t, "bob", rows[old].Holder)

	out, err := runDebt(t, "locks", "--store", stores[0])
	require.NoError(t, err, out)
	require.Contains(t, out, fresh)
	require.Contains(t, out, "older than 12h0m0s")
	require.Contains(t, out, "item resolved")
	require.Contains(t, out, "2 of 3 locks flagged")

	require.Equal(t, before, remoteLockRefs(t, remote), "locks must never free a lock")
}

func TestDebtLocks_OlderThanOverrides(t *testing.T) {
	isolate(t)
	_, stores := itemLockClones(t, 1)
	recs := debtSampleRecords()
	_, err := localdebt.ItemLocker{Dir: stores[0], Holder: "bob", Now: func() time.Time {
		return time.Now().Add(-13 * time.Hour)
	}}.Lock(context.Background(), []string{recs[0].ID})
	require.NoError(t, err)
	_, err = localdebt.ItemLocker{Dir: stores[0], Holder: "bob", Now: func() time.Time {
		return time.Now().Add(-2 * time.Hour)
	}}.Lock(context.Background(), []string{recs[2].ID})
	require.NoError(t, err)

	rep, rows := debtLocksJSON(t, "--store", stores[0], "--older-than", "14h")
	require.Equal(t, "14h0m0s", rep.OlderThan)
	require.Empty(t, rows[recs[0].ID].Flags)
	require.Empty(t, rows[recs[2].ID].Flags)

	_, rows = debtLocksJSON(t, "--store", stores[0], "--older-than", "1h")
	require.Equal(t, []string{debtLockFlagOld}, rows[recs[0].ID].Flags)
	require.Equal(t, []string{debtLockFlagOld}, rows[recs[2].ID].Flags)
}

func TestDebtLocks_ResolvedAndOldCarriesBothFlags(t *testing.T) {
	isolate(t)
	_, stores := itemLockClones(t, 1)
	id := debtSampleRecords()[1].ID
	_, err := localdebt.ItemLocker{Dir: stores[0], Holder: "bob", Now: func() time.Time {
		return time.Now().Add(-24 * time.Hour)
	}}.Lock(context.Background(), []string{id})
	require.NoError(t, err)
	_, rows := debtLocksJSON(t, "--store", stores[0])
	require.Equal(t, []string{debtLockFlagOld, debtLockFlagResolved}, rows[id].Flags)
}

// A lock on an id the store does not hold is listed with no status and is not
// flagged as resolved: the plan flags age and a resolved status only.
func TestDebtLocks_LockOnUnknownItemIsListed(t *testing.T) {
	isolate(t)
	_, stores := itemLockClones(t, 1)
	_, err := localdebt.ItemLocker{Dir: stores[0], Holder: "bob"}.Lock(context.Background(), []string{"gone"})
	require.NoError(t, err)
	_, rows := debtLocksJSON(t, "--store", stores[0])
	require.Contains(t, rows, "gone")
	require.Empty(t, rows["gone"].Status)
	require.Empty(t, rows["gone"].Flags)
}

func TestDebtLocks_NoLocks(t *testing.T) {
	isolate(t)
	_, stores := itemLockClones(t, 1)
	out, err := runDebt(t, "locks", "--store", stores[0])
	require.NoError(t, err, out)
	require.Contains(t, out, "No item locks")

	rep, _ := debtLocksJSON(t, "--store", stores[0])
	require.NotNil(t, rep.Locks, "--json prints an empty array, not null")
}

func TestDebtLocks_BadOlderThanIsUsageError(t *testing.T) {
	isolate(t)
	_, stores := itemLockClones(t, 1)
	for _, v := range []string{"0", "-1h", "soon"} {
		_, err := runDebt(t, "locks", "--store", stores[0], "--older-than", v)
		require.Error(t, err, v)
		require.Equal(t, 2, exitCode(err), v)
	}
}

func TestDebtLocks_TakesNoArgs(t *testing.T) {
	isolate(t)
	_, stores := itemLockClones(t, 1)
	_, err := runDebt(t, "locks", "x", "--store", stores[0])
	require.Error(t, err)
}

func TestDebtLocks_NoGitRepoIsError(t *testing.T) {
	isolate(t)
	store := writeLocalDebt(t)
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(filepath.Dir(filepath.Dir(store))))
	_, err := runDebt(t, "locks", "--store", store)
	require.ErrorContains(t, err, "not inside a git repository")
}

func TestDebtLocks_HelpSaysItNeverFrees(t *testing.T) {
	out, err := runDebt(t, "locks", "--help")
	require.NoError(t, err)
	flat := strings.Join(strings.Fields(out), " ")
	require.Contains(t, flat, "never frees a lock")
	require.Contains(t, flat, "no liveness check")
}
