package reconcile

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/astgroup"
	"github.com/samestrin/atcr/internal/metrics"
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
		require.NoError(t, renderMarkdown(&b, Summary{UnresolvedState: state}, nil, DisagreementsFile{}, 0))
		assert.Contains(t, b.String(), "- Unresolved check: "+state,
			"state %q must render even with a zero count", state)
	}

	// An unstamped Summary (a pure in-memory embedder) renders nothing, keeping
	// report.md byte-identical for callers that never run content resolution.
	var unstamped bytes.Buffer
	require.NoError(t, renderMarkdown(&unstamped, Summary{}, nil, DisagreementsFile{}, 0))
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

// TestUnresolvedState_IncompleteOutranksUnavailable pins the case order in
// state(), where two conditions can hold at once and only one of them is true.
//
// When every parser failed AND a region of the tree went unread, the
// parser-failure case used to answer first and report "unavailable". But
// !complete withholds every NO-MATCH verdict, so nothing is routed and nothing
// could be — resolutions still occur, since the locate branches run before the
// completeness gate — and build already incremented
// tier4IncompleteMetric for that same run. An operator correlating
// atcr_tier4_index_incomplete_total against unresolved_state per docs/metrics.md
// would conclude the incomplete counter had misfired: the metric-vs-report
// disagreement the parser-failure case was added to REMOVE, reproduced in the
// opposite direction.
//
// "Incomplete" is also the more useful answer. Unavailable says the index could
// not be reached; incomplete says the search had a hole. When both are true the
// hole is what withheld the verdicts, and it is the one an operator can act on.
func TestUnresolvedState_IncompleteOutranksUnavailable(t *testing.T) {
	t.Run("both conditions hold", func(t *testing.T) {
		lz := &lazySymbolIndex{idx: &symbolIndex{
			byName:           map[string][]string{},
			parserLoadFailed: true,
			complete:         false,
		}}
		assert.Equal(t, reclib.UnresolvedStateIncomplete, lz.state(),
			"a search with a hole in it is incomplete, whatever else also failed")
	})

	// "parser failure alone is still unavailable" is deliberately NOT repeated
	// here: TestSymbolIndex_StateUnavailableWhenEveryParserFailed already pins it
	// through a real build, which a literal cannot.

	t.Run("a hole alone is still incomplete", func(t *testing.T) {
		lz := &lazySymbolIndex{idx: &symbolIndex{
			byName:   map[string][]string{"DialPeer": {"internal/net/pool.go"}},
			complete: false,
		}}
		assert.Equal(t, reclib.UnresolvedStateIncomplete, lz.state())
	})

	// The literals above pin the SWITCH. This one pins that the combination they
	// describe is reachable from a real build at all, and that the state agrees
	// with the counters that build increments — which is the entire reason the
	// order matters. A tracked-but-absent file makes complete false; a parser
	// factory that always fails makes parserLoadFailed true and leaves byName
	// empty.
	t.Run("reachable from a real build, and agrees with both counters", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "net"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "net", "pool.go"),
			[]byte("package net\n\nfunc DialPeer(addr string) error { return nil }\n"), 0o644))

		lz := newLazySymbolIndex(root, []string{"internal/net/pool.go", "internal/net/deleted.go"})
		lz.newParser = func(string) (astgroup.Parser, error) { return nil, errors.New("wasm load failed") }

		beforeUnavailable := metrics.Counter(tier4UnavailableMetric).Value()
		beforeIncomplete := metrics.Counter(tier4IncompleteMetric).Value()
		_, _ = lz.resolve(context.Background(), []string{"DialPeer"}, nil)

		require.NotNil(t, lz.idx, "an index was built: this is not the no-index case")
		require.True(t, lz.idx.parserLoadFailed)
		require.Empty(t, lz.idx.byName, "every parser failed, so nothing was declared")
		require.False(t, lz.idx.complete, "a tracked-but-absent file is a hole in the search")

		assert.Equal(t, reclib.UnresolvedStateIncomplete, lz.state(),
			"the hole is what withheld the verdicts, so that is what the state must name")
		assert.Equal(t, beforeIncomplete+1, metrics.Counter(tier4IncompleteMetric).Value(),
			"the state must not contradict the counter the same run incremented")
		assert.Equal(t, beforeUnavailable+1, metrics.Counter(tier4UnavailableMetric).Value(),
			"both degradations happened and both are counted; only the state has to pick one")
	})

	t.Run("neither is applied", func(t *testing.T) {
		lz := &lazySymbolIndex{idx: &symbolIndex{
			byName:   map[string][]string{"DialPeer": {"internal/net/pool.go"}},
			complete: true,
		}}
		assert.Equal(t, reclib.UnresolvedStateApplied, lz.state())
	})
}
