package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveRedactRoot_ReturnsAbsolute verifies the happy path resolves the
// repo root to an absolute path used for AC6 path relativization.
func TestResolveRedactRoot_ReturnsAbsolute(t *testing.T) {
	got := resolveRedactRoot(context.Background(), ".")
	require.True(t, filepath.IsAbs(got), "resolveRedactRoot(\".\") should return an absolute path, got %q", got)
}

// TestResolveRedactRoot_LogsWarnOnAbsError verifies that when absolute
// resolution fails the silent loss of AC6 path redaction is made observable via
// a warning, and the original root is returned unchanged (fail-open, not silent).
func TestResolveRedactRoot_LogsWarnOnAbsError(t *testing.T) {
	orig := absFn
	absFn = func(string) (string, error) { return "", errors.New("boom") }
	defer func() { absFn = orig }()

	var buf bytes.Buffer
	logger, err := log.New("debug", "text", &buf)
	require.NoError(t, err)
	ctx := log.NewContext(context.Background(), logger)

	got := resolveRedactRoot(ctx, "some/rel/path")
	require.Equal(t, "some/rel/path", got, "on Abs failure the input root is returned unchanged")
	require.Contains(t, buf.String(), "path redaction may be incomplete", "a warning must make the silent failure observable")
}

// TestResolveRedactRoot_ResolvesSymlinks verifies the review root is symlink-
// resolved so a path logged in its real form (e.g. macOS resolves /tmp ->
// /private/var/...) still matches the review-root prefix for AC6 relativization.
func TestResolveRedactRoot_ResolvesSymlinks(t *testing.T) {
	realResolved, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(realResolved, link))

	got := resolveRedactRoot(context.Background(), link)
	require.Equal(t, realResolved, got, "resolveRedactRoot should resolve the symlinked root to its real path")
}

// TestResolveRedactRoot_FailsOpenOnEvalSymlinksError verifies that when symlink
// resolution fails (e.g. the root is not yet on disk) the absolute form is used,
// so relativization of the un-resolved path still works.
func TestResolveRedactRoot_FailsOpenOnEvalSymlinksError(t *testing.T) {
	orig := evalSymlinksFn
	evalSymlinksFn = func(string) (string, error) { return "", errors.New("boom") }
	defer func() { evalSymlinksFn = orig }()

	got := resolveRedactRoot(context.Background(), ".")
	require.True(t, filepath.IsAbs(got), "on EvalSymlinks failure the absolute root is returned, got %q", got)
}

// TestReviewIDSurvivesRedaction locks the AC9-vs-AC5/6 contract: binding
// review_id BEFORE the redactor (review.go:162 then :172) stores it in the inner
// handler, so a correlation key that itself looks secret-shaped (sk-...) is NOT
// scrubbed and stays greppable by review_id. Guards against a future reorder that
// would bind review_id on top of the redactor and silently redact it.
func TestReviewIDSurvivesRedaction(t *testing.T) {
	var buf bytes.Buffer
	base, err := log.New("info", "json", &buf)
	require.NoError(t, err)

	reviewID := "2026-06-17_sk-feature-branch" // a correlation key with an sk--shaped substring
	logger := log.WithReviewID(base, reviewID)
	logger = log.WithRedactor(logger, log.NewRedactor("/tmp/repo"))

	logger.Info("review started")

	require.Contains(t, buf.String(), reviewID,
		"review_id must survive redaction so logs stay greppable by review_id (AC9)")
}

// slotWithKeys builds a one-slot chain whose agents read the given env vars.
func slotWithKeys(envs ...string) fanout.Slot {
	s := fanout.Slot{Primary: fanout.Agent{Invocation: llmclient.Invocation{APIKeyEnv: envs[0]}}}
	for _, e := range envs[1:] {
		s.Fallbacks = append(s.Fallbacks, fanout.Agent{Invocation: llmclient.Invocation{APIKeyEnv: e}})
	}
	return s
}

// --- Review correlation (sprint 4.0, task 3.4) ----------------------------

