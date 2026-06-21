package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
)

func findDebateCmd(t *testing.T) bool {
	t.Helper()
	for _, c := range newRootCmd().Commands() {
		if c.Name() == "debate" {
			return true
		}
	}
	return false
}

func TestDebateCmd_Exists(t *testing.T) {
	require.True(t, findDebateCmd(t), "debate subcommand must be registered")
	_, help := execCmdCapture(t, "debate", "--help")
	require.Contains(t, help, "--single-model")
}

// TestDebateCmd_MissingReconciledFindings: a review without reconciled/findings.json
// exits non-zero with the reconcile-first guidance, identical to verify/CLI parity.
func TestDebateCmd_MissingReconciledFindings(t *testing.T) {
	isolate(t)
	writeVerifyRegistry(t)
	dir := filepath.Join(".atcr", "reviews", "r")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "host"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"a","head":"HEAD","roster":["bruce"],"partial":false,"stages":["review"]}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "latest"), []byte("r\n"), 0o644))

	code, out := execCmdCapture(t, "debate", "r")
	require.NotEqual(t, 0, code)
	require.Contains(t, out, "no reconciled findings found")
	require.Contains(t, out, "atcr reconcile")
}

// TestDebateCmd_UnresolvedWithoutRoles: a disputed finding with no skeptic/judge
// roles configured leaves the item unresolved (no distinct models, no opt-in) and
// the command still succeeds (exit 0) — failure isolation, not a hard error.
func TestDebateCmd_UnresolvedWithoutRoles(t *testing.T) {
	isolate(t)
	writeVerifyRegistry(t)
	verifyFixture(t, "r", []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "HIGH",
		Reviewers: []string{"bruce"}, Disagreement: "MEDIUM vs HIGH",
	}})
	code, out := execCmdCapture(t, "debate", "r")
	require.Equal(t, 0, code)
	require.Contains(t, out, "debated 1 item(s)")
	require.Contains(t, out, "1 unresolved")
}
