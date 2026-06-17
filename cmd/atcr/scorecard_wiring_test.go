package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/scorecard"
)

// execCmdSplit runs the atcr command tree with args and returns the resolved
// exit code plus stdout and stderr captured into SEPARATE buffers. Unlike
// execCmdCapture (which merges the two), the split lets a wiring test prove a
// scorecard diagnostic is routed to cmd.ErrOrStderr() specifically and is not
// conflated with stdout — the property the AC3 wiring tests assert.
func execCmdSplit(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	err := root.ExecuteContext(context.Background())
	return exitCode(err), outBuf.String(), errBuf.String()
}

// seedMalformedStore writes one valid reviewer record for runID under the
// isolated store, then appends a malformed JSONL line to the same month file, so
// a read-path command (ReadAll / FindByRunID) emits the "skipping malformed
// record" diagnostic while still returning its valid rows. isolate(t) must run
// first so DefaultDir() resolves into the per-test temp store.
func seedMalformedStore(t *testing.T, runID, reviewer string) {
	t.Helper()
	storeRecord(t, reviewerRec(runID, reviewer, "claude-sonnet-4-6", 12, 7))
	dir, err := scorecard.DefaultDir()
	require.NoError(t, err)
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	require.NoError(t, err)
	require.Len(t, matches, 1, "expected exactly one month file after storeRecord")
	f, err := os.OpenFile(matches[0], os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("{not valid json\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

// TestLeaderboardCmd_ReadDiagnosticRoutesToErrOrStderr locks AC3 for the
// `leaderboard` entry point: runLeaderboard must pass cmd.ErrOrStderr() as
// scorecard.ReadOpts.Writer, so a store read diagnostic (a malformed JSONL line
// skipped by ReadAll) lands on the command's stderr — not stdout, and not the
// process-global os.Stderr. A regression to ReadOpts{} (nil Writer → os.Stderr)
// routes the diagnostic to the real stderr and fails this assertion.
func TestLeaderboardCmd_ReadDiagnosticRoutesToErrOrStderr(t *testing.T) {
	isolate(t)
	seedMalformedStore(t, "2026-06-14T10:00:00Z-corrupt", "bruce")

	code, stdout, stderr := execCmdSplit(t, "leaderboard", "--since", "all")
	require.Equal(t, 0, code)
	require.Contains(t, stderr, scorecard.MsgMalformedSkip,
		"leaderboard read diagnostic must route to cmd.ErrOrStderr()")
	require.NotContains(t, stdout, scorecard.MsgMalformedSkip,
		"the diagnostic must not leak onto stdout")
}

// TestScorecardCmd_ReadDiagnosticRoutesToErrOrStderr locks AC3 for the
// `scorecard` entry point: runScorecard must pass cmd.ErrOrStderr() as
// scorecard.ReadOpts.Writer, so FindByRunID's "skipping malformed record"
// diagnostic lands on the command's stderr rather than stdout or the
// process-global os.Stderr. A regression to a default writer fails this test.
func TestScorecardCmd_ReadDiagnosticRoutesToErrOrStderr(t *testing.T) {
	isolate(t)
	runID := "2026-06-14T10:00:00Z-corrupt"
	seedMalformedStore(t, runID, "bruce")

	code, stdout, stderr := execCmdSplit(t, "scorecard", runID)
	require.Equal(t, 0, code)
	require.Contains(t, stderr, scorecard.MsgMalformedSkip,
		"scorecard read diagnostic must route to cmd.ErrOrStderr()")
	require.NotContains(t, stdout, scorecard.MsgMalformedSkip,
		"the diagnostic must not leak onto stdout")
}

// TestReconcileCmd_EmitDiagnosticRoutesToErrOrStderr locks AC3 for the
// `reconcile` entry point — the emit path. runReconcile must pass
// cmd.ErrOrStderr() as scorecard.EmitOpts.Diag. Pointing the resolved store path
// at a regular file makes Append's MkdirAll fail, so Emit writes its "scorecard:
// write failed" diagnostic; that diagnostic must reach the command's stderr.
// Scorecard emission is best-effort, so the write failure must NOT fail the
// reconcile (exit 0). A regression to EmitOpts{} (nil Diag → os.Stderr) routes
// the diagnostic to the real stderr and fails this assertion.
func TestReconcileCmd_EmitDiagnosticRoutesToErrOrStderr(t *testing.T) {
	isolate(t)
	fixtureReview(t, "2026-06-10_feat", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
	})
	// Force a scorecard write-failure: create the store's parent dir, then place
	// a regular file where the scorecard store directory should be, so Append's
	// MkdirAll(dir) fails. This works cross-platform: Go's os.MkdirAll returns
	// ENOTDIR on any OS when a regular file occupies the target path, and the
	// test asserts only the diagnostic + exit 0, never the errno — so it passes
	// on Windows too. (The genuinely POSIX-specific caveat in this area is
	// store.go's O_APPEND append-atomicity guarantee, TD-004 — production
	// behavior, not this test.)
	dir, err := scorecard.DefaultDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(dir), 0o755))
	require.NoError(t, os.WriteFile(dir, []byte("x"), 0o600))

	code, _, stderr := execCmdSplit(t, "reconcile", "2026-06-10_feat")
	require.Equal(t, 0, code,
		"a scorecard emit failure is best-effort and must not fail the reconcile")
	require.Contains(t, stderr, scorecard.MsgWriteFailed,
		"reconcile emit diagnostic must route to cmd.ErrOrStderr()")
}