// TestRunReview_AttachesReviewID verifies correlateReviewID tags the context
// logger so every subsequent log line carries review_id=<id> (AC9).
func TestRunReview_AttachesReviewID(t *testing.T) {
	var buf bytes.Buffer
	base, err := log.New("info", "text", &buf)
	require.NoError(t, err)
	ctx := log.NewContext(context.Background(), base)

	ctx = correlateReviewID(ctx, "2026-06-17_feat")
	log.FromContext(ctx).Info("executing review")

	assert.Contains(t, buf.String(), "review_id=2026-06-17_feat",
		"every log line after correlation must carry the review id")
}

// TestRunReview_ContextLoggerFlowsToExecuteReview verifies the correlated logger
// is reachable via log.FromContext on the returned context — the same access
// path ExecuteReview/RunReconcile/Verify use — and differs from the base logger.
func TestRunReview_ContextLoggerFlowsToExecuteReview(t *testing.T) {
	base, err := log.New("info", "text", &bytes.Buffer{})
	require.NoError(t, err)
	ctx := log.NewContext(context.Background(), base)

	correlated := correlateReviewID(ctx, "rev-1")
	require.NotSame(t, base, log.FromContext(correlated),
		"correlation must store a derived logger reachable by downstream stages")
}

// TestRunReview_NoLocalLogger verifies correlateReviewID builds on the existing
// context logger rather than constructing a new one: an attribute attached
// upstream survives alongside review_id.
func TestRunReview_NoLocalLogger(t *testing.T) {
	var buf bytes.Buffer
	base, err := log.New("info", "text", &buf)
	require.NoError(t, err)
	// Attribute attached upstream (e.g. a future agent/source tag).
	base = base.With("upstream", "present")
	ctx := log.NewContext(context.Background(), base)

	ctx = correlateReviewID(ctx, "rev-2")
	log.FromContext(ctx).Info("line")

	out := buf.String()
	assert.Contains(t, out, "upstream=present", "must preserve the upstream context logger, not replace it")
	assert.Contains(t, out, "review_id=rev-2", "must also attach the review id")
}

// --- Graceful-shutdown interrupt notice (epic 4.1) ------------------------

// TestInterruptMessage_NilResultFallsBackToPrep verifies the warning is rendered
// from the PreparedReview when ExecuteReview returned no result (interrupted
// before producing one), reports 0/0, and points at both `atcr status` and the
// `atcr review --resume` flow that finishes the remaining agents (epic 4.1.1
// makes --resume real, so the notice now advertises it — AC5/AC6).
func TestInterruptMessage_NilResultFallsBackToPrep(t *testing.T) {
	prep := &fanout.PreparedReview{ID: "2026-06-17_feat", Dir: "/x/.atcr/reviews/2026-06-17_feat"}
	msg := interruptMessage(nil, prep)
	assert.Contains(t, msg, "Review interrupted", "AC5: clear interrupt notice")
	assert.Contains(t, msg, "0/0 agents completed", "nil result falls back to a zero tally")
	assert.Contains(t, msg, "/x/.atcr/reviews/2026-06-17_feat", "AC6: prints the review directory")
	assert.Contains(t, msg, "atcr status 2026-06-17_feat", "AC6: points at a command that exists")
	assert.Contains(t, msg, "atcr review --resume 2026-06-17_feat", "epic 4.1.1: advertises the resume flow")
}

// TestInterruptMessage_UsesResultCounts verifies that when a partial result is
// present the warning reports its succeeded/total tally and its on-disk dir.
func TestInterruptMessage_UsesResultCounts(t *testing.T) {
	prep := &fanout.PreparedReview{ID: "rev", Dir: "/fallback"}
	result := &fanout.ReviewResult{ID: "rev", Dir: "/real/dir", Summary: fanout.Summary{Total: 5, Succeeded: 2, Failed: 1, Partial: true}}
	msg := interruptMessage(result, prep)
	assert.Contains(t, msg, "2/5 agents completed", "reports the partial tally")
	assert.Contains(t, msg, "/real/dir", "uses the result's dir when present")
}

