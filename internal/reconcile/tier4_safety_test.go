package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests in this file all guard ONE property: a REAL finding must never be
// routed to the unresolved sidecar. Each is a scenario where the Tier 4 search
// can reach "checked and found nothing" while the construct the finding names
// plainly exists — the epic's worst-case failure, since a sidecar-routed finding
// leaves report.md and never reaches the skeptic pass.

// TestTier4Safety_GoTypeIsNotAFalseNoMatch is the Go-specific false-drop: the
// embedded go.wasm parser's nodeName names ONLY *ast.FuncDecl, so a Go type,
// interface, const or var is absent from the declaration index even though it is
// right there in the tracked tree. A finding naming one must not be sidecar-routed.
func TestTier4Safety_GoTypeIsNotAFalseNoMatch(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/stream/fileindex.go": "package stream\n\n" +
			"type FileIndex struct {\n\ttracked map[string]struct{}\n}\n\n" +
			"func BuildFileIndex(root string) *FileIndex {\n\treturn nil\n}\n",
	})

	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/idx/candidates.go:12|`FileIndex` is not safe for concurrent use|add a mutex|correctness|20|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)

	assert.Zero(t, res.Summary.UnresolvedFiltered,
		"FileIndex is a Go type: the parser cannot name it, so its absence from the declaration index is not evidence of anything")
	require.Len(t, res.Findings, 1, "the finding stays in the primary stream")
}

// TestTier4Safety_FixOnlyAnchorIsNotEvidence: a FIX names the symbol the
// reviewer wants CREATED. That name is absent from the tree by definition, so it
// must never be the evidence that a finding is fabricated.
func TestTier4Safety_FixOnlyAnchorIsNotEvidence(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/auth/session.go": "package auth\n\nfunc refreshBearerToken() error {\n\treturn nil\n}\n",
	})

	reviewDir := t.TempDir()
	// PROBLEM names no identifier at all; FIX proposes a helper that does not exist yet.
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"MEDIUM|internal/auth/handler.go:40|this function is far too long to follow|extract the retry loop into `splitRetryHelper`|maintainability|30|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)

	assert.Zero(t, res.Summary.UnresolvedFiltered,
		"a proposed-remedy name is not evidence the finding is fabricated")
	require.Len(t, res.Findings, 1)
}

// TestTier4Safety_NoParserLanguageIsNeverRouted is the recorded binding
// clarification: a finding citing a file with no parser language could not be
// checked by a symbol search at all, so it is never sidecar-routed.
func TestTier4Safety_NoParserLanguageIsNeverRouted(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/auth/session.go": "package auth\n\nfunc refreshBearerToken() error {\n\treturn nil\n}\n",
	})

	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"LOW|docs/deployBook.md:12|`deployStagingCluster` is documented with the wrong flag|correct the flag name|docs|10|ev|greta\n"+
			"LOW|config/appSettings.yaml:4|`maxRetryBudget` is set too high|lower it|config|5|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)

	assert.Zero(t, res.Summary.UnresolvedFiltered,
		"a symbol index over .go files cannot adjudicate a .md or .yaml citation")
	assert.Len(t, res.Findings, 2)
}

// TestTier4Safety_IncompleteIndexIsNeverNoMatch: if any eligible tracked file
// could not be read, the search was incomplete, so "not found" is unproven — the
// same reasoning the file-cap branch already applies.
func TestTier4Safety_IncompleteIndexIsNeverNoMatch(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "net"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "net", "pool.go"),
		[]byte("package net\n\nfunc DialPeer() error { return nil }\n"), 0o644))

	// "internal/net/gone.go" is in the tracked set but absent from disk.
	lz := newLazySymbolIndex(root, []string{"internal/net/pool.go", "internal/net/gone.go"})

	_, outcome := lz.resolve(context.Background(), []string{"SomethingNotHere"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome,
		"an unreadable eligible file leaves a hole in the search, so no-match is unproven")
}

// TestTier4Safety_TruncatedAnchorSetIsNeverNoMatch: the anchor cap drops the
// lexically-last anchors, and the dropped one may have been the only match.
func TestTier4Safety_TruncatedAnchorSetIsNeverNoMatch(t *testing.T) {
	problem := ""
	for _, n := range []string{"aOne", "bTwo", "cThree", "dFour", "eFive", "fSix", "gSeven", "hEight", "zLast"} {
		problem += "`" + n + "` "
	}
	anchors, truncated := extractAnchorSet(problem)
	require.True(t, truncated, "more identifiers than the cap were named")
	require.Len(t, anchors, maxAnchorsPerFinding)
	assert.NotContains(t, anchors, "zLast", "the lexically-last anchor was dropped")

	root := gitRepoWithSources(t, map[string]string{
		"internal/net/pool.go": "package net\n\nfunc zLast() error { return nil }\n",
	})
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/ghost/phantom.go:3|"+problem+"|fix it|correctness|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	assert.Zero(t, res.Summary.UnresolvedFiltered,
		"a truncated anchor set means the search was partial, so no-match is unproven")
}

// TestTier4Safety_CaseOnlyMismatchIsNeverRouted: an AMBIGUOUS case-only
// mismatch leaves PathWarning set with no suggestion, which would otherwise let
// Tier 4 route a finding whose file demonstrably exists under a different case.
func TestTier4Safety_CaseOnlyMismatchIsNeverRouted(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/net/Pool.go": "package net\n\nfunc DialPeer() error { return nil }\n",
	})
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/net/pool.go:3|`quantumFluxCapacitor` leaks a handle|close it|security|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	assert.Zero(t, res.Summary.UnresolvedFiltered,
		"the cited file exists under a different case; it is not a phantom path")
}

// TestTier4Safety_SingleWordEnglishIsNotAnAnchor: a quoted user-facing string is
// not a symbol citation. Accepting it both manufactures no-match evidence from
// prose and can emit a confidently wrong suggestion off a same-named symbol.
func TestTier4Safety_SingleWordEnglishIsNotAnAnchor(t *testing.T) {
	for _, word := range []string{"Timeout", "Error", "Cannot", "Unresolved"} {
		anchors, _ := extractAnchorSet("the message `" + word + "` should be capitalized differently")
		assert.Empty(t, anchors, "%q is prose, not a symbol citation", word)
	}
	// Real multi-word identifiers still qualify.
	anchors, _ := extractAnchorSet("`RefreshToken` and `dial_peer` and `readTree`")
	assert.Equal(t, []string{"RefreshToken", "dial_peer", "readTree"}, anchors)
}

// TestTier4Safety_BuildRespectsContext: the repo-wide sweep must be
// interruptible, and an aborted build must never answer no-match.
func TestTier4Safety_BuildRespectsContext(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/net/pool.go": "package net\n\nfunc DialPeer() error { return nil }\n",
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	lz := newLazySymbolIndex(root, []string{"internal/net/pool.go"})
	_, outcome := lz.resolve(ctx, []string{"SomethingNotHere"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome, "a cancelled build never yields a no-match verdict")
}

// TestTier4Safety_UnicodeIdentifierIsNotAFalseNoMatch: identifiers with
// non-ASCII letters are legal in Go, Python and TypeScript.
func TestTier4Safety_UnicodeIdentifierIsNotAFalseNoMatch(t *testing.T) {
	anchors, _ := extractAnchorSet("the `refreshJetón` helper drops the error")
	assert.Equal(t, []string{"refreshJetón"}, anchors,
		"a non-ASCII identifier must be extractable, or its finding is judged on a search that never looked for it")
}
