package reconcile

import (
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
	assert.Equal(t, 1, CountAtOrAbove(findings, SevCritical))
	assert.Equal(t, 2, CountAtOrAbove(findings, SevHigh), "HIGH counts HIGH+CRITICAL")
	assert.Equal(t, 3, CountAtOrAbove(findings, SevMedium))
	assert.Equal(t, 4, CountAtOrAbove(findings, SevLow), "LOW counts everything")
}

func TestRunReconcile_EndToEnd(t *testing.T) {
	reviewDir := t.TempDir()
	// Two sources agreeing on one finding.
	writeFindings(t, filepath.Join(reviewDir, "sources"), "pool/raw/agent/greta/findings.txt",
		"HIGH|a.go:1|same issue text|fix|security|10|ev|greta\n")
	writeFindings(t, filepath.Join(reviewDir, "sources"), "host/findings.txt",
		"HIGH|a.go:1|same issue text|fix|security|10|ev|host\n")

	res, err := RunReconcile(reviewDir, nil, Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Equal(t, ConfHigh, res.Findings[0].Confidence)

	// Artifacts landed under reconciled/.
	assert.FileExists(t, filepath.Join(reviewDir, "reconciled", FindingsTxt))
	assert.FileExists(t, filepath.Join(reviewDir, "reconciled", FindingsJSON))

	// Gate: 1 finding at/above HIGH; 0 at/above CRITICAL.
	assert.Equal(t, 1, CountAtOrAbove(res.Findings, SevHigh))
	assert.Equal(t, 0, CountAtOrAbove(res.Findings, SevCritical))
}

func TestRunReconcile_MissingSourcesDirErrors(t *testing.T) {
	// No sources/ dir at all → discovery error (caller maps to exit 2).
	_, err := RunReconcile(t.TempDir(), nil, Options{ReconciledAt: time.Unix(1, 0).UTC()})
	assert.Error(t, err)
}

func TestRunReconcile_EmptySourcesProducesEmptyArtifacts(t *testing.T) {
	reviewDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(reviewDir, "sources"), 0o755))
	res, err := RunReconcile(reviewDir, nil, Options{ReconciledAt: time.Unix(1, 0).UTC()})
	require.NoError(t, err)
	assert.Equal(t, 0, res.Summary.TotalFindings)
	assert.Equal(t, 0, CountAtOrAbove(res.Findings, SevLow), "no findings → gate passes")
	assert.FileExists(t, filepath.Join(reviewDir, "reconciled", AmbiguousJSON))
}
