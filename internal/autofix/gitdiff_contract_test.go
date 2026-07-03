package autofix

// Retained go-gitdiff behavioral contract (Sprint 17.0, Phase 1).
//
// This is NOT throwaway spike code: it is a permanent regression guard for the
// single go-gitdiff invariant Story 1's ApplyPatch depends on —
//
//	a hunk that cannot be located in the source is a HARD FAILURE
//	(gitdiff.Apply returns a non-nil error), never a silent mis-apply.
//
// It also pins the create/delete/modify flag detection ApplyPatch branches on.
// If a future go-gitdiff upgrade changes any of these behaviors, this test fails
// loudly here rather than silently corrupting a working tree at apply time.
//
// It doubles as the durable importer that keeps go-gitdiff in go.mod/go.sum
// (an unimported require is pruned by `go mod tidy`). Fixtures mirror
// .planning/sprints/active/17.0_auto_merged_fixes/phase1-spike-findings.md.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fixtureModify = `diff --git a/foo.txt b/foo.txt
index 1111111..2222222 100644
--- a/foo.txt
+++ b/foo.txt
@@ -1,3 +1,3 @@
 line1
-line2
+line2-modified
 line3
`
	fixtureCreate = `diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+hello
+world
`
	fixtureDelete = `diff --git a/del.txt b/del.txt
deleted file mode 100644
index 4444444..0000000 100644
--- a/del.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-gone1
-gone2
`
	// fixtureDrift: hunk context that does NOT exist in the source.
	fixtureDrift = `diff --git a/drift.txt b/drift.txt
index 5555555..6666666 100644
--- a/drift.txt
+++ b/drift.txt
@@ -1,3 +1,3 @@
 alpha
-beta
+beta-modified
 gamma
`
)

func parseOne(t *testing.T, patch string) *gitdiff.File {
	t.Helper()
	files, _, err := gitdiff.Parse(strings.NewReader(patch))
	require.NoError(t, err)
	require.Len(t, files, 1)
	return files[0]
}

func applyTo(f *gitdiff.File, src string) (string, error) {
	var out bytes.Buffer
	err := gitdiff.Apply(&out, strings.NewReader(src), f)
	return out.String(), err
}

func TestGitdiffContract_ModifyAppliesCleanly(t *testing.T) {
	f := parseOne(t, fixtureModify)
	assert.False(t, f.IsNew)
	assert.False(t, f.IsDelete)
	out, err := applyTo(f, "line1\nline2\nline3\n")
	require.NoError(t, err)
	assert.Equal(t, "line1\nline2-modified\nline3\n", out)
}

func TestGitdiffContract_CreateDetectedAndApplies(t *testing.T) {
	f := parseOne(t, fixtureCreate)
	assert.True(t, f.IsNew, "create diff must set IsNew (ApplyPatch branches on this)")
	assert.Equal(t, "new.txt", f.NewName)
	out, err := applyTo(f, "") // create applies against empty source
	require.NoError(t, err)
	assert.Equal(t, "hello\nworld\n", out)
}

func TestGitdiffContract_DeleteDetectedAndApplies(t *testing.T) {
	f := parseOne(t, fixtureDelete)
	assert.True(t, f.IsDelete, "delete diff must set IsDelete (ApplyPatch branches on this)")
	assert.Equal(t, "del.txt", f.OldName)
	out, err := applyTo(f, "gone1\ngone2\n")
	require.NoError(t, err)
	assert.Equal(t, "", out)
}

// TestGitdiffContract_DriftIsHardFailure is THE invariant Story 1 relies on: a
// non-locatable hunk MUST return a non-nil error. If go-gitdiff ever starts
// applying a drifted hunk to the wrong location silently, this fails.
func TestGitdiffContract_DriftIsHardFailure(t *testing.T) {
	f := parseOne(t, fixtureDrift)
	_, err := applyTo(f, "totally\ndifferent\ncontent\nhere\n")
	require.Error(t, err, "a hunk that cannot be located must be a hard failure, never a silent mis-apply")
	// ApplyPatch treats ANY non-nil Apply error as a hard per-file failure and
	// does not branch on the error type; the wrapping is documented only.
	var applyErr *gitdiff.ApplyError
	assert.ErrorAs(t, err, &applyErr, "go-gitdiff v0.8.1 wraps apply failures as *gitdiff.ApplyError (informational)")
}
