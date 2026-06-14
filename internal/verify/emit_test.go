package verify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeReconciledFindings writes findings as reconciled/findings.json under
// reviewDir, matching the on-disk contract ReadReconciledFindings reads.
func writeReconciledFindings(t *testing.T, reviewDir string, findings []reconcile.JSONFinding) {
	t.Helper()
	reconDir := filepath.Join(reviewDir, "reconciled")
	require.NoError(t, os.MkdirAll(reconDir, 0o755))
	data, err := json.MarshalIndent(findings, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(reconDir, "findings.json"), append(data, '\n'), 0o644))
}

// --- WriteVerification (AC 03-02) ---

func TestWriteVerification_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled"), 0o755))
	results := []VerificationResult{
		{File: "main.go", Line: 10, Problem: "nil deref", Verdict: "confirmed",
			Skeptic: "agent-b", Model: "gpt-4", Reasoning: "confirmed path", DurationMs: 500},
		{File: "util.go", Line: 42, Problem: "race", Verdict: "refuted",
			Skeptic: "agent-c", Model: "claude-3", Reasoning: "mutex held", DurationMs: 300},
	}
	require.NoError(t, WriteVerification(dir, results))

	data, err := os.ReadFile(filepath.Join(dir, "reconciled", "verification.json"))
	require.NoError(t, err)
	var got VerificationFile
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Len(t, got.Findings, 2)
	assert.Equal(t, 1, got.VerdictCounts.Confirmed)
	assert.Equal(t, 1, got.VerdictCounts.Refuted)
	assert.Equal(t, 0, got.VerdictCounts.Unverifiable)
	assert.NotEmpty(t, got.VerifiedAt)
}

func TestWriteVerification_EmptyResults(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled"), 0o755))
	require.NoError(t, WriteVerification(dir, []VerificationResult{}))

	data, err := os.ReadFile(filepath.Join(dir, "reconciled", "verification.json"))
	require.NoError(t, err)
	var got VerificationFile
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Empty(t, got.Findings)
	assert.Equal(t, VerdictCounts{}, got.VerdictCounts)
}

func TestWriteVerification_TrippedBudgetsSerializeAsArray(t *testing.T) {
	// AC 03-02 EC1: nil TrippedBudgets must serialize as [] not null.
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled"), 0o755))
	results := []VerificationResult{
		{File: "a.go", Line: 1, Problem: "p", Verdict: "unverifiable", Skeptic: "s", Reasoning: "", TrippedBudgets: nil},
	}
	require.NoError(t, WriteVerification(dir, results))

	data, err := os.ReadFile(filepath.Join(dir, "reconciled", "verification.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"trippedBudgets": []`)
	assert.NotContains(t, string(data), `"trippedBudgets": null`)
}

func TestWriteVerification_NonCanonicalVerdictCounted(t *testing.T) {
	// 3.2.A MEDIUM: verdict counting normalizes casing/whitespace so a
	// non-canonical verdict is not silently dropped from verdictCounts.
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled"), 0o755))
	results := []VerificationResult{
		{File: "a.go", Line: 1, Problem: "p", Verdict: "Confirmed", Skeptic: "s"},
		{File: "b.go", Line: 2, Problem: "q", Verdict: " refuted ", Skeptic: "s"},
	}
	require.NoError(t, WriteVerification(dir, results))

	data, err := os.ReadFile(filepath.Join(dir, "reconciled", "verification.json"))
	require.NoError(t, err)
	var got VerificationFile
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, 1, got.VerdictCounts.Confirmed)
	assert.Equal(t, 1, got.VerdictCounts.Refuted)
}

func TestWriteVerification_CreatesReconciledDir(t *testing.T) {
	// AC 03-02 EC2: reconciled/ absent → created before write.
	dir := t.TempDir()
	require.NoError(t, WriteVerification(dir, []VerificationResult{}))
	assert.FileExists(t, filepath.Join(dir, "reconciled", "verification.json"))
}

// --- ReEmitFindings (AC 03-03) ---

func TestReEmitFindings_VerificationBlocks(t *testing.T) {
	dir := t.TempDir()
	writeReconciledFindings(t, dir, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "main.go", Line: 10, Problem: "nil deref", Confidence: "HIGH", Reviewers: []string{"agent-a"}},
		{Severity: "MEDIUM", File: "util.go", Line: 5, Problem: "leak", Confidence: "MEDIUM", Reviewers: []string{"agent-a"}},
	})
	verdicts := map[FindingKey]*reconcile.Verification{
		{File: "main.go", Line: 10, Problem: "nil deref"}: {Verdict: "confirmed", Skeptic: "agent-b", Notes: "path valid"},
		{File: "util.go", Line: 5, Problem: "leak"}:       {Verdict: "confirmed", Skeptic: "agent-b", Notes: "leak real"},
	}
	require.NoError(t, ReEmitFindings(dir, verdicts))

	updated, err := reconcile.ReadReconciledFindings(dir)
	require.NoError(t, err)
	require.Len(t, updated, 2)
	for _, f := range updated {
		assert.Equal(t, "VERIFIED", f.Confidence)
		require.NotNil(t, f.Verification)
		assert.Equal(t, "confirmed", f.Verification.Verdict)
		assert.Equal(t, "agent-b", f.Verification.Skeptic)
	}
}

