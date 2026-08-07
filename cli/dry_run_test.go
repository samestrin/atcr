package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --dry-run is the canonical "show what would happen, do not do it" flag on
// review and reconcile (the same concept debt compact and personas upgrade
// already express as --dry-run); --preview remains a deprecated hidden alias
// with identical behavior.
func TestDryRun_RegisteredOnReviewAndReconcile(t *testing.T) {
	_, reviewHelp := execCmdCapture(t, "review", "--help")
	require.Contains(t, reviewHelp, "--dry-run", "review help must name the canonical --dry-run flag")

	_, reconcileHelp := execCmdCapture(t, "reconcile", "--help")
	require.Contains(t, reconcileHelp, "--dry-run", "reconcile help must name the canonical --dry-run flag")
}

// --dry-run and the deprecated --preview alias render the identical payload
// preview: same short-circuit, same bytes.
func TestDryRun_AliasEquivalence(t *testing.T) {
	isolate(t)
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")

	dryRunOut, err := runPreview(t, newReviewCmd(), nil, "--dry-run")
	require.NoError(t, err)
	previewOut, err := runPreview(t, newReviewCmd(), nil, "--preview")
	require.NoError(t, err)
	assert.Equal(t, previewOut, dryRunOut, "--dry-run must render byte-identically to --preview")

	dryRunOut, err = runPreview(t, newReconcileCmd(), nil, "--dry-run")
	require.NoError(t, err)
	previewOut, err = runPreview(t, newReconcileCmd(), nil, "--preview")
	require.NoError(t, err)
	assert.Equal(t, previewOut, dryRunOut, "reconcile: --dry-run must render byte-identically to --preview")
}

// --dry-run suppresses the --sync-cloud refusal exactly as --preview does: the
// preview short-circuit pushes nothing, so the sync-cloud PreRunE refusal must
// not fire.
func TestDryRun_TakesPrecedenceOverSyncCloud(t *testing.T) {
	isolate(t)

	_, _, code, _ := runRootPreview(t, "review", "--dry-run", "--sync-cloud")
	require.Equal(t, 0, code, "--dry-run must take precedence over --sync-cloud through the real Execute() path")
}
