package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/payload"
)

// Sprint 35.13 T6 (AC8): the MCP atcr_reconcile handler persists reconciled
// findings to the .atcr/debt store through the SAME localdebt.PersistForReconcile
// bridge the CLI calls, closing TD-002 — findings produced over MCP used to reach
// the scorecard and then silently vanish before the backlog.
//
// The store root is resolved repo > manifest, never CWD: the MCP server's CWD is
// whatever launched it, and e.root is hardcoded to "." in serve mode, so both are
// unusable by construction.

// recordRootInManifest stamps a review fixture's manifest with a repo root, the way
// the review path does at review time.
func recordRootInManifest(t *testing.T, root, id, recorded string) {
	t.Helper()
	path := filepath.Join(root, ".atcr", "reviews", id, "manifest.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(data, &m))
	m.Root = recorded
	out, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, out, 0o644))
}

// chdir moves the process into dir for the test. Every case here needs a CWD that
// is NOT the store root — that divergence is the defect being fixed, and a test run
// from the root would pass against the CWD-relative code this replaces.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// debtRecords reads every record in a root's local debt store, returning nil when
// the store does not exist.
func debtRecords(t *testing.T, root string) []localdebt.Record {
	t.Helper()
	recs, err := localdebt.ReadAll(localdebt.DefaultDir(root), localdebt.ReadOpts{})
	require.NoError(t, err)
	return recs
}

// TestReconcileHandler_PersistsToManifestRootWithCWDElsewhere is AC8's headline: an
// MCP-driven reconcile lands findings in <root>/.atcr/debt/ while the process CWD is
// a different directory entirely.
func TestReconcileHandler_PersistsToManifestRootWithCWDElsewhere(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	id := reviewFixture(t, root)
	recordRootInManifest(t, root, id, root)
	// Finding-path validation now runs against the resolved root (TD-019), so the
	// fixture's auth.go must exist there or the finding is path-warned and dropped.
	require.NoError(t, os.WriteFile(filepath.Join(root, "auth.go"), []byte("package auth\n"), 0o644))

	cwd := t.TempDir()
	chdir(t, cwd)

	var diag bytes.Buffer
	e := &engine{root: root, diag: &diag}
	_, out, err := e.handleReconcile(context.Background(), nil, ReconcileArgs{})
	require.NoError(t, err)

	recs := debtRecords(t, root)
	require.Len(t, recs, 1, "the MCP reconcile must land its finding in the manifest root's store (diag: %s)", diag.String())
	assert.Equal(t, "auth.go", recs[0].File)
	assert.Equal(t, localdebt.SchemaVersion, recs[0].SchemaVersion,
		"the MCP path writes current-schema records, like the CLI path")

	_, err = os.Stat(filepath.Join(cwd, ".atcr"))
	assert.True(t, os.IsNotExist(err), "nothing may be written under the server's CWD")

	// The result must be untouched by a side effect that is best-effort by contract.
	assert.True(t, out.Pass)
	assert.Equal(t, 1, out.TotalFindings)
}

// TestReconcileHandler_ExplicitRepoOverridesManifestRoot covers AC8(a)'s argument
// doing real work: repo outranks a valid recorded root.
func TestReconcileHandler_ExplicitRepoOverridesManifestRoot(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	id := reviewFixture(t, root)
	recordRootInManifest(t, root, id, root)

	explicit := t.TempDir()
	// The MCP explicit tier requires a repo-root marker (RequireMarker, TD
	// internal/localdebt/root.go:49) — a model-supplied path to a bare directory
	// no longer persists. A legitimate repo carries one.
	require.NoError(t, os.Mkdir(filepath.Join(explicit, ".git"), 0o755))
	// Finding-path validation runs against the explicit root too (TD-019).
	require.NoError(t, os.WriteFile(filepath.Join(explicit, "auth.go"), []byte("package auth\n"), 0o644))
	chdir(t, t.TempDir())

	e := &engine{root: root, diag: &bytes.Buffer{}}
	_, _, err := e.handleReconcile(context.Background(), nil, ReconcileArgs{Repo: explicit})
	require.NoError(t, err)

	assert.Len(t, debtRecords(t, explicit), 1, "the explicit repo argument must win")
	assert.Empty(t, debtRecords(t, root), "the manifest root must not also be written")
}

// TestReconcileHandler_NoRootDoesNotPersist covers the no-persist-with-warning path
// for an unresolvable root. The store-directory absence is the real assertion: a
// resolver that warned and then fell through to CWD would satisfy a warning-only
// test while writing to an unrelated directory.
func TestReconcileHandler_NoRootDoesNotPersist(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	reviewFixture(t, root) // fixture manifest records no root

	cwd := t.TempDir()
	chdir(t, cwd)

	var diag bytes.Buffer
	e := &engine{root: root, diag: &diag}
	_, out, err := e.handleReconcile(context.Background(), nil, ReconcileArgs{})
	require.NoError(t, err, "an unresolvable root must never fail the reconcile")

	assert.Empty(t, debtRecords(t, root))
	_, statErr := os.Stat(filepath.Join(cwd, ".atcr"))
	assert.True(t, os.IsNotExist(statErr), "no store may be created under the CWD as a fallback")
	assert.Contains(t, diag.String(), "no repo root recorded in the review manifest",
		"the skip must be visible on the engine's diagnostics sink")
	assert.True(t, out.Pass, "the tool result is unchanged by a skipped persistence")
}

