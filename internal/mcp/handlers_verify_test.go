package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// verifyReviewFixture builds a completed, reconciled review under
// root/.atcr/reviews/<id>: reconciled/findings.json (the verify input),
// reconciled/summary.json, a manifest with stages ["review"], the pool
// completion signal, and the .atcr/latest pointer. Returns the review id.
func verifyReviewFixture(t *testing.T, root string, findings []reconcile.JSONFinding) string {
	t.Helper()
	id := "2026-06-10_verify"
	dir := filepath.Join(root, ".atcr", "reviews", id)
	recon := filepath.Join(dir, "reconciled")
	require.NoError(t, os.MkdirAll(recon, 0o755))

	data, err := json.MarshalIndent(findings, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(recon, "findings.json"), append(data, '\n'), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(recon, "summary.json"),
		[]byte(`{"total_findings":1}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"aaa","head":"HEAD","roster":["greta"],"partial":false,"stages":["review"]}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sources", "pool", "summary.json"),
		[]byte(`{"total":1,"succeeded":1,"failed":0,"partial":false,"total_findings":1}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "latest"), []byte(id+"\n"), 0o644))
	return id
}

// TestHandleVerify_Basic: atcr_verify on a reconciled review returns a
// VerifyResult with verdict counts and the count of findings processed. With a
// reviewer-only registry no skeptic is eligible, so the single finding is
// recorded unverifiable without any LLM call (AC 04-03 Scenario 1).
// FindingsProcessed is 0 because no finding was sent through a live skeptic.
func TestHandleVerify_Basic(t *testing.T) {
	root := t.TempDir()
	writeReviewConfig(t, root)
	id := verifyReviewFixture(t, root, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "MEDIUM", Reviewers: []string{"greta"}},
	})
	cs := connectTest(t, root, fakeCompleter{})

	out := callOK[VerifyResult](t, cs, ToolVerify, map[string]any{"id_or_path": id})
	assert.Equal(t, 0, out.FindingsProcessed, "no eligible skeptic: no finding sent through a live skeptic")
	assert.Equal(t, 1, out.VerdictCounts.Unverifiable, "no eligible skeptic -> unverifiable")
	assert.Equal(t, 0, out.VerdictCounts.Confirmed)
	assert.Equal(t, 0, out.VerdictCounts.Refuted)

	// Artifacts are emitted: verification.json and the "verify" manifest stage.
	assert.FileExists(t, filepath.Join(root, ".atcr", "reviews", id, "reconciled", "verification.json"))
}

