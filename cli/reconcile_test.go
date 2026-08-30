package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/circuitbreaker"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/scorecard"
	reclib "github.com/samestrin/atcr/reconcile"
)

// countScorecardLines totals the JSONL record lines in the isolated per-user
// scorecard store, so a test can assert how many records a reconcile run wrote
// (or that --no-scorecard wrote none). A missing store directory counts as zero.
func countScorecardLines(t *testing.T) int {
	t.Helper()
	dir, err := scorecard.DefaultDir()
	require.NoError(t, err)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	total := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		for _, ln := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if strings.TrimSpace(ln) != "" {
				total++
			}
		}
	}
	return total
}

// isolate chdirs into a fresh temp working dir, points HOME/XDG at another temp
// dir, and resets process-global state (the circuitbreaker.DefaultRegistry) both
// before and after the test, so tests stay hermetic against the dev machine and
// against each other.
func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(t.TempDir())
	circuitbreaker.DefaultRegistry.Reset()
	t.Cleanup(circuitbreaker.DefaultRegistry.Reset)
}

// TestIsolateCleanupResetsBreakers_Trip opens the circuit for a provider after
// calling isolate. The paired Observe test below then proves isolate's cleanup
// reset the process-global registry: without a t.Cleanup reset the breaker
// stays open and leaks forward into tests that never called isolate.
func TestIsolateCleanupResetsBreakers_Trip(t *testing.T) {
	isolate(t)
	b := circuitbreaker.DefaultRegistry.Get("p-cleanup-check")
	for i := 0; i < circuitbreaker.DefaultThreshold; i++ {
		b.RecordFailure()
	}
	require.False(t, b.Allow(), "breaker should be open after threshold failures")
}

func TestIsolateCleanupResetsBreakers_Observe(t *testing.T) {
	// Deliberately does NOT call isolate — it observes whether breaker state
	// leaked forward from the Trip test above (tests run in source order).
	require.True(t, circuitbreaker.DefaultRegistry.Get("p-cleanup-check").Allow(),
		"breaker state leaked forward from a test that called isolate")
}

// touchFiles creates the given repo-root-relative source files so reconcile's
// path-validation stage does not flag them as hallucinated in tests that intend
// to exercise local-debt persistence.
func touchFiles(t *testing.T, files ...string) {
	t.Helper()
	for _, f := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(f), 0o755))
		require.NoError(t, os.WriteFile(f, []byte("package x\n"), 0o644))
	}
}

// execCmd runs the atcr command tree with args and returns the resolved exit
// code (the same mapping main() applies).
func execCmd(t *testing.T, args ...string) int {
	t.Helper()
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.ExecuteContext(context.Background())
	return exitCode(err)
}

// fixtureReview writes a review dir under ./.atcr/reviews/<id> with the given
// per-source findings bodies (header prepended) and a .atcr/latest pointer.
func fixtureReview(t *testing.T, id string, files map[string]string) {
	t.Helper()
	base := filepath.Join(".atcr", "reviews", id)
	for rel, body := range files {
		full := filepath.Join(base, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte("# atcr-findings/v1\n"+body), 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(base, "reconciled"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "latest"), []byte(id+"\n"), 0o644))
}

// --- Reconcile logger wiring (sprint 4.0, task 3.5) -----------------------

// runReconcileWithLogger drives runReconcile directly (bypassing the root
// PersistentPreRunE) with a buffer-backed context logger and the given args, so
// a test can assert on the diagnostic output the context logger captures.
func runReconcileWithLogger(t *testing.T, ctxLogBuf *bytes.Buffer, errBuf *bytes.Buffer, args ...string) {
	t.Helper()
	logger, err := log.New("info", "text", ctxLogBuf)
	require.NoError(t, err)
	cmd := newReconcileCmd()
	cmd.SetContext(log.NewContext(context.Background(), logger))
	cmd.SetOut(io.Discard)
	cmd.SetErr(errBuf)
	require.NoError(t, cmd.ParseFlags(args))
	_ = runReconcile(cmd, cmd.Flags().Args()) // gate may return exit-1; not asserted here
}

// TestRunReconcile_RequireVerifiedWarning verifies the --require-verified-without-
// verify warning is emitted at the default info level through the context logger.
func TestRunReconcile_RequireVerifiedWarning(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n",
	})
	var logBuf, errBuf bytes.Buffer
	runReconcileWithLogger(t, &logBuf, &errBuf, "--require-verified", "--fail-on", "LOW", "r")

	assert.Contains(t, logBuf.String(), "--require-verified set but verify never ran",
		"the warning must be visible at the default info level")
}

// TestRunReconcile_UsesContextLogger verifies the warning routes through the
// context logger and NOT directly to the command's stderr.
func TestRunReconcile_UsesContextLogger(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n",
	})
	var logBuf, errBuf bytes.Buffer
	runReconcileWithLogger(t, &logBuf, &errBuf, "--require-verified", "--fail-on", "LOW", "r")

	assert.Contains(t, logBuf.String(), "--require-verified set but",
		"diagnostic must reach the context logger")
	assert.NotContains(t, errBuf.String(), "--require-verified set but",
		"diagnostic must not bypass the logger to direct stderr")
}

// TestRunReconcile_NoSlogDefault verifies reconcile relies on the FromContext
// discard fallback (not slog.Default): with no logger in context it runs without
// panicking.
func TestRunReconcile_NoSlogDefault(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n",
	})
	cmd := newReconcileCmd()
	cmd.SetContext(context.Background()) // no logger → discard fallback
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.ParseFlags([]string{"r"}))
	require.NotPanics(t, func() { _ = runReconcile(cmd, cmd.Flags().Args()) })
}

// TestRunReconcile_ConsensusLevelLoggedOnFailure verifies the resolved consensus
// level is logged even when RunReconcile itself fails — the failed run is exactly
// where an operator most needs to know which configuration was in effect, and a
// post-run-only log line never fires on that path.
func TestRunReconcile_ConsensusLevelLoggedOnFailure(t *testing.T) {
	isolate(t)
	// A review dir with no sources/ makes RunReconcile fail at Discover, after
	// the consensus level has been resolved.
	base := filepath.Join(".atcr", "reviews", "r")
	require.NoError(t, os.MkdirAll(filepath.Join(base, "reconciled"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "latest"), []byte("r\n"), 0o644))

	var logBuf, errBuf bytes.Buffer
	runReconcileWithLogger(t, &logBuf, &errBuf, "--consensus", "lenient", "r")

	assert.Contains(t, logBuf.String(), "consensus filter level resolved",
		"the resolved level must be logged even when the reconcile fails")
	assert.Contains(t, logBuf.String(), "lenient",
		"the log must name the level that was in effect")
}

// TestRunReconcile_UnresolvedFilteredLogged verifies the Tier 4
// content-resolution count is surfaced on the CLI: the consensus filter has had
// its own post-run line since epic 35.9.1, but unresolved_filtered showed up
// only inside report.md and summary.json, so `atcr reconcile` printed a
// quietly smaller finding count with no stated cause.
func TestRunReconcile_UnresolvedFilteredLogged(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n",
	})
	var logBuf, errBuf bytes.Buffer
	runReconcileWithLogger(t, &logBuf, &errBuf, "r")

	assert.Contains(t, logBuf.String(), "tier-4 content resolution",
		"the unresolved sidecar count must be logged post-run beside the consensus line")
	assert.Contains(t, logBuf.String(), "unresolved_filtered=0",
		"even a run routing nothing logs the count, so a nonzero count is never silent")
	// Pinned to the exact value, not just the key: this fixture is not a git
	// repository, so there is no tracked file index and the content check never
	// runs. That is precisely the run whose `unresolved_filtered=0` above reads
	// as a clean bill of health and is not one.
	assert.Contains(t, logBuf.String(), `"tier-4 content resolution" state=disabled`,
		"a 0 count with no state is indistinguishable from a healthy run; the state must ride with it")
}

// TestRunReconcile_NonStrictScorecardWarn verifies the docs/scorecard.md
// caution is surfaced in-run: a non-strict consensus level with scorecard
// emission enabled warns naming --no-scorecard, because the relaxed run's
// records durably depress the reviewer trust priors later strict runs read.
// Strict runs and runs already passing --no-scorecard stay silent.
func TestRunReconcile_NonStrictScorecardWarn(t *testing.T) {
	t.Run("lenient warns", func(t *testing.T) {
		isolate(t)
		fixtureReview(t, "r", trustPanelSources())
		var logBuf, errBuf bytes.Buffer
		runReconcileWithLogger(t, &logBuf, &errBuf, "--consensus", "lenient", "r")
		assert.Contains(t, logBuf.String(), "--no-scorecard",
			"a non-strict run with scorecard emission must name the documented mitigation")
	})
	t.Run("strict is silent", func(t *testing.T) {
		isolate(t)
		fixtureReview(t, "r", trustPanelSources())
		var logBuf, errBuf bytes.Buffer
		runReconcileWithLogger(t, &logBuf, &errBuf, "--consensus", "strict", "r")
		assert.NotContains(t, logBuf.String(), "--no-scorecard")
	})
	t.Run("no-scorecard is silent", func(t *testing.T) {
		isolate(t)
		fixtureReview(t, "r", trustPanelSources())
		var logBuf, errBuf bytes.Buffer
		runReconcileWithLogger(t, &logBuf, &errBuf, "--consensus", "lenient", "--no-scorecard", "r")
		assert.NotContains(t, logBuf.String(), "--no-scorecard")
	})
}

// TestReconcileCmd_InProgressReviewRejected verifies a fan-out-managed review
// (manifest.json present) without its completion signal (summary.json) is a
// usage error rather than a silent partial reconcile.
func TestReconcileCmd_InProgressReviewRejected(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|x|f|sec|10|ev|host\n",
	})
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "reviews", "r", "manifest.json"),
		[]byte(`{"base":"a","head":"b","roster":["greta"],"partial":false}`), 0o644))
	require.Equal(t, 2, execCmd(t, "reconcile", "r"))
}

// TestReconcileCmd_InheritsExternalOutputDir proves the clarified contract for
// epic 1.8: a review created with `atcr review --output-dir <path>` lives at an
// arbitrary absolute path (not under .atcr/reviews/), and `atcr reconcile`
// operates on it via its existing [id-or-path] argument with NO new flag.
func TestReconcileCmd_InheritsExternalOutputDir(t *testing.T) {
	isolate(t)
	ext := filepath.Join(t.TempDir(), "ext-review")
	require.NoError(t, os.MkdirAll(filepath.Join(ext, "sources", "host"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(ext, "reconciled"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ext, "sources", "host", "findings.txt"),
		[]byte("# atcr-findings/v1\nHIGH|a.go:1|boom|fix|security|10|ev|host\n"), 0o644))

	require.Equal(t, 0, execCmd(t, "reconcile", ext))
	require.FileExists(t, filepath.Join(ext, "reconciled", "findings.txt"))
}

func TestReconcileCmd_FailOnExitCodes(t *testing.T) {
	isolate(t)
	fixtureReview(t, "2026-06-10_feat", map[string]string{
		"sources/pool/raw/agent/greta/findings.txt": "HIGH|a.go:1|same issue here|fix|security|10|ev|greta\n",
		"sources/host/findings.txt":                 "HIGH|a.go:1|same issue here|fix|security|10|ev|host\n",
	})

	// No fail-on → exit 0.
	require.Equal(t, 0, execCmd(t, "reconcile", "2026-06-10_feat"))
	// HIGH present, threshold CRITICAL → nothing at/above → exit 0.
	require.Equal(t, 0, execCmd(t, "reconcile", "--fail-on", "CRITICAL", "2026-06-10_feat"))
	// threshold HIGH → a HIGH survives → exit 1.
	require.Equal(t, 1, execCmd(t, "reconcile", "--fail-on", "HIGH", "2026-06-10_feat"))
	// case-insensitive threshold also fails.
	require.Equal(t, 1, execCmd(t, "reconcile", "--fail-on", "high", "2026-06-10_feat"))
}

func TestReconcileCmd_ProjectConfigFailOnGatesByDefault(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|x|f|sec|10|ev|host\n",
	})
	// No .atcr/config.yaml → no default gate → exit 0 even with a HIGH finding.
	require.Equal(t, 0, execCmd(t, "reconcile", "r"))

	// A project config with fail_on: HIGH gates by default (no flag) → exit 1.
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"),
		[]byte("agents:\n  - host\nfail_on: HIGH\n"), 0o644))
	require.Equal(t, 1, execCmd(t, "reconcile", "r"))

	// An explicit --fail-on CRITICAL flag overrides the config default → exit 0.
	require.Equal(t, 0, execCmd(t, "reconcile", "--fail-on", "CRITICAL", "r"))
}