func TestReEmitFindings_RefutedDemoted(t *testing.T) {
	dir := t.TempDir()
	writeReconciledFindings(t, dir, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "main.go", Line: 10, Problem: "nil deref", Confidence: "HIGH", Reviewers: []string{"agent-a"}},
	})
	verdicts := map[FindingKey]*reconcile.Verification{
		{File: "main.go", Line: 10, Problem: "nil deref"}: {Verdict: "refuted", Skeptic: "agent-b", Notes: "false positive"},
	}
	require.NoError(t, ReEmitFindings(dir, verdicts))

	updated, err := reconcile.ReadReconciledFindings(dir)
	require.NoError(t, err)
	require.Len(t, updated, 1, "refuted finding retained, not deleted")
	assert.Equal(t, "LOW", updated[0].Confidence)
	require.NotNil(t, updated[0].Verification)
	assert.Equal(t, "refuted", updated[0].Verification.Verdict)
}

func TestReEmitFindings_UnmatchedUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeReconciledFindings(t, dir, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "main.go", Line: 10, Problem: "verified one", Confidence: "HIGH", Reviewers: []string{"agent-a"}},
		{Severity: "LOW", File: "x.go", Line: 1, Problem: "untouched", Confidence: "MEDIUM", Reviewers: []string{"agent-a"}},
	})
	verdicts := map[FindingKey]*reconcile.Verification{
		{File: "main.go", Line: 10, Problem: "verified one"}: {Verdict: "confirmed", Skeptic: "agent-b"},
	}
	require.NoError(t, ReEmitFindings(dir, verdicts))

	updated, err := reconcile.ReadReconciledFindings(dir)
	require.NoError(t, err)
	require.Len(t, updated, 2)
	byProblem := map[string]reconcile.JSONFinding{}
	for _, f := range updated {
		byProblem[f.Problem] = f
	}
	assert.Equal(t, "VERIFIED", byProblem["verified one"].Confidence)
	assert.Equal(t, "MEDIUM", byProblem["untouched"].Confidence, "unmatched finding keeps v1 confidence")
	assert.Nil(t, byProblem["untouched"].Verification, "unmatched finding has no verification block")
}

func TestReEmitFindings_EmptyVerdictMap(t *testing.T) {
	dir := t.TempDir()
	orig := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "main.go", Line: 10, Problem: "nil deref", Confidence: "HIGH", Reviewers: []string{"agent-a"}},
	}
	writeReconciledFindings(t, dir, orig)
	require.NoError(t, ReEmitFindings(dir, map[FindingKey]*reconcile.Verification{}))

	updated, err := reconcile.ReadReconciledFindings(dir)
	require.NoError(t, err)
	require.Len(t, updated, 1)
	assert.Equal(t, "HIGH", updated[0].Confidence)
	assert.Nil(t, updated[0].Verification)
}

func TestReEmitFindings_MissingFile(t *testing.T) {
	dir := t.TempDir()
	err := ReEmitFindings(dir, map[FindingKey]*reconcile.Verification{})
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// --- UpdateManifestStage (AC 03-04) ---

func TestUpdateManifestStage_AppendsVerify(t *testing.T) {
	dir := t.TempDir()
	m := payload.Manifest{Base: "abc", Head: "def", Stages: []string{"review"}}
	require.NoError(t, payload.WriteManifest(filepath.Join(dir, "manifest.json"), &m))

	require.NoError(t, UpdateManifestStage(dir))

	var got payload.Manifest
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, []string{"review", "verify"}, got.Stages)
}

func TestUpdateManifestStage_Idempotent(t *testing.T) {
	dir := t.TempDir()
	m := payload.Manifest{Base: "abc", Head: "def", Stages: []string{"review", "verify"}}
	require.NoError(t, payload.WriteManifest(filepath.Join(dir, "manifest.json"), &m))

	require.NoError(t, UpdateManifestStage(dir))

	var got payload.Manifest
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, []string{"review", "verify"}, got.Stages, "no duplicate verify")
}

