package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReconcileCmd_UnconfiguredProjectDoesNotGate pins the opt-in gate
// decision (sprint-plan: "embedded default excluded (opt-in preserved)"):
// with no --fail-on flag, no project config, and no registry, a surviving
// HIGH finding must NOT fail the gate. The embedded fail_on=HIGH default
// applies only to the config template `atcr init` generates.
func TestReconcileCmd_UnconfiguredProjectDoesNotGate(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|issue|fix|security|10|ev|host\n",
	})
	require.Equal(t, 0, execCmd(t, "reconcile", "r"))
}

// TestReconcileCmd_RequireVerifiedRequiresFailOn pins AC 05-01 Edge Case 3:
// --require-verified without --fail-on is a usage error (exit 2), not a silent
// no-op — a strict gate that never runs gives false confidence.
func TestReconcileCmd_RequireVerifiedRequiresFailOn(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|issue|fix|security|10|ev|host\n",
	})
	code, out := execCmdCapture(t, "reconcile", "--require-verified", "r")
	require.Equal(t, 2, code)
	require.Contains(t, out, "--require-verified requires --fail-on")
}

// TestReconcileCmd_RequireVerifiedPassesWithoutVerified pins AC 05-01 Scenario 4:
// a HIGH finding with no verification block (verify never ran) is NOT VERIFIED,
// so --fail-on HIGH --require-verified passes (exit 0) where plain --fail-on HIGH
// fails (exit 1).
func TestReconcileCmd_RequireVerifiedPassesWithoutVerified(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|issue|fix|security|10|ev|host\n",
	})
	require.Equal(t, 1, execCmd(t, "reconcile", "--fail-on", "HIGH", "r"))
	require.Equal(t, 0, execCmd(t, "reconcile", "--fail-on", "HIGH", "--require-verified", "r"))
}
