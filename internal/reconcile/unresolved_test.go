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
	require.NoError(t, renderMarkdown(&withCount, Summary{UnresolvedFiltered: 3}, nil, DisagreementsFile{}, 0))
	assert.Contains(t, withCount.String(),
		"- Unresolved findings: 3 (no symbol correspondence in the tracked tree; routed to unresolved.json)")

	var withoutCount bytes.Buffer
	require.NoError(t, renderMarkdown(&withoutCount, Summary{}, nil, DisagreementsFile{}, 0))
	assert.NotContains(t, withoutCount.String(), "Unresolved findings:")
}

// TestRenderMarkdown_DocShieldedRoutingsAreNotClaimedAbsent closes the wording
// defect the doc-shield carve-out left behind.
//
// "no symbol correspondence in the tracked tree" is FALSE for a doc-shielded
// record: namedInDocs just proved the subject IS in the tree, in a file isDocExt
// classified as prose. Reporting the two shapes under one count tells an operator
// the finding named nothing real when it named something the extension heuristic
// declined to treat as a declaration — the opposite conclusion, and the one that
// gets a real finding discarded.
func TestRenderMarkdown_DocShieldedRoutingsAreNotClaimedAbsent(t *testing.T) {
	t.Run("all shielded: the absent-from-the-tree claim must not be made", func(t *testing.T) {
		var b bytes.Buffer
		require.NoError(t, renderMarkdown(&b, Summary{UnresolvedFiltered: 2}, nil, DisagreementsFile{}, 2))
		out := b.String()
		assert.Contains(t, out, "named only in documentation")
		assert.NotContains(t, out, "no symbol correspondence in the tracked tree; routed",
			"every routed finding here WAS named in the tree")
	})

	t.Run("mixed: both shapes are counted separately", func(t *testing.T) {
		var b bytes.Buffer
		require.NoError(t, renderMarkdown(&b, Summary{UnresolvedFiltered: 5}, nil, DisagreementsFile{}, 2))
		out := b.String()
		assert.Contains(t, out, "- Unresolved findings: 5")
		assert.Contains(t, out, "3 with no symbol correspondence in the tracked tree",
			"the split must name the count for each shape, not just the total")
		assert.Contains(t, out, "2 named only in documentation")
	})

	t.Run("none shielded: the line is unchanged", func(t *testing.T) {
		var b bytes.Buffer
		require.NoError(t, renderMarkdown(&b, Summary{UnresolvedFiltered: 3}, nil, DisagreementsFile{}, 0))
		assert.Contains(t, b.String(),
			"- Unresolved findings: 3 (no symbol correspondence in the tracked tree; routed to unresolved.json)")
	})
}

// TestRenderMarkdown_DerivesTheShieldedCountFromTheRecords covers the ONE step
// the test above cannot reach: countDocShielded itself.
//
// That test calls renderMarkdown directly and hands it the shielded count as an
// int literal, so the derivation is never exercised — replacing the reason
// comparison in countDocShielded with a predicate that matches every routed
// record leaves the whole package green. This test goes in through the exported
// RenderMarkdown, which derives the count from Result.Unresolved, so the wrong
// records produce the wrong sentence and the assertion fails.
//
// The set is deliberately MIXED. An all-shielded or none-shielded fixture cannot
// discriminate: both are reproduced by a filter that matches everything and by
// one that matches nothing respectively.
func TestRenderMarkdown_DerivesTheShieldedCountFromTheRecords(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, RenderMarkdown(&b, Result{
		Summary: Summary{UnresolvedFiltered: 3},
		Unresolved: []JSONFinding{
			// Named only in documentation — the carve-out's own shape.
			{Severity: "HIGH", File: "docs/guide.md", Line: 1, Problem: "`DocOnlyThing` leaks",
				UnresolvedReason: UnresolvedReasonDocShield},
			// No symbol correspondence anywhere: no reason stamped.
			{Severity: "HIGH", File: "internal/ghost/a.go", Line: 2, Problem: "`PhantomOne` leaks"},
			{Severity: "LOW", File: "internal/ghost/b.go", Line: 3, Problem: "`PhantomTwo` leaks"},
		},
	}))

	assert.Contains(t, b.String(),
		"- Unresolved findings: 3 (2 with no symbol correspondence in the tracked tree, 1 named only in documentation; routed to unresolved.json)",
		"the split must be derived from each record's UnresolvedReason, not from the total")
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

// TestReadUnresolvedFindings_ErrorContract covers the three branches of the
// sidecar recovery reader that TestReadUnresolvedFindings_MissingIsEmpty leaves
// untouched: a read error that is NOT os.IsNotExist, a present-but-empty file,
// and an unparseable body. Only the missing-file case had a test, so the error
// contract of the exported reader was entirely unverified — and the three
// outcomes are deliberately different (propagate, empty, wrapped parse error).
func TestReadUnresolvedFindings_ErrorContract(t *testing.T) {
	write := func(t *testing.T, body string, mode os.FileMode) string {
		t.Helper()
		dir := t.TempDir()
		recon := filepath.Join(dir, reconciledSubdir)
		require.NoError(t, os.MkdirAll(recon, 0o755))
		path := filepath.Join(recon, UnresolvedJSON)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		if mode != 0o644 {
			require.NoError(t, os.Chmod(path, mode))
			t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
		}
		return dir
	}

	t.Run("unreadable file propagates the error", func(t *testing.T) {
		dir := write(t, `[{"file":"a.go"}]`, 0o000)
		if _, err := os.ReadFile(filepath.Join(dir, reconciledSubdir, UnresolvedJSON)); err == nil {
			t.Skip("filesystem or privileges ignore mode 0000")
		}
		got, err := ReadUnresolvedFindings(dir)
		require.Error(t, err, "a sidecar that exists but cannot be read is NOT an empty sidecar")
		assert.False(t, os.IsNotExist(err), "the not-exist branch must not swallow this")
		assert.Nil(t, got)
	})

	t.Run("zero-byte file is empty, not an error", func(t *testing.T) {
		got, err := ReadUnresolvedFindings(write(t, "", 0o644))
		assert.NoError(t, err, "a run that routed nothing is legitimately empty")
		assert.Empty(t, got)
	})

	t.Run("whitespace-only file is empty, not an error", func(t *testing.T) {
		got, err := ReadUnresolvedFindings(write(t, "  \n\t\n", 0o644))
		assert.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("malformed JSON is a wrapped parse error", func(t *testing.T) {
		got, err := ReadUnresolvedFindings(write(t, `[{"file": `, 0o644))
		require.Error(t, err, "a present-but-unparseable sidecar must never read as empty")
		assert.Contains(t, err.Error(), UnresolvedJSON, "the error names the file it failed on")
		assert.Nil(t, got)
	})

	t.Run("well-formed body round-trips", func(t *testing.T) {
		got, err := ReadUnresolvedFindings(write(t, `[{"file":"internal/ghost/phantom.go","line":9,"severity":"HIGH"}]`, 0o644))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "internal/ghost/phantom.go", got[0].File)
	})
}