func TestReconcileCmd_BrokenProjectConfigFailsLoudly(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|x|f|sec|10|ev|host\n",
	})
	// A present-but-invalid project config must fail (exit 2), not silently
	// disable the gate.
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"),
		[]byte("agents: []\n"), 0o644)) // empty roster → load error
	require.Equal(t, 2, execCmd(t, "reconcile", "r"))
}

func TestReconcileCmd_InvalidFailOnIsUsageError(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n",
	})
	// Invalid severity → exit 2, validated before any reconcile I/O.
	require.Equal(t, 2, execCmd(t, "reconcile", "--fail-on", "BLOCKER", "r"))
}

func TestReconcileCmd_DefaultsToLatest(t *testing.T) {
	isolate(t)
	fixtureReview(t, "2026-06-10_latest", map[string]string{
		"sources/host/findings.txt": "CRITICAL|a.go:1|boom|f|security|10|ev|host\n",
	})
	// No anchor arg → resolves .atcr/latest → CRITICAL survives → exit 1.
	require.Equal(t, 1, execCmd(t, "reconcile", "--fail-on", "HIGH"))
	// Artifacts were written under the latest review.
	require.FileExists(t, filepath.Join(".atcr", "reviews", "2026-06-10_latest", "reconciled", "findings.txt"))
}

func TestReconcileCmd_MissingReviewIsUsageError(t *testing.T) {
	isolate(t)
	// No review at all → exit 2 (run atcr review first).
	require.Equal(t, 2, execCmd(t, "reconcile"))
	require.Equal(t, 2, execCmd(t, "reconcile", "nonexistent-id"))
}

func TestReconcileCmd_NoScorecardFlagInHelp(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "reconcile", "--help")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "--no-scorecard", "reconcile --help must list the suppression flag")
}

func TestReconcileCmd_NoScorecardSuppressesWrite(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
	})
	require.Equal(t, 0, execCmd(t, "reconcile", "--no-scorecard", "r"))
	require.Equal(t, 0, countScorecardLines(t), "--no-scorecard writes zero records")
}

func TestReconcileCmd_DefaultWritesScorecard(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
	})
	require.Equal(t, 0, execCmd(t, "reconcile", "r"))
	require.Greater(t, countScorecardLines(t), 0,
		"a default reconcile (no flag) still writes scorecard records (regression guard)")
}

func TestReconcileCmd_NoScorecardExitCodeUnchanged(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
	})
	// Success exit code is unchanged...
	require.Equal(t, 0, execCmd(t, "reconcile", "--no-scorecard", "r"))
	// ...and the gate's exit 1 still fires with suppression on (the flag has no
	// effect on reconcile's own exit semantics).
	require.Equal(t, 1, execCmd(t, "reconcile", "--no-scorecard", "--fail-on", "HIGH", "r"))
}

func TestReconcileCmd_NoScorecardNoSideEffects(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
	})
	_, out := execCmdCapture(t, "reconcile", "--no-scorecard", "r")
	// Suppression is silent: no scorecard-related text leaks into output.
	require.NotContains(t, strings.ToLower(out), "scorecard",
		"--no-scorecard must not print any scorecard-related message")
}

func TestReconcileCmd_TraversalIdRejected(t *testing.T) {
	isolate(t)
	// A bare ".." id must not resolve above .atcr/reviews/ — exit 2, not a read
	// of the parent directory.
	require.Equal(t, 2, execCmd(t, "reconcile", ".."))
}

func TestVerifyStageRan_RejectsDirectory(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	reconciled := filepath.Join(base, "reconciled")
	require.NoError(t, os.MkdirAll(reconciled, 0o755))
	// A directory named verification.json must not be treated as a verification
	// artifact; only a regular file should count.
	require.NoError(t, os.MkdirAll(filepath.Join(reconciled, "verification.json"), 0o755))
	require.Error(t, reconcile.ValidateRequireVerified(base))
}

func TestReconcileCmd_SourcesAllowlist(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/pool/raw/agent/greta/findings.txt": "HIGH|a.go:1|p|f|sec|10|ev|greta\n",
		"sources/host/findings.txt":                 "CRITICAL|b.go:2|p|f|sec|10|ev|host\n",
	})
	// Restrict to pool only → host's CRITICAL excluded → --fail-on HIGH still
	// fails on pool's HIGH, but --fail-on CRITICAL passes (host filtered out).
	require.Equal(t, 0, execCmd(t, "reconcile", "--sources", "pool", "--fail-on", "CRITICAL", "r"))
	require.Equal(t, 1, execCmd(t, "reconcile", "--sources", "pool", "--fail-on", "HIGH", "r"))
}

// --- Local TD store persistence hook (sprint 20.1, Story 2) ---------------

// readLocalDebtRecords reads every record from the CWD-rooted local TD store
// (./.atcr/debt), the same store the reconcile persistence hook writes to under
// the Root:"." convention. A missing store is zero records, not an error, so a
// suppressed or zero-finding run reads back empty.
func readLocalDebtRecords(t *testing.T) []localdebt.Record {
	t.Helper()
	recs, err := localdebt.ReadAll(localdebt.DefaultDir("."), localdebt.ReadOpts{})
	require.NoError(t, err)
	return recs
}

// TestRunReconcile_PersistsFindingsToLocalDebt covers AC 02-01 Scenario 1: a
// completed reconcile persists one local-debt record per reconciled finding,
// each carrying schema_version 1, a non-empty id, a run_id matching the
// scorecard runID shape (…-<reviewID>), and the required v1 fields.
func TestRunReconcile_PersistsFindingsToLocalDebt(t *testing.T) {
	isolate(t)
	touchFiles(t, "a.go")
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|leaks a file handle|close it|resource|10|ev|host\n",
	})
	require.Equal(t, 0, execCmd(t, "reconcile", "r"))

	recs := readLocalDebtRecords(t)
	require.Len(t, recs, 1, "one reconciled finding persists exactly one record")
	rec := recs[0]
	require.Equal(t, localdebt.SchemaVersion, rec.SchemaVersion)
	require.NotEmpty(t, rec.ID, "record id is stamped via history.FindingID")
	require.True(t, strings.HasSuffix(rec.RunID, "-r"),
		"run_id must mirror scorecard: ReconciledAt-<reviewID basename>, got %q", rec.RunID)
	require.Equal(t, "HIGH", rec.Severity)
	require.Equal(t, "a.go", rec.File)
	require.Equal(t, 1, rec.Line)
	require.NotEmpty(t, rec.Problem)
	require.NotEmpty(t, rec.Reviewers)
	require.NotEmpty(t, rec.Confidence)
}

// TestRunReconcile_LocalDebtCarriesJustification covers AC 02-01 Scenario 2:
// when a source review.md narrative matches a finding's file:line,
// stampJustifications stamps Justification/SourceReport on the JSONFinding, and
// the persisted record must carry them through (sourced, not re-derived).
func TestRunReconcile_LocalDebtCarriesJustification(t *testing.T) {
	isolate(t)
	touchFiles(t, "a.go")
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|leaks a file handle|close it|resource|10|ev|host\n",
	})
	// A review.md whose heading anchors on a.go:1 gives stampJustifications a
	// tier-3 exact match, so the finding gains a Justification + SourceReport.
	reviewMD := "# Host Review\n\n## a.go:1 leaks a file handle\n\nThe handler opens the file but never closes it on the error path.\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(".atcr", "reviews", "r", "sources", "host", "review.md"),
		[]byte(reviewMD), 0o644))

	require.Equal(t, 0, execCmd(t, "reconcile", "r"))

	recs := readLocalDebtRecords(t)
	require.Len(t, recs, 1)
	require.NotEmpty(t, recs[0].Justification, "justification must carry through when present")
	require.NotNil(t, recs[0].SourceReport, "source_report must carry through when present")
	require.NotEmpty(t, recs[0].SourceReport.Path, "source_report.path is the review-dir-relative back-reference")
}

// TestRunReconcile_LocalDebtOmitsJustificationWhenAbsent covers AC 02-01
// Scenario 3: a finding with no matching narrative persists all required fields
// but omits the optional justification/source_report block.
func TestRunReconcile_LocalDebtOmitsJustificationWhenAbsent(t *testing.T) {
	isolate(t)
	touchFiles(t, "a.go")
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|leaks a file handle|close it|resource|10|ev|host\n",
	})
	require.Equal(t, 0, execCmd(t, "reconcile", "r"))

	recs := readLocalDebtRecords(t)
	require.Len(t, recs, 1)
	require.Empty(t, recs[0].Justification, "no matching narrative → empty justification")
	require.Nil(t, recs[0].SourceReport, "no matching narrative → nil source_report")
	require.NotEmpty(t, recs[0].Severity, "required fields still present")
}

// TestRunReconcile_ZeroFindingsNoLocalDebtWrite covers AC 02-01 Edge Case 1: a
// reconcile that produces zero findings performs no persistence I/O — no
// .atcr/debt/ directory is created.
func TestRunReconcile_ZeroFindingsNoLocalDebtWrite(t *testing.T) {
	isolate(t)
	// A source that produced a findings.txt with a header but no finding rows:
	// zero reconciled findings, the success path.
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "",
	})
	require.Equal(t, 0, execCmd(t, "reconcile", "r"))

	require.Empty(t, readLocalDebtRecords(t), "zero findings → no records")
	_, err := os.Stat(localdebt.DefaultDir("."))
	require.True(t, os.IsNotExist(err), "zero-finding reconcile must not create .atcr/debt/")
}

// TestRunReconcile_DefaultWritesLocalDebt is the regression guard that the
// persistence hook is on by default (no flag), mirroring
// TestReconcileCmd_DefaultWritesScorecard.
func TestRunReconcile_DefaultWritesLocalDebt(t *testing.T) {
	isolate(t)
	touchFiles(t, "a.go")
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
	})
	require.Equal(t, 0, execCmd(t, "reconcile", "r"))
	require.NotEmpty(t, readLocalDebtRecords(t),
		"a default reconcile (no flag) writes local-debt records")
}

// TestReconcileCmd_NoLocalDebtFlagInHelp covers AC 02-02 Scenario 3: the
// --no-local-debt flag is listed in reconcile --help.
func TestReconcileCmd_NoLocalDebtFlagInHelp(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "reconcile", "--help")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "--no-local-debt", "reconcile --help must list the suppression flag")
}

