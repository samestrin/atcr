package reconcile

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runReconcileWithFakeTier4 wires a scripted Tier 4 resolver into a real
// RunReconcile against a real git repo, so sidecar routing can be exercised
// without depending on what the wasm parsers happen to extract.
func runReconcileWithFakeTier4(t *testing.T, root, reviewDir string, fake *fakeTier4) Result {
	t.Helper()
	withFakeTier4(t, fake)
	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	return res
}

// TestRunReconcile_RoutesUnresolvedToSidecar is AC3 + AC4: a finding that
// exhausts all four tiers leaves the primary stream entirely — absent from
// findings.json (which is exactly what internal/verify reads) and from
// report.md — while being preserved on disk in the new sidecar.
func TestRunReconcile_RoutesUnresolvedToSidecar(t *testing.T) {
	root := gitRepoWithFiles(t, "internal/auth/session.go")
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/ghost/phantom.go:9|`quantumFlux` leaks a handle|close it|security|10|ev|greta\n"+
			"LOW|internal/auth/session.go:3|`RefreshToken` is slow|cache it|perf|5|ev|greta\n")

	res := runReconcileWithFakeTier4(t, root, reviewDir, &fakeTier4{})

	require.Len(t, res.Unresolved, 1)
	assert.Equal(t, "internal/ghost/phantom.go", res.Unresolved[0].File)
	assert.Equal(t, 1, res.Summary.UnresolvedFiltered)

	js, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, js, 1, "the phantom never reaches findings.json, so internal/verify never sees it")
	assert.Equal(t, "internal/auth/session.go", js[0].File)

	assert.Equal(t, len(js), res.Summary.TotalFindings,
		"TotalFindings must keep matching the findings.json record count")

	md, err := os.ReadFile(filepath.Join(reviewDir, "reconciled", ReportMD))
	require.NoError(t, err)
	assert.NotContains(t, string(md), "internal/ghost/phantom.go")
	assert.Contains(t, string(md), "Unresolved findings: 1")

	sidecar, err := ReadUnresolvedFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, sidecar, 1, "AC4: never hard-deleted — preserved on disk")
	assert.Equal(t, "internal/ghost/phantom.go", sidecar[0].File)
	assert.Equal(t, "HIGH", sidecar[0].Severity)
}

// TestRunReconcile_UnresolvedKeepsFindingsInLockstep guards the strict 1:1
// index correspondence between Result.Findings and Result.JSONFindings() that
// internal/mcp's failingFindings walks. Filtering one without the other would
// mis-pair every record after the drop, or index out of range.
func TestRunReconcile_UnresolvedKeepsFindingsInLockstep(t *testing.T) {
	root := gitRepoWithFiles(t, "internal/auth/session.go")
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"CRITICAL|internal/ghost/phantom.go:9|`quantumFlux` leaks a handle|close it|security|10|ev|greta\n"+
			"HIGH|internal/auth/session.go:3|`RefreshToken` is slow|cache it|perf|5|ev|greta\n")

	res := runReconcileWithFakeTier4(t, root, reviewDir, &fakeTier4{})

	jsons := res.JSONFindings()
	require.Len(t, res.Findings, len(jsons))
	for i := range res.Findings {
		assert.Equal(t, res.Findings[i].File, jsons[i].File, "index %d drifted", i)
		assert.Equal(t, res.Findings[i].Severity, jsons[i].Severity, "index %d drifted", i)
	}
	assert.Equal(t, 0, CountAtOrAbove(res.Findings, SevCritical, false),
		"a sidecar-routed CRITICAL must not fail the gate on a phantom")
}

// TestRunReconcile_NoUnresolvedLeavesEverythingIntact pins that the common path
// is unchanged: nothing routed, no summary line, an empty sidecar.
func TestRunReconcile_NoUnresolvedLeavesEverythingIntact(t *testing.T) {
	root := gitRepoWithFiles(t, "internal/auth/session.go")
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"LOW|internal/auth/session.go:3|`RefreshToken` is slow|cache it|perf|5|ev|greta\n")

	res := runReconcileWithFakeTier4(t, root, reviewDir, &fakeTier4{})

	assert.Empty(t, res.Unresolved)
	assert.Zero(t, res.Summary.UnresolvedFiltered)

	md, err := os.ReadFile(filepath.Join(reviewDir, "reconciled", ReportMD))
	require.NoError(t, err)
	assert.NotContains(t, string(md), "Unresolved findings:",
		"the line renders only when nonzero, keeping report.md byte-identical on the common path")

	sidecar, err := ReadUnresolvedFindings(reviewDir)
	require.NoError(t, err)
	assert.Empty(t, sidecar)

	raw, err := os.ReadFile(filepath.Join(reviewDir, "reconciled", UnresolvedJSON))
	require.NoError(t, err)
	assert.Equal(t, "[]\n", string(raw),
		"an empty sidecar is [] like ambiguous.json, never null")
}

