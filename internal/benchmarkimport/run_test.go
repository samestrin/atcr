package benchmarkimport

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runIngest(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(context.Background(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRun_RejectsAnUnknownFlag(t *testing.T) {
	code, _, _ := runIngest(t, "--nope")

	assert.Equal(t, 1, code, "an unknown flag is a usage error, not a silent default")
}

func TestRun_ReportsAMissingDatasetFile(t *testing.T) {
	code, _, stderr := runIngest(t, "-dataset", filepath.Join(t.TempDir(), "absent.json"))

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "reading dataset", "the failure names the stage that failed")
}

func TestRun_ReportsAMalformedDataset(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte("{}"), 0o644))

	code, _, stderr := runIngest(t, "-dataset", bad)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "ingest:", "errors are prefixed so they are attributable in CI output")
}

func TestRun_ReportsANonPositiveLimit(t *testing.T) {
	code, _, stderr := runIngest(t, "-dataset", fixturePath, "-limit", "0")

	assert.Equal(t, 1, code, "a zero limit is refused rather than writing an empty suite")
	assert.Contains(t, stderr, "sample size")
}

func TestRun_DefaultsAreTheCommittedSuitesParameters(t *testing.T) {
	// The committed benchmarks/standard-v1 was produced by the tool's defaults.
	// Pinning them keeps a later flag edit from silently making the shipped
	// suite unreproducible.
	code, _, stderr := runIngest(t, "-dataset", fixturePath, "-limit", "1", "-h")

	assert.Equal(t, 0, code, "-h is a successful usage request, not a failure")
	assert.Contains(t, stderr, "benchmarks/standard-v1", "default output directory")
	assert.Contains(t, stderr, "20260807", "default seed")
	assert.Contains(t, stderr, "standard-v1", "default suite identity")
}

func TestGithubToken_PrefersGitHubTokenThenGhToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	assert.Empty(t, githubToken(), "no token configured yields an empty string, not a placeholder")

	t.Setenv("GH_TOKEN", "second")
	assert.Equal(t, "second", githubToken(), "GH_TOKEN is honored when GITHUB_TOKEN is unset")

	t.Setenv("GITHUB_TOKEN", "first")
	assert.Equal(t, "first", githubToken(), "GITHUB_TOKEN wins when both are set")
}
