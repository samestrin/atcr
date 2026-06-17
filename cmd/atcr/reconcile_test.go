package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