// TestReconcileCmd_NoLocalDebtSuppressesWrite covers AC 02-02 Scenario 2: the
// flag suppresses local-debt persistence for a run while leaving the exit code
// unaffected.
func TestReconcileCmd_NoLocalDebtSuppressesWrite(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
	})
	require.Equal(t, 0, execCmd(t, "reconcile", "--no-local-debt", "r"))
	require.Empty(t, readLocalDebtRecords(t), "--no-local-debt writes zero records")
}

// TestReconcileCmd_NoLocalDebtIndependentOfScorecard covers AC 02-02 Edge Case
// 1: --no-scorecard and --no-local-debt suppress independently.
func TestReconcileCmd_NoLocalDebtIndependentOfScorecard(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
	})
	// --no-local-debt alone: scorecard still writes, local-debt does not.
	require.Equal(t, 0, execCmd(t, "reconcile", "--no-local-debt", "r"))
	require.Greater(t, countScorecardLines(t), 0, "--no-local-debt must not suppress scorecard")
	require.Empty(t, readLocalDebtRecords(t), "--no-local-debt suppresses local debt")
}

// TestRunReconcile_LocalDebtAccumulatesAcrossRuns covers AC 02-03 Scenario 1:
// reconcile runs against different review dirs accumulate additively.
func TestRunReconcile_LocalDebtAccumulatesAcrossRuns(t *testing.T) {
	isolate(t)
	touchFiles(t, "a.go", "b.go", "c.go", "d.go", "e.go")
	fixtureReview(t, "ra", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:10|prob a|fix|security|10|ev|host\n" +
			"HIGH|b.go:20|prob b|fix|security|10|ev|host\n",
	})
	fixtureReview(t, "rb", map[string]string{
		"sources/host/findings.txt": "HIGH|c.go:30|prob c|fix|security|10|ev|host\n" +
			"HIGH|d.go:40|prob d|fix|security|10|ev|host\n" +
			"HIGH|e.go:50|prob e|fix|security|10|ev|host\n",
	})
	require.Equal(t, 0, execCmd(t, "reconcile", "ra"))
	require.Len(t, readLocalDebtRecords(t), 2, "first run persists 2 findings")

	require.Equal(t, 0, execCmd(t, "reconcile", "rb"))
	require.Len(t, readLocalDebtRecords(t), 5,
		"second run accumulates additively (2 + 3), not overwrites")
}

// TestPersistLocalDebt_PopulatesModelFromAgentStatus covers Sprint 30.0 AC
// 01-02 Scenario 1: persistLocalDebt copies the model bound to a finding's
// reviewer (from the fan-out pool summary's AgentStatus.Model) onto the persisted
// record, and stamps it with the current schema version. The reviewer "host" is
// bound to "claude-sonnet-4-6" in the pool summary, so the record's Model must
// carry it.
func TestPersistLocalDebt_PopulatesModelFromAgentStatus(t *testing.T) {
	isolate(t)
	touchFiles(t, "a.go")
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|leaks a file handle|close it|resource|10|ev|host\n",
	})
	// Bind reviewer "host" to a concrete model in the pool summary. Written
	// directly (not via fixtureReview, which prepends a findings header that would
	// corrupt the JSON), mirroring writePoolSummary.
	poolDir := filepath.Join(".atcr", "reviews", "r", "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	summary := `{"agents":[{"agent":"host","model":"claude-sonnet-4-6","tokens_in":200,"tokens_out":60,"duration_ms":1200}],"total":1,"succeeded":1}`
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "summary.json"), []byte(summary), 0o644))

	require.Equal(t, 0, execCmd(t, "reconcile", "r"))

	recs := readLocalDebtRecords(t)
	require.Len(t, recs, 1)
	assert.Equal(t, "claude-sonnet-4-6", recs[0].Model,
		"persistLocalDebt must populate Model from the pool summary's AgentStatus.Model")
	// Version-relative, matching the writer (cli/reconcile.go's
	// SchemaVersion: localdebt.SchemaVersion) and the sibling constructions in
	// telemetry_report_test.go / qualitysignal_test.go. The assertion's intent is
	// "the writer stamps the current schema version", not "the version is N" —
	// pinning a literal here re-breaks this test on every additive bump, which is
	// exactly what it did at the v2 -> v3 bump (Plan 35.13 T1).
	assert.Equal(t, localdebt.SchemaVersion, recs[0].SchemaVersion,
		"the persisted record is stamped with the current localdebt schema version")
}

// TestResolveRecordModel moved to internal/localdebt/reconcile_test.go with the
// resolveRecordModel function itself (Plan 35.13 T6): the helper is now part of the
// shared PersistForReconcile bridge both entry points call, so its unit coverage
// belongs in the package that owns it rather than in one of the two callers.

// TestPersistLocalDebt_CrossModelMergeExcludedFromModelAttribution covers the
// Phase 1 gate fix end-to-end: a finding flagged by two personas that ran on
// different models (a merged consensus finding) persists with an empty Model, so
// it is attribution-incomplete and excluded from per-(persona, model) rows rather
// than corrupting a group with a model a persona never ran on.
func TestPersistLocalDebt_CrossModelMergeExcludedFromModelAttribution(t *testing.T) {
	isolate(t)
	touchFiles(t, "a.go")
	// Two personas independently flag the identical file:line:problem, so reconcile
	// merges them into one record with Reviewers=[bruce, greta].
	fixtureReview(t, "r", map[string]string{
		"sources/pool/raw/agent/bruce/findings.txt": "HIGH|a.go:1|same issue here|fix|security|10|ev|bruce\n",
		"sources/pool/raw/agent/greta/findings.txt": "HIGH|a.go:1|same issue here|fix|security|10|ev|greta\n",
	})
	// bruce and greta ran on DIFFERENT models.
	poolDir := filepath.Join(".atcr", "reviews", "r", "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	summary := `{"agents":[{"agent":"bruce","model":"claude-sonnet-4-6"},{"agent":"greta","model":"gpt-5.1"}],"total":2,"succeeded":2}`
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "summary.json"), []byte(summary), 0o644))

	require.Equal(t, 0, execCmd(t, "reconcile", "r"))

	recs := readLocalDebtRecords(t)
	require.Len(t, recs, 1, "the two personas' identical finding merges to one record")
	assert.Len(t, recs[0].Reviewers, 2, "the merged record unions both reviewers")
	assert.Equal(t, "", recs[0].Model,
		"a cross-model merge is attribution-incomplete: Model is empty, not one reviewer's model")
}

// TestPersistLocalDebt_UnknownModelReviewerNotCreditedUnderSibling covers the
// mixed-attribution merge: two personas flag the identical finding, but only one
// has a recorded pool-summary model. The record resolves to that one model, and
// AggregateQualitySignal credits the subset in ModelReviewers under it — so the
// persona with no recorded model is excluded from per-model credit WITHOUT
// being stripped from the record's Reviewers (the store is the only persistent
// copy; resolve-time credit unions the full list).
func TestPersistLocalDebt_UnknownModelReviewerNotCreditedUnderSibling(t *testing.T) {
	isolate(t)
	touchFiles(t, "a.go")
	// Two personas independently flag the identical file:line:problem, so reconcile
	// merges them into one record with Reviewers=[bruce, greta].
	fixtureReview(t, "r", map[string]string{
		"sources/pool/raw/agent/bruce/findings.txt": "HIGH|a.go:1|same issue here|fix|security|10|ev|bruce\n",
		"sources/pool/raw/agent/greta/findings.txt": "HIGH|a.go:1|same issue here|fix|security|10|ev|greta\n",
	})
	// Only bruce has a recorded model; greta's is unrecorded (no model key).
	poolDir := filepath.Join(".atcr", "reviews", "r", "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	summary := `{"agents":[{"agent":"bruce","model":"claude-sonnet-4-6"},{"agent":"greta"}],"total":2,"succeeded":2}`
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "summary.json"), []byte(summary), 0o644))

	require.Equal(t, 0, execCmd(t, "reconcile", "r"))

	recs := readLocalDebtRecords(t)
	require.Len(t, recs, 1, "the two personas' identical finding merges to one record")
	assert.Equal(t, "claude-sonnet-4-6", recs[0].Model,
		"the one recorded model still resolves for the merged record")
	assert.ElementsMatch(t, []string{"bruce", "greta"}, recs[0].Reviewers,
		"the record retains every reviewer — narrowing would destroy greta's resolve-time credit")
	assert.Equal(t, []string{"bruce"}, recs[0].ModelReviewers,
		"greta has no recorded model and is excluded from per-model credit, carried on ModelReviewers")
}

// TestRunReconcile_LocalDebtDedupsSameFinding covers AC 02-03 Scenario 2:
// re-running reconcile on the same review dir with unchanged findings does not
// duplicate records (write-time dedup by FindingID over full-history ReadAll).
func TestRunReconcile_LocalDebtDedupsSameFinding(t *testing.T) {
	isolate(t)
	touchFiles(t, "a.go")
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|leaks a file handle|close it|resource|10|ev|host\n",
	})
	require.Equal(t, 0, execCmd(t, "reconcile", "r"))
	require.Len(t, readLocalDebtRecords(t), 1)

	// Second run over the identical finding → same FindingID → no new record.
	require.Equal(t, 0, execCmd(t, "reconcile", "r"))
	require.Len(t, readLocalDebtRecords(t), 1,
		"re-running with unchanged findings must not duplicate the record")
}

// TestPersistLocalDebt_WontfixSuppressesReappend covers Epic 24.0 AC #3: once a
// finding is marked wontfix, re-detecting the same finding (same FindingID) must not
// re-persist it. persistLocalDebt seeds its dedup set from a full-history ReadAll
// that includes the terminal wontfix record, so the re-detected finding's id is
// already `seen` and is skipped — suppression is by id-presence, independent of the
// terminal status value. This is a regression lock on existing behavior.
func TestPersistLocalDebt_DedupReadFailureWarnsAboutDuplicateGrowth(t *testing.T) {
	isolate(t)
	dir := localdebt.DefaultDir(".")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	// A shard file that cannot be opened makes ReadAll fail with a permission error,
	// exercising the fail-open branch.
	path := filepath.Join(dir, "2026-07.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	res := reconcile.Result{
		Findings: []reclib.Merged{
			{Finding: reclib.Finding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "flagged false positive", Fix: "n/a", Category: "correctness", EstMinutes: 10}},
		},
		Summary: reclib.Summary{ReconciledAt: "2026-07-13T01:00:00Z"},
	}
	var diag bytes.Buffer
	persistLocalDebt("review", res, ".", true, false, &diag)

	out := diag.String()
	// The warning names the REAL effect: duplicates and unbounded growth. The old
	// "dismissed/wontfix findings may be re-surfaced" claim was false — foldByID
	// rule 1 makes a wontfix survive any re-detection (TD
	// internal/localdebt/reconcile.go:112).
	assert.Contains(t, strings.ToLower(out), "duplicate", "dedup-read failure warning must name the duplicate-append effect")
	assert.Contains(t, strings.ToLower(out), "unbounded", "dedup-read failure warning must name the unbounded-growth effect")
	assert.NotContains(t, strings.ToLower(out), "re-surfaced", "the dismissal claim was false: the fold suppresses wontfix, not the seed")
}

