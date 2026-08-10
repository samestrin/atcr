package fanout

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/stream"
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

// AC 06-03 Edge Case 2 / Performance Requirements: clustering over a realistic-scale
// baseline source set shows no order-of-magnitude regression versus an equivalent
// diff-provenance source set.
//
// Both sides reconcile the SAME finding set — one finding per marker file, per persona,
// over the same repo root — so the only difference is how those sources were produced:
// the baseline side fans out across dozens of chunks and collapses per persona, the diff
// side reviews a range in a single chunk per persona. Only RunReconcile is timed, so the
// measurement isolates discovery + clustering, not fan-out. The bound is an order of
// magnitude over the diff pass with an absolute floor, so sub-millisecond scheduling
// jitter cannot flake the test while a pathological (superlinear) clustering regression
// at baseline scale still fails it.
func TestBaselineReconcile_NoOrderOfMagnitudeClusteringRegressionAtScale(t *testing.T) {
	const fileCount = 60 // "dozens of files across several chunks"
	// Floor for the order-of-magnitude bound: the measured passes are ~25ms each, so
	// 500ms is ~20x headroom on an unloaded machine, while the adaptive 10x term takes
	// over on a loaded runner (a slow diff pass raises the bound with it).
	const clusteringTimeFloor = 500 * time.Millisecond

	files := make(map[string]string, fileCount)
	names := make([]string, 0, fileCount)
	for i := 0; i < fileCount; i++ {
		n := fmt.Sprintf("f%02d.go", i)
		names = append(names, n)
		files[n] = markerFile(n, 90)
	}
	// The marker files land in the SECOND commit so the diff side's range actually
	// contains them: a range-based review drops findings not grounded in its patch.
	repo := baselineRepo(t, map[string]string{"seed.md": "seed\n"})
	base, head := secondCommit(t, repo, files)

	// --- Equivalent diff-provenance source set: one chunk per persona, same findings.
	diffCfg := twoAgentConfig("http://unused")
	diffOut := filepath.Join(t.TempDir(), "diff-review")
	diffReq := reviewReq(repo, repo, base, head)
	diffReq.OutputDir = diffOut
	diffPrep, err := PrepareReview(context.Background(), diffCfg, diffReq)
	require.NoError(t, err)
	require.Equal(t, 2, len(diffPrep.Slots), "a diff review fans out exactly one slot per persona")
	_, err = ExecuteReview(context.Background(), fixedFileFindingsCompleter{files: names}, diffPrep)
	require.NoError(t, err)

	diffStart := time.Now()
	_, err = reconcile.RunReconcile(context.Background(), diffOut, nil,
		reconcile.Options{Root: repo, ReconciledAt: time.Unix(1000, 0).UTC()})
	diffElapsed := time.Since(diffStart)
	require.NoError(t, err)

	// --- Baseline source set: same findings, produced across dozens of chunks.
	baseCfg := twoAgentConfig("http://unused")
	baseCfg.Settings.PayloadByteBudget = 100 // small cap → one file per chunk
	baseOut := filepath.Join(t.TempDir(), "baseline-review")
	basePrep, err := PrepareReviewFromRepo(context.Background(), baseCfg, repoReq(repo, baseOut))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(basePrep.Slots), 2*fileCount,
		"realistic-scale baseline fixture must fan out across dozens of chunks per persona")
	_, err = ExecuteReview(context.Background(), baselineChunkFindingCompleter{}, basePrep)
	require.NoError(t, err)

	baseStart := time.Now()
	_, err = reconcile.RunReconcile(context.Background(), baseOut, nil,
		reconcile.Options{Root: repo, ReconciledAt: time.Unix(1000, 0).UTC()})
	baseElapsed := time.Since(baseStart)
	require.NoError(t, err)

	// The two passes must have done equivalent clustering work — otherwise the timing
	// comparison would be measuring different workloads, not baseline provenance.
	diffFindings, err := reconcile.ReadReconciledFindings(diffOut)
	require.NoError(t, err)
	baseFindings, err := reconcile.ReadReconciledFindings(baseOut)
	require.NoError(t, err)
	require.NotEmpty(t, baseFindings, "realistic-scale baseline fixture must produce findings to cluster")
	require.Equal(t, len(diffFindings), len(baseFindings),
		"baseline and diff source sets must reconcile to the same finding count for the timing comparison to be meaningful")

	t.Logf("clustering %d findings: baseline sources %s vs diff sources %s", len(baseFindings), baseElapsed, diffElapsed)

	bound := 10 * diffElapsed
	if bound < clusteringTimeFloor {
		bound = clusteringTimeFloor
	}
	assert.LessOrEqual(t, baseElapsed, bound,
		"baseline-scale clustering (%s over %d findings) regressed by more than an order of magnitude "+
			"versus the equivalent diff-source reconcile (%s) — escalate to a follow-up story rather than raising this bound",
		baseElapsed, len(baseFindings), diffElapsed)
}

// emptyCompleter returns no findings for any chunk, using the clean-review
// sentinel every persona prompt instructs. It is deliberately NOT "" — an empty
// response is a dead call, which the engine fails over; a reviewer with nothing
// to report says so.
type emptyCompleter struct{}

func (emptyCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return stream.NoFindingsSentinel, nil
}

// fixedFileFindingsCompleter returns one finding per configured file for EVERY
// invocation, regardless of the prompt — so a single-chunk diff review yields the same
// finding set a marker-routed multi-chunk baseline review yields after its per-persona
// collapse. Line format matches baselineChunkFindingCompleter exactly.
type fixedFileFindingsCompleter struct{ files []string }

func (c fixedFileFindingsCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	out := make([]string, 0, len(c.files))
	for _, f := range c.files {
		out = append(out, fmt.Sprintf("HIGH|%s:1|problem %s|fix %s|security|10|evidence %s", f, f, f, f))
	}
	return strings.Join(out, "\n"), nil
}

// secondCommit commits the given path→content files on top of an existing fixture repo
// and returns the base..head range it creates, so a range-based (diff-provenance) review
// sees exactly those files as its patch.
func secondCommit(t *testing.T, dir string, files map[string]string) (base, head string) {
	t.Helper()
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	base = run("rev-parse", "HEAD")
	for p, c := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, p), []byte(c), 0o644))
	}
	run("add", "-A")
	run("commit", "-q", "-m", "second")
	head = run("rev-parse", "HEAD")
	return base, head
}
