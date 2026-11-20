//go:build unix

package localdebt

import (
	"os"
	"path/filepath"
	"testing"

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
