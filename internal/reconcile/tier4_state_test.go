package reconcile

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnresolvedState_DiscriminatesTheZeroCount pins the observability fix for
// Summary.UnresolvedFiltered: a count of 0 is produced by several conditions and
// most of them mean Tier 4 never adjudicated anything, so the count alone cannot
// tell a healthy run from a silently-disabled one. UnresolvedState is the
// discriminator, exactly as ConsensusLevel is for ConsensusFiltered.
func TestUnresolvedState_DiscriminatesTheZeroCount(t *testing.T) {
	sources := func(t *testing.T, body string) string {
		t.Helper()
		reviewDir := t.TempDir()
		writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt", body)
		return reviewDir
	}
	// A finding whose cited path does not exist, so Tier 4 is actually consulted.
	const phantom = "HIGH|internal/ghost/phantom.go:9|`quantumFlux` leaks a handle|close it|security|10|ev|greta\n"

	t.Run("applied when the check ran and routed nothing", func(t *testing.T) {
		root := gitRepoWithSources(t, map[string]string{
			"internal/auth/session.go": "package auth\n\nfunc quantumFlux() error { return nil }\n",
		})
		res, err := RunReconcile(context.Background(), sources(t, phantom), nil, Options{
			ReconciledAt: time.Unix(1700000000, 0).UTC(), Root: root,
		})
		require.NoError(t, err)
		assert.Zero(t, res.Summary.UnresolvedFiltered, "the construct exists, so nothing is routed")
		assert.Equal(t, reclib.UnresolvedStateApplied, res.Summary.UnresolvedState,
			"a 0 count here means the check ran and found nothing to route — the healthy case")
	})

	t.Run("applied when the check ran and DID route", func(t *testing.T) {
		root := gitRepoWithSources(t, map[string]string{
			"internal/auth/session.go": "package auth\n\nfunc loadSession() error { return nil }\n",
		})
		res, err := RunReconcile(context.Background(), sources(t, phantom), nil, Options{
			ReconciledAt: time.Unix(1700000000, 0).UTC(), Root: root,
		})
		require.NoError(t, err)
		assert.Equal(t, 1, res.Summary.UnresolvedFiltered)
		assert.Equal(t, reclib.UnresolvedStateApplied, res.Summary.UnresolvedState)
	})

	t.Run("disabled by the AST opt-out", func(t *testing.T) {
		t.Setenv("ATCR_DISABLE_AST_GROUPING", "1")
		root := gitRepoWithSources(t, map[string]string{
			"internal/auth/session.go": "package auth\n\nfunc loadSession() error { return nil }\n",
		})
		res, err := RunReconcile(context.Background(), sources(t, phantom), nil, Options{
			ReconciledAt: time.Unix(1700000000, 0).UTC(), Root: root,
		})
		require.NoError(t, err)
		assert.Zero(t, res.Summary.UnresolvedFiltered)
		assert.Equal(t, reclib.UnresolvedStateDisabled, res.Summary.UnresolvedState,
			"the opt-out produces the same 0 as a healthy run and must be distinguishable")
	})

	t.Run("disabled with no tracked file index", func(t *testing.T) {
		root := t.TempDir() // not a git repo: no tracked set to search
		res, err := RunReconcile(context.Background(), sources(t, phantom), nil, Options{
			ReconciledAt: time.Unix(1700000000, 0).UTC(), Root: root,
		})
		require.NoError(t, err)
		assert.Zero(t, res.Summary.UnresolvedFiltered)
		assert.Equal(t, reclib.UnresolvedStateDisabled, res.Summary.UnresolvedState)
	})

	t.Run("unavailable over the index file cap", func(t *testing.T) {
		t.Setenv(tier4IndexMaxFilesEnv, "1")
		root := gitRepoWithSources(t, map[string]string{
			"internal/auth/session.go": "package auth\n\nfunc loadSession() error { return nil }\n",
			"internal/net/pool.go":     "package net\n\nfunc dialPeer() error { return nil }\n",
		})
		res, err := RunReconcile(context.Background(), sources(t, phantom), nil, Options{
			ReconciledAt: time.Unix(1700000000, 0).UTC(), Root: root,
		})
		require.NoError(t, err)
		assert.Zero(t, res.Summary.UnresolvedFiltered,
			"over the cap every lookup is inconclusive, so nothing is routed")
		assert.Equal(t, reclib.UnresolvedStateUnavailable, res.Summary.UnresolvedState,
			"an index that could not be built is NOT a clean run")
	})

	t.Run("incomplete when a tracked file could not be read", func(t *testing.T) {
		root := gitRepoWithSources(t, map[string]string{
			"internal/auth/session.go": "package auth\n\nfunc loadSession() error { return nil }\n",
			"internal/net/pool.go":     "package net\n\nfunc dialPeer() error { return nil }\n",
		})
		locked := filepath.Join(root, "internal", "net", "pool.go")
		require.NoError(t, os.Chmod(locked, 0o000))
		t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
		if _, err := os.ReadFile(locked); err == nil {
			t.Skip("filesystem or privileges ignore mode 0000")
		}

		res, err := RunReconcile(context.Background(), sources(t, phantom), nil, Options{
			ReconciledAt: time.Unix(1700000000, 0).UTC(), Root: root,
		})
		require.NoError(t, err)
		assert.Zero(t, res.Summary.UnresolvedFiltered,
			"a hole in the search withholds every no-match verdict")
		assert.Equal(t, reclib.UnresolvedStateIncomplete, res.Summary.UnresolvedState,
			"an incomplete index is neither a clean run nor an unavailable one")
	})
}