func TestUpdateManifestStage_PreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	raw := `{"base":"abc","head":"def","stages":["review"],"futureField":"survive"}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(raw), 0o644))

	require.NoError(t, UpdateManifestStage(dir))

	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, []any{"review", "verify"}, got["stages"])
	assert.Equal(t, "survive", got["futureField"], "unknown fields must survive round-trip")
}

func TestUpdateManifestStage_MissingFile(t *testing.T) {
	dir := t.TempDir()
	err := UpdateManifestStage(dir)
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// --- UpdateSummaryVerdicts (AC 03-04) ---

func TestUpdateSummaryVerdicts_Counts(t *testing.T) {
	dir := t.TempDir()
	reconDir := filepath.Join(dir, "reconciled")
	require.NoError(t, os.MkdirAll(reconDir, 0o755))
	summary := map[string]any{"total_findings": 10}
	data, _ := json.MarshalIndent(summary, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(reconDir, "summary.json"), data, 0o644))

	require.NoError(t, UpdateSummaryVerdicts(dir, VerdictCounts{Confirmed: 5, Refuted: 2, Unverifiable: 1}))

	updated, err := os.ReadFile(filepath.Join(reconDir, "summary.json"))
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(updated, &got))
	assert.Equal(t, float64(10), got["total_findings"], "existing fields preserved")
	vc, ok := got["verdictCounts"].(map[string]any)
	require.True(t, ok, "verdictCounts present")
	assert.Equal(t, float64(5), vc["confirmed"])
	assert.Equal(t, float64(2), vc["refuted"])
	assert.Equal(t, float64(1), vc["unverifiable"])
}

func TestUpdateSummaryVerdicts_PreservesLargeIntegers(t *testing.T) {
	dir := t.TempDir()
	reconDir := filepath.Join(dir, "reconciled")
	require.NoError(t, os.MkdirAll(reconDir, 0o755))
	raw := `{"total_findings": 9223372036854775807, "epoch_ms": 1700000000000}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(reconDir, "summary.json"), []byte(raw), 0o644))

	require.NoError(t, UpdateSummaryVerdicts(dir, VerdictCounts{Confirmed: 1}))

	updated, err := os.ReadFile(filepath.Join(reconDir, "summary.json"))
	require.NoError(t, err)
	assert.Contains(t, string(updated), "9223372036854775807", "large int must survive round-trip unchanged")
	assert.Contains(t, string(updated), "1700000000000", "large epoch_ms must survive round-trip unchanged")
}

func TestUpdateSummaryVerdicts_MissingFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled"), 0o755))
	err := UpdateSummaryVerdicts(dir, VerdictCounts{})
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestUpdateSummaryVerdicts_Malformed(t *testing.T) {
	dir := t.TempDir()
	reconDir := filepath.Join(dir, "reconciled")
	require.NoError(t, os.MkdirAll(reconDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(reconDir, "summary.json"), []byte("{not json"), 0o644))
	err := UpdateSummaryVerdicts(dir, VerdictCounts{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing summary.json")
}

// --- additional manifest coverage ---

func TestUpdateManifestStage_NoStagesField(t *testing.T) {
	// AC 03-04 EC1: a 1.x manifest without a stages field is seeded with "review"
	// before "verify" is appended.
	dir := t.TempDir()
	raw := `{"base":"abc","head":"def","per_agent_payload":{}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(raw), 0o644))

	require.NoError(t, UpdateManifestStage(dir))

	var got payload.Manifest
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, []string{"review", "verify"}, got.Stages)
}

func TestUpdateSummaryVerdicts_NullContent(t *testing.T) {
	// A summary.json holding the literal `null` unmarshals to a nil map; the
	// defensive guard initializes a fresh object before adding verdictCounts.
	dir := t.TempDir()
	reconDir := filepath.Join(dir, "reconciled")
	require.NoError(t, os.MkdirAll(reconDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(reconDir, "summary.json"), []byte("null"), 0o644))

	require.NoError(t, UpdateSummaryVerdicts(dir, VerdictCounts{Confirmed: 3}))

	data, err := os.ReadFile(filepath.Join(reconDir, "summary.json"))
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	vc := got["verdictCounts"].(map[string]any)
	assert.Equal(t, float64(3), vc["confirmed"])
}

func TestUpdateManifestStage_Malformed(t *testing.T) {
	// AC 03-04 Error Scenario 3: a malformed manifest is a parse error, file untouched.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{not json"), 0o644))
	err := UpdateManifestStage(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing manifest.json")
}

// --- atomic-write error paths ---

func TestWriteVerification_MkdirError(t *testing.T) {
	// reviewDir is a regular file, so MkdirAll(reviewDir/reconciled) fails.
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	err := WriteVerification(file, []VerificationResult{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating reconciled dir")
}

func TestWriteFileAtomic_BadDir(t *testing.T) {
	// A path whose parent directory does not exist fails at temp-file creation.
	err := writeFileAtomic(filepath.Join(t.TempDir(), "missing-subdir", "f.json"), []byte("data"))
	require.Error(t, err)
}
