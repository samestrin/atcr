package reconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitRepoWithFiles initializes a throwaway git repo, commits the given tracked
// relpaths, and returns the root. The candidate index reads `git ls-files`, so
// suggestions only work against tracked files.
func gitRepoWithFiles(t *testing.T, relpaths ...string) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	run("config", "user.email", "t@t.t")
	run("config", "user.name", "t")
	for _, rel := range relpaths {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte("package x\n"), 0o644))
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return root
}

// TestRunReconcile_SuggestsHallucinatedPathEndToEnd is the Epic 5.4 AC8 end-to-
// end acceptance test: a review citing a typo'd path (validator.go, where the
// real tracked file is validate.go in the same directory) flows through the full
// reconcile pipeline against a real git repo, and the correction surfaces as a
// PathSuggestion in the merged result, in findings.json (path_suggestion), and
// as a "(did you mean …)" clause in report.md — while the original hallucinated
// path is preserved (suggest-only, AC7).
func TestRunReconcile_SuggestsHallucinatedPathEndToEnd(t *testing.T) {
	root := gitRepoWithFiles(t, "internal/auth/validate.go")

	reviewDir := t.TempDir()
	sources := filepath.Join(reviewDir, "sources")
	writeFindings(t, sources, "greta/findings.txt",
		"HIGH|internal/auth/validator.go:12|hallucinated path finding|fix|security|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)

	// Path-validation fields ride on the JSONFinding records (Epic 8.0 Q1), read
	// from res.JSONFindings() (the cached, path-stamped records), not res.Findings.
	hall := res.JSONFindings()[0]
	assert.Equal(t, "internal/auth/validator.go", hall.File, "original cited path preserved (AC7)")
	assert.False(t, hall.PathValid)
	assert.Equal(t, stream.PathNotFoundWarning, hall.PathWarning)
	assert.Equal(t, "internal/auth/validate.go", hall.PathSuggestion)

	// findings.json carries the suggestion (report command's input).
	js, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, js, 1)
	assert.Equal(t, "internal/auth/validate.go", js[0].PathSuggestion)
	assert.Equal(t, "internal/auth/validator.go", js[0].File)

	// report.md shows the "(did you mean …)" correction.
	reportMD, err := os.ReadFile(filepath.Join(reviewDir, "reconciled", ReportMD))
	require.NoError(t, err)
	md := string(reportMD)
	assert.Contains(t, md, "⚠️ File not found: internal/auth/validator.go")
	assert.Contains(t, md, "did you mean")
	assert.Contains(t, md, "internal/auth/validate.go")
}

// gitRepoWithSources initializes a throwaway git repo and commits each
// relpath -> source-text pair, so an end-to-end test can exercise the real wasm
// parsers against real code rather than the "package x" placeholder
// gitRepoWithFiles writes.
func gitRepoWithSources(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	run("config", "user.email", "t@t.t")
	run("config", "user.name", "t")
	for rel, src := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(src), 0o644))
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return root
}

// TestRunReconcile_Tier4ResolvesDissimilarFilenameEndToEnd is Epic 35.16.6.5's
// AC1 acceptance test, and the case Epic 5.4 explicitly could not reach: a real
// issue attributed to a file with a DISSIMILAR name, where every filename-level
// tier misses because there is no filename-level signal to match on.
//
// Nothing is stubbed — a real git repo, the real `git ls-files` candidate index,
// and the real embedded go.wasm parser build the symbol index. The finding's own
// prose is the only input to the anchor extractor.
func TestRunReconcile_Tier4ResolvesDissimilarFilenameEndToEnd(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/auth/session.go": "package auth\n\n" +
			"func refreshBearerToken(raw string) (string, error) {\n\treturn raw, nil\n}\n",
		"internal/net/pool.go": "package net\n\nfunc DialPeer(addr string) error {\n\treturn nil\n}\n",
	})

	reviewDir := t.TempDir()
	// "internal/tokens/renewal.go" shares no basename with any tracked file
	// (Tier 1 misses), its directory tracks nothing (Tier 2 misses), and it is not
	// a case variant of anything (Tier 3 misses). Only the symbol name resolves it.
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/tokens/renewal.go:31|`refreshBearerToken` reissues without checking expiry|compare the issued-at claim first|security|20|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1, "a Tier 4 resolution keeps the finding in the primary stream")

	got := res.JSONFindings()[0]
	assert.Equal(t, "internal/tokens/renewal.go", got.File,
		"suggest-only: 5.4 AC7 still holds, File is never rewritten")
	assert.False(t, got.PathValid)
	assert.Equal(t, stream.PathNotFoundWarning, got.PathWarning)
	assert.Equal(t, "internal/auth/session.go", got.PathSuggestion,
		"Tier 4 located the finding by the symbol its prose names")

	js, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, js, 1)
	assert.Equal(t, "internal/auth/session.go", js[0].PathSuggestion)

	unresolved, err := ReadUnresolvedFindings(reviewDir)
	require.NoError(t, err)
	assert.Empty(t, unresolved, "a resolved finding is never sidecar-routed")
	assert.Zero(t, res.Summary.UnresolvedFiltered)
}

