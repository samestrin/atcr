package localdebt

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plantEventTemp writes an event temp of the CreateTemp shape into items with its
// modification time set age in the past, and returns its path.
func plantEventTemp(t *testing.T, items, name string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(items, name)
	require.NoError(t, os.WriteFile(p, []byte("{partial"), 0o600))
	when := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(p, when, when))
	return p
}

// testLegacyLock is a legacy lock with lock_other.go's production stale age and
// a wait short enough that a refused acquire fails fast.
func testLegacyLock() legacyLock {
	return legacyLock{wait: 2 * time.Second, stale: 30 * time.Second, sleep: time.Millisecond}
}

// installLegacySweepPolicy installs lk's sweep policy, as lock_other.go's init
// does, and restores the kernel-lock policy when the test ends.
func installLegacySweepPolicy(t *testing.T, lk legacyLock) {
	t.Helper()
	orig := eventTempSweep
	t.Cleanup(func() { eventTempSweep = orig })
	lk.installSweepPolicy()
}

// setOwnerEpoch rewrites the legacy lock's owner file under dir as session
// holding since epoch, as a holder that acquired then would have left it.
func setOwnerEpoch(t *testing.T, dir, session string, epoch time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, legacyLockSubdir, legacyLockOwner),
		[]byte(fmt.Sprintf("session=%s|epoch=%d", session, epoch.Unix())), 0o600))
}

func TestParseOwnerEpoch(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"session=s|epoch=1700000000", 1700000000},
		{"  session=s|epoch=42\n", 42},
		{"epoch=7", 7},
		{"session=s", 0},
		{"session=s|epoch=", 0},
		{"session=s|epoch=12x", 0},
		{"", 0},
	} {
		assert.Equal(t, tc.want, parseOwnerEpoch([]byte(tc.in)), "%q", tc.in)
	}
}

// Rows 127b6186816482da and 3e159ab087dce2bd: the reclaim decision is a pure
// function of the owner file, the time and the stale age, so it is checked here
// without a clock.
func TestLegacyOwnerStale(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	const stale = 30 * time.Second
	owner := func(epoch time.Time) []byte {
		return []byte(fmt.Sprintf("session=s|epoch=%d", epoch.Unix()))
	}
	for _, tc := range []struct {
		name  string
		data  []byte
		stale bool
	}{
		{"older than stale", owner(now.Add(-stale - time.Second)), true},
		{"exactly stale", owner(now.Add(-stale)), false},
		{"fresh", owner(now.Add(-time.Second)), false},
		{"future epoch", owner(now.Add(time.Hour)), false},
		{"no epoch yet", []byte("session=s"), false},
		{"unparsable epoch", []byte("session=s|epoch=soon"), false},
		{"zero epoch", []byte("session=s|epoch=0"), false},
	} {
		assert.Equal(t, tc.stale, legacyOwnerStale(tc.data, now, stale), tc.name)
	}
}

// A holder whose owner epoch is fresh keeps the lock: a waiter gives up at its
// deadline instead of reclaiming, and never runs fn.
func TestLegacyLock_LiveHolderIsNotReclaimed(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	lk := testLegacyLock()
	lk.wait = 50 * time.Millisecond

	err := lk.with(dir, "holder", func() error {
		ran := false
		err := lk.with(dir, "waiter", func() error { ran = true; return nil })
		assert.False(t, ran, "the waiter must not run while a live holder has the lock")
		return err
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout acquiring localdebt lock")
}

// Rows 127b6186816482da, 3e159ab087dce2bd and 75ba51c4cad5007a, the harm end to
// end with two holders and no sleeps: holder A takes the legacy lock and stalls
// mid-write with its event temp on disk; its owner epoch and its temp's mtime
// are then older than lockStale. Waiter B reclaims the lock and writes its own
// event. B's write must not delete A's temp, so A's rename into place succeeds
// when it resumes.
func TestLegacyLock_ReclaimerSparesLiveHoldersTemp(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	lk := testLegacyLock()
	installLegacySweepPolicy(t, lk)

	stalled, resume := make(chan string), make(chan struct{})
	var holding bool
	beforeEventRename = func(tmp string) {
		if !holding {
			return // only A stalls; B writes straight through
		}
		holding = false
		stalled <- tmp
		<-resume
	}
	t.Cleanup(func() { beforeEventRename = func(string) {} })

	holderErr := make(chan error, 1)
	holding = true
	go func() {
		holderErr <- lk.with(dir, "A", func() error {
			return appendLocked(dir, eventRec(idP, "2026-06-14T10:00:00Z", ""), &itemsPrep{})
		})
	}()
	aTemp := <-stalled

	// A has been stuck longer than lockStale: its owner epoch and its temp
	// both date from before then.
	past := time.Now().Add(-2 * lk.stale)
	setOwnerEpoch(t, dir, "A", past)
	require.NoError(t, os.Chtimes(aTemp, past, past))

	reclaimed := false
	require.NoError(t, lk.with(dir, "B", func() error {
		reclaimed = true
		return appendLocked(dir, eventRec(idX, "2026-06-14T10:00:01Z", ""), &itemsPrep{})
	}), "B reclaims the stale lock instead of timing out")
	assert.True(t, reclaimed)
	assert.FileExists(t, aTemp, "the reclaimer's write must not delete the live holder's temp")

	close(resume)
	require.NoError(t, <-holderErr, "A's rename into place succeeds after the reclaim")
	assert.Len(t, eventFiles(t, dir), 2, "both holders' events are published")
	assert.NoFileExists(t, aTemp, "A's temp was renamed into place")
}

// Rows 0e3cdd8b3aea8619 and 3e159ab087dce2bd: with the legacy lock's policy a
// write sweeps no temp at all, so no mtime (fresh, past lockStale, or in the
// future after a clock step) can get a live writer's temp deleted. The temps
// stay harmless: readers skip them and items/.gitignore keeps them out of
// `git add -A` (TestEventLayout_GitAddNeverStagesCrashLeftover).
func TestLegacyLock_WriteLeavesEveryEventTempInPlace(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	lk := testLegacyLock()
	installLegacySweepPolicy(t, lk)
	items := ItemsDir(dir)
	temps := []string{
		plantEventTemp(t, items, ".a.json.tmp-1", time.Second),
		plantEventTemp(t, items, ".b.json.tmp-2", 2*time.Hour),
		plantEventTemp(t, items, ".c.json.tmp-3", -time.Hour),
	}

	require.NoError(t, lk.with(dir, "append", func() error {
		return appendLocked(dir, eventRec(idP, "2026-06-14T10:00:00Z", ""), &itemsPrep{})
	}))

	for _, p := range temps {
		assert.FileExists(t, p, "the legacy lock sweeps no temp")
	}
	recs, err := ReadEventRecords(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 1, "readers skip every temp")
	assert.Equal(t, idP, recs[0].ID)
	gi, err := os.ReadFile(filepath.Join(items, itemsGitignore))
	require.NoError(t, err)
	assert.Equal(t, itemsGitignoreBody, string(gi), "items/.gitignore covers the temps")
}