func TestPersistLocalDebt_WontfixSuppressesReappend(t *testing.T) {
	isolate(t)
	dir := localdebt.DefaultDir(".")

	seed := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion,
		RunID:         "2026-07-13T00:00:00Z-wontfix",
		Timestamp:     "2026-07-13T00:00:00Z",
		Severity:      "HIGH",
		File:          "a.go",
		Line:          1,
		Problem:       "flagged false positive",
		Status:        "wontfix",
		Justification: "accepted pattern",
	}
	seed.StampID()
	require.NoError(t, localdebt.Append(dir, seed))

	// Reconcile re-detects the identical finding (same file/line/problem → same id).
	res := reconcile.Result{
		Findings: []reclib.Merged{
			{Finding: reclib.Finding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "flagged false positive", Fix: "n/a", Category: "correctness", EstMinutes: 10}},
		},
		Summary: reclib.Summary{ReconciledAt: "2026-07-13T01:00:00Z"},
	}
	var diag bytes.Buffer
	persistLocalDebt("review", res, ".", true, false, &diag)

	recs := readLocalDebtRecords(t)
	require.Len(t, recs, 1, "a wontfix-marked finding must not be re-appended on re-detection")
	require.Equal(t, "wontfix", recs[0].Status, "only the terminal wontfix record remains")

	// The suppression must actually flow through isClosedStatus: a live `debt resolve
	// --list` must not show the wontfix'd finding. This assertion locks the wontfix
	// semantics; removing wontfix from isClosedStatus would make the finding re-appear.
	out, err := runDebt(t, "resolve")
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "no items",
		"a wontfix-suppressed finding must not appear in the open list")
}

// TestPersistLocalDebt_SkipsGateExcludedFindings verifies that the reconcile
// persistence hook applies the same out-of-scope and refuted exclusions the
// gate uses, so the local TD store's open backlog matches what the gate would
// consider a real finding.
func TestPersistLocalDebt_SkipsGateExcludedFindings(t *testing.T) {
	isolate(t)

	findings := []reclib.Merged{
		{Finding: reclib.Finding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "real bug", Fix: "fix it", Category: "correctness", EstMinutes: 10}},
		{Finding: reclib.Finding{Severity: "CRITICAL", File: "b.go", Line: 2, Problem: "out of scope", Fix: "n/a", Category: reclib.CategoryOutOfScope, EstMinutes: 5}},
		{Finding: reclib.Finding{Severity: "HIGH", File: "c.go", Line: 3, Problem: "refuted", Fix: "n/a", Category: "security", EstMinutes: 10, Verification: &reclib.Verification{Verdict: reclib.VerdictRefuted, Skeptic: "skeptic"}}},
	}
	res := reconcile.Result{
		Findings: findings,
		Summary:  reclib.Summary{ReconciledAt: "2026-07-12T00:00:00Z"},
	}

	var diag bytes.Buffer
	persistLocalDebt("review", res, ".", true, false, &diag)

	recs := readLocalDebtRecords(t)
	require.Len(t, recs, 1, "only the in-scope, non-refuted finding persists")
	require.Equal(t, "a.go", recs[0].File)
}

// TestRunReconcile_PathWarnedFindingSkipped verifies that findings whose cited
// file does not exist under the repo root are not persisted to the local TD
// store, mirroring the Epic 5.0 hallucinated-path signal.
func TestRunReconcile_PathWarnedFindingSkipped(t *testing.T) {
	isolate(t)
	touchFiles(t, "real.go")
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|real.go:1|real problem|fix it|correctness|10|ev|host\n" +
			"HIGH|missing.go:1|phantom problem|fix it|correctness|10|ev|host\n",
	})
	require.Equal(t, 0, execCmd(t, "reconcile", "r"))

	recs := readLocalDebtRecords(t)
	require.Len(t, recs, 1, "only the path-valid finding persists")
	require.Equal(t, "real.go", recs[0].File)
}

// TestGateThresholdReaders_OneWhitespaceSemantic verifies the --fail-on readers
// (failOnThreshold, flag-only; and the tiered readers that back the review and
// reconcile paths) share one semantic: a whitespace-only flag value is unset
// (no gate), not a usage error, and a real value canonicalizes identically.
func TestGateThresholdReaders_OneWhitespaceSemantic(t *testing.T) {
	isolate(t)
	readers := map[string]func(*cobra.Command) (string, error){
		"failOnThreshold": failOnThreshold,
		"resolveGateAndRawConsensus": func(cmd *cobra.Command) (string, error) {
			gate, _, err := resolveGateAndRawConsensus(cmd)
			return gate, err
		},
		"resolveGateAndConsensus": func(cmd *cobra.Command) (string, error) {
			gate, _, err := resolveGateAndConsensus(gateFlagValue(cmd), "")
			return gate, err
		},
	}
	cases := []struct {
		flag string
		want string
	}{
		{"", ""},
		{"  ", ""},
		{"high", "HIGH"},
	}
	for _, tc := range cases {
		for name, reader := range readers {
			cmd := newReconcileCmd()
			require.NoError(t, cmd.Flags().Set("fail-on", tc.flag))
			got, err := reader(cmd)
			require.NoError(t, err, "%s(%q)", name, tc.flag)
			require.Equal(t, tc.want, got, "%s(%q)", name, tc.flag)
		}
	}
}

// reconciledPathWarning reads the first reconciled finding's path_warning from a
// review's reconciled/findings.json, so a test can assert whether path
// validation flagged the finding as hallucinated (Epic 5.0 signal).
func reconciledPathWarning(t *testing.T, id string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(".atcr", "reviews", id, "reconciled", "findings.json"))
	require.NoError(t, err)
	var findings []reconcile.JSONFinding
	require.NoError(t, json.Unmarshal(data, &findings))
	require.NotEmpty(t, findings, "expected at least one reconciled finding")
	return findings[0].PathWarning
}

// TestReconcileCmd_AppliesScorecardTrustPrior (epic 35.9 AC1): `atcr reconcile`
// threads the reviewer trust prior end-to-end. "trusted" has
// DefaultTrustMinRuns of scorecard history at a 1.0 corroboration rate (at/above
// trustHighThreshold) and raises a singleton with no in-run corroboration and no
// PageRank authority (no reviewer pair agrees on anything) — it must still
// reach findings.json, proving RunReconcile's call site actually resolves and
// attaches scorecard.ResolveTrustPriors(), not just tolerates its absence.
func TestReconcileCmd_AppliesScorecardTrustPrior(t *testing.T) {
	isolate(t) // isolate() points HOME/XDG_CONFIG_HOME at a fresh temp dir

	seedTrustedReviewer(t, "trusted")
	fixtureReview(t, "r", trustPanelSources())

	require.Equal(t, 0, execCmd(t, "reconcile", "r"))

	data, err := os.ReadFile(filepath.Join(".atcr", "reviews", "r", "reconciled", "findings.json"))
	require.NoError(t, err)
	var findings []reconcile.JSONFinding
	require.NoError(t, json.Unmarshal(data, &findings))

	var trustedSurvived bool
	for _, f := range findings {
		if f.File == "foo.go" {
			trustedSurvived = true
		}
	}
	assert.True(t, trustedSurvived,
		"atcr reconcile threads the reviewer trust prior through RunReconcile end-to-end")
}

// TestReconcileCmd_RepoFlagValidatesAgainstOtherRepo proves the Epic 22.1 fix:
// --repo threads the reviewed-repo root into path validation, so a finding whose
// cited file exists in <other-repo> (but not the CWD) validates clean instead of
// being falsely flagged "file not found". The control run (default --repo=.)
// still flags the same finding, guarding the common case against regression.
func TestReconcileCmd_RepoFlagValidatesAgainstOtherRepo(t *testing.T) {
	isolate(t) // CWD is an empty temp dir — x.go does not exist here

	// The "other repo" is a separate dir that DOES contain the cited file.
	otherRepo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(otherRepo, "x.go"), []byte("package x\n"), 0o644))

	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|x.go:1|boom|fix|security|10|ev|host\n",
	})

	// With --repo pointing at the other repo, x.go resolves → no path warning.
	require.Equal(t, 0, execCmd(t, "reconcile", "r", "--repo", otherRepo))
	require.Empty(t, reconciledPathWarning(t, "r"),
		"a finding validated against --repo <other-repo> must carry no path warning")

	// Control: default --repo=. (the CWD, where x.go is absent) still flags it.
	require.Equal(t, 0, execCmd(t, "reconcile", "r"))
	require.NotEmpty(t, reconciledPathWarning(t, "r"),
		"the default validation root must still flag a hallucinated path (no regression)")

	// An explicit empty --repo normalizes to "." rather than silently disabling
	// validation (Epic 22.1 hardening): the hallucinated path is still flagged.
	require.Equal(t, 0, execCmd(t, "reconcile", "r", "--repo", ""))
	require.NotEmpty(t, reconciledPathWarning(t, "r"),
		"an empty --repo must normalize to the CWD, not disable path validation")
}

// TestReconcileCmd_RepoFlagNonexistentFails verifies that a nonexistent --repo
// path is rejected with a usage error (exit 2) instead of silently degrading
// path validation and dropping every finding.
func TestReconcileCmd_RepoFlagNonexistentFails(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|x.go:1|boom|fix|security|10|ev|host\n",
	})

	require.Equal(t, 2, execCmd(t, "reconcile", "r", "--repo", "/nonexistent/path"),
		"a nonexistent --repo must fail loudly with exit 2")
}

// --repo-root is the canonical name for the filesystem-repo-root flag on the
// commands where --repo collided with the owner/name slug flag (review/github);
// --repo remains a deprecated hidden alias resolving identically.
func TestRepoRootFlag_CanonicalAndDeprecatedAlias(t *testing.T) {
	dir := t.TempDir()

	canonical := newReconcileCmd()
	require.NoError(t, canonical.Flags().Parse([]string{"--repo-root", dir}))
	gotCanonical, err := normalizeRepoFlag(canonical)
	require.NoError(t, err)

	alias := newReconcileCmd()
	require.NoError(t, alias.Flags().Parse([]string{"--repo", dir}))
	gotAlias, err := normalizeRepoFlag(alias)
	require.NoError(t, err)

	require.Equal(t, gotAlias, gotCanonical,
		"--repo-root and the deprecated --repo alias must resolve identically")

	// --repo-root is the documented spelling on all three path-root commands;
	// the deprecated alias is hidden from help.
	_, reconcileHelp := execCmdCapture(t, "reconcile", "--help")
	require.Contains(t, reconcileHelp, "--repo-root")
	require.NotContains(t, reconcileHelp, "--repo ")

	_, verifyHelp := execCmdCapture(t, "verify", "--help")
	require.Contains(t, verifyHelp, "--repo-root")

	_, diffHelp := execCmdCapture(t, "verify", "diff", "--help")
	require.Contains(t, diffHelp, "--repo-root")
}

// The owner/name slug --repo on review is a different concept from the
// filesystem --repo-root and must never be interpreted as a path.
func TestRepoRootFlag_ReviewRepoStaysSlug(t *testing.T) {
	_, reviewHelp := execCmdCapture(t, "review", "--help")
	require.Contains(t, reviewHelp, "--repo")
	require.Contains(t, reviewHelp, "owner/name")
	require.NotContains(t, reviewHelp, "--repo-root")
}

// --- Cloud sync (--sync-cloud) end-to-end -----------------------------------

// writePoolSummary writes a valid sources/pool/summary.json under the fixture
// review so scorecard.NewCloudSyncRecord can source per-persona metadata. It is
// written directly (NOT via fixtureReview, which prepends a findings header that
// would corrupt the JSON).
func writePoolSummary(t *testing.T, id string) {
	t.Helper()
	poolDir := filepath.Join(".atcr", "reviews", id, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	summary := `{"agents":[{"agent":"host","model":"m","tokens_in":200,"tokens_out":60,"duration_ms":1200}],"total":1,"succeeded":1}`
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "summary.json"), []byte(summary), 0o644))
}

