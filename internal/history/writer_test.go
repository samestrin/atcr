package history

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The ledger now lives under .atcr/ — local, uncommitted state, on the same
// footing as internal/localdebt's store. It therefore inherits localdebt's file
// modes (0700 dirs / 0600 files) rather than the 0755/0644 that suited the old
// version-controlled .planning/ home: a trend ledger records which packages
// accrue which findings, which is not something to hand to every local account.
func TestAppend_CreatesPrivateDirAndFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not modeled on Windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".atcr", "history")
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	path := ShardPath(dir, ts)

	require.NoError(t, Append(path, []Record{{Timestamp: ts, ID: "x", File: "a.go"}}))

	di, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), di.Mode().Perm(), "shard dir must not be group- or world-readable")

	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "shard file must not be group- or world-readable")

	// Stat alone would pass against an Append that created the file and wrote
	// nothing, so round-trip the payload back through Load: the modes are only
	// worth asserting on a ledger that actually holds the records.
	got, err := Load(path)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "x", got[0].ID)
	assert.Equal(t, "a.go", got[0].File)
	assert.True(t, got[0].Timestamp.Equal(ts), "timestamp must survive the JSONL round trip")
}

// Append's three error branches had no negative coverage anywhere in the package.
// Each case below drives exactly one of them and asserts the wrapping, so a
// refactor that swallows an error or drops the context is caught here.

// MkdirAll fails when a component of the shard dir path is a regular file.
func TestAppend_ErrorsWhenShardDirCannotBeCreated(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, ".atcr")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	err := Append(ShardPath(filepath.Join(blocker, "history"), ts), []Record{{Timestamp: ts, ID: "x"}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating history dir")
}

// OpenFile fails when the shard path itself is already a directory.
func TestAppend_ErrorsWhenLedgerPathIsADirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".atcr", "history")
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	path := ShardPath(dir, ts)
	require.NoError(t, os.MkdirAll(path, 0o700))

	err := Append(path, []Record{{Timestamp: ts, ID: "x"}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "opening history ledger")
}

// The write-error branch closes the handle before returning, and nothing else in
// the package reaches it. /dev/full accepts the open and fails every write with
// ENOSPC, which is the only portable-ish way to drive a short/failed write without
// mocking the filesystem. Absent on macOS, so the case skips there.
func TestAppend_ErrorsWhenWriteFails(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("/dev/full is not available on this platform")
	}
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	err := Append("/dev/full", []Record{{Timestamp: ts, ID: "x"}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing history ledger")
}

// Only the leaf shard dir is created by the write path; an existing parent keeps
// whatever mode it already had, so tightening history's modes never rewrites the
// permissions of a .atcr/ tree another component created.
//
// The premise is captured with os.Stat rather than assumed to be the 0o755 passed
// to MkdirAll: MkdirAll applies the process umask, so under a hardened umask the
// dir is never 0755 to begin with and a hardcoded assertion fails without Append
// doing anything wrong. Comparing before/after also states the actual contract —
// "unchanged" — instead of one particular bit pattern.
func TestAppend_LeavesExistingParentModeAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not modeled on Windows")
	}
	root := t.TempDir()
	atcr := filepath.Join(root, ".atcr")
	require.NoError(t, os.MkdirAll(atcr, 0o755))
	before, err := os.Stat(atcr)
	require.NoError(t, err)
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	require.NoError(t, Append(ShardPath(filepath.Join(atcr, "history"), ts), []Record{{Timestamp: ts, ID: "x", File: "a.go"}}))

	after, err := os.Stat(atcr)
	require.NoError(t, err)
	assert.Equal(t, before.Mode().Perm(), after.Mode().Perm(), "an existing .atcr/ must keep its own mode")
}

// The leaf shard dir gets the same treatment as the ledger file and as
// localdebt's store dir (ensureStoreDir in internal/localdebt/paths.go returns
// early for a dir that already exists): 0700 applies to a directory this call
// CREATES, and a pre-existing one keeps whatever mode it has. That distinction is
// load-bearing for the documented migration recipe, which is why it is pinned
// here rather than left to the doc comment — `mkdir -p .atcr/history` leaves 0755
// under a default umask and Append will not tighten it, so docs/history.md tells
// migrating users to `install -d -m 700` instead.
func TestAppend_LeavesExistingShardDirModeAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not modeled on Windows")
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".atcr", "history")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	// Capture what the umask actually allowed rather than assuming 0755: the
	// contract under test is "unchanged", not a specific bit pattern.
	before, err := os.Stat(dir)
	require.NoError(t, err)
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	require.NoError(t, Append(ShardPath(dir, ts), []Record{{Timestamp: ts, ID: "x", File: "a.go"}}))

	after, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, before.Mode().Perm(), after.Mode().Perm(),
		"an existing shard dir keeps its mode — Append never tightens one it did not create")
}

// Appending to a ledger that already exists must not change its mode: a repo
// that accrued history under the old 0644 default keeps working, and Append
// never silently re-permissions a file it did not create.
//
// As with the parent-mode case above, the fixture's mode is observed rather than
// assumed: os.WriteFile applies the umask too, so under `umask 077` the file is
// created 0600, Append correctly preserves it, and a hardcoded 0o644 assertion
// fails anyway. The property is preservation, so assert preservation.
func TestAppend_PreservesExistingFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not modeled on Windows")
	}
	root := t.TempDir()
	path := filepath.Join(root, "findings-history.jsonl")
	require.NoError(t, os.WriteFile(path, nil, 0o644))
	before, err := os.Stat(path)
	require.NoError(t, err)
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	require.NoError(t, Append(path, []Record{{Timestamp: ts, ID: "x", File: "a.go"}}))

	after, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, before.Mode().Perm(), after.Mode().Perm(),
		"an existing ledger keeps the mode it already had")
}