// TestUnresolvedState_RenderedUnconditionally pins that the state reaches
// report.md on EVERY run, the way ConsensusLevel does and unlike the count,
// which renders only when nonzero. A report that omits it leaves no record of
// whether the content check was in force when these artifacts were produced.
func TestUnresolvedState_RenderedUnconditionally(t *testing.T) {
	for _, state := range []string{
		reclib.UnresolvedStateApplied,
		reclib.UnresolvedStateDisabled,
		reclib.UnresolvedStateUnavailable,
		reclib.UnresolvedStateIncomplete,
	} {
		var b bytes.Buffer
		require.NoError(t, renderMarkdown(&b, Summary{UnresolvedState: state}, nil, DisagreementsFile{}))
		assert.Contains(t, b.String(), "- Unresolved check: "+state,
			"state %q must render even with a zero count", state)
	}

	// An unstamped Summary (a pure in-memory embedder) renders nothing, keeping
	// report.md byte-identical for callers that never run content resolution.
	var unstamped bytes.Buffer
	require.NoError(t, renderMarkdown(&unstamped, Summary{}, nil, DisagreementsFile{}))
	assert.NotContains(t, unstamped.String(), "Unresolved check:")
}

// TestUnresolvedState_SubmoduleIsNotASearchHole pins that a tracked entry which
// is not a regular file does not disable the Tier 4 no-match verdict.
//
// `git ls-files` emits a submodule as a single gitlink row naming its DIRECTORY.
// eligiblePaths stopped filtering by parser language (Epic 35.16.6.5), so that
// row now reaches the read loop, where os.ReadFile returns "is a directory". Read
// as a hole that would clear `complete`, which withholds EVERY no-match verdict:
// on any repo carrying a submodule the state would read "incomplete" and nothing
// would ever be routed. No declaration lives in a non-file, so it is a resolution
// limit, not a search hole.
func TestUnresolvedState_SubmoduleIsNotASearchHole(t *testing.T) {
	inner := gitRepoWithSources(t, map[string]string{
		"lib/inner.go": "package lib\n\nfunc innerHelper() error { return nil }\n",
	})
	root := gitRepoWithSources(t, map[string]string{
		"internal/auth/session.go": "package auth\n\nfunc loadSession() error { return nil }\n",
	})
	gitIn := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	gitIn("-c", "protocol.file.allow=always", "submodule", "add", "-q", inner, "sub")
	gitIn("commit", "-q", "-m", "add submodule")

	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/ghost/phantom.go:9|`quantumFlux` leaks a handle|close it|security|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(), Root: root,
	})
	require.NoError(t, err)
	assert.Equal(t, reclib.UnresolvedStateApplied, res.Summary.UnresolvedState,
		"a submodule gitlink is not a readable source file, so it must not clear `complete`")
	assert.Equal(t, 1, res.Summary.UnresolvedFiltered,
		"the phantom must still be routed in a repo that carries a submodule")
}

// TestUnresolvedState_AppliedWithoutBuildingAnIndex pins what "applied" actually
// asserts. The published godoc used to say it meant "an index was built over the
// tracked tree", and on the most common path that is false: when every finding
// cites a file that exists, no finding is Tier-4-eligible, AC5 laziness means the
// index is never constructed and not one file is read — yet "applied" is
// correctly what gets stamped, because the check WAS in force and simply had
// nothing to adjudicate.
//
// The state is the claim; this test is its referent. Keep the doc wording in
// reconcile/reconcile.go and docs/code-review-backend.md matched to it.
func TestUnresolvedState_AppliedWithoutBuildingAnIndex(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/auth/session.go": "package auth\n\nfunc loadSession() error { return nil }\n",
	})
	built := withFakeTier4(t, &fakeTier4{})

	reviewDir := t.TempDir()
	// The cited path EXISTS, so path validation resolves at Tier 1 and Tier 4 is
	// never consulted.
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/auth/session.go:3|`loadSession` leaks a handle|close it|correctness|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(), Root: root,
	})
	require.NoError(t, err)
	assert.Zero(t, *built, "AC5: no finding needed Tier 4, so no index may be constructed")
	assert.Equal(t, reclib.UnresolvedStateApplied, res.Summary.UnresolvedState,
		"the check was in force with nothing to adjudicate — that is applied, not disabled")
	assert.Zero(t, res.Summary.UnresolvedFiltered)
}