// TestReconcileCmd_SyncCloud_SuccessfulPush covers AC 04-02 Scenario 2: a
// reconcile with --sync-cloud POSTs a Bearer-authed, allowlisted body to the
// (loopback) endpoint and exits 0.
func TestReconcileCmd_SyncCloud_SuccessfulPush(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
	})
	writePoolSummary(t, "r")

	got := false
	auth := ""
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = true
		auth = r.Header.Get("Authorization")
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("ATCR_API_KEY", "valid-key")
	require.Equal(t, 0, execCmd(t, "reconcile", "--sync-cloud", "--cloud-endpoint", srv.URL, "r"))
	require.True(t, got, "the cloud endpoint received no request")
	require.Equal(t, "Bearer valid-key", auth)
	require.Contains(t, string(body), "persona_id_hash")
	require.NotContains(t, string(body), "valid-key", "the API key must never appear in the body")
	require.NotContains(t, string(body), "\"reviewer\"", "raw reviewer identifier must not be in the payload")
}

// TestReconcileCmd_SyncCloud_FlagOmitted_ZeroNetwork covers AC 04-02 EC1.
func TestReconcileCmd_SyncCloud_FlagOmitted_ZeroNetwork(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
	})
	writePoolSummary(t, "r")

	got := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = true }))
	defer srv.Close()

	t.Setenv("ATCR_API_KEY", "valid-key")
	require.Equal(t, 0, execCmd(t, "reconcile", "--cloud-endpoint", srv.URL, "r"))
	require.False(t, got, "no --sync-cloud → zero cloud network calls")
}

// TestReconcileCmd_SyncCloud_InvalidKey_ExitsAuth covers AC 04-04: a 401 or 403
// from the endpoint maps to exitAuth (3).
func TestReconcileCmd_SyncCloud_InvalidKey_ExitsAuth(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		isolate(t)
		fixtureReview(t, "r", map[string]string{
			"sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
		})
		writePoolSummary(t, "r")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		t.Setenv("ATCR_API_KEY", "bad-key")
		code := execCmd(t, "reconcile", "--sync-cloud", "--cloud-endpoint", srv.URL, "r")
		srv.Close()
		require.Equal(t, exitAuth, code, "status %d must exit exitAuth", status)
	}
}

// TestReconcileCmd_SyncCloud_ServerError_NonFatal covers AC 04-02 ErrScenario2 /
// AC 04-04 ErrScenario3: a 500 is a non-fatal cloud-sync failure — the
// reconcile's own exit code (0, no gate) is preserved, NOT exitAuth.
func TestReconcileCmd_SyncCloud_ServerError_NonFatal(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
	})
	writePoolSummary(t, "r")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("ATCR_API_KEY", "valid-key")
	require.Equal(t, 0, execCmd(t, "reconcile", "--sync-cloud", "--cloud-endpoint", srv.URL, "r"))
}

// TestReconcileCmd_SyncCloud_MissingKey_FailFast covers AC 04-03: a missing key
// exits exitAuth with zero network (fail fast, before the push).
func TestReconcileCmd_SyncCloud_MissingKey_FailFast(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
	})
	got := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = true }))
	defer srv.Close()

	t.Setenv("ATCR_API_KEY", "")
	require.Equal(t, exitAuth, execCmd(t, "reconcile", "--sync-cloud", "--cloud-endpoint", srv.URL, "r"))
	require.False(t, got, "a missing key must fail fast with zero network")
}

// --- Configurable consensus filter: flag + config surface (epic 35.9.1) -----

// writeProjectConfig writes a minimal .atcr/config.yaml carrying the given extra
// keys, so a test can exercise the config tier of a precedence chain.
func writeProjectConfig(t *testing.T, extra string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"),
		[]byte("agents:\n  - a\n"+extra), 0o644))
}

// TestReconcileCmd_InvalidConsensusExitsTwo (AC2): an out-of-vocabulary level is
// a usage error (exit 2) naming the valid values, mirroring --fail-on.
func TestReconcileCmd_InvalidConsensusExitsTwo(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", trustPanelSources())

	require.Equal(t, 2, execCmd(t, "reconcile", "--consensus", "bogus", "r"))

	// The exit code alone does not prove the message is actionable, and the
	// command tree surfaces usage errors through main() rather than
	// cmd.ErrOrStderr(), so assert the message at its source.
	_, err := resolveConsensusLevel("bogus")
	require.Error(t, err)
	for _, level := range reclib.ConsensusLevels() {
		assert.Contains(t, err.Error(), level, "the usage error must name every valid level")
	}
	assert.Contains(t, err.Error(), "bogus", "the usage error must echo the rejected value")
}

// TestReconcileCmd_InvalidConsensusInConfigExitsTwo (AC2/AC3): the config tier
// is validated at the same call site, so a bad .atcr/config.yaml value is a
// usage error too — config consensus is not validated at load time.
func TestReconcileCmd_InvalidConsensusInConfigExitsTwo(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", trustPanelSources())
	writeProjectConfig(t, "consensus: bogus\n")

	require.Equal(t, 2, execCmd(t, "reconcile", "r"))
}

// TestResolveConsensusLevel_Precedence (AC3) drives the CLI-side resolver
// directly: nothing configured maps to strict, a config value is honored, an
// explicit flag beats the config, and the token is case- and
// whitespace-insensitive (the validateGate convention, not on_overflow's).
func TestResolveConsensusLevel_Precedence(t *testing.T) {
	isolate(t)

	// Nothing configured anywhere → "" from the resolver → strict at the call site.
	got, err := resolveConsensusLevel("")
	require.NoError(t, err)
	assert.Equal(t, reclib.ConsensusStrict, got)

	// Case- and whitespace-insensitive canonicalization.
	for _, raw := range []string{"lenient", "LENIENT", "  Lenient  "} {
		got, err = resolveConsensusLevel(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, reclib.ConsensusLenient, got, raw)
	}

	// Config tier honored when no explicit value is given.
	writeProjectConfig(t, "consensus: off\n")
	got, err = resolveConsensusLevel("")
	require.NoError(t, err)
	assert.Equal(t, reclib.ConsensusOff, got)

	// An explicit value still beats the config tier.
	got, err = resolveConsensusLevel("strict")
	require.NoError(t, err)
	assert.Equal(t, reclib.ConsensusStrict, got)

	// An invalid value is an error at the call site.
	_, err = resolveConsensusLevel("bogus")
	require.Error(t, err)
}

// TestConsensusFlagValue_ExplicitEmptyIsUsageError pins the deliberate
// asymmetry with gateFlagValue: an ABSENT --consensus is unset (the config
// chain decides), but an explicitly set empty or whitespace-only value is a
// usage error. An empty --fail-on can only inherit a STRICTER gate, whereas an
// empty --consensus can inherit a WEAKER one, so `atcr reconcile --consensus
// "$LEVEL"` with an unset shell variable must not silently disable the
// corroboration filter. Mirrors outputDirFromFlags' "--output-dir must not be
// empty" rejection (cli/review.go:115-117).
func TestConsensusFlagValue_ExplicitEmptyIsUsageError(t *testing.T) {
	// Absent flag → unset, no error: the config/registry chain still decides.
	got, err := consensusFlagValue(newReconcileCmd())
	require.NoError(t, err)
	assert.Equal(t, "", got)

	for _, raw := range []string{"", "   "} {
		cmd := newReconcileCmd()
		require.NoError(t, cmd.Flags().Set("consensus", raw))

		_, err := consensusFlagValue(cmd)
		require.Error(t, err, "explicit %q must be rejected, not treated as unset", raw)
		for _, level := range reclib.ConsensusLevels() {
			assert.Contains(t, err.Error(), level,
				"the usage error must name every valid level")
		}
	}
}

// TestReconcileCmd_ExplicitEmptyConsensusExitsTwo drives the same rejection
// end-to-end, so the call-site wiring is locked too: without it runReconcile
// would swallow the error and inherit whatever ~/.config/atcr/registry.yaml
// says. The config tier here says off — the weaker level the empty flag would
// otherwise silently pick up.
func TestReconcileCmd_ExplicitEmptyConsensusExitsTwo(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", trustPanelSources())
	writeProjectConfig(t, "consensus: off\n")

	require.Equal(t, 2, execCmd(t, "reconcile", "--consensus", "", "r"))
}

// consensusSummary reads a reconciled run's summary.json and returns the
// consensus_filtered count, so a test can assert the filter was inert (0)
// rather than inferring it from the surviving-finding set alone.
func consensusSummary(t *testing.T, id string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(".atcr", "reviews", id, "reconciled", "summary.json"))
	require.NoError(t, err)
	var s struct {
		ConsensusFiltered int `json:"consensus_filtered"`
	}
	require.NoError(t, json.Unmarshal(data, &s))
	return s.ConsensusFiltered
}

// TestReconcileCmd_ConsensusOffKeepsEverySingleton (AC1) drives the flag
// end-to-end: off makes the filter inert, so every uncorroborated singleton on a
// 3-reviewer panel reaches findings.json with consensus_filtered at 0.
func TestReconcileCmd_ConsensusOffKeepsEverySingleton(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", trustPanelSources())

	require.Equal(t, 0, execCmd(t, "reconcile", "--consensus", "off", "r"))

	assert.ElementsMatch(t, []string{"foo.go", "bar.go", "baz.go"},
		reconciledFiles(t, filepath.Join(".atcr", "reviews", "r")))
	assert.Equal(t, 0, consensusSummary(t, "r"), "off must leave consensus_filtered at 0")
}

// seedUntrustedReviewer is seedTrustedReviewer's counterpart: it appends
// DefaultTrustMinRuns scorecard records at a 0.0 corroboration rate, so
// scorecard.ResolveTrustPriors() resolves the reviewer at or below the
// reconcile-time LOW-trust threshold and demoteByTrust demotes its singleton to
// ConfLow. That is the only way to produce a ConfLow finding at the CLI layer,
// which is what makes lenient distinguishable from off end-to-end.
func seedUntrustedReviewer(t *testing.T, reviewer string) {
	t.Helper()
	dir, err := scorecard.DefaultDir()
	require.NoError(t, err)
	// The run_id timestamp decides which month file the record lands in, and
	// scorecard.ResolveTrustPriors reads only the month files inside
	// defaultTrustWindow (epic 35.11). A hardcoded date would silently fall out
	// of that window as it aged, disabling the prior this fixture exists to
	// exercise — so stamp "now" and take it once so a loop crossing midnight on
	// the 1st cannot split the fixture across two month files.
	stamp := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < scorecard.DefaultTrustMinRuns; i++ {
		require.NoError(t, scorecard.Append(dir, scorecard.Record{
			SchemaVersion:        1,
			RecordType:           scorecard.RecordTypeReviewer,
			RunID:                fmt.Sprintf("%s-u%02d", stamp, i),
			Reviewer:             reviewer,
			Model:                "m",
			Role:                 "reviewer",
			FindingsRaised:       1,
			FindingsCorroborated: 0,
		}))
	}
}

// TestReconcileCmd_ConsensusLenientKeepsMediumSingletons (AC1): lenient keeps
// MEDIUM-confidence singletons but still sidecars LOW-confidence ones.
//
// "stranger" is seeded as a low-trust reviewer so its singleton is demoted to
// ConfLow — without that, every finding in the panel is ConfMedium and lenient
// would be indistinguishable from off at this layer, letting a wiring bug that
// mapped lenient to off pass unnoticed.
func TestReconcileCmd_ConsensusLenientKeepsMediumSingletons(t *testing.T) {
	isolate(t)
	seedUntrustedReviewer(t, "stranger") // owns bar.go in trustPanelSources
	fixtureReview(t, "r", trustPanelSources())

	require.Equal(t, 0, execCmd(t, "reconcile", "--consensus", "lenient", "r"))

	assert.ElementsMatch(t, []string{"foo.go", "baz.go"},
		reconciledFiles(t, filepath.Join(".atcr", "reviews", "r")),
		"lenient keeps the two ConfMedium singletons")
	assert.Equal(t, 1, consensusSummary(t, "r"),
		"and sidecars exactly the demoted ConfLow one — the assertion that separates lenient from off")
}

