package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/scorecard"
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

// isolate chdirs into a fresh temp working dir AND points HOME/XDG at another
// temp dir, so resolveGateThreshold's registry probe (~/.config/atcr) cannot
// pick up a real registry on the dev machine — tests stay hermetic.
func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(t.TempDir())
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
	root := newRootCmd()
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

// TestPersistLocalDebt_SkipsGateExcludedFindings verifies that the reconcile
// persistence hook applies the same out-of-scope and refuted exclusions the
// gate uses, so the local TD store's open backlog matches what the gate would
// consider a real finding.
func TestPersistLocalDebt_SkipsGateExcludedFindings(t *testing.T) {
	isolate(t)

	findings := []reconcile.Merged{
		{Finding: reconcile.Finding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "real bug", Fix: "fix it", Category: "correctness", EstMinutes: 10}},
		{Finding: reconcile.Finding{Severity: "CRITICAL", File: "b.go", Line: 2, Problem: "out of scope", Fix: "n/a", Category: reconcile.CategoryOutOfScope, EstMinutes: 5}},
		{Finding: reconcile.Finding{Severity: "HIGH", File: "c.go", Line: 3, Problem: "refuted", Fix: "n/a", Category: "security", EstMinutes: 10, Verification: &reconcile.Verification{Verdict: reconcile.VerdictRefuted, Skeptic: "skeptic"}}},
	}
	res := reconcile.Result{
		Findings: findings,
		Summary:  reconcile.Summary{ReconciledAt: "2026-07-12T00:00:00Z"},
	}

	var diag bytes.Buffer
	persistLocalDebt("review", res, false, &diag)

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

// TestGateThresholdReaders_OneWhitespaceSemantic verifies the two --fail-on
// readers (failOnThreshold on the one-shot review path, resolveGateThreshold on
// the reconcile path) share one semantic: a whitespace-only flag value is unset
// (no gate), not a usage error, and a real value canonicalizes identically.
func TestGateThresholdReaders_OneWhitespaceSemantic(t *testing.T) {
	isolate(t)
	readers := map[string]func(*cobra.Command) (string, error){
		"failOnThreshold":      failOnThreshold,
		"resolveGateThreshold": resolveGateThreshold,
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