// TestInterruptedBeforeFanout_ExitOneWithNotice verifies an interrupt that lands
// before the fan-out starts exits 1 with a graceful notice — not the misleading
// exit-2 "review failed" usage error (independent-review MED).
func TestInterruptedBeforeFanout_ExitOneWithNotice(t *testing.T) {
	cmd := newReviewCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)

	err := interruptedBeforeFanout(cmd)
	require.Error(t, err)
	assert.Equal(t, exitFailure, exitCode(err), "interrupt before fan-out exits 1, not the exit-2 usage error")
	assert.Contains(t, buf.String(), "Review interrupted before it started")
	assert.NotContains(t, err.Error(), "review failed", "must not surface as a range/usage failure")
}

func TestCLIOverrides_MaxParallelSet(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--max-parallel", "3"}))
	o := cliOverrides(cmd)
	require.NotNil(t, o.MaxParallel, "--max-parallel must populate the override")
	require.Equal(t, 3, *o.MaxParallel)
}

func TestCLIOverrides_MaxParallelUnset(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags(nil))
	o := cliOverrides(cmd)
	require.Nil(t, o.MaxParallel, "an unset flag must not override lower tiers")
}

func TestPreflightAPIKeys_AllChainsKeylessIsUsageError(t *testing.T) {
	// AC 03-02 Error Scenario 1: a missing API key is a configuration error
	// (exit 2) naming the env var — never the gate's exit 1.
	err := preflightAPIKeys([]fanout.Slot{
		slotWithKeys("ATCR_TEST_KEY_A", "ATCR_TEST_KEY_B"),
		slotWithKeys("ATCR_TEST_KEY_C"),
	})
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err))
	require.Contains(t, err.Error(), "API key env var not set")
	require.Contains(t, err.Error(), "ATCR_TEST_KEY_A")
	require.Contains(t, err.Error(), "ATCR_TEST_KEY_C")
}

func TestPreflightAPIKeys_KeyedFallbackRescuesRun(t *testing.T) {
	// Partial success is binding (original-requirements: exit 0 if ≥1 agent
	// succeeds), so the pre-flight must pass when ANY agent in any slot's
	// chain can authenticate — keys resolve per-invocation at run time.
	t.Setenv("ATCR_TEST_KEY_FB", "k")
	require.NoError(t, preflightAPIKeys([]fanout.Slot{
		slotWithKeys("ATCR_TEST_KEY_A", "ATCR_TEST_KEY_FB"),
	}))
}

// writeReviewFixtureConfig writes a user registry (under the isolated HOME)
// and a project .atcr/config.yaml with a single agent whose provider key env
// is never set.
func writeReviewFixtureConfig(t *testing.T) {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(`providers:
  testprov:
    api_key_env: ATCR_TEST_REVIEW_KEY
    base_url: http://127.0.0.1:1/v1
agents:
  bruce:
    provider: testprov
    model: test-model
`), 0o644))
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"), []byte("agents: [bruce]\n"), 0o644))
}

// initGitRepoWithChange creates a repo in the current dir with two commits
// that differ in file content, so payload building has a real diff.
func initGitRepoWithChange(t *testing.T) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	require.NoError(t, os.WriteFile("a.txt", []byte("one\n"), 0o644))
	run("add", "a.txt")
	run("commit", "-q", "-m", "init")
	require.NoError(t, os.WriteFile("a.txt", []byte("one\ntwo\n"), 0o644))
	run("add", "a.txt")
	run("commit", "-q", "-m", "second")
}

func TestReviewCmd_MissingAPIKeyIsUsageError(t *testing.T) {
	isolate(t)
	initGitRepoWithChange(t)
	writeReviewFixtureConfig(t)
	require.Equal(t, 2, execCmd(t, "review", "--base", "HEAD^"))
}

func TestOutputDirFromFlags_Unset(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags(nil))
	dir, err := outputDirFromFlags(cmd)
	require.NoError(t, err)
	require.Equal(t, "", dir, "unset --output-dir must yield empty (default review dir)")
}

func TestOutputDirFromFlags_MutuallyExclusiveWithID(t *testing.T) {
	// AC: --output-dir and --id together are a usage error (exit 2).
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--output-dir", "/tmp/x", "--id", "y"}))
	_, err := outputDirFromFlags(cmd)
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err))
}

func TestOutputDirFromFlags_RelativeResolvedToAbs(t *testing.T) {
	// A relative --output-dir resolves against CWD at flag-parse time.
	isolate(t)
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--output-dir", "out/review"}))
	dir, err := outputDirFromFlags(cmd)
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(dir), "relative path must resolve to absolute, got %q", dir)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(cwd, "out", "review"), dir)
}

