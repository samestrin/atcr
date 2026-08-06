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
}

// Only the leaf shard dir is created by the write path; an existing parent keeps
// whatever mode it already had, so tightening history's modes never rewrites the
// permissions of a .atcr/ tree another component created.
func TestAppend_LeavesExistingParentModeAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not modeled on Windows")
	}
	root := t.TempDir()
	atcr := filepath.Join(root, ".atcr")
	require.NoError(t, os.MkdirAll(atcr, 0o755))
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	require.NoError(t, Append(ShardPath(filepath.Join(atcr, "history"), ts), []Record{{Timestamp: ts, ID: "x", File: "a.go"}}))

	fi, err := os.Stat(atcr)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), fi.Mode().Perm(), "an existing .atcr/ must keep its own mode")
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
func TestAppend_PreservesExistingFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not modeled on Windows")
	}
	root := t.TempDir()
	path := filepath.Join(root, "findings-history.jsonl")
	require.NoError(t, os.WriteFile(path, nil, 0o644))
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	require.NoError(t, Append(path, []Record{{Timestamp: ts, ID: "x", File: "a.go"}}))

	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), fi.Mode().Perm())
}
