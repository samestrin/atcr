package fanout

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/sast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sastFindingsIn returns the reconciled findings the fake scanner contributed.
func sastFindingsIn(t *testing.T, dir string) []reconcile.JSONFinding {
	t.Helper()
	findings, err := reconcile.ReadReconciledFindings(dir)
	require.NoError(t, err)
	var out []reconcile.JSONFinding
	for _, f := range findings {
		for _, r := range f.Reviewers {
			if r == "sast-fake" {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// AC 2 and 3 end to end (Epic 50.0 F1): the review stage hands its scan to
// reconcile through the sidecar, so a real run puts the SAST findings in the
// reconciled report the Fixer reads. No sidecar is written by hand here.
func TestRunReview_SASTFindingsReachTheReconciledReport(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	srv := mockProvider(t)
	withSASTRunner(t, &fakeSASTRunner{sarif: sastFixture}, nil)

	res, err := RunReview(context.Background(), llmclient.New(), sastEnabledConfig(srv.URL), reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	_, err = reconcile.RunReconcile(context.Background(), res.Dir, nil,
		reconcile.Options{Root: repo, ReconciledAt: time.Unix(1000, 0).UTC()})
	require.NoError(t, err)

	got := sastFindingsIn(t, res.Dir)
	require.Len(t, got, 1, "the one in-root scanner finding is merged; the escaping one never is")
	assert.Equal(t, "auth.go", got[0].File)
	assert.Equal(t, 3, got[0].Line)
	assert.Contains(t, got[0].Problem, "call result is never checked")
	assert.Contains(t, got[0].Problem, "go.lang.security.unchecked-call")
	assert.Equal(t, "HIGH", got[0].Confidence)
}

// AC 2: the SAST findings reach the report even when no reviewer found anything.
func TestPrepareReview_SASTFindingsReachTheReportWithNoLLMFindings(t *testing.T) {
	repo, base, head := initRepo(t)
	withSASTRunner(t, &fakeSASTRunner{sarif: sastFixture}, nil)

	prep, err := PrepareReview(context.Background(), sastEnabledConfig("http://unused"), reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	_, err = ExecuteReview(context.Background(), emptyCompleter{}, prep)
	require.NoError(t, err)
	_, err = reconcile.RunReconcile(context.Background(), prep.Dir, nil,
		reconcile.Options{Root: repo, ReconciledAt: time.Unix(1000, 0).UTC()})
	require.NoError(t, err)

	all, err := reconcile.ReadReconciledFindings(prep.Dir)
	require.NoError(t, err)
	require.Len(t, all, 1, "the scanner's finding is the whole report")
	assert.Equal(t, []string{"sast-fake"}, all[0].Reviewers)
}

// A failed scan, a scan with no findings and a disabled scanner write no
// sidecar, so reconcile adds nothing.
func TestPrepareReview_SASTWritesNoSidecarWithoutFindings(t *testing.T) {
	cases := map[string]struct {
		runner  *fakeSASTRunner
		enabled bool
	}{
		"no findings": {runner: &fakeSASTRunner{sarif: `{"version":"2.1.0","runs":[]}`}, enabled: true},
		"failed scan": {runner: &fakeSASTRunner{err: errors.New("exit status 2")}, enabled: true},
		"scanner off": {runner: &fakeSASTRunner{sarif: sastFixture}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			repo, base, head := initRepo(t)
			withSASTRunner(t, tc.runner, nil)
			cfg := twoAgentConfig("http://unused")
			if tc.enabled {
				cfg = sastEnabledConfig("http://unused")
			}
			var prep *PreparedReview
			var err error
			captureStderr(t, func() {
				prep, err = PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
			})
			require.NoError(t, err)
			assert.NoFileExists(t, filepath.Join(prep.Dir, sast.ReportFile))
		})
	}
}

// A reused review dir must not keep an earlier run's sidecar when this run
// produced no findings: reconcile would report findings this scan never made.
func TestWriteSASTSidecar_NoFindingsRemovesAStaleSidecar(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, sast.WriteReport(dir, sast.Report{Scanner: "fake", Findings: []sast.Finding{{File: "a.go", Line: 1, Rule: "r"}}}))

	writeSASTSidecar(dir, sastScan{})
	assert.NoFileExists(t, filepath.Join(dir, sast.ReportFile))

	rep, err := sast.ReadReport(dir)
	require.NoError(t, err)
	assert.Nil(t, rep)
}

func TestWriteSASTSidecar_WritesTheScan(t *testing.T) {
	dir := t.TempDir()
	findings := []sast.Finding{{File: "a.go", Line: 4, Rule: "r", Message: "m"}}
	writeSASTSidecar(dir, sastScan{scanner: "fake", findings: findings})

	rep, err := sast.ReadReport(dir)
	require.NoError(t, err)
	require.NotNil(t, rep)
	assert.Equal(t, "fake", rep.Scanner)
	assert.Equal(t, findings, rep.Findings)
}

// Fail open: a sidecar that cannot be written (or a stale one that cannot be
// removed) prints the visible notice and never aborts the review.
func TestWriteSASTSidecar_WriteErrorFailsOpenWithANotice(t *testing.T) {
	cases := map[string]sastScan{
		"write fails":  {scanner: "fake", findings: []sast.Finding{{File: "a.go", Line: 1, Rule: "r"}}},
		"remove fails": {},
		"bad scanner":  {scanner: "Bad Name", findings: []sast.Finding{{File: "a.go", Line: 1, Rule: "r"}}},
	}
	for name, scan := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if name != "bad scanner" {
				// A non-empty directory at the sidecar path defeats both the
				// rename onto it and its removal.
				require.NoError(t, os.MkdirAll(filepath.Join(dir, sast.ReportFile, "x"), 0o755))
			}
			stderr := captureStderr(t, func() { writeSASTSidecar(dir, scan) })
			assert.Contains(t, stderr, "atcr: notice: SAST pre-filter skipped")
			assert.Contains(t, stderr, "the review continues without deterministic findings")
		})
	}
}