// TestEmit_WritesUnresolvedSidecar pins the artifact itself: always written
// (like ambiguous.json), holding the full JSONFinding records so nothing about
// a routed finding is lost.
func TestEmit_WritesUnresolvedSidecar(t *testing.T) {
	dir := t.TempDir()
	res := Result{
		Findings: []Merged{},
		Unresolved: []JSONFinding{{
			Severity: "HIGH", File: "internal/ghost/phantom.go", Line: 9,
			Problem: "`quantumFlux` leaks a handle", Fix: "close it",
			Category: "security", EstMinutes: 10, Evidence: "ev",
			Reviewers: []string{"greta"}, Confidence: ConfLow,
			PathWarning: "file not found",
		}},
	}
	require.NoError(t, Emit(dir, res))

	data, err := os.ReadFile(filepath.Join(dir, UnresolvedJSON))
	require.NoError(t, err)

	var got []JSONFinding
	require.NoError(t, json.Unmarshal(data, &got))
	require.Len(t, got, 1)
	assert.Equal(t, "internal/ghost/phantom.go", got[0].File)
	assert.Equal(t, "`quantumFlux` leaks a handle", got[0].Problem)
	assert.Equal(t, "file not found", got[0].PathWarning)
	assert.True(t, bytes.HasSuffix(data, []byte("\n")), "artifacts end with a newline")
}

// TestReadUnresolvedFindings_MissingIsEmpty pins reader tolerance: a reconciled
// dir written before this epic has no sidecar, which is an empty set, not an
// error — matching ReadAmbiguousClusters.
func TestReadUnresolvedFindings_MissingIsEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := ReadUnresolvedFindings(dir)
	assert.NoError(t, err)
	assert.Empty(t, got)
}

// TestRenderMarkdown_UnresolvedLine pins the summary line's wording and its
// render-only-when-nonzero guard, mirroring the "Consensus filtered: N" line.
func TestRenderMarkdown_UnresolvedLine(t *testing.T) {
	var withCount bytes.Buffer
	require.NoError(t, renderMarkdown(&withCount, Summary{UnresolvedFiltered: 3}, nil, DisagreementsFile{}))
	assert.Contains(t, withCount.String(),
		"- Unresolved findings: 3 (no symbol correspondence in the tracked tree; routed to unresolved.json)")

	var withoutCount bytes.Buffer
	require.NoError(t, renderMarkdown(&withoutCount, Summary{}, nil, DisagreementsFile{}))
	assert.NotContains(t, withoutCount.String(), "Unresolved findings:")
}

// TestRunReconcile_UnresolvedRecountsOutOfScope covers the post-routing
// out-of-scope recount in gate.go and the countOutOfScope helper it calls.
//
// Summary.OutOfScope is computed by the library over the PRE-routing merged
// slice. When routing then drops records, that count describes a set that no
// longer exists — and no test noticed, because every routing test so far routed
// a finding that was not itself out-of-scope, leaving the pre- and post-routing
// counts identical. Here the ROUTED finding is out-of-scope, so the count must
// actually drop: 2 before, 1 after.
//
// Mutating the recount to `_ = countOutOfScope` must fail here.
func TestRunReconcile_UnresolvedRecountsOutOfScope(t *testing.T) {
	root := gitRepoWithFiles(t, "internal/auth/session.go")
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		// Routed: a phantom path whose anchor resolves nowhere, AND out-of-scope.
		"HIGH|internal/ghost/phantom.go:9|`quantumFlux` predates this change|ignore it|out-of-scope|10|ev|greta\n"+
			// Survives: a real tracked path, also out-of-scope.
			"LOW|internal/auth/session.go:3|`RefreshToken` predates this change|ignore it|out-of-scope|5|ev|greta\n")

	res := runReconcileWithFakeTier4(t, root, reviewDir, &fakeTier4{})

	require.Len(t, res.Unresolved, 1, "the phantom is routed")
	require.Len(t, res.Findings, 1, "the real out-of-scope finding survives")

	assert.Equal(t, 1, res.Summary.OutOfScope,
		"out_of_scope must describe the POST-routing set; without the recount it reports the pre-routing 2")
	assert.Equal(t, 1, res.Summary.TotalFindings)

	js, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	assert.Len(t, js, res.Summary.OutOfScope,
		"every surviving finding here is out-of-scope, so the count and findings.json must agree")
}
