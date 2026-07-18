package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// AXI mode governs stdout payload SHAPE only; it must never alter the process
// exit-code contract (0 success / 1 gate-failure / 2 usage / 3 auth). These tests
// run the same scenario with and without --axi and assert the exit codes are
// identical — the AC 02-01 headline: no --axi-specific branch in exitCode(), the
// four codes preserved. `atcr verify`'s 0/1/2 mapping (independently derived, see
// documentation/exit-code-cli-mcp-precedent.md) is the cross-validation precedent
// this parity is measured against.

// TestAXIExitParity_CleanReviewExitsZero: a completed review with no gate exits 0
// under both --axi and non-axi (Scenario 1). Partial-success-is-not-failure rides
// the same path — exit 0 is decided before any output formatting.
func TestAXIExitParity_CleanReviewExitsZero(t *testing.T) {
	run := func(axi bool) int {
		isolate(t)
		t.Setenv(testReviewKeyEnv, "secret")
		initGitRepoWithChange(t)
		srv := liveMockProvider(t)
		liveReviewConfig(t, srv.URL, "bruce")
		args := []string{"review", "--base", "HEAD^"}
		if axi {
			args = append(args, "--axi")
		}
		return execCmd(t, args...)
	}
	require.Equal(t, 0, run(false), "non-axi clean review exits 0")
	require.Equal(t, 0, run(true), "--axi clean review exits 0 — identical to non-axi")
}

// TestAXIExitParity_GateFailureExitsOne: a surviving finding at/above the --fail-on
// threshold gates to exit 1 under both modes (Scenario 2). The mock returns a
// CRITICAL finding, so --fail-on high gates.
func TestAXIExitParity_GateFailureExitsOne(t *testing.T) {
	run := func(axi bool) int {
		isolate(t)
		t.Setenv(testReviewKeyEnv, "secret")
		initGitRepoWithChange(t)
		srv := liveMockProvider(t)
		liveReviewConfig(t, srv.URL, "bruce")
		args := []string{"review", "--fail-on", "high", "--base", "HEAD^"}
		if axi {
			args = append(args, "--axi")
		}
		return execCmd(t, args...)
	}
	require.Equal(t, 1, run(false), "non-axi gate failure exits 1")
	require.Equal(t, 1, run(true), "--axi gate failure exits 1 — identical to non-axi")
}

// TestAXIExitParity_UsageErrorExitsTwo: an invalid --fail-on severity is a usage
// error (exit 2) under both modes (Error Scenario 1). Validation runs before any
// output formatting, so --axi cannot change the classification.
func TestAXIExitParity_UsageErrorExitsTwo(t *testing.T) {
	run := func(axi bool) int {
		isolate(t)
		args := []string{"review", "--fail-on", "bogus"}
		if axi {
			args = append(args, "--axi")
		}
		return execCmd(t, args...)
	}
	require.Equal(t, 2, run(false), "non-axi invalid --fail-on exits 2")
	require.Equal(t, 2, run(true), "--axi invalid --fail-on exits 2 — identical to non-axi")
}

// TestAXIExitParity_AuthErrorExitsThree: `--sync-cloud` with an unset ATCR_API_KEY
// exits 3 (auth) under both modes (Error Scenario 2). The auth precondition is
// resolved fail-fast before any review work or output formatting.
func TestAXIExitParity_AuthErrorExitsThree(t *testing.T) {
	run := func(axi bool) int {
		isolate(t)
		t.Setenv("ATCR_API_KEY", "")
		args := []string{"review", "--sync-cloud"}
		if axi {
			args = append(args, "--axi")
		}
		return execCmd(t, args...)
	}
	require.Equal(t, exitAuth, run(false), "non-axi missing-key --sync-cloud exits 3")
	require.Equal(t, exitAuth, run(true), "--axi missing-key --sync-cloud exits 3 — identical to non-axi")
}

// TestAXIExitParity_ReportAXIFormatExitsZero: `atcr report --format axi` over a
// reconciled review exits 0, matching `--format md` for the same input (Edge Case
// 2). report's AXI surface is `--format axi` (Phase 1), not a separate flag.
func TestAXIExitParity_ReportAXIFormatExitsZero(t *testing.T) {
	setup := func() {
		isolate(t)
		t.Setenv(testReviewKeyEnv, "secret")
		initGitRepoWithChange(t)
		srv := liveMockProvider(t)
		liveReviewConfig(t, srv.URL, "bruce")
		require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^"))
		require.Equal(t, 0, execCmd(t, "reconcile"))
	}
	setup()
	require.Equal(t, 0, execCmd(t, "report", "--format", "md"), "report --format md exits 0")
	require.Equal(t, 0, execCmd(t, "report", "--format", "axi"), "report --format axi exits 0 — identical to md")
}