// TestReconcileCmd_ConsensusOffKeepsDemotedSingleton (AC6, CLI layer): the same
// low-trust panel under off keeps ALL three findings, proving off is not merely
// an alias for lenient.
func TestReconcileCmd_ConsensusOffKeepsDemotedSingleton(t *testing.T) {
	isolate(t)
	seedUntrustedReviewer(t, "stranger")
	fixtureReview(t, "r", trustPanelSources())

	require.Equal(t, 0, execCmd(t, "reconcile", "--consensus", "off", "r"))

	assert.ElementsMatch(t, []string{"foo.go", "bar.go", "baz.go"},
		reconciledFiles(t, filepath.Join(".atcr", "reviews", "r")),
		"off keeps the demoted ConfLow singleton that lenient sidecars")
	assert.Equal(t, 0, consensusSummary(t, "r"))
}

// TestReconcileCmd_ConsensusStrictMatchesNoFlag (AC1 golden): strict, an
// uppercase STRICT, a whitespace-padded value, and no flag at all reproduce the
// identical pre-change sidecar set.
func TestReconcileCmd_ConsensusStrictMatchesNoFlag(t *testing.T) {
	for _, args := range [][]string{
		{"reconcile", "r"},
		{"reconcile", "--consensus", "strict", "r"},
		{"reconcile", "--consensus", "STRICT", "r"},
		{"reconcile", "--consensus", "  strict  ", "r"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			isolate(t)
			fixtureReview(t, "r", trustPanelSources())

			require.Equal(t, 0, execCmd(t, args...))

			assert.Empty(t, reconciledFiles(t, filepath.Join(".atcr", "reviews", "r")),
				"strict sidecars every uncorroborated singleton")
			assert.Equal(t, 3, consensusSummary(t, "r"))
		})
	}
}

// TestReconcileCmd_ConsensusConfigPrecedence (AC3) exercises the config tier
// end-to-end: .atcr/config.yaml consensus is honored without a flag, and an
// explicit flag overrides it.
func TestReconcileCmd_ConsensusConfigPrecedence(t *testing.T) {
	t.Run("config honored without flag", func(t *testing.T) {
		isolate(t)
		fixtureReview(t, "r", trustPanelSources())
		writeProjectConfig(t, "consensus: lenient\n")

		require.Equal(t, 0, execCmd(t, "reconcile", "r"))
		assert.Len(t, reconciledFiles(t, filepath.Join(".atcr", "reviews", "r")), 3)
		assert.Equal(t, 0, consensusSummary(t, "r"))
	})

	t.Run("flag overrides config", func(t *testing.T) {
		isolate(t)
		fixtureReview(t, "r", trustPanelSources())
		writeProjectConfig(t, "consensus: off\n")

		require.Equal(t, 0, execCmd(t, "reconcile", "--consensus", "strict", "r"))
		assert.Empty(t, reconciledFiles(t, filepath.Join(".atcr", "reviews", "r")))
		assert.Equal(t, 3, consensusSummary(t, "r"))
	})
}

// TestReconcileCmd_LongHelpDocumentsConsensus (T3): the command's long help must
// document --consensus, every level, that strict is the default, and what off
// actually does — a flag whose only documentation is a one-line usage string is
// not discoverable.
//
// "off restores pre-14.2 behavior" is specifically NOT what the help may say:
// epic 35.9's trust demotion is gated only by the panel floor, so it still runs
// under off and its ConfLow findings reach findings.json — a confidence tier
// unreachable at reconcile time before 35.9, let alone before 14.2 (asserted in
// TestDemoteByTrust_ObservableViaConsensusLevel). A user who sets off expecting
// the old artifact shape must be told about that caveat here.
func TestReconcileCmd_LongHelpDocumentsConsensus(t *testing.T) {
	long := newReconcileCmd().Long

	assert.Contains(t, long, "--consensus")
	for _, level := range reclib.ConsensusLevels() {
		assert.Contains(t, long, level, "long help must name the %s level", level)
	}
	assert.Contains(t, long, "default", "long help must say which level is the default")
	assert.Contains(t, long, "pre-14.2", "long help must situate off against the pre-14.2 baseline")
	assert.NotRegexp(t, `(?i)restor\w*\s+pre-14\.2`, long,
		"off does NOT restore pre-14.2 behavior — the trust demotion still runs")
	assert.Regexp(t, `(?is)off.*(LOW-confidence|LOW confidence)`, long,
		"long help must warn that off output can still carry LOW-confidence findings")
	assert.Contains(t, long, "consensus:", "long help must point at the config key too")
}

// TestResolveConsensusLevel_BrokenProjectConfigIsUsageError: a present-but-broken
// .atcr/config.yaml is the repo's own config, so it surfaces as a usage error
// (exit 2) rather than being silently skipped — the same asymmetry
// ResolveGateThreshold establishes against the best-effort registry tier.
func TestResolveConsensusLevel_BrokenProjectConfigIsUsageError(t *testing.T) {
	isolate(t)
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"),
		[]byte("agents: []\n"), 0o644)) // an empty roster is a load error

	_, err := resolveConsensusLevel("")
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err), "a broken project config is a usage error")
}

// TestRunReconcile_ResolvesSharedSettingsInOneLoad pins the call-site half of
// the shared-settings single load: runReconcile needs both fail_on and
// consensus, and resolving them through two independent resolvers costs two
// parses of .atcr/config.yaml and — under ATCR_REGISTRY_URL — two separate HTTP
// GETs of the user registry. Because the registry tier is swallowed best-effort,
// one fetch can succeed while the other fails, leaving the gate and the
// consensus level resolved from DIFFERENT tiers inside one run.
func TestRunReconcile_ResolvesSharedSettingsInOneLoad(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", trustPanelSources())

	const remote = "providers:\n  p:\n    api_key_env: K\n    base_url: https://example.invalid/v1\n" +
		"agents:\n  a:\n    provider: p\n    model: m\nfail_on: CRITICAL\nconsensus: off\n"
	var fetches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&fetches, 1)
		_, _ = w.Write([]byte(remote))
	}))
	t.Cleanup(srv.Close)

	// The tier is consulted only when the local registry file exists; its bytes
	// come from the URL (see registry.loadRegistryBytes).
	regDir := filepath.Join(os.Getenv("HOME"), ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(remote), 0o644))
	t.Setenv("ATCR_REGISTRY_URL", srv.URL+"/registry.yaml")

	require.Equal(t, 0, execCmd(t, "reconcile", "r"))
	assert.Equal(t, int32(1), atomic.LoadInt32(&fetches),
		"one reconcile must load the user registry once for both shared settings")
}

// --- Plan 35.13 T3: dedup seeding scoped to suppressing-or-open ids ---------

// AC3(d), the write half of the coupled change: seeding the dedup set from EVERY
// id in the store means a regressed finding is never re-appended, so the
// recency-aware fold has nothing newer to select and the read-side change is a
// no-op. The seed must therefore cover only ids whose effective record
// suppresses (wontfix) or is still open.
func TestPersistLocalDebt_ReappendsARegressedResolvedID(t *testing.T) {
	isolate(t)
	dir := localdebt.DefaultDir(".")

	open := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-07-13T00:00:00Z-r",
		Timestamp: "2026-07-13T00:00:00Z", Severity: "HIGH",
		File: "a.go", Line: 1, Problem: "leaks a file handle",
	}
	open.StampID()
	resolved := open
	resolved.RunID = "2026-07-13T00:30:00Z-resolved"
	resolved.Timestamp = "2026-07-13T00:30:00Z"
	resolved.Status = "resolved"
	require.NoError(t, localdebt.Append(dir, open))
	require.NoError(t, localdebt.Append(dir, resolved))

	// Reconcile re-detects the identical finding: same file/line/problem → same id.
	res := reconcile.Result{
		Findings: []reclib.Merged{
			{Finding: reclib.Finding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "leaks a file handle", Fix: "close it", Category: "resource", EstMinutes: 10}},
		},
		Summary: reclib.Summary{ReconciledAt: "2026-07-14T00:00:00Z"},
	}
	var diag bytes.Buffer
	persistLocalDebt("review", res, ".", true, false, &diag)

	recs := readLocalDebtRecords(t)
	require.Len(t, recs, 3, "the regression is appended as a fresh open record")
	assert.Equal(t, "2026-07-14T00:00:00Z", recs[2].Timestamp)
	assert.Empty(t, recs[2].Status)

	// And the fold must return it to the open backlog.
	out, err := runDebt(t, "resolve")
	require.NoError(t, err)
	assert.Contains(t, out, "a.go", "a resolved-then-regressed id is open again")
}

// The mirror case, and the reason the seed is scoped rather than removed: a
// wontfix id must still suppress the re-append entirely.
func TestPersistLocalDebt_StillSuppressesAWontfixID(t *testing.T) {
	isolate(t)
	dir := localdebt.DefaultDir(".")

	open := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-07-13T00:00:00Z-r",
		Timestamp: "2026-07-13T00:00:00Z", Severity: "HIGH",
		File: "a.go", Line: 1, Problem: "flagged false positive",
	}
	open.StampID()
	wontfix := open
	wontfix.RunID = "2026-07-13T00:30:00Z-wontfix"
	wontfix.Timestamp = "2026-07-13T00:30:00Z"
	wontfix.Status = "wontfix"
	wontfix.Justification = "accepted pattern"
	require.NoError(t, localdebt.Append(dir, open))
	require.NoError(t, localdebt.Append(dir, wontfix))

	res := reconcile.Result{
		Findings: []reclib.Merged{
			{Finding: reclib.Finding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "flagged false positive", Fix: "n/a", Category: "correctness", EstMinutes: 10}},
		},
		Summary: reclib.Summary{ReconciledAt: "2026-07-14T00:00:00Z"},
	}
	var diag bytes.Buffer
	persistLocalDebt("review", res, ".", true, false, &diag)

	recs := readLocalDebtRecords(t)
	require.Len(t, recs, 2, "a dismissed finding is never re-appended")
}

// A deferred id re-surfaces too, so it must also be re-appended.
func TestPersistLocalDebt_ReappendsADeferredID(t *testing.T) {
	isolate(t)
	dir := localdebt.DefaultDir(".")

	open := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-07-13T00:00:00Z-r",
		Timestamp: "2026-07-13T00:00:00Z", Severity: "HIGH",
		File: "a.go", Line: 1, Problem: "not now",
	}
	open.StampID()
	deferred := open
	deferred.RunID = "2026-07-13T00:30:00Z-deferred"
	deferred.Timestamp = "2026-07-13T00:30:00Z"
	deferred.Status = "deferred"
	require.NoError(t, localdebt.Append(dir, open))
	require.NoError(t, localdebt.Append(dir, deferred))

	res := reconcile.Result{
		Findings: []reclib.Merged{
			{Finding: reclib.Finding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "not now", Fix: "later", Category: "correctness", EstMinutes: 10}},
		},
		Summary: reclib.Summary{ReconciledAt: "2026-07-14T00:00:00Z"},
	}
	var diag bytes.Buffer
	persistLocalDebt("review", res, ".", true, false, &diag)

	require.Len(t, readLocalDebtRecords(t), 3, "a deferred id re-surfaces, so re-detection appends")
}

