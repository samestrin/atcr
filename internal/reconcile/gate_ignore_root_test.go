package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TD internal/reconcile/gate.go:259: RunReconcile loaded .atcr/ignore.yaml from
// opts.Root, the REVIEWED tree. Under `atcr review --config-dir` that tree is an
// untrusted pull request head, so the PR could ship an ignore rule and hide its
// own findings from the gate and the auto-fix. Options.IgnoreRoot names the
// directory the rules come from; Root stays the reviewed tree for path
// validation and grouping.
func runIgnoreRootReconcile(t *testing.T, root, ignoreRoot, reviewDir string) (Result, error) {
	t.Helper()
	withFakeTier4(t, &fakeTier4{})
	return RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
		IgnoreRoot:   ignoreRoot,
	})
}

const unsafeIgnoreRule = "rules:\n" +
	"  - file: internal/auth/session.go\n" +
	"    category: security\n" +
	"    match: unsafe\\.Pointer\n" +
	"    reason: this package needs unsafe for the mmap view\n"

// A rule the reviewed tree ships is not applied when IgnoreRoot names another
// directory: both findings stay on the primary stream.
func TestRunReconcile_IgnoreRootSkipsReviewedTreeRules(t *testing.T) {
	root, reviewDir := ignoreReview(t)
	writeIgnoreYAML(t, root, unsafeIgnoreRule)

	res, err := runIgnoreRootReconcile(t, root, t.TempDir(), reviewDir)
	require.NoError(t, err)
	assert.Empty(t, res.Suppressed, "the reviewed tree's ignore.yaml must not suppress anything")
	require.Len(t, res.Findings, 2)
	assert.Equal(t, 1, CountAtOrAbove(res.Findings, SevHigh, false),
		"the HIGH the reviewed tree tried to hide still reaches the gate")
}

// A rule in IgnoreRoot is applied, and its file glob still matches the finding's
// path relative to the reviewed tree.
func TestRunReconcile_IgnoreRootRulesApply(t *testing.T) {
	root, reviewDir := ignoreReview(t)
	ignoreRoot := t.TempDir()
	writeIgnoreYAML(t, ignoreRoot, unsafeIgnoreRule)

	res, err := runIgnoreRootReconcile(t, root, ignoreRoot, reviewDir)
	require.NoError(t, err)
	require.Len(t, res.Suppressed, 1)
	assert.Equal(t, "internal/auth/session.go", res.Suppressed[0].File)
	require.Len(t, res.Findings, 1)
	assert.Equal(t, "internal/store/db.go", res.Findings[0].File)
}

// The reviewed tree's ignore.yaml is never read under IgnoreRoot, so a file
// that does not parse cannot fail the run either.
func TestRunReconcile_IgnoreRootNeverReadsReviewedTreeFile(t *testing.T) {
	root, reviewDir := ignoreReview(t)
	writeIgnoreYAML(t, root, "rules: [not valid yaml\n")

	_, err := runIgnoreRootReconcile(t, root, t.TempDir(), reviewDir)
	require.NoError(t, err)
}

// An empty IgnoreRoot keeps the pre-existing behaviour: rules load from Root,
// which is what `atcr reconcile`, resume and the MCP server rely on.
func TestRunReconcile_EmptyIgnoreRootFallsBackToRoot(t *testing.T) {
	root, reviewDir := ignoreReview(t)
	writeIgnoreYAML(t, root, unsafeIgnoreRule)

	res, err := runIgnoreRootReconcile(t, root, "", reviewDir)
	require.NoError(t, err)
	require.Len(t, res.Suppressed, 1)
}
