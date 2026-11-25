//go:build unix

package localdebt

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// optedInStoreWithoutGitignore builds an opted-in store as an older binary left
// it (items/ and marker, no ignore file) and returns the items dir.
func optedInStoreWithoutGitignore(t *testing.T, dir string) string {
	t.Helper()
	items := ItemsDir(dir)
	require.NoError(t, os.MkdirAll(items, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(items, eventLayoutMarker), nil, 0o600))
	return items
}

// assertStillSymlink fails unless path is still a symlink pointing at target.
func assertStillSymlink(t *testing.T, path, target string) {
	t.Helper()
	got, err := os.Readlink(path)
	require.NoError(t, err, "items/.gitignore is still a symlink")
	assert.Equal(t, target, got)
}

// Epic 122 F2(a): a shared store can carry a repository-supplied items/.gitignore
// that is a dangling symlink. os.Stat said not-exist and os.WriteFile followed the
// link, creating a file at its target outside the store. The write must not go
// through the link.
func TestAppend_EventLayoutDanglingGitignoreSymlinkNotWrittenThrough(t *testing.T) {
	root := t.TempDir()
	dir := DefaultDir(root)
	items := optedInStoreWithoutGitignore(t, dir)
	target := filepath.Join(root, "outside", "planted")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	gi := filepath.Join(items, itemsGitignore)
	require.NoError(t, os.Symlink(target, gi))

	require.NoError(t, Append(dir, eventRec(idP, "2026-06-14T10:00:00Z", "")))

	assert.NoFileExists(t, target, "the append must not create a file through the symlink")
	assertStillSymlink(t, gi, target)
	assert.Len(t, eventFiles(t, dir), 1, "the event is still written")
}

// Epic 122 F2(a): opting in over a dangling ignore-file symlink is refused, not
// written through.
func TestEnableEventLayout_DanglingGitignoreSymlinkRefused(t *testing.T) {
	root := t.TempDir()
	dir := DefaultDir(root)
	items := ItemsDir(dir)
	require.NoError(t, os.MkdirAll(items, 0o700))
	target := filepath.Join(root, "outside", "planted")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	gi := filepath.Join(items, itemsGitignore)
	require.NoError(t, os.Symlink(target, gi))

	err := EnableEventLayout(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing localdebt items .gitignore")
	assert.NotContains(t, err.Error(), dir, "the absolute store path is redacted")
	assert.NoFileExists(t, target, "opt-in must not create a file through the symlink")
	assertStillSymlink(t, gi, target)
}

// Epic 122 F2(a): an ignore-file symlink to an existing file outside the store was
// truncated and rewritten by EnableEventLayout. Neither path may touch the target.
func TestEventLayout_GitignoreSymlinkTargetNeverTruncated(t *testing.T) {
	root := t.TempDir()
	dir := DefaultDir(root)
	items := optedInStoreWithoutGitignore(t, dir)
	target := filepath.Join(root, "outside", "precious")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	require.NoError(t, os.WriteFile(target, []byte("keep me\n"), 0o600))
	gi := filepath.Join(items, itemsGitignore)
	require.NoError(t, os.Symlink(target, gi))

	require.Error(t, EnableEventLayout(dir), "opt-in refuses a symlinked ignore file")
	require.NoError(t, Append(dir, eventRec(idP, "2026-06-14T10:00:00Z", "")))

	b, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "keep me\n", string(b), "the symlink target is never truncated or rewritten")
	assertStillSymlink(t, gi, target)
}

// Epic 122 F2(a), review: the Lstat check alone is not the guard; a link planted
// between the Lstat and the open must fail the create itself. createItemsGitignore
// is called directly with a symlink already at the name, as that race would leave
// it, so only the O_EXCL|O_NOFOLLOW flags stand between it and the target.
func TestCreateItemsGitignore_RefusesSymlinkPlantedAfterLstat(t *testing.T) {
	for _, tc := range []struct {
		name        string
		plantTarget bool
	}{
		{"dangling", false},
		{"existing target", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			items := filepath.Join(root, "items")
			require.NoError(t, os.MkdirAll(items, 0o700))
			target := filepath.Join(root, "outside")
			if tc.plantTarget {
				require.NoError(t, os.WriteFile(target, []byte("keep me\n"), 0o600))
			}
			gi := filepath.Join(items, itemsGitignore)
			require.NoError(t, os.Symlink(target, gi))

			err := createItemsGitignore(gi)
			require.Error(t, err, "the create must fail on a symlink at the name")
			assert.NotContains(t, err.Error(), root, "the absolute path is redacted")
			if tc.plantTarget {
				b, rerr := os.ReadFile(target)
				require.NoError(t, rerr)
				assert.Equal(t, "keep me\n", string(b), "the target is not truncated")
			} else {
				assert.NoFileExists(t, target, "no file is created through the link")
			}
			assertStillSymlink(t, gi, target)
		})
	}
}

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

// Rows a55384eaf4b4f883 and 84e68def42784348: unix and Windows hold a kernel
// lock, so any temp the sweep sees belongs to a dead process and no age is
// required. Only lock_other.go's legacy mkdir lock sets one.
func TestEventTempSweepMinAge_ZeroOnKernelLocks(t *testing.T) {
	assert.Zero(t, eventTempSweepMinAge)
}

// The sweep helper keeps an event temp younger than minAge and removes an older
// one; a zero minAge removes every temp. The ages are faked with Chtimes.
func TestSweepEventTemps_MinAge(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	items := ItemsDir(dir)

	live := plantEventTemp(t, items, ".a.json.tmp-1", time.Second)
	dead := plantEventTemp(t, items, ".b.json.tmp-2", 2*time.Hour)
	sweepEventTemps(items, time.Hour)
	assert.FileExists(t, live, "a temp younger than minAge may be a live writer's and is kept")
	assert.NoFileExists(t, dead, "a temp older than minAge is reaped")

	sweepEventTemps(items, 0)
	assert.NoFileExists(t, live, "a zero minAge reaps every temp")
	assert.FileExists(t, filepath.Join(items, eventLayoutMarker), "the marker is never swept")
}

// The harm the rows name: on the legacy mkdir lock a waiter can reclaim a lock
// whose holder is still writing, and the reclaimer's sweep then deleted the
// holder's in-flight temp, failing its rename into place. With the legacy age
// set, a write's sweep spares a fresh temp and still reaps a stale leftover.
func TestAppend_LegacyLockSweepSparesLiveHoldersTemp(t *testing.T) {
	orig := eventTempSweepMinAge
	eventTempSweepMinAge = time.Hour
	t.Cleanup(func() { eventTempSweepMinAge = orig })

	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	items := ItemsDir(dir)
	live := plantEventTemp(t, items, ".a.json.tmp-1", time.Second)
	dead := plantEventTemp(t, items, ".b.json.tmp-2", 2*time.Hour)

	require.NoError(t, Append(dir, eventRec(idP, "2026-06-14T10:00:00Z", "")))

	assert.FileExists(t, live, "a live holder's in-flight temp survives the reclaimer's sweep")
	assert.NoFileExists(t, dead, "a stale crash leftover is still reaped")
	assert.Len(t, eventFiles(t, dir), 1)
}