// TestReconcileHandler_StaleManifestRootDoesNotPersist is the copied-artifacts case:
// the tree carries an absolute root from another machine that no longer resolves.
// The required failure mode is no-persist-with-warning, never a write elsewhere.
func TestReconcileHandler_StaleManifestRootDoesNotPersist(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	id := reviewFixture(t, root)
	gone := filepath.Join(t.TempDir(), "repo-on-another-machine")
	recordRootInManifest(t, root, id, gone)

	cwd := t.TempDir()
	chdir(t, cwd)

	var diag bytes.Buffer
	e := &engine{root: root, diag: &diag}
	_, out, err := e.handleReconcile(context.Background(), nil, ReconcileArgs{})
	require.NoError(t, err)

	assert.Empty(t, debtRecords(t, root))
	assert.Empty(t, debtRecords(t, gone))
	_, statErr := os.Stat(filepath.Join(cwd, ".atcr"))
	assert.True(t, os.IsNotExist(statErr))
	assert.Contains(t, diag.String(), "no longer a valid repository root")
	assert.True(t, out.Pass)
}

// TestReconcileHandler_InvalidExplicitRepoDoesNotPersist pins the explicit tier's
// no-fall-through: an operator who named a root and got it wrong must not have
// findings quietly written somewhere else, including the otherwise-valid manifest
// root sitting right there.
func TestReconcileHandler_InvalidExplicitRepoDoesNotPersist(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	id := reviewFixture(t, root)
	recordRootInManifest(t, root, id, root)
	chdir(t, t.TempDir())

	var diag bytes.Buffer
	e := &engine{root: root, diag: &diag}
	_, _, err := e.handleReconcile(context.Background(), nil,
		ReconcileArgs{Repo: filepath.Join(t.TempDir(), "nope")})
	require.NoError(t, err)

	assert.Empty(t, debtRecords(t, root), "a bad explicit root must not fall through to the manifest root")
	assert.Contains(t, diag.String(), "does not exist or is not a directory")
}

// TestReconcileInputSchema_ExposesRepo covers AC8(a)'s schema half: the argument is
// inferred from the struct, so a missing jsonschema tag or a mistyped json tag would
// leave the parameter invisible to every MCP client while the Go field compiles fine.
func TestReconcileInputSchema_ExposesRepo(t *testing.T) {
	schema, err := reconcileInputSchema()
	require.NoError(t, err)
	require.NotNil(t, schema)
	require.Contains(t, schema.Properties, "repo",
		"the atcr_reconcile schema must expose the repo argument")
	assert.NotEmpty(t, schema.Properties["repo"].Description,
		"repo must be self-describing: a client cannot guess that it selects the debt store root")
}

// TestReconcileHandler_DropsFindingsMissingUnderResolvedRoot pins the TD-019
// behavior change: with finding-path validation now running against the
// resolved store root (previously a no-op at Root: ""), a finding whose path
// does NOT exist in the reviewed repo is PathWarning-stamped and the bridge
// drops it — an MCP store can no longer accumulate the hallucinated paths a
// CLI run would have rejected. The fixture's auth.go is deliberately absent.
func TestReconcileHandler_DropsFindingsMissingUnderResolvedRoot(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	id := reviewFixture(t, root)
	recordRootInManifest(t, root, id, root)
	chdir(t, t.TempDir())

	var diag bytes.Buffer
	e := &engine{root: root, diag: &diag}
	_, out, err := e.handleReconcile(context.Background(), nil, ReconcileArgs{})
	require.NoError(t, err)

	assert.Empty(t, debtRecords(t, root),
		"a finding missing under the resolved root must be dropped, not persisted (diag: %s)", diag.String())
	assert.True(t, out.Pass, "the drop is a persistence-scope change, not a reconcile failure")
}

// TestReconcileHandler_MarkerlessExplicitRepoDoesNotPersist pins the MCP-side
// close of TD internal/localdebt/root.go:49: in.Repo is model-supplied, so an
// existing-but-unmarked directory must NOT become <dir>/.atcr/debt/ from a tool
// argument — no persist, with a warning, and no fall-through to the otherwise
// valid manifest root.
func TestReconcileHandler_MarkerlessExplicitRepoDoesNotPersist(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	id := reviewFixture(t, root)
	recordRootInManifest(t, root, id, root)
	chdir(t, t.TempDir())

	bare := t.TempDir() // exists, but carries no .git/.atcr marker

	var diag bytes.Buffer
	e := &engine{root: root, diag: &diag}
	_, _, err := e.handleReconcile(context.Background(), nil, ReconcileArgs{Repo: bare})
	require.NoError(t, err, "a rejected root must never fail the reconcile")

	assert.Empty(t, debtRecords(t, bare), "a markerless model-supplied root must not gain a store")
	assert.Empty(t, debtRecords(t, root), "a rejected explicit root must not fall through to the manifest root")
	assert.Contains(t, diag.String(), "no repository marker")
}
