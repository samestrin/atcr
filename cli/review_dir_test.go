package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC 02-04 Happy Path 1 + AC 02-02 end-to-end: `atcr review --dir internal` scopes
// the baseline scan to the subtree (payload contains internal/c.go only, not the
// repo-root b.go/a.txt), records the scope + empty range in the manifest, and writes
// the standard review tree (payload/sources/reconciled) — reconcile needs no
// --dir-specific branching (it consumes the range-less, provenance-agnostic output).
func TestReviewDir_ScopesToSubtreeAndReconciles(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t) // commits a.txt, b.go, internal/c.go
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code := execCmd(t, "review", "--dir", "internal")
	require.Equal(t, 0, code, "a completed scoped baseline review exits 0")

	dir := latestReviewDir(t)
	for _, sub := range []string{"payload", "sources", "reconciled"} {
		assert.DirExists(t, filepath.Join(dir, sub))
	}

	// Scoped payload: only the in-scope file's content is present.
	body, err := os.ReadFile(filepath.Join(dir, "payload", "files.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "package c", "internal/c.go must be in the scoped payload")
	assert.NotContains(t, string(body), "package b", "repo-root b.go must be OUT of the internal scope")
	assert.NotContains(t, string(body), "one", "repo-root a.txt must be OUT of the internal scope")

	// Manifest records the --dir scope and no git range (range resolution skipped).
	mdata, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(mdata, &m))
	assert.Equal(t, "files", m.PayloadMode)
	assert.Equal(t, "internal", m.Dir, "manifest must record the --dir scope for resume parity")
	assert.Empty(t, m.Base, "a scoped baseline scan has no base")
	assert.Empty(t, m.Head, "a scoped baseline scan has no head")

	// Reconcile (the step that populates reconciled/) works unmodified on the
	// scoped output and produces the standard report.md + findings.json layout.
	latest, err := fanout.ReadLatest(".")
	require.NoError(t, err)
	require.Equal(t, 0, execCmd(t, "reconcile", latest), "reconcile works on --dir output")
	assert.FileExists(t, filepath.Join(dir, "reconciled", "findings.json"))
	assert.FileExists(t, filepath.Join(dir, "reconciled", "report.md"))
}

// AC 02-04 Happy Path 2-3: `atcr reconcile <id>` and `atcr report <id>` work
// unmodified against a --dir-produced review directory (provenance-agnostic — no
// scope-aware code changes needed downstream).
func TestReviewDir_ReconcileAndReportWorkOnScopedOutput(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--dir", "internal"))
	latest, err := fanout.ReadLatest(".")
	require.NoError(t, err)

	assert.Equal(t, 0, execCmd(t, "reconcile", latest), "reconcile works on --dir output")
	assert.Equal(t, 0, execCmd(t, "report", latest), "report works on --dir output")
}

// AC 02-04 Edge Case 1: a --dir scan has no base/head changed lines to ground
// against, so grounding is disabled with the same "range-less request" reason a
// range-less diff-ingestion run uses — recorded in sources/pool/summary.json.
func TestReviewDir_GroundingDisabledRangeLess(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 0, execCmd(t, "review", "--dir", "internal"))
	dir := latestReviewDir(t)

	sdata, err := os.ReadFile(filepath.Join(dir, "sources", "pool", "summary.json"))
	require.NoError(t, err)
	var summary struct {
		GroundingEnabled        bool   `json:"grounding_enabled"`
		GroundingDisabledReason string `json:"grounding_disabled_reason"`
	}
	require.NoError(t, json.Unmarshal(sdata, &summary))
	assert.False(t, summary.GroundingEnabled, "a range-less --dir scan disables grounding")
	assert.Contains(t, summary.GroundingDisabledReason, "range-less request",
		"the disabled reason must be the shared range-less reason")
}
