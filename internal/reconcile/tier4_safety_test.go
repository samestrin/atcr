package reconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// gitRepoWithCaseAliases builds a repo from files, then adds extra TRACKED
// entries that differ from an existing path only by case.
//
// The aliases go straight into the git index via update-index --cacheinfo rather
// than onto disk, because the case-insensitive filesystems this scenario exists
// for (macOS, Windows) cannot hold both spellings at once — which is precisely
// how a repo ends up with two fold-colliding tracked paths in the first place.
func gitRepoWithCaseAliases(t *testing.T, files map[string]string, aliases map[string]string) string {
	t.Helper()
	root := gitRepoWithSources(t, files)
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	for alias, source := range aliases {
		blob := run("hash-object", "-w", filepath.FromSlash(source))
		run("update-index", "--add", "--cacheinfo", "100644,"+blob+","+alias)
	}
	return root
}

// TestTier4Safety_AmbiguousCaseOnlyMismatchIsNeverRouted covers the fold gate in
// tier4Eligible — `len(idx.ByFold(file)) == 0` — which
// TestTier4Safety_CaseOnlyMismatchIsNeverRouted above does NOT reach.
//
// With ONE fold match, stream.CaseCorrection returns a suggestion, so
// validateFindingPaths short-circuits on `sf.PathSuggestion != ""` before Tier 4
// is consulted at all. With SEVERAL, CaseCorrection reports mismatch=true with an
// EMPTY suggestion — leaving PathWarning set and PathSuggestion empty, which is
// indistinguishable at that layer from a path resolving to nothing. The fold gate
// is then the ONLY thing standing between a real finding and the sidecar.
//
// Mutating that gate to `return true` must fail here: the cited file
// demonstrably exists in the tracked tree under two spellings, and the finding's
// anchor is absent from the tree, so without the gate it reaches tier4NoMatch and
// is routed out of findings.json as fabricated.
func TestTier4Safety_AmbiguousCaseOnlyMismatchIsNeverRouted(t *testing.T) {
	root := gitRepoWithCaseAliases(t,
		map[string]string{
			"internal/auth/session.go": "package auth\n\nfunc loadSession() error { return nil }\n",
		},
		// A second tracked spelling of the same path: both fold to
		// "internal/auth/session.go", so no single correction is preferable.
		map[string]string{"internal/auth/Session.go": "internal/auth/session.go"},
	)

	reviewDir := t.TempDir()
	// Cited with a THIRD casing, matching neither tracked spelling exactly, and
	// naming a construct that is genuinely absent from the tree.
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/auth/SESSION.go:12|`parseBearerHeader` never checks the scheme|validate it|security|15|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)

	require.Len(t, res.Findings, 1, "the finding stays in the primary stream")
	assert.Zero(t, res.Summary.UnresolvedFiltered,
		"the cited file exists in the tracked tree — an ambiguous case mismatch is a Tier 3 concern, never fabrication evidence")
	assert.Empty(t, res.Unresolved, "nothing may reach the sidecar on a case-only mismatch")
}

// TestTier4Safety_GluedSpacelessProseIsNeverNoMatch: two adjacent spaceless
// scripts do not break the backwards run, so ordinary prose in such a script is
// welded onto a snake_case call name. `設定を解析_処理()` yields the glued token
// where the tree declares `解析_処理` — measured old-vs-new, resolve went
// tier4Resolved -> tier4NoMatch, replacing the BEST outcome with the worst.
//
// The glued token cannot simply be dropped: the crossing it rides on is present
// in `データ_解析` too, which is one real identifier and must keep yielding
// whole. So the anchor is contributed and the EXTRACTION is marked imprecise,
// which is what this test pins at the only layer that matters — the routing
// decision in validateFindingPaths.
func TestTier4Safety_GluedSpacelessProseIsNeverNoMatch(t *testing.T) {
	name := string([]rune{0x89E3, 0x6790, 0x005F, 0x51E6, 0x7406}) // 解析_処理
	prose := string([]rune{0x8A2D, 0x5B9A, 0x3092})                // 設定を
	problem := prose + name + "() drops the returned error"

	anchors, truncated := extractAnchorSet(problem)
	require.Equal(t, []string{prose + name}, anchors, "the glued token is still the anchor")
	require.True(t, truncated, "a glued span is not a faithful reading of what the reviewer wrote")

	root := gitRepoWithSources(t, map[string]string{
		"internal/jp/parse.go": "package jp\n\nfunc " + name + "() error { return nil }\n",
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
		"the tree declares the name the reviewer wrote; gluing prose onto it must not delete the finding")
}

// TestTier4Safety_SilencedSpanIsNeverNoMatch: the undecidable-underscore
// suppression contributes no anchor for its span. That silence is per-SPAN
// while its safety argument ("no anchor keeps the finding") is per-FINDING, so
// with a co-cited anchor that is ABSENT from the tree, silencing the one span
// that would have matched routes the whole finding out.
//
// Measured old-vs-new on this exact fixture: anchors went [_loadFile] (resolve
// = tier4Resolved) to [retryOnce] alone (resolve = tier4NoMatch), so gate.go
// deleted a real finding and scorecard.go charged the reviewer a phantom.
//
// `_loadFile` is the ordinary JS/TS private convention, and
// collectSourceIdentifiers harvests it as its own token — verified.
func TestTier4Safety_SilencedSpanIsNeverNoMatch(t *testing.T) {
	glued := string([]rune{0x8A2D, 0x5B9A, 0x005F}) + "loadFile" // 設定_loadFile
	problem := glued + "() ignores the deadline set by `retryOnce`"

	anchors, truncated := extractAnchorSet(problem)
	require.Equal(t, []string{"retryOnce"}, anchors, "the undecidable span contributes no anchor")
	require.True(t, truncated, "the silence is a loss and must be reported as one")

	root := gitRepoWithSources(t, map[string]string{
		"src/loader.js": "function _loadFile() { return null; }\n",
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
		"a co-cited absent anchor must not route a finding whose other span was silenced")
}