// An id that is still OPEN must not be re-appended: the seed covers open ids so
// an unchanged finding does not accumulate one record per reconcile run.
func TestPersistLocalDebt_StillDedupsAnOpenID(t *testing.T) {
	isolate(t)
	dir := localdebt.DefaultDir(".")

	open := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-07-13T00:00:00Z-r",
		Timestamp: "2026-07-13T00:00:00Z", Severity: "HIGH",
		File: "a.go", Line: 1, Problem: "still open",
	}
	open.StampID()
	require.NoError(t, localdebt.Append(dir, open))

	res := reconcile.Result{
		Findings: []reclib.Merged{
			{Finding: reclib.Finding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "still open", Fix: "f", Category: "correctness", EstMinutes: 10}},
		},
		Summary: reclib.Summary{ReconciledAt: "2026-07-14T00:00:00Z"},
	}
	var diag bytes.Buffer
	persistLocalDebt("review", res, ".", true, false, &diag)

	require.Len(t, readLocalDebtRecords(t), 1, "an unchanged open finding is not duplicated per run")
}

// --- T4: streaming dedup seed (AC6 first clause, AC3(d) regression guard) ----

// seedFinding is the one finding every streaming-seed test re-detects, spelled
// once so a test's store fixture and its reconcile result cannot drift into two
// different ids.
func seedFinding(problem string) reclib.Merged {
	return reclib.Merged{Finding: reclib.Finding{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: problem,
		Fix: "fix it", Category: "correctness", EstMinutes: 10,
	}}
}

// seedStore appends an open record for problem plus, when status is non-empty, a
// terminal record for the same id, and returns the shared id.
func seedStore(t *testing.T, dir, problem, status string) string {
	t.Helper()
	open := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-07-13T00:00:00Z-r",
		Timestamp: "2026-07-13T00:00:00Z", Severity: "HIGH",
		File: "a.go", Line: 1, Problem: problem,
	}
	open.StampID()
	require.NoError(t, localdebt.Append(dir, open))
	if status != "" {
		term := open
		term.RunID = "2026-07-13T00:30:00Z-" + status
		term.Timestamp = "2026-07-13T00:30:00Z"
		term.Status = status
		require.NoError(t, localdebt.Append(dir, term))
	}
	return open.ID
}

// TestPersistLocalDebt_StreamingSeedKeepsSuppressingScope is T4's AC3(d) guard.
// The obvious streaming shape — a per-record `seen[s.ID] = true` callback —
// restores terminal-forever dedup and silently reverts Task 03 while every
// allocation assertion still passes, because AC6 measures bytes and AC3 measures
// behavior. The seed must therefore FOLD first and cover only suppressing-or-open
// effective ids: a resolved id re-appends on re-detection, a wontfix id does not.
func TestPersistLocalDebt_StreamingSeedKeepsSuppressingScope(t *testing.T) {
	isolate(t)
	dir := localdebt.DefaultDir(".")

	resolvedID := seedStore(t, dir, "regressed after a fix", "resolved")
	// A second id in the same store, dismissed rather than fixed. Both live under
	// one seed so a widened seed cannot pass by getting the wontfix case right in
	// isolation.
	wontfix := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-07-13T00:00:00Z-r",
		Timestamp: "2026-07-13T00:00:00Z", Severity: "HIGH",
		File: "b.go", Line: 2, Problem: "known false positive",
	}
	wontfix.StampID()
	require.NoError(t, localdebt.Append(dir, wontfix))
	dismissed := wontfix
	dismissed.RunID = "2026-07-13T00:30:00Z-wontfix"
	dismissed.Timestamp = "2026-07-13T00:30:00Z"
	dismissed.Status = "wontfix"
	require.NoError(t, localdebt.Append(dir, dismissed))

	res := reconcile.Result{
		Findings: []reclib.Merged{
			seedFinding("regressed after a fix"),
			{Finding: reclib.Finding{
				Severity: "HIGH", File: "b.go", Line: 2, Problem: "known false positive",
				Fix: "n/a", Category: "correctness", EstMinutes: 10,
			}},
		},
		Summary: reclib.Summary{ReconciledAt: "2026-07-14T00:00:00Z"},
	}
	var diag bytes.Buffer
	persistLocalDebt("review", res, ".", true, false, &diag)

	recs := readLocalDebtRecords(t)
	byID := map[string]int{}
	for _, r := range recs {
		byID[r.ID]++
	}
	assert.Equal(t, 3, byID[resolvedID],
		"a re-detected resolved id appends a fresh open record (AC3(d) via the streaming seed)")
	assert.Equal(t, 2, byID[wontfix.ID],
		"a dismissed id is still suppressed by the streaming seed")
}

// TestPersistLocalDebt_DedupParityWithStreamingSeed pins that the streaming seed
// dedups exactly as the previous full-ReadAll seed did: reconciling the same
// findings twice against one store leaves one record per id.
func TestPersistLocalDebt_DedupParityWithStreamingSeed(t *testing.T) {
	isolate(t)

	res := reconcile.Result{
		Findings: []reclib.Merged{seedFinding("unchanged finding")},
		Summary:  reclib.Summary{ReconciledAt: "2026-07-14T00:00:00Z"},
	}
	var diag bytes.Buffer
	persistLocalDebt("review", res, ".", true, false, &diag)
	res.Summary.ReconciledAt = "2026-07-15T00:00:00Z"
	persistLocalDebt("review", res, ".", true, false, &diag)

	require.Len(t, readLocalDebtRecords(t), 1,
		"the streaming seed dedups across runs identically to the ReadAll seed")
}

// TestPersistLocalDebt_FailsOpenOnUnreadableStore locks the fail-open contract
// through the new read path: a dedup read failure logs the existing warning
// verbatim and appends anyway rather than dropping the run's backlog.
func TestPersistLocalDebt_FailsOpenOnUnreadableStore(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a permission-denied open cannot be provoked as root")
	}
	isolate(t)
	dir := localdebt.DefaultDir(".")

	// The seeded record lives in an EARLIER month than the run's append target, so
	// making its shard unreadable breaks the dedup read without also blocking the
	// write this test is asserting still happens.
	existing := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-06-13T00:00:00Z-r",
		Timestamp: "2026-06-13T00:00:00Z", Severity: "HIGH",
		File: "a.go", Line: 1, Problem: "unreadable-store finding",
	}
	existing.StampID()
	require.NoError(t, localdebt.Append(dir, existing))

	shard := filepath.Join(dir, "2026-06.jsonl")
	require.NoError(t, os.Chmod(shard, 0o000))
	t.Cleanup(func() { _ = os.Chmod(shard, 0o600) })

	res := reconcile.Result{
		Findings: []reclib.Merged{seedFinding("unreadable-store finding")},
		Summary:  reclib.Summary{ReconciledAt: "2026-07-14T00:00:00Z"},
	}
	var diag bytes.Buffer
	persistLocalDebt("review", res, ".", true, false, &diag)

	assert.Contains(t, diag.String(), "dedup read failed, appending without dedup",
		"the fail-open warning text is unchanged")
	require.NoError(t, os.Chmod(shard, 0o600))
	assert.Len(t, readLocalDebtRecords(t), 2,
		"the finding is appended anyway: an unreadable store never drops a run's backlog")
}

// TestPersistLocalDebt_PartialReadDoesNotSuppressAReopen is the regression guard
// for the fail-open seed being ALL-OR-NOTHING.
//
// Seeding from a partially read store looks strictly safer and is not: the seed
// keys on an id's EFFECTIVE status, which the fold can only compute from that id's
// COMPLETE history. Here the id is open in one shard and `resolved` in another; if
// the resolution's shard is unreadable, a partial seed folds the id to `open`,
// seeds it as still-outstanding, and SKIPS the re-detection that should have
// re-opened it — silently suppressing the regression the store exists to surface.
//
// The single-shard fail-open test above cannot catch this: blocking the only shard
// leaves the partial set empty, which behaves identically to seeding nothing.
func TestPersistLocalDebt_PartialReadDoesNotSuppressAReopen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a permission-denied open cannot be provoked as root")
	}
	isolate(t)
	dir := localdebt.DefaultDir(".")

	open := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-06-13T00:00:00Z-r",
		Timestamp: "2026-06-13T00:00:00Z", Severity: "HIGH",
		File: "a.go", Line: 1, Problem: "regressed after a fix",
	}
	open.StampID()
	require.NoError(t, localdebt.Append(dir, open))

	// The resolution lands in a DIFFERENT month shard, which is the one made
	// unreadable — so the partial read sees the open record and misses the fix.
	resolved := open
	resolved.RunID = "2026-07-01T00:00:00Z-resolved"
	resolved.Timestamp = "2026-07-01T00:00:00Z"
	resolved.Status = "resolved"
	require.NoError(t, localdebt.Append(dir, resolved))

	blocked := filepath.Join(dir, "2026-07.jsonl")
	require.NoError(t, os.Chmod(blocked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o600) })

	res := reconcile.Result{
		Findings: []reclib.Merged{seedFinding("regressed after a fix")},
		Summary:  reclib.Summary{ReconciledAt: "2026-08-01T00:00:00Z"},
	}
	var diag bytes.Buffer
	persistLocalDebt("review", res, ".", true, false, &diag)

	require.NoError(t, os.Chmod(blocked, 0o600))
	recs := readLocalDebtRecords(t)
	count := 0
	for _, r := range recs {
		if r.ID == open.ID {
			count++
		}
	}
	assert.Equal(t, 3, count,
		"a partially read store must not suppress the re-detection: seed nothing rather than seed a wrong effective status")
}

// --- T5: automatic compaction after a reconcile append ----------------------

// withAutoCompactPolicy overrides the package-level trigger policy for one test and
// restores it, so the threshold can be exercised with a handful of records instead
// of the 100k the production default requires.
func withAutoCompactPolicy(t *testing.T, p localdebt.CompactPolicy) {
	t.Helper()
	prev := autoCompactPolicy
	autoCompactPolicy = p
	t.Cleanup(func() { autoCompactPolicy = prev })
}

// TestPersistLocalDebt_AutoCompactsAtThreshold covers AC5: an over-threshold store
// is compacted once, after the append loop, as a side effect of a reconcile that
// actually wrote something.
func TestPersistLocalDebt_AutoCompactsAtThreshold(t *testing.T) {
	isolate(t)
	dir := localdebt.DefaultDir(".")
	withAutoCompactPolicy(t, localdebt.CompactPolicy{MaxRecords: 1})

	// Superseded churn for one id: four records that fold to one.
	base := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, Severity: "HIGH",
		File: "a.go", Line: 1, Problem: "churning finding",
	}
	base.StampID()
	for i := 1; i <= 4; i++ {
		rec := base
		rec.RunID = fmt.Sprintf("2026-06-%02dT00:00:00Z-run", i)
		rec.Timestamp = fmt.Sprintf("2026-06-%02dT00:00:00Z", i)
		require.NoError(t, localdebt.Append(dir, rec))
	}
	require.Len(t, readLocalDebtRecords(t), 4)

	res := reconcile.Result{
		Findings: []reclib.Merged{seedFinding("a brand new finding")},
		Summary:  reclib.Summary{ReconciledAt: "2026-07-01T00:00:00Z"},
	}
	var diag bytes.Buffer
	persistLocalDebt("review", res, ".", true, false, &diag)

	recs := readLocalDebtRecords(t)
	assert.Less(t, len(recs), 5, "compaction ran: the superseded churn was dropped")
	assert.Contains(t, diag.String(), "localdebt: compacted", "the trigger reports what it did")

	// The backlog itself is unchanged: both findings are still live and open.
	open := 0
	for _, r := range localdebt.FoldRecords(recs) {
		if r.Status == "" {
			open++
		}
	}
	assert.Equal(t, 2, open, "compaction bounds growth; it never drops a live finding")
}

