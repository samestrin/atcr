package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// execCmd runs the atcr command tree with args and returns the resolved exit
// code (the same mapping main() applies).
func execCmd(t *testing.T, args ...string) int {
	t.Helper()
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.ExecuteContext(context.Background())
	return exitCode(err)
}

// fixtureReview writes a review dir under ./.atcr/reviews/<id> with the given
// per-source findings bodies (header prepended) and a .atcr/latest pointer.
func fixtureReview(t *testing.T, id string, files map[string]string) {
	t.Helper()
	base := filepath.Join(".atcr", "reviews", id)
	for rel, body := range files {
		full := filepath.Join(base, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte("# atcr-findings/v1\n"+body), 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(base, "reconciled"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "latest"), []byte(id+"\n"), 0o644))
}

func TestReconcileCmd_FailOnExitCodes(t *testing.T) {
	t.Chdir(t.TempDir())
	fixtureReview(t, "2026-06-10_feat", map[string]string{
		"sources/pool/raw/agent/greta/findings.txt": "HIGH|a.go:1|same issue here|fix|security|10|ev|greta\n",
		"sources/host/findings.txt":                 "HIGH|a.go:1|same issue here|fix|security|10|ev|host\n",
	})

	// No fail-on → exit 0.
	require.Equal(t, 0, execCmd(t, "reconcile", "2026-06-10_feat"))
	// HIGH present, threshold CRITICAL → nothing at/above → exit 0.
	require.Equal(t, 0, execCmd(t, "reconcile", "--fail-on", "CRITICAL", "2026-06-10_feat"))
	// threshold HIGH → a HIGH survives → exit 1.
	require.Equal(t, 1, execCmd(t, "reconcile", "--fail-on", "HIGH", "2026-06-10_feat"))
	// case-insensitive threshold also fails.
	require.Equal(t, 1, execCmd(t, "reconcile", "--fail-on", "high", "2026-06-10_feat"))
}

func TestReconcileCmd_InvalidFailOnIsUsageError(t *testing.T) {
	t.Chdir(t.TempDir())
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n",
	})
	// Invalid severity → exit 2, validated before any reconcile I/O.
	require.Equal(t, 2, execCmd(t, "reconcile", "--fail-on", "BLOCKER", "r"))
}

func TestReconcileCmd_DefaultsToLatest(t *testing.T) {
	t.Chdir(t.TempDir())
	fixtureReview(t, "2026-06-10_latest", map[string]string{
		"sources/host/findings.txt": "CRITICAL|a.go:1|boom|f|security|10|ev|host\n",
	})
	// No anchor arg → resolves .atcr/latest → CRITICAL survives → exit 1.
	require.Equal(t, 1, execCmd(t, "reconcile", "--fail-on", "HIGH"))
	// Artifacts were written under the latest review.
	require.FileExists(t, filepath.Join(".atcr", "reviews", "2026-06-10_latest", "reconciled", "findings.txt"))
}

func TestReconcileCmd_MissingReviewIsUsageError(t *testing.T) {
	t.Chdir(t.TempDir())
	// No review at all → exit 2 (run atcr review first).
	require.Equal(t, 2, execCmd(t, "reconcile"))
	require.Equal(t, 2, execCmd(t, "reconcile", "nonexistent-id"))
}

func TestReconcileCmd_TraversalIdRejected(t *testing.T) {
	t.Chdir(t.TempDir())
	// A bare ".." id must not resolve above .atcr/reviews/ — exit 2, not a read
	// of the parent directory.
	require.Equal(t, 2, execCmd(t, "reconcile", ".."))
}

func TestReconcileCmd_SourcesAllowlist(t *testing.T) {
	t.Chdir(t.TempDir())
	fixtureReview(t, "r", map[string]string{
		"sources/pool/raw/agent/greta/findings.txt": "HIGH|a.go:1|p|f|sec|10|ev|greta\n",
		"sources/host/findings.txt":                 "CRITICAL|b.go:2|p|f|sec|10|ev|host\n",
	})
	// Restrict to pool only → host's CRITICAL excluded → --fail-on HIGH still
	// fails on pool's HIGH, but --fail-on CRITICAL passes (host filtered out).
	require.Equal(t, 0, execCmd(t, "reconcile", "--sources", "pool", "--fail-on", "CRITICAL", "r"))
	require.Equal(t, 1, execCmd(t, "reconcile", "--sources", "pool", "--fail-on", "HIGH", "r"))
}
