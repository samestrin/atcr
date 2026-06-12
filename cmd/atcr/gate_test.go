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
