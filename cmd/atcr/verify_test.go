package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	reclib "github.com/samestrin/atcr/reconcile"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/require"
)

// execCmdCapture runs the atcr command tree with args and returns the resolved
// exit code plus the combined stdout+stderr, so a test can assert both the code
// and the user-facing message (the missing-findings guidance, etc.).
func execCmdCapture(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(&buf)
	root.SetErr(&buf)
	err := root.ExecuteContext(context.Background())
	code := exitCode(err)
	out := buf.String()
	if err != nil {
		out += err.Error()
	}
	return code, out
}

// writeVerifyRegistry writes a user registry (under the isolated HOME) with a
// single REVIEWER-role agent and no skeptics, plus a project config rostering
// it. With no skeptic eligible for any finding, SelectEligibleSkeptics returns
// empty and the pipeline records `no_eligible_skeptic` WITHOUT making any LLM
// call — so the CLI plumbing (flags, errors, artifact emission, skip logic) is
// exercised hermetically.
func writeVerifyRegistry(t *testing.T) {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(`providers:
  p:
    api_key_env: ATCR_TEST_KEY
    base_url: https://example.invalid/v1
agents:
  bruce:
    provider: p
    model: m-bruce
`), 0o644))
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"),
		[]byte("agents:\n  - bruce\n"), 0o644))
}

