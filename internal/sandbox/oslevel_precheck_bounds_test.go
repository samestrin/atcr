package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedTreeWithFiles writes n regular files of size bytes each into a fresh temp
// dir and returns it.
func seedTreeWithFiles(t *testing.T, n, size int) string {
	t.Helper()
	dir := t.TempDir()
	body := make([]byte, size)
	for i := range body {
		body[i] = 'x'
	}
	for i := 0; i < n; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d", i)), body, 0o644))
	}
	return dir
}

// TestCheckSnapshotUsable_RefusesPastTheWritableEntryBound closes the gap between
// what the --auto-fix gate claims and what it checks.
//
// CheckOSLevelSnapshotUsable's godoc says it exists to catch at the gate every
// reason the caller's repository root would fault at Run, "before anything is
// touched". It only exercised osLevelContainmentArgs, so seedWritableCopy's hard
// bounds (osLevelMaxWritableCopyBytes / osLevelMaxWritableCopyEntries, enforced
// only during the real run) were invisible to it: a working tree over either
// bound passed the gate, --auto-fix applied the patch, and validation THEN
// faulted with the opaque "validation could not run" — the precise failure the
// gate was added to close.
func TestCheckSnapshotUsable_RefusesPastTheWritableEntryBound(t *testing.T) {
	origEntries := osLevelMaxWritableCopyEntries
	t.Cleanup(func() { osLevelMaxWritableCopyEntries = origEntries })

	snapshot := seedTreeWithFiles(t, 10, 1)
	cfg := DefaultOSLevelConfig()

	// Paired positive control FIRST: at the real bound this same tree is
	// acceptable. Without it, a gate that refused every writable snapshot for an
	// unrelated reason (the scratch/snapshot disjointness guard is the measured
	// one) would satisfy the refusal below while proving nothing.
	require.NoError(t, CheckSnapshotUsable(cfg, snapshot, true),
		"a small tree must pass at the production bound, or the refusal below is not attributable to the bound")

	osLevelMaxWritableCopyEntries = 3
	err := CheckSnapshotUsable(cfg, snapshot, true)
	require.Error(t, err, "a tree past the entry bound must be refused at the gate, not at Run")
	assert.Contains(t, err.Error(), "too large",
		"the gate must give seedWritableCopy's own specific reason, not a generic refusal")
}

// TestCheckSnapshotUsable_RefusesPastTheWritableByteBound is the sibling bound.
// Both are asserted because seedWritableCopy checks them at different points
// (entries per visited entry, bytes per regular file before the copy), so one
// working says nothing about the other.
func TestCheckSnapshotUsable_RefusesPastTheWritableByteBound(t *testing.T) {
	origBytes := osLevelMaxWritableCopyBytes
	t.Cleanup(func() { osLevelMaxWritableCopyBytes = origBytes })

	snapshot := seedTreeWithFiles(t, 4, 512)
	cfg := DefaultOSLevelConfig()

	require.NoError(t, CheckSnapshotUsable(cfg, snapshot, true))

	osLevelMaxWritableCopyBytes = 64
	err := CheckSnapshotUsable(cfg, snapshot, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

// TestCheckSnapshotUsable_BoundsDoNotApplyToReadOnly is the negative control that
// keeps the new check honest. A read-only run seeds NO copy, so applying the copy
// bounds there would refuse `--exec` runs that are perfectly capable of running —
// a false refusal in the fail-closed direction is still a defect.
func TestCheckSnapshotUsable_BoundsDoNotApplyToReadOnly(t *testing.T) {
	origEntries := osLevelMaxWritableCopyEntries
	t.Cleanup(func() { osLevelMaxWritableCopyEntries = origEntries })

	snapshot := seedTreeWithFiles(t, 10, 1)
	cfg := DefaultOSLevelConfig()

	osLevelMaxWritableCopyEntries = 1
	assert.NoError(t, CheckSnapshotUsable(cfg, snapshot, false),
		"a read-only run copies nothing, so the writable-copy bounds must not gate it")
}