// TestRunReconcile_Tier4RoutesFabricatedFindingEndToEnd is AC3 + AC4: a finding
// that corresponds to nothing at all — a file that does not exist naming a
// symbol declared nowhere in the tracked tree — leaves report.md and
// findings.json (the subset internal/verify's skeptic pipeline consumes) while
// being preserved in the sidecar and counted in the summary.
func TestRunReconcile_Tier4RoutesFabricatedFindingEndToEnd(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/auth/session.go": "package auth\n\nfunc refreshBearerToken() error {\n\treturn nil\n}\n",
	})

	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"CRITICAL|internal/quantum/flux.go:14|`quantumFluxCapacitor` leaks a handle on every retry|close it in `quantumFluxCapacitor`|security|30|ev|greta\n"+
			"LOW|internal/auth/session.go:3|`refreshBearerToken` could cache|cache it|perf|5|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)

	require.Len(t, res.Findings, 1, "the fabricated finding left the primary stream")
	assert.Equal(t, "internal/auth/session.go", res.Findings[0].File)
	assert.Equal(t, 1, res.Summary.UnresolvedFiltered)
	assert.Equal(t, 1, res.Summary.TotalFindings)

	// AC3: absent from the findings.json internal/verify reads, with no change to
	// internal/verify itself — exclusion happened upstream at emit time.
	js, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, js, 1)
	assert.Equal(t, "internal/auth/session.go", js[0].File)

	// AC3: absent from report.md's primary findings section.
	md, err := os.ReadFile(filepath.Join(reviewDir, "reconciled", ReportMD))
	require.NoError(t, err)
	assert.NotContains(t, string(md), "internal/quantum/flux.go")
	assert.Contains(t, string(md), "Unresolved findings: 1")

	// AC4: preserved on disk, never hard-deleted, with its full record intact.
	unresolved, err := ReadUnresolvedFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, unresolved, 1)
	assert.Equal(t, "internal/quantum/flux.go", unresolved[0].File)
	assert.Equal(t, 14, unresolved[0].Line)
	assert.Equal(t, "CRITICAL", unresolved[0].Severity)
	assert.Contains(t, unresolved[0].Problem, "quantumFluxCapacitor")
	assert.Equal(t, stream.PathNotFoundWarning, unresolved[0].PathWarning)
}

// TestRunReconcile_Tier4DisabledEndToEnd is AC6 against the real pipeline: with
// the AST opt-out set, the same fabricated finding stays in the primary report
// with 5.4's plain file-not-found warning and nothing is routed anywhere.
func TestRunReconcile_Tier4DisabledEndToEnd(t *testing.T) {
	t.Setenv(astGroupingDisabledEnv, "1")

	root := gitRepoWithSources(t, map[string]string{
		"internal/auth/session.go": "package auth\n\nfunc refreshBearerToken() error {\n\treturn nil\n}\n",
	})

	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"CRITICAL|internal/quantum/flux.go:14|`quantumFluxCapacitor` leaks a handle|close it|security|30|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)

	require.Len(t, res.Findings, 1, "AC6 degrades to Tier 1-3 behavior, it does not error")
	got := res.JSONFindings()[0]
	assert.Equal(t, stream.PathNotFoundWarning, got.PathWarning)
	assert.Empty(t, got.PathSuggestion)
	assert.Zero(t, res.Summary.UnresolvedFiltered)

	unresolved, err := ReadUnresolvedFindings(reviewDir)
	require.NoError(t, err)
	assert.Empty(t, unresolved)
}

// TestRunReconcile_DocShieldReasonEndToEnd joins the two halves the unit tests
// pin separately: the real lazySymbolIndex decides the routing, and the reason it
// stamps has to be computed from the SAME anchors that decided it.
//
// Both halves are green in isolation with a scripted resolver on one side and a
// direct namedInDocs call on the other, so a mismatch between the anchor set
// resolve judged and the set namedInDocs was asked about would never show up.
// This drives one finding through RunReconcile against a real git repo whose only
// mention of the subject is a CHANGELOG line, and reads the reason off the
// sidecar record.
func TestRunReconcile_DocShieldReasonEndToEnd(t *testing.T) {
	root := gitRepoWithFiles(t, "internal/auth/validate.go", "CHANGELOG.md")
	// gitRepoWithFiles writes placeholder content; the index reads from disk, so
	// overwrite with the content this test is actually about. Tracked-ness comes
	// from git ls-files and is unaffected.
	require.NoError(t, os.WriteFile(filepath.Join(root, "CHANGELOG.md"),
		[]byte("## 2.0.0\n\n- Removed `quantumFlux`, the retry handle helper.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "auth", "validate.go"),
		[]byte("package auth\n\nfunc Validate() error { return nil }\n"), 0o644))

	reviewDir := t.TempDir()
	sources := filepath.Join(reviewDir, "sources")
	writeFindings(t, sources, "greta/findings.txt",
		"HIGH|internal/tokens/renewal.go:12|`quantumFlux` never checks the expiry|compare the issued-at claim first|security|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)

	require.Len(t, res.Unresolved, 1, "a subject declared nowhere in source is routed")
	assert.Equal(t, UnresolvedReasonDocShield, res.Unresolved[0].UnresolvedReason,
		"the subject IS in the tree, in the changelog: the routing rests on the doc-extension heuristic and must say so")

	// The reason rides the persisted sidecar, which is what the scorecard bridge
	// and any later reader actually see.
	sidecar, err := ReadUnresolvedFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, sidecar, 1)
	assert.Equal(t, UnresolvedReasonDocShield, sidecar[0].UnresolvedReason)
}