func TestOutputDirFromFlags_EmptyValueIsUsageError(t *testing.T) {
	// An explicit empty --output-dir is a usage error, not a scaffold into CWD.
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--output-dir", ""}))
	_, err := outputDirFromFlags(cmd)
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err))
}

func TestOutputDirFromFlags_WhitespacePaddedResolvesTrimmed(t *testing.T) {
	// The validated value (TrimSpace) must equal the resolved absolute path;
	// a leading-space input must not produce a path with a literal space component.
	isolate(t)
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--output-dir", "  out"}))
	dir, err := outputDirFromFlags(cmd)
	require.NoError(t, err)
	cwd, _ := os.Getwd()
	require.Equal(t, filepath.Join(cwd, "out"), dir, "leading spaces must be trimmed before filepath.Abs")
}

// TestReviewCmd_VerifyFlagsRegistered verifies the --verify chaining flags exist
// on reviewCmd with the documented defaults (AC 04-02).
func TestReviewCmd_VerifyFlagsRegistered(t *testing.T) {
	cmd := newReviewCmd()
	for _, name := range []string{"verify", "fresh", "thorough", "min-severity"} {
		require.NotNil(t, cmd.Flags().Lookup(name), "review must define --%s", name)
	}
	v, err := cmd.Flags().GetBool("verify")
	require.NoError(t, err)
	require.False(t, v, "--verify defaults to false")
}

// TestReviewCmd_VerifyInvalidMinSeverity verifies `review --verify` validates
// --min-severity before any review work, failing fast as a usage error (exit 2).
func TestReviewCmd_VerifyInvalidMinSeverity(t *testing.T) {
	isolate(t) // empty CWD: no git repo, but validation runs before range resolution
	code, out := execCmdCapture(t, "review", "--verify", "--min-severity", "BLOCKER")
	require.Equal(t, 2, code)
	require.Contains(t, out, "CRITICAL")
}

// TestReviewCmd_RequireVerifiedNeedsVerifyAndFailOn verifies `review
// --require-verified` is a usage error (exit 2) without both --verify and
// --fail-on: a strict gate with no verdicts would silently pass everything.
func TestReviewCmd_RequireVerifiedNeedsVerifyAndFailOn(t *testing.T) {
	isolate(t)
	// --require-verified alone (no --verify, no --fail-on) → exit 2.
	code, out := execCmdCapture(t, "review", "--require-verified")
	require.Equal(t, 2, code)
	require.Contains(t, out, "--require-verified requires --fail-on and --verify")
	// --require-verified --verify but no --fail-on → exit 2.
	code, _ = execCmdCapture(t, "review", "--require-verified", "--verify")
	require.Equal(t, 2, code)
}

// TestBoolFlag_UndefinedFlagPanics verifies that boolFlag panics when called
// with an undefined flag name — a programming error that must fail loudly
// rather than silently returning false.
func TestBoolFlag_UndefinedFlagPanics(t *testing.T) {
	cmd := newReviewCmd()
	require.Panics(t, func() {
		boolFlag(cmd, "nonexistent-flag")
	})
}

// TestRunReview_ProjectConfigGateActivatedWithoutFlag reproduces the bug where
// runReview calls failOnThreshold (flag-only) instead of resolveGateThreshold
// (flag > project config > registry). A project with fail_on:HIGH must gate
// atcr review even without --fail-on, just as atcr reconcile does.
//
// Observable: --require-verified --verify with project fail_on:HIGH must NOT
// fire the precondition error ("--require-verified requires --fail-on and
// --verify"), because the config-supplied threshold satisfies the gate check.
func TestRunReview_ProjectConfigGateActivatedWithoutFlag(t *testing.T) {
	isolate(t)
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"),
		[]byte("fail_on: HIGH\n"), 0o644))

	_, out := execCmdCapture(t, "review", "--require-verified", "--verify")
	require.NotContains(t, out, "--require-verified requires --fail-on and --verify",
		"project config fail_on must satisfy --require-verified gate precondition without --fail-on flag")
}