// TestPersistLocalDebt_AutoCompactFailureIsNonFatal locks the best-effort contract:
// persistLocalDebt has no return value and a compaction problem must not reach the
// reconcile's exit code.
func TestPersistLocalDebt_AutoCompactFailureIsNonFatal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a permission-denied open cannot be provoked as root")
	}
	isolate(t)
	dir := localdebt.DefaultDir(".")
	withAutoCompactPolicy(t, localdebt.CompactPolicy{MaxRecords: 1})

	seed := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-06-01T00:00:00Z-run",
		Timestamp: "2026-06-01T00:00:00Z", Severity: "HIGH",
		File: "a.go", Line: 1, Problem: "existing finding",
	}
	seed.StampID()
	require.NoError(t, localdebt.Append(dir, seed))

	// An unreadable JUNE shard fails the compaction pass, while the run's own
	// append lands in JULY and succeeds — so the trigger genuinely runs and
	// genuinely fails, instead of being skipped because nothing was appended.
	blocked := filepath.Join(dir, "2026-06.jsonl")
	require.NoError(t, os.Chmod(blocked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o600) })

	res := reconcile.Result{
		Findings: []reclib.Merged{seedFinding("a brand new finding")},
		Summary:  reclib.Summary{ReconciledAt: "2026-07-01T00:00:00Z"},
	}
	var diag bytes.Buffer
	assert.NotPanics(t, func() { persistLocalDebt("review", res, ".", true, false, &diag) },
		"a compaction failure is logged, never fatal")

	assert.Contains(t, diag.String(), "localdebt: automatic compaction failed",
		"the failure is reported to diagnostics")
	assert.NotContains(t, diag.String(), dir,
		"and the failure message stays inside the store's path-redaction contract")
	require.NoError(t, os.Chmod(blocked, 0o600))
	ids := map[string]bool{}
	for _, r := range readLocalDebtRecords(t) {
		ids[r.ID] = true
	}
	assert.Len(t, ids, 2, "the run's findings landed regardless of the compaction failure")
}

// TestPersistLocalDebt_NoAppendSkipsAutoCompact covers the zero-added-I/O clause: a
// fully-deduped run cannot have pushed the store over a threshold, so it must not
// even stat the store — let alone rewrite it.
func TestPersistLocalDebt_NoAppendSkipsAutoCompact(t *testing.T) {
	isolate(t)
	dir := localdebt.DefaultDir(".")
	withAutoCompactPolicy(t, localdebt.CompactPolicy{MaxRecords: 1})

	base := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, Severity: "HIGH",
		File: "a.go", Line: 1, Problem: "unchanged finding",
	}
	base.StampID()
	for i := 1; i <= 3; i++ {
		rec := base
		rec.RunID = fmt.Sprintf("2026-06-%02dT00:00:00Z-run", i)
		rec.Timestamp = fmt.Sprintf("2026-06-%02dT00:00:00Z", i)
		require.NoError(t, localdebt.Append(dir, rec))
	}
	before, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)

	// The id is already open in the store, so the seed dedups it and nothing is
	// appended.
	res := reconcile.Result{
		Findings: []reclib.Merged{seedFinding("unchanged finding")},
		Summary:  reclib.Summary{ReconciledAt: "2026-07-01T00:00:00Z"},
	}
	var diag bytes.Buffer
	persistLocalDebt("review", res, ".", true, false, &diag)

	after, err := os.ReadFile(filepath.Join(dir, "2026-06.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, before, after, "a zero-append run performs no compaction")
	assert.NotContains(t, diag.String(), "localdebt: compacted")
	assert.NoFileExists(t, filepath.Join(dir, ".compact-watermark"),
		"and no compaction watermark is recorded, because none ran")
}

// --- Sprint 35.13 T6: store-root resolution ------------------------------

// writeReviewManifestRoot stamps a fixture review's manifest with a recorded repo
// root, the way the review path does at review time.
//
// A manifest makes the review fan-out-managed, so EnsureReviewComplete then demands
// the pool completion signal — reconciling mid-run would read a partial agent set.
// The helper writes both, because a review that carries a recorded root is by
// definition one the review path produced, and that path always writes both.
func writeReviewManifestRoot(t *testing.T, id, root string) {
	t.Helper()
	dir := filepath.Join(".atcr", "reviews", id)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool"), 0o755))
	data, err := json.Marshal(payload.Manifest{
		Base: "aaa", Head: "bbb", Roster: []string{"host"}, Root: root,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sources", "pool", "summary.json"),
		[]byte(`{"total":1,"succeeded":1,"failed":0,"partial":false,"total_findings":1}`), 0o644))
}

// TestReconcile_PersistsToManifestRootNotCWD is the CLI half of AC7(c): reconciling
// from a directory that is not the reviewed repo writes to the root the manifest
// recorded, not to the CWD. Before the manifest tier existed, the CWD was the only
// answer the CLI had. Since TD-024, finding-path validation runs against that same
// resolved root, so the finding's file must exist under it — the reviewed repo —
// not under the CWD the operator happens to stand in.
func TestReconcile_PersistsToManifestRootNotCWD(t *testing.T) {
	isolate(t)
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|leaks a file handle|close it|resource|10|ev|host\n",
	})

	// A separate, valid repo root: the recorded review root, distinct from the CWD.
	manifestRoot := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(manifestRoot, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(manifestRoot, "a.go"), []byte("package a\n"), 0o644))
	writeReviewManifestRoot(t, "r", manifestRoot)

	require.Equal(t, 0, execCmd(t, "reconcile", "r"))

	recs, err := localdebt.ReadAll(localdebt.DefaultDir(manifestRoot), localdebt.ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 1, "the finding must land under the manifest's recorded root")
	assert.Equal(t, "a.go", recs[0].File)

	_, statErr := os.Stat(localdebt.DefaultDir("."))
	assert.True(t, os.IsNotExist(statErr), "no store may be created under the CWD when a manifest root resolved")
}

// TestReconcile_ExplicitRepoOverridesManifestRoot pins AC7(c)'s ordering AND the
// raw-flag keying that makes it work. --repo carries a "." default, so keying the
// explicit tier off the NORMALIZED value would make the explicit tier win on every
// run — this test would still pass, but its sibling above would break. The pair is
// what pins the behavior: one asserts the manifest tier is reachable, this one
// asserts an explicit flag still outranks it.
func TestReconcile_ExplicitRepoOverridesManifestRoot(t *testing.T) {
	isolate(t)
	touchFiles(t, "a.go")
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|leaks a file handle|close it|resource|10|ev|host\n",
	})

	manifestRoot := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(manifestRoot, ".git"), 0o755))
	writeReviewManifestRoot(t, "r", manifestRoot)

	// --repo is also the root finding file paths are validated against, so a.go has
	// to exist there or the finding is path-warned and never persisted — which would
	// make this test pass for the wrong reason under a broken resolver.
	explicit := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(explicit, "a.go"), []byte("package a\n"), 0o644))

	require.Equal(t, 0, execCmd(t, "reconcile", "r", "--repo", explicit))

	recs, err := localdebt.ReadAll(localdebt.DefaultDir(explicit), localdebt.ReadOpts{})
	require.NoError(t, err)
	assert.Len(t, recs, 1, "an explicit --repo outranks the recorded manifest root")

	fromManifest, err := localdebt.ReadAll(localdebt.DefaultDir(manifestRoot), localdebt.ReadOpts{})
	require.NoError(t, err)
	assert.Empty(t, fromManifest, "the manifest root must not also be written")
}

// TestReconcile_StaleManifestRootDoesNotFallBackToCWD covers AC7(e) at the CLI, the
// tier where a fall-through is most tempting because the CWD tier is right there and
// legitimate. It must still be a no-persist: a root that was recorded and no longer
// resolves is a stop signal, and writing to the CWD instead would be an undetectable
// wrong write dressed up as success.
func TestReconcile_StaleManifestRootDoesNotFallBackToCWD(t *testing.T) {
	isolate(t)
	touchFiles(t, "a.go")
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|leaks a file handle|close it|resource|10|ev|host\n",
	})
	writeReviewManifestRoot(t, "r", filepath.Join(t.TempDir(), "repo-on-another-machine"))

	code, _, stderr := execCmdSplit(t, "reconcile", "r")
	require.Equal(t, 0, code, "a skipped persistence must never change the exit code")

	_, statErr := os.Stat(localdebt.DefaultDir("."))
	assert.True(t, os.IsNotExist(statErr), "no CWD fall-through on a stale recorded root")
	assert.Contains(t, stderr, "no longer a valid repository root")
}

// TestReconcile_NoManifestRootUsesCWD locks the CLI's third tier as byte-for-byte
// the pre-field behavior: a review with no recorded root still persists to the CWD,
// which is what every existing CLI test relies on and what `atcr reconcile` from the
// repo root has always meant.
func TestReconcile_NoManifestRootUsesCWD(t *testing.T) {
	isolate(t)
	touchFiles(t, "a.go")
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|leaks a file handle|close it|resource|10|ev|host\n",
	})

	require.Equal(t, 0, execCmd(t, "reconcile", "r"))

	assert.Len(t, readLocalDebtRecords(t), 1,
		"with nothing recorded, the CWD convention still applies — a missing claim is not a stale one")
}

// TestReconcile_ValidatesAgainstResolvedStoreRoot pins the TD-024 fix: finding-path
// validation runs against the SAME root the findings persist under, resolved
// BEFORE RunReconcile. Before the fix, validation used the --repo value (default
// ".", the CWD): reconciling from a directory that is not the reviewed repo
// PathWarning-stamped every finding whose file lived only under the manifest's
// recorded root, and the bridge dropped all of them — the correctly-resolved
// store received ZERO records.
func TestReconcile_ValidatesAgainstResolvedStoreRoot(t *testing.T) {
	isolate(t)
	// a.go exists ONLY under the manifest's recorded root, never under the CWD.
	fixtureReview(t, "r", map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|leaks a file handle|close it|resource|10|ev|host\n",
	})
	manifestRoot := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(manifestRoot, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(manifestRoot, "a.go"), []byte("package a\n"), 0o644))
	writeReviewManifestRoot(t, "r", manifestRoot)

	require.Equal(t, 0, execCmd(t, "reconcile", "r"))

	recs, err := localdebt.ReadAll(localdebt.DefaultDir(manifestRoot), localdebt.ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 1,
		"a finding whose path exists under the resolved root must validate and persist, even though it is absent under the CWD")
	assert.Equal(t, "a.go", recs[0].File)
}

// TestReview_OneShotPersistsLocalDebt pins the TD cli/review.go:747 fix: the
// one-shot `atcr review --fail-on` path runs reconcile in-process and, before
// the fix, persisted NOTHING to the local debt store — a user whose whole
// workflow is the primary CI invocation accumulated an empty backlog forever.
// The inline site now mirrors persistLocalDebt (resolve store root, then the
// shared localdebt.PersistForReconcile bridge).
func TestReview_OneShotPersistsLocalDebt(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	require.Equal(t, 1, execCmd(t, "review", "--fail-on", "high", "--base", "HEAD^"),
		"the CRITICAL mock finding gates at the high threshold")

	assert.NotEmpty(t, readLocalDebtRecords(t),
		"the one-shot review's inline reconcile must persist its findings to the local debt store")
}
