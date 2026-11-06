package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/verify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubVerifyRun replaces the verify stage for one test and records the review
// dir it was handed.
func stubVerifyRun(t *testing.T, res verify.Result) *string {
	t.Helper()
	var gotDir string
	orig := verifyRun
	verifyRun = func(_ context.Context, _, reviewDir string, _ *registry.Registry, _ verify.Options) (verify.Result, error) {
		gotDir = reviewDir
		return res, nil
	}
	t.Cleanup(func() { verifyRun = orig })
	return &gotDir
}

// `atcr review --scope <dir> --verify` on a clean tree records HEAD in the
// manifest, which is what verify's harness snapshots (EST-5102).
func TestReviewScopeVerify_RecordsHeadForTheHarness(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	head := gitRevParse(t, "HEAD")
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")
	gotDir := stubVerifyRun(t, verify.Result{FindingsProcessed: 1, VerdictCounts: verify.VerdictCounts{Confirmed: 1}})

	code, out := execCmdCapture(t, "review", "--scope", "internal", "--verify")
	require.Equal(t, 0, code, out)
	require.NotEmpty(t, *gotDir, "verify ran")
	m, err := fanout.ReadManifest(*gotDir)
	require.NoError(t, err)
	assert.Equal(t, head, m.Head)
}

// A tracked file under the scope with uncommitted edits is refused before any
// provider call: the reviewers would read it from disk and the skeptics from HEAD.
func TestReviewScopeVerify_DirtyTreeIsRefused(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join("internal", "c.go"), []byte("package c // edited\n"), 0o644))
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")
	stubVerifyRun(t, verify.Result{})

	code, out := execCmdCapture(t, "review", "--scope", "internal", "--verify")
	assert.Equal(t, 2, code)
	assert.Contains(t, out, "internal/c.go")
	assert.Contains(t, out, "commit or stash")
}

// Without --verify and with no tool-enabled reviewer nothing reads HEAD, so a
// dirty tree is still reviewed as before.
func TestReviewScope_DirtyTreeWithoutVerifyStillReviews(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join("internal", "c.go"), []byte("package c // edited\n"), 0o644))
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	code, out := execCmdCapture(t, "review", "--scope", "internal")
	assert.Equal(t, 0, code, out)
}

// When every finding sent to a skeptic came back unverifiable because the tool
// harness could not be built, `atcr review --verify` exits non-zero and says why
// instead of printing a normal summary.
func TestReviewVerify_HarnessUnavailableExitsNonZero(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initBaselineRepo(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")
	stubVerifyRun(t, verify.Result{
		VerdictCounts:      verify.VerdictCounts{Unverifiable: 18},
		HarnessUnavailable: 18,
		HarnessReason:      "the review recorded no head commit to snapshot",
	})

	code, out := execCmdCapture(t, "review", "--all", "--verify")
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "18 finding(s)")
	assert.Contains(t, out, "the review recorded no head commit to snapshot")
}

// `atcr verify` on a review with no head commit runs the real pipeline: the
// harness cannot be built, the finding degrades to unverifiable, and the
// command exits non-zero naming the cause rather than reporting success.
func TestVerifyCmd_HeadlessReviewExitsNonZero(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TEST_KEY", "secret")
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
  skep:
    provider: p
    model: m-skep
    role: skeptic
    supports_function_calling: true
`), 0o644))
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"), []byte("agents:\n  - bruce\n"), 0o644))
	verifyFixture(t, "r", []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "MEDIUM", Reviewers: []string{"bruce"},
	}})
	manifest := filepath.Join(".atcr", "reviews", "r", "manifest.json")
	require.NoError(t, os.WriteFile(manifest, []byte(`{"head":"","roster":["bruce"],"stages":["review"]}`), 0o644))

	code, out := execCmdCapture(t, "verify", "r")
	assert.Equal(t, 1, code, out)
	assert.Contains(t, out, "no head commit")
	assert.Equal(t, "unverifiable", readFindingVerdict(t, "r"))
}
