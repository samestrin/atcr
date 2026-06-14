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
func TestHandleVerify_Basic(t *testing.T) {
	root := t.TempDir()
	writeReviewConfig(t, root)
	id := verifyReviewFixture(t, root, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "MEDIUM", Reviewers: []string{"greta"}},
	})
	cs := connectTest(t, root, fakeCompleter{})

	out := callOK[VerifyResult](t, cs, ToolVerify, map[string]any{"id_or_path": id})
	assert.Equal(t, 1, out.FindingsProcessed)
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