// TestHandleVerify_MissingReconciled: atcr_verify on a review without
// reconciled/findings.json returns the reconcile-first error (AC 04-03 / 04-04
// Error Scenario 1 — identical message across entry points).
func TestHandleVerify_MissingReconciled(t *testing.T) {
	root := t.TempDir()
	writeReviewConfig(t, root)
	// A review dir with a completion signal but no reconciled/findings.json.
	id := "2026-06-10_noverify"
	dir := filepath.Join(root, ".atcr", "reviews", id)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"a","head":"HEAD","roster":["greta"],"partial":false,"stages":["review"]}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sources", "pool", "summary.json"),
		[]byte(`{"total":1,"succeeded":1,"failed":0,"partial":false}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "latest"), []byte(id+"\n"), 0o644))
	cs := connectTest(t, root, fakeCompleter{})

	msg := callErr(t, cs, ToolVerify, map[string]any{"id_or_path": id})
	assert.Contains(t, msg, "no reconciled findings found")
	assert.Contains(t, msg, "atcr reconcile")
}

// TestHandleReconcile_RequireVerified: MCP atcr_reconcile with require_verified
// and a CRITICAL finding that carries no verification block (verify never ran)
// passes the gate — nil verification is not VERIFIED (AC 05-02 Edge Case 2 /
// Scenario 4). Without require_verified, the same CRITICAL fails a HIGH gate.
func TestHandleReconcile_RequireVerified(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	reviewFixture(t, root) // one CRITICAL finding, nil verification
	cs := connectTest(t, root, fakeCompleter{})

	strict := callOK[ReconcileResult](t, cs, ToolReconcile,
		map[string]any{"fail_on": "HIGH", "require_verified": true})
	assert.True(t, strict.Pass, "no VERIFIED finding -> require_verified gate passes")
	assert.Empty(t, strict.Findings)

	plain := callOK[ReconcileResult](t, cs, ToolReconcile, map[string]any{"fail_on": "HIGH"})
	assert.False(t, plain.Pass, "CRITICAL fails a HIGH gate without require_verified")
}

// TestHandleVerify_RegistryPathTraversalRejected verifies an MCP client cannot
// redirect the registry read to an arbitrary file via registryPath (path
// containment, AC 04-03 Security): absolute paths and ".." escapes are rejected.
func TestHandleVerify_RegistryPathTraversalRejected(t *testing.T) {
	root := t.TempDir()
	writeReviewConfig(t, root)
	id := verifyReviewFixture(t, root, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "MEDIUM", Reviewers: []string{"greta"}},
	})
	cs := connectTest(t, root, fakeCompleter{})

	for _, bad := range []string{"/etc/passwd", "../../../etc/passwd", ".."} {
		msg := callErr(t, cs, ToolVerify, map[string]any{"id_or_path": id, "registryPath": bad})
		assert.Contains(t, msg, "invalid registryPath", "registryPath %q must be rejected", bad)
	}
}

// TestHandleVerify_InvalidMinSeverity: an out-of-vocabulary minSeverity is
// rejected before any work, surfacing the structured severity error (handleVerify
// validates minSeverity via parseOptionalSeverity first — AC 04-03/04-04).
func TestHandleVerify_InvalidMinSeverity(t *testing.T) {
	root := t.TempDir()
	writeReviewConfig(t, root)
	id := verifyReviewFixture(t, root, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "MEDIUM", Reviewers: []string{"greta"}},
	})
	cs := connectTest(t, root, fakeCompleter{})

	msg := callErr(t, cs, ToolVerify, map[string]any{"id_or_path": id, "minSeverity": "SEVERE"})
	assert.Contains(t, msg, "invalid severity")
}

// TestHandleVerify_InvalidFailOn: an out-of-vocabulary failOn is rejected with
// the structured severity error before any verification work runs.
func TestHandleVerify_InvalidFailOn(t *testing.T) {
	root := t.TempDir()
	writeReviewConfig(t, root)
	id := verifyReviewFixture(t, root, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "MEDIUM", Reviewers: []string{"greta"}},
	})
	cs := connectTest(t, root, fakeCompleter{})

	msg := callErr(t, cs, ToolVerify, map[string]any{"id_or_path": id, "failOn": "NOPE"})
	assert.Contains(t, msg, "invalid severity")
}

// TestHandleVerify_RequireVerifiedWithoutFailOn: require_verified without a
// failOn gate is a fail-fast error — a strict gate that never runs gives false
// confidence (mirrors handleReconcile's require_verified rule).
func TestHandleVerify_RequireVerifiedWithoutFailOn(t *testing.T) {
	root := t.TempDir()
	writeReviewConfig(t, root)
	id := verifyReviewFixture(t, root, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "MEDIUM", Reviewers: []string{"greta"}},
	})
	cs := connectTest(t, root, fakeCompleter{})

	msg := callErr(t, cs, ToolVerify, map[string]any{"id_or_path": id, "requireVerified": true})
	assert.Contains(t, msg, "requireVerified requires failOn")
}

// TestHandleVerify_InvalidReviewID: a path-traversal id_or_path is rejected by
// resolveReviewDir before any verification work (path-containment invariant).
func TestHandleVerify_InvalidReviewID(t *testing.T) {
	root := t.TempDir()
	writeReviewConfig(t, root)
	cs := connectTest(t, root, fakeCompleter{})

	msg := callErr(t, cs, ToolVerify, map[string]any{"id_or_path": "../../etc/passwd"})
	assert.Contains(t, msg, "invalid review id")
}

// TestHandleVerify_GateStatus: with a failOn threshold the result carries a
// GateStatus computed from the reconciled findings. A HIGH finding fails a LOW
// gate, so Pass is false and the failing count is reported (AC 04-03 Scenario 7).
func TestHandleVerify_GateStatus(t *testing.T) {
	root := t.TempDir()
	writeReviewConfig(t, root)
	id := verifyReviewFixture(t, root, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "MEDIUM", Reviewers: []string{"greta"}},
	})
	cs := connectTest(t, root, fakeCompleter{})

	out := callOK[VerifyResult](t, cs, ToolVerify, map[string]any{"id_or_path": id, "failOn": "LOW"})
	require.NotNil(t, out.GateStatus, "a failOn threshold must populate GateStatus")
	assert.Equal(t, "LOW", out.GateStatus.FailOn)
	assert.False(t, out.GateStatus.Pass, "a HIGH finding fails a LOW gate")
	assert.GreaterOrEqual(t, out.GateStatus.FailingCount, 1)
}

// TestParseOptionalSeverity exercises the optional-severity canonicalizer
// directly: blank (and whitespace) is the unset sentinel, a valid token
// round-trips, and an unknown token is a structured error.
func TestParseOptionalSeverity(t *testing.T) {
	got, err := parseOptionalSeverity("  ")
	require.NoError(t, err)
	assert.Equal(t, "", got, "blank/whitespace means unset")

	got, err = parseOptionalSeverity("HIGH")
	require.NoError(t, err)
	assert.Equal(t, "HIGH", got)

	_, err = parseOptionalSeverity("SEVERE")
	require.Error(t, err, "an unknown severity must be a structured error")
	assert.Contains(t, err.Error(), "invalid severity")
}

// TestLoadVerifyRegistry_ValidRelativePath: a relative registryPath contained
// within the project root is loaded (the success branch of the containment
// check), not rejected as a traversal.
func TestLoadVerifyRegistry_ValidRelativePath(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "cfg")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	regYAML := `providers:
  p:
    api_key_env: ATCR_TEST_KEY
    base_url: https://example.invalid/v1
agents:
  greta:
    provider: p
    model: m-greta
`
	require.NoError(t, os.WriteFile(filepath.Join(sub, "registry.yaml"), []byte(regYAML), 0o644))
	e := &engine{root: root}

	reg, err := e.loadVerifyRegistry("cfg/registry.yaml")
	require.NoError(t, err, "a contained relative registryPath must load")
	require.NotNil(t, reg)
}

// TestLoadVerifyRegistry_DefaultConfigError: with no explicit registryPath and
// no resolvable review config (isolated HOME, no registry anywhere), the default
// LoadReviewConfig failure surfaces as the handler error rather than a nil registry.
func TestLoadVerifyRegistry_DefaultConfigError(t *testing.T) {
	isolateUserConfig(t)
	root := t.TempDir()
	e := &engine{root: root}

	_, err := e.loadVerifyRegistry("")
	require.Error(t, err, "no resolvable registry must surface the config-load error")
}
