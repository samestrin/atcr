package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSeverity_CaseInsensitiveAndInvalid(t *testing.T) {
	for in, want := range map[string]string{
		"critical": SevCritical, "HIGH": SevHigh, " medium ": SevMedium, "Low": SevLow,
	} {
		got, err := ParseSeverity(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got)
	}
	_, err := ParseSeverity("BLOCKER")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid severity threshold: "BLOCKER"`)
}

func TestCountAtOrAbove_ThresholdInclusive(t *testing.T) {
	findings := []Merged{
		{Finding: stream.Finding{Severity: "CRITICAL"}},
		{Finding: stream.Finding{Severity: "HIGH"}},
		{Finding: stream.Finding{Severity: "MEDIUM"}},
		{Finding: stream.Finding{Severity: "LOW"}},
	}
	assert.Equal(t, 1, CountAtOrAbove(findings, SevCritical, false))
	assert.Equal(t, 2, CountAtOrAbove(findings, SevHigh, false), "HIGH counts HIGH+CRITICAL")
	assert.Equal(t, 3, CountAtOrAbove(findings, SevMedium, false))
	assert.Equal(t, 4, CountAtOrAbove(findings, SevLow, false), "LOW counts everything")
}

func TestCountAtOrAbove_ExcludesOutOfScope(t *testing.T) {
	// AC 06-04 Scenario 4: a pre-existing CRITICAL annotated out-of-scope must
	// not trip --fail-on HIGH — the gate counts only in-scope findings.
	findings := []Merged{
		{Finding: stream.Finding{Severity: "CRITICAL", Category: CategoryOutOfScope}},
		{Finding: stream.Finding{Severity: "HIGH", Category: "security"}},
	}
	assert.Equal(t, 1, CountAtOrAbove(findings, SevHigh, false), "only the in-scope HIGH counts")
	assert.Equal(t, 0, CountAtOrAbove(findings[:1], SevHigh, false), "a lone out-of-scope CRITICAL never gates")
}

func TestRunReconcile_EndToEnd(t *testing.T) {
	reviewDir := t.TempDir()
	// Two sources agreeing on one finding.
	writeFindings(t, filepath.Join(reviewDir, "sources"), "pool/raw/agent/greta/findings.txt",
		"HIGH|a.go:1|same issue text|fix|security|10|ev|greta\n")
	writeFindings(t, filepath.Join(reviewDir, "sources"), "host/findings.txt",
		"HIGH|a.go:1|same issue text|fix|security|10|ev|host\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Equal(t, ConfHigh, res.Findings[0].Confidence)

	// Artifacts landed under reconciled/.
	assert.FileExists(t, filepath.Join(reviewDir, "reconciled", FindingsTxt))
	assert.FileExists(t, filepath.Join(reviewDir, "reconciled", FindingsJSON))

	// Gate: 1 finding at/above HIGH; 0 at/above CRITICAL.
	assert.Equal(t, 1, CountAtOrAbove(res.Findings, SevHigh, false))
	assert.Equal(t, 0, CountAtOrAbove(res.Findings, SevCritical, false))
}

// --- ValidateRequireVerified (TD-004) ---

func TestValidateRequireVerified_NoVerifyRan(t *testing.T) {
	dir := t.TempDir() // no verification.json, no manifest.json
	err := ValidateRequireVerified(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verify stage has not run")
}

func TestValidateRequireVerified_VerificationJSONPresent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reconciled", "verification.json"), []byte(`{}`), 0o644))
	assert.NoError(t, ValidateRequireVerified(dir))
}

func TestValidateRequireVerified_ManifestVerifyStage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"stages":["review","verify"]}`), 0o644))
	assert.NoError(t, ValidateRequireVerified(dir))
}

func TestRunReconcile_PreCancelledContext(t *testing.T) {
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "host/findings.txt",
		"HIGH|a.go:1|issue|fix|security|10|ev|host\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RunReconcile(ctx, reviewDir, nil, Options{ReconciledAt: time.Unix(1, 0).UTC()})
	require.ErrorIs(t, err, context.Canceled)
	assert.NoDirExists(t, filepath.Join(reviewDir, "reconciled"), "a cancelled run must not emit artifacts")
}

func TestRunReconcile_MissingSourcesDirErrors(t *testing.T) {
	// No sources/ dir at all → discovery error (caller maps to exit 2).
	_, err := RunReconcile(context.Background(), t.TempDir(), nil, Options{ReconciledAt: time.Unix(1, 0).UTC()})
	assert.Error(t, err)
}

func TestRunReconcile_EmptySourcesProducesEmptyArtifacts(t *testing.T) {
	reviewDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(reviewDir, "sources"), 0o755))
	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{ReconciledAt: time.Unix(1, 0).UTC()})
	require.NoError(t, err)
	assert.Equal(t, 0, res.Summary.TotalFindings)
	assert.Equal(t, 0, CountAtOrAbove(res.Findings, SevLow, false), "no findings → gate passes")
	assert.FileExists(t, filepath.Join(reviewDir, "reconciled", AmbiguousJSON))
}

func TestIsFailing_NormalizesThreshold(t *testing.T) {
	// A hand-edited or externally-produced findings.json may reach CountFailingJSON
	// with a non-canonical threshold; IsFailing must normalize it the same way it
	// normalizes severity.
	assert.True(t, IsFailing("HIGH", "security", nil, " high ", false), "padded lower-case threshold must gate HIGH")
	assert.True(t, IsFailing("CRITICAL", "security", nil, "high", false), "lower-case threshold must gate CRITICAL")
	assert.False(t, IsFailing("LOW", "security", nil, "high", false), "lower-case threshold must not gate LOW")
}
