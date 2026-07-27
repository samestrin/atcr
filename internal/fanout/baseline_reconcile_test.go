package fanout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC 06-03: `atcr reconcile` runs UNMODIFIED over aggregated baseline sources.
// These drive the full baseline chain — PrepareReviewFromRepo -> ExecuteReview
// (fan-out + per-persona merge collapse) -> reconcile.RunReconcile — and assert the
// reconcile engine needs zero baseline-aware changes.

// markerFile builds file content carrying the `// FILE:<name>` marker the
// baselineChunkFindingCompleter routes on, padded to sz bytes so each file lands in
// its own byte-budget chunk.
func markerFile(name string, sz int) string {
	head := fmt.Sprintf("// FILE:%s\n", name)
	if pad := sz - len(head); pad > 0 {
		return head + strings.Repeat("x", pad)
	}
	return head
}

// AC 06-03 Happy Path 1+2+3: a 3-chunk × 2-persona baseline review reconciles into
// exactly one report.md + findings.json; findings reference content from every chunk;
// and a finding both personas flag clusters with exactly the 2 distinct persona names
// in its reviewers array (not 4 — proving per-persona collapse, not per-chunk).
func TestBaselineReconcile_MultiChunkOneReportWithConsensus(t *testing.T) {
	cfg := twoAgentConfig("http://unused") // greta + kai
	cfg.Settings.PayloadByteBudget = 100   // small cap → one file per chunk
	repo := baselineRepo(t, map[string]string{
		"f1.go": markerFile("f1.go", 90),
		"f2.go": markerFile("f2.go", 90),
		"f3.go": markerFile("f3.go", 90),
	})
	out := filepath.Join(t.TempDir(), "review")
	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, out))
	require.NoError(t, err)
	// 3 files × 2 personas = 6 (persona × chunk) slots before the per-persona collapse.
	require.Equal(t, 6, len(prep.Slots), "3 chunks × 2 personas fans out to 6 slots")

	_, err = ExecuteReview(context.Background(), baselineChunkFindingCompleter{}, prep)
	require.NoError(t, err)

	res, err := reconcile.RunReconcile(context.Background(), out, nil,
		reconcile.Options{Root: repo, ReconciledAt: time.Unix(1000, 0).UTC()})
	require.NoError(t, err)
	_ = res

	// Exactly one reconciled report.md + findings.json.
	assert.FileExists(t, filepath.Join(out, "reconciled", "report.md"))
	assert.FileExists(t, filepath.Join(out, "reconciled", "findings.json"))

	findings, err := reconcile.ReadReconciledFindings(out)
	require.NoError(t, err)

	// Findings span every chunk's content (no chunk silently excluded).
	byFile := map[string]reconcile.JSONFinding{}
	for _, f := range findings {
		byFile[f.File] = f
	}
	for _, f := range []string{"f1.go", "f2.go", "f3.go"} {
		_, ok := byFile[f]
		assert.True(t, ok, "reconciled output missing a finding from chunk %s", f)
	}

	// Consensus: each file was flagged identically by BOTH personas, so its clustered
	// finding records exactly the 2 distinct persona names — not 4 (chunk fan-out is
	// collapsed per persona first).
	for _, f := range []string{"f1.go", "f2.go", "f3.go"} {
		assert.ElementsMatch(t, []string{"greta", "kai"}, byFile[f].Reviewers,
			"clustered finding for %s must record 2 distinct personas, not per-chunk duplicates", f)
	}
}

// AC 06-03 Edge Case 1: a baseline scan with no findings from any persona in any
// chunk produces a valid, empty findings set — not an error.
func TestBaselineReconcile_ZeroFindingsEmptySet(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = 100
	repo := baselineRepo(t, map[string]string{
		"f1.go": markerFile("f1.go", 90),
		"f2.go": markerFile("f2.go", 90),
	})
	out := filepath.Join(t.TempDir(), "review")
	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, out))
	require.NoError(t, err)

	// A completer that finds nothing in any chunk.
	_, err = ExecuteReview(context.Background(), emptyCompleter{}, prep)
	require.NoError(t, err)

	_, err = reconcile.RunReconcile(context.Background(), out, nil,
		reconcile.Options{Root: repo, ReconciledAt: time.Unix(1000, 0).UTC()})
	require.NoError(t, err)

	findings, err := reconcile.ReadReconciledFindings(out)
	require.NoError(t, err)
	assert.Empty(t, findings, "a clean baseline scan reconciles to an empty findings set, not an error")
}

// AC 06-03 Edge Case 4: reconciliation over a baseline manifest's zero-valued Range
// completes exactly as for a diff review and never emits a blank/malformed base..head
// range reference into reconciled/report.md.
func TestBaselineReconcile_ZeroRangeNoMalformedRangeInReport(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = 100
	repo := baselineRepo(t, map[string]string{"f1.go": markerFile("f1.go", 90)})
	out := filepath.Join(t.TempDir(), "review")
	prep, err := PrepareReviewFromRepo(context.Background(), cfg, repoReq(repo, out))
	require.NoError(t, err)
	_, err = ExecuteReview(context.Background(), baselineChunkFindingCompleter{}, prep)
	require.NoError(t, err)

	_, err = reconcile.RunReconcile(context.Background(), out, nil,
		reconcile.Options{Root: repo, ReconciledAt: time.Unix(1000, 0).UTC()})
	require.NoError(t, err)

	report, err := os.ReadFile(filepath.Join(out, "reconciled", "report.md"))
	require.NoError(t, err)
	body := string(report)
	require.NotEmpty(t, body)
	assert.NotContains(t, body, "base..head", "no literal base..head placeholder for a range-less scan")
	assert.NotContains(t, body, "..head", "no malformed empty-base range reference")
	assert.NotContains(t, body, "base..", "no malformed empty-head range reference")
}

// AC 06-03 Error Scenario 2: RunReconcile over a malformed baseline review dir (no
// sources/ tree) surfaces the error exactly as it would for a diff review.
func TestBaselineReconcile_MissingSourcesErrors(t *testing.T) {
	dir := t.TempDir() // no sources/ subdir
	_, err := reconcile.RunReconcile(context.Background(), dir, nil,
		reconcile.Options{ReconciledAt: time.Unix(1000, 0).UTC()})
	require.Error(t, err, "a review dir with no sources/ tree must error, same as diff provenance")
}

// emptyCompleter returns no findings for any chunk.
type emptyCompleter struct{}

func (emptyCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "", nil
}