// verifyFixture writes a completed, reconciled review dir under
// ./.atcr/reviews/<id> with the given findings as reconciled/findings.json, a
// minimal reconciled/summary.json, a manifest.json with stages ["review"], and
// the .atcr/latest pointer. Returns the review id.
func verifyFixture(t *testing.T, id string, findings []reconcile.JSONFinding) string {
	t.Helper()
	dir := filepath.Join(".atcr", "reviews", id)
	recon := filepath.Join(dir, "reconciled")
	require.NoError(t, os.MkdirAll(recon, 0o755))
	// A reconciled review always carries a sources/ tree; resolveReviewDir
	// requires it. Verify reads reconciled/findings.json, not sources/.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "host"), 0o755))

	data, err := json.MarshalIndent(findings, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(recon, "findings.json"), append(data, '\n'), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(recon, "summary.json"),
		[]byte(`{"total_findings":`+strconv.Itoa(len(findings))+`}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"a","head":"HEAD","roster":["bruce"],"partial":false,"stages":["review"]}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "latest"), []byte(id+"\n"), 0o644))
	return id
}

// readFindingVerdict reads reconciled/findings.json under review id and returns
// the verdict of the first finding (or "" / "<nil>" when absent), so a test can
// assert the skip-already-verified and --fresh behaviors.
func readFindingVerdict(t *testing.T, id string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(".atcr", "reviews", id, "reconciled", "findings.json"))
	require.NoError(t, err)
	var fs []reconcile.JSONFinding
	require.NoError(t, json.Unmarshal(data, &fs))
	require.NotEmpty(t, fs)
	if fs[0].Verification == nil {
		return "<nil>"
	}
	return fs[0].Verification.Verdict
}

// findVerifyCmd locates the registered `verify` subcommand in the command tree.
func findVerifyCmd(t *testing.T) bool {
	t.Helper()
	for _, c := range newRootCmd().Commands() {
		if c.Name() == "verify" {
			return true
		}
	}
	return false
}

// TestVerifyCmd_Exists: `atcr verify` is registered and its help lists the three
// flags with their defaults (AC 04-01 Scenario 5).
func TestVerifyCmd_Exists(t *testing.T) {
	require.True(t, findVerifyCmd(t), "verify subcommand must be registered")
	_, help := execCmdCapture(t, "verify", "--help")
	require.Contains(t, help, "--fresh")
	require.Contains(t, help, "--thorough")
	require.Contains(t, help, "--min-severity")
	require.Contains(t, help, "--exec")
}

// TestVerifyCmd_ExecRefusesWithoutSandbox: `verify --exec` against a project with
// no [sandbox] block hard-errors (exit 2) without running anything (Epic 11.0 SC-1).
func TestVerifyCmd_ExecRefusesWithoutSandbox(t *testing.T) {
	isolate(t)
	writeVerifyRegistry(t) // writes .atcr/config.yaml with NO sandbox block
	id := verifyFixture(t, "2026-06-25_exec", []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"bruce"}},
	})
	code, out := execCmdCapture(t, "verify", id, "--exec")
	require.Equal(t, 2, code, "--exec without a sandbox block must exit 2")
	require.Contains(t, out, "sandbox")
}

// TestVerifyCmd_MissingReconciledFindings: a review without reconciled/findings.json
// exits non-zero with the reconcile-first guidance (AC 04-01 Error Scenario 1).
func TestVerifyCmd_MissingReconciledFindings(t *testing.T) {
	isolate(t)
	writeVerifyRegistry(t)
	// A review dir with a sources/ tree but no reconciled/findings.json.
	dir := filepath.Join(".atcr", "reviews", "r")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "host"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"a","head":"HEAD","roster":["bruce"],"partial":false,"stages":["review"]}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "latest"), []byte("r\n"), 0o644))

	code, out := execCmdCapture(t, "verify", "r")
	require.NotEqual(t, 0, code)
	require.Contains(t, out, "no reconciled findings found")
	require.Contains(t, out, "atcr reconcile")
}

// TestVerifyCmd_InvalidMinSeverity: a bad --min-severity is a usage error (exit 2)
// listing the valid levels, validated before any I/O (AC 04-01 Error Scenario 2).
func TestVerifyCmd_InvalidMinSeverity(t *testing.T) {
	isolate(t)
	writeVerifyRegistry(t)
	verifyFixture(t, "r", []reconcile.JSONFinding{{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "MEDIUM"}})
	code, out := execCmdCapture(t, "verify", "--min-severity", "BLOCKER", "r")
	require.Equal(t, 2, code)
	require.Contains(t, out, "CRITICAL")
}

// TestVerifyCmd_SkipAlreadyVerified: without --fresh, a finding that already
// carries a verdict is not re-verified — the existing verdict is preserved
// (AC 04-05 Scenario 1).
func TestVerifyCmd_SkipAlreadyVerified(t *testing.T) {
	isolate(t)
	writeVerifyRegistry(t)
	verifyFixture(t, "r", []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "VERIFIED",
		Verification: &reclib.Verification{Verdict: "confirmed", Skeptic: "prior"},
	}})
	code, _ := execCmdCapture(t, "verify", "r")
	require.Equal(t, 0, code)
	// No skeptic re-invoked (registry has none anyway); the prior confirmed verdict
	// survives rather than being overwritten by no_eligible_skeptic.
	require.Equal(t, "confirmed", readFindingVerdict(t, "r"))
}

// TestVerifyFailureError_ConsistentWrapping verifies that verifyFailureError wraps
// a non-ErrNoReconciledFindings error with a consistent prefix so that `atcr verify`
// and `atcr review --verify` produce identical stderr shapes (TD verify.go:72).
func TestVerifyFailureError_ConsistentWrapping(t *testing.T) {
	inner := errors.New("manifest has no head SHA")
	got := verifyFailureError(inner)
	require.Error(t, got)
	require.Contains(t, got.Error(), "verify failed:")
	require.Contains(t, got.Error(), "manifest has no head SHA")
}

// TestVerifyCmd_FreshReverifies: with --fresh, an already-verified finding is
// re-verified as if it had no prior verdict. With no eligible skeptic, that means
// it becomes unverifiable rather than retaining its prior confirmed verdict
// (AC 04-05 Scenario 3).
func TestVerifyCmd_FreshReverifies(t *testing.T) {
	isolate(t)
	writeVerifyRegistry(t)
	verifyFixture(t, "r", []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "VERIFIED",
		Verification: &reclib.Verification{Verdict: "confirmed", Skeptic: "prior"},
	}})
	code, _ := execCmdCapture(t, "verify", "--fresh", "r")
	require.Equal(t, 0, code)
	require.Equal(t, "unverifiable", readFindingVerdict(t, "r"), "--fresh re-attempts; no skeptic -> unverifiable")
}

// TestVerifyCmd_RepoFlagThreadsReviewedRoot proves Epic 22.1 task 2: `atcr verify`
// grows a --repo flag that threads the reviewed-repo root (default ".") into
// verify.Verify's repoRoot — the root skeptics inspect and the exec validator
// resolves go.mod against — replacing the hardcoded "." convention. Asserted
// hermetically via flag acceptance plus no-regression of the no-skeptic pipeline:
// repoRoot's deep effect only surfaces when a skeptic snapshot is built (which
// needs a live model), so the reconcile behavioral test covers path validation
// end to end while this guards the verify-side threading and the common case.
func TestVerifyCmd_RepoFlagThreadsReviewedRoot(t *testing.T) {
	isolate(t)
	writeVerifyRegistry(t)
	otherRepo := t.TempDir()
	verifyFixture(t, "r", []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "x",
	}})
	code, _ := execCmdCapture(t, "verify", "r", "--repo", otherRepo)
	require.Equal(t, 0, code, "--repo must be accepted and must not regress the pipeline")
	// The no-skeptic pipeline still ran to completion against the fixture.
	require.Equal(t, "unverifiable", readFindingVerdict(t, "r"))
}

// TestVerifyCmd_RepoFlagInHelp documents the --repo flag surface (Epic 22.1).
func TestVerifyCmd_RepoFlagInHelp(t *testing.T) {
	isolate(t)
	_, help := execCmdCapture(t, "verify", "--help")
	require.Contains(t, help, "--repo")
}
