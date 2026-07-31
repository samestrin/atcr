package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/audit"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// perAgentMockProvider returns an httptest server speaking the OpenAI
// chat-completions shape that replies with a DIFFERENT finding per model, so a
// multi-agent roster produces a panel of uncorroborated singletons rather than
// the same finding N times. byModel is keyed by the model id liveReviewConfig
// assigns each agent ("m-<agent>"); an unknown model yields no findings.
func perAgentMockProvider(t *testing.T, byModel map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		resp := map[string]any{"choices": []map[string]any{
			{"message": map[string]string{"role": "assistant", "content": byModel[req.Model]}},
		}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// initGitRepoWithWideChange seeds a repo whose HEAD commit adds a single wide
// file, so a review over HEAD^..HEAD has many grounded lines to cite. The stock
// initGitRepoWithChange only touches one line, which the fan-out's grounding
// filter would reject every finding against but the one on that exact line —
// too narrow for a panel of three findings that must stay far enough apart not
// to cluster together.
func initGitRepoWithWideChange(t *testing.T) {
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
	require.NoError(t, os.WriteFile("seed.txt", []byte("seed\n"), 0o644))
	run("add", "seed.txt")
	run("commit", "-q", "-m", "init")

	var b bytes.Buffer
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	require.NoError(t, os.WriteFile("wide.go", b.Bytes(), 0o644))
	run("add", "wide.go")
	run("commit", "-q", "-m", "wide")
}

// TestReviewCmd_OneShotAppliesScorecardTrustPrior covers the one-shot half of the
// epic-35.9 wiring that TestReconcileCmd_AppliesScorecardTrustPrior covers for
// `atcr reconcile`: runReview's in-process reconcile (cli/review.go) must resolve
// and attach scorecard.ResolveTrustPriors() to reclib.Options, not merely tolerate
// its absence. A --fail-on gate is what routes the run into that one-shot block;
// CRITICAL is chosen so the MEDIUM panel never trips the gate and the exit code
// stays 0. "trusted" clears DefaultTrustMinRuns at a 1.0 rate, so its otherwise
// uncorroborated singleton must survive the consensus filter while the two
// no-history reviewers' singletons are sidecarred. All three cite the same file
// at widely separated lines (the only file in the patch), so the assertions key
// on line number. Deleting the TrustPriors line from the one-shot RunReconcile
// call drops the wide.go:10 finding and fails this test.
func TestReviewCmd_OneShotAppliesScorecardTrustPrior(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithWideChange(t)
	seedTrustedReviewer(t, "trusted")

	srv := perAgentMockProvider(t, map[string]string{
		"m-trusted":  "MEDIUM|wide.go:10|possible nil deref on this path|Guard it|correctness|10|ev",
		"m-stranger": "MEDIUM|wide.go:30|unused import lingers in this file|Drop it|style|10|ev",
		"m-third":    "MEDIUM|wide.go:50|request body is not validated|Validate it|correctness|10|ev",
	})
	liveReviewConfig(t, srv.URL, "trusted", "stranger", "third")

	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^", "--fail-on", "CRITICAL"),
		"a MEDIUM-only panel does not trip a CRITICAL gate")

	id, err := os.ReadFile(filepath.Join(".atcr", "latest"))
	require.NoError(t, err)
	dir := filepath.Join(".atcr", "reviews", strings.TrimSpace(string(id)))

	lines := reconciledLines(t, dir)
	require.Contains(t, lines, 10,
		"the one-shot reconcile threads the reviewer trust prior into RunReconcile")
	require.NotContains(t, lines, 30,
		"a singleton from a reviewer with no scorecard history is still sidecarred")
}

// TestReviewCmd_OneShotHonorsConfiguredConsensusOff covers the review half of
// epic 35.9.1's widened T1: runReview must thread the RESOLVED consensus level
// into its in-process reconcile, not just resolve it. No reviewer has scorecard
// history here, so under the strict default all three uncorroborated singletons
// are sidecarred and findings.json is empty — with `consensus: off` in
// .atcr/config.yaml all three must survive at consensus_filtered 0. Replacing
// `Consensus: consensusLevel` with `Consensus: ""` at the one-shot RunReconcile
// call site (cli/review.go) fails this test.
func TestReviewCmd_OneShotHonorsConfiguredConsensusOff(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithWideChange(t)

	srv := perAgentMockProvider(t, map[string]string{
		"m-one":   "MEDIUM|wide.go:10|possible nil deref on this path|Guard it|correctness|10|ev",
		"m-two":   "MEDIUM|wide.go:30|unused import lingers in this file|Drop it|style|10|ev",
		"m-three": "MEDIUM|wide.go:50|request body is not validated|Validate it|correctness|10|ev",
	})
	liveReviewConfig(t, srv.URL, "one", "two", "three")
	appendProjectConfig(t, "consensus: off\n")

	// --fail-on is what routes the run into the one-shot reconcile block; CRITICAL
	// keeps the MEDIUM-only panel from tripping the gate, so exit stays 0.
	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^", "--fail-on", "CRITICAL"))

	dir := latestReviewDir(t)
	require.ElementsMatch(t, []int{10, 30, 50}, reconciledLines(t, dir),
		"off keeps every uncorroborated singleton on the one-shot path")
	require.Equal(t, 0, consensusSummary(t, filepath.Base(dir)),
		"off must leave consensus_filtered at 0")
}

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

// TestReviewCmd_ForceFlagRegistered verifies the --force flag exists on the
// review command and defaults to false (Epic 4.7 AC2).
func TestReviewCmd_ForceFlagRegistered(t *testing.T) {
	cmd := newReviewCmd()
	require.NotNil(t, cmd.Flags().Lookup("force"), "review must define --force")
	v, err := cmd.Flags().GetBool("force")
	require.NoError(t, err)
	require.False(t, v, "--force defaults to false")
}

// TestReviewCmd_NoCacheFlagRegistered verifies the --no-cache flag exists on the
// review command and defaults to false (Epic 5.2: caching on unless opted out).
func TestReviewCmd_NoCacheFlagRegistered(t *testing.T) {
	cmd := newReviewCmd()
	require.NotNil(t, cmd.Flags().Lookup("no-cache"), "review must define --no-cache")
	v, err := cmd.Flags().GetBool("no-cache")
	require.NoError(t, err)
	require.False(t, v, "--no-cache defaults to false (caching active)")
}

// TestReviewCmd_NoIgnoreFlagRegistered verifies the --no-ignore flag exists on
// the review command and defaults to false (Epic 26.0: ignore filtering on
// unless opted out).
func TestReviewCmd_NoIgnoreFlagRegistered(t *testing.T) {
	cmd := newReviewCmd()
	require.NotNil(t, cmd.Flags().Lookup("no-ignore"), "review must define --no-ignore")
	v, err := cmd.Flags().GetBool("no-ignore")
	require.NoError(t, err)
	require.False(t, v, "--no-ignore defaults to false (ignore filtering active)")
}

// TestReviewCmd_SprintPlanFlagRegistered verifies the --sprint-plan flag exists
// on the review command and defaults to empty (diff-wide review when unset), per
// Epic 12.2 AC1.
func TestReviewCmd_SprintPlanFlagRegistered(t *testing.T) {
	cmd := newReviewCmd()
	require.NotNil(t, cmd.Flags().Lookup("sprint-plan"), "review must define --sprint-plan")
	v, err := cmd.Flags().GetString("sprint-plan")
	require.NoError(t, err)
	require.Equal(t, "", v, "--sprint-plan defaults to empty (diff-wide review)")
}

// TestSprintPlanPath_MapsFlag verifies the flag value is read (and trimmed) so it
// can populate ReviewRequest.SprintPlanPath; an unset flag yields empty.
func TestSprintPlanPath_MapsFlag(t *testing.T) {
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--sprint-plan", "  plans/sprint.md  "}))
	require.Equal(t, "plans/sprint.md", sprintPlanPath(cmd), "value is read and trimmed")

	cmd2 := newReviewCmd()
	require.NoError(t, cmd2.ParseFlags(nil))
	require.Equal(t, "", sprintPlanPath(cmd2), "unset flag yields empty")
}

// TestSprintPlanPath_UndefinedFlagPanics verifies sprintPlanPath asserts flag
// existence — an undefined "sprint-plan" flag is a programming error that must
// fail loudly rather than silently returning empty (mirrors boolFlag).
func TestSprintPlanPath_UndefinedFlagPanics(t *testing.T) {
	cmd := &cobra.Command{}
	require.Panics(t, func() {
		sprintPlanPath(cmd)
	})
}

// TestReviewCmd_ResumeAndForceMutuallyExclusive locks AC1b: passing both
// --resume and --force is a usage error (exit 2) regardless of a git repo,
// because the guard fires at the resume branch before any range resolution.
func TestReviewCmd_ResumeAndForceMutuallyExclusive(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "review", "--resume", "latest", "--force")
	require.Equal(t, 2, code, "AC1b: --resume + --force is a usage error (exit 2)")
	require.Contains(t, out, "--resume and --force are mutually exclusive")
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

func TestOutputDirFromFlags_SystemDirRejected(t *testing.T) {
	// Epic 4.3: --output-dir resolving to a system directory is rejected by the
	// input-validation layer (usage error, exit 2) before any review work — not
	// left to the filesystem to refuse mid-run.
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--output-dir", "/etc/atcr"}))
	_, err := outputDirFromFlags(cmd)
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err))
	require.Contains(t, err.Error(), "must not reference system directories")
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

// TestReviewCmd_RequireVerifiedAllowedWithDebate verifies the CLI guard no longer
// rejects --require-verified when --debate is set without --verify. A debate
// uphold/split writes verdict "confirmed" (promoted to VERIFIED), so debate alone
// genuinely produces verified findings — matching the MCP handleDebate, which
// requires only a threshold. The earlier guard made the CLI a usage error while the
// equivalent MCP call worked (a CLI-vs-MCP divergence).
func TestReviewCmd_RequireVerifiedAllowedWithDebate(t *testing.T) {
	isolate(t)
	// --require-verified --debate --fail-on HIGH must NOT fire the precondition
	// error (it will fail later for unrelated reasons in this bare workspace, but
	// not with the require-verified usage message).
	_, out := execCmdCapture(t, "review", "--require-verified", "--debate", "--fail-on", "HIGH")
	require.NotContains(t, out, "--require-verified requires",
		"--require-verified must be allowed with --debate alone (parity with MCP handleDebate)")

	// Still a usage error without a threshold: a strict gate with nothing to gate on.
	code, out2 := execCmdCapture(t, "review", "--require-verified", "--debate")
	require.Equal(t, 2, code)
	require.Contains(t, out2, "--require-verified requires")
}

// TestReviewCmd_SingleModelNeedsDebate verifies `review --single-model` without
// --debate is a usage error (exit 2): the flag only affects the debate stage's
// casting, so set alone it is silently a no-op with no feedback — mirroring the
// --require-verified guard.
func TestReviewCmd_SingleModelNeedsDebate(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "review", "--single-model")
	require.Equal(t, 2, code)
	require.Contains(t, out, "--single-model requires --debate")

	// With --debate it is accepted (fails later for unrelated reasons in this bare
	// workspace, but not with the single-model usage message).
	_, out2 := execCmdCapture(t, "review", "--single-model", "--debate")
	require.NotContains(t, out2, "--single-model requires --debate")
}

// TestReviewCmd_AllRejectsAutoFix verifies `review --all --auto-fix` is a usage
// error (exit 2): a baseline scan resolves no diff range, so auto-fix has no base
// branch to open its fix PR against. Rejecting up front avoids running (and paying
// for) a full repository review and reconcile only to fail at the auto-fix step
// (Sprint 35.0 task 2.11.A finding).
func TestReviewCmd_AllRejectsAutoFix(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "review", "--all", "--auto-fix")
	require.Equal(t, 2, code)
	require.Contains(t, out, "--all cannot be combined with --auto-fix")
}

// TestReviewCmd_DirRejectsAutoFix pins the baseline+auto-fix rejection wording
// for the --dir path: the message must name --dir (the flag the user actually
// passed), not --all (TD: the rejection was hardcoded to --all even though
// baseline := Changed("all") || Changed("dir")).
func TestReviewCmd_DirRejectsAutoFix(t *testing.T) {
	isolate(t)
	require.NoError(t, os.Mkdir("sub", 0o755))
	code, out := execCmdCapture(t, "review", "--dir", "sub", "--auto-fix")
	require.Equal(t, 2, code)
	require.Contains(t, out, "--dir cannot be combined with --auto-fix")
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
// TestReviewCmd_InvalidConfigReportsAllErrors is the Epic 4.2 command-boundary
// acceptance test: `atcr review` with an invalid registry fails fast as a usage
// error (exit 2 — AC5) before any provider call or review-directory creation
// (AC7), and the message lists every validation fault at once (AC6), so the user
// fixes them in one edit (AC9 — fails fast with a clear error).
func TestReviewCmd_InvalidConfigReportsAllErrors(t *testing.T) {
	isolate(t)
	initGitRepoWithChange(t)

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(`timeout_secs: -1
payload_mode: bogus
providers:
  testprov:
    api_key_env: ATCR_TEST_REVIEW_KEY
agents:
  bruce:
    provider: testprov
    min_severity: BOGUS
`), 0o644))
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"), []byte("agents: [bruce]\n"), 0o644))

	code, out := execCmdCapture(t, "review", "--base", "HEAD^")
	require.Equal(t, 2, code, "AC5: invalid config is a usage error (exit 2)")
	// AC6: all faults reported together, not one-at-a-time.
	assert.Contains(t, out, "timeout_secs", "AC3 fault must surface")
	assert.Contains(t, out, "payload_mode", "AC2 fault must surface")
	assert.Contains(t, out, "required field 'model'", "AC1 fault must surface")
	assert.Contains(t, out, "min_severity", "AC2/AC4 fault must surface")
	// AC7: validation must fail BEFORE any review directory is scaffolded. Exit 2
	// above only proves the run aborted; assert no filesystem side effect by
	// proving the managed reviews root was never created on the invalid path. A
	// future regression that validated late (after scaffolding) but still exited 2
	// would pass the checks above yet leave this directory behind.
	_, statErr := os.Stat(filepath.Join(".atcr", "reviews"))
	assert.True(t, os.IsNotExist(statErr),
		"AC7: no review output directory may be created on the invalid-config fail-fast path")
}

func TestRunReview_ProjectConfigGateActivatedWithoutFlag(t *testing.T) {
	isolate(t)
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"),
		[]byte("fail_on: HIGH\n"), 0o644))

	_, out := execCmdCapture(t, "review", "--require-verified", "--verify")
	require.NotContains(t, out, "--require-verified requires --fail-on and --verify",
		"project config fail_on must satisfy --require-verified gate precondition without --fail-on flag")
}

// TestRunReview_SummaryPrintsOnAllAgentsFailed verifies the end-of-review metrics
// summary (Epic 4.4 AC3) is emitted even when every agent fails (exit 1, artifacts
// preserved) — the run an operator most needs the breakdown for. The agent points at
// an unreachable URL so the fan-out fails completely.
func TestRunReview_SummaryPrintsOnAllAgentsFailed(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	writeReviewFixtureConfig(t) // bruce -> http://127.0.0.1:1/v1, unreachable

	code, out := execCmdCapture(t, "review", "--base", "HEAD^")
	require.Equal(t, 1, code, "all agents failed -> exit 1")
	require.Contains(t, out, "Total elapsed:", "AC3 summary must print on the all-agents-failed path")
	require.Contains(t, out, "Agents: 0/", "summary reports zero successes when every agent failed")
}

// TestWriteAuditRecord_EmitsStderrWarningOnFailure verifies that a compliance-
// ledger write failure is surfaced on stderr in addition to the structured log,
// so a systematically failing audit trail cannot be missed in log-only output.
func TestWriteAuditRecord_EmitsStderrWarningOnFailure(t *testing.T) {
	var stderr bytes.Buffer
	dir := t.TempDir()
	// Make the audit path's parent directory read-only so Append cannot create
	// the ledger file, forcing a write failure without relying on platform specifics.
	badDir := filepath.Join(dir, "readonly")
	require.NoError(t, os.Mkdir(badDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(badDir, 0o755) })

	auditPath := filepath.Join(badDir, "audit.log.jsonl")
	n := writeAuditRecord(&stderr, context.Background(), auditPath, dir, time.Now(), 1, "base", "head")
	require.Equal(t, 0, n, "a failed audit write appends zero records")
	assert.Contains(t, stderr.String(), "warning: failed to append audit record:",
		"stderr must carry a visible warning when the audit ledger cannot be written")
}

// TestRunReview_AllAgentsFailedAppendsNoAudit pins the wiring contract documented
// in internal/audit/record.go: the audit hook sits after the all-agents-failed
// error guard, so a review where every agent failed (exit 1, artifacts preserved)
// stamps no compliance-ledger record. A future refactor that moved the hook above
// the guard — recording failed runs — would break this and must be a deliberate
// choice, not an accident.
func TestRunReview_AllAgentsFailedAppendsNoAudit(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	writeReviewFixtureConfig(t) // bruce -> unreachable URL, so every agent fails

	code, _ := execCmdCapture(t, "review", "--base", "HEAD^")
	require.Equal(t, 1, code, "all agents failed -> exit 1")

	recs, err := audit.Load(filepath.Join(".", ".atcr", "audit.log.jsonl"))
	require.NoError(t, err)
	require.Empty(t, recs, "an all-agents-failed review must append no audit record")
}

// TestReviewCmd_SyncCloud_MissingKey_FailFast covers AC 04-03: `review
// --sync-cloud` with an unset ATCR_API_KEY exits exitAuth (3) — and fails fast,
// before any review work, so the check needs no git range or roster.
func TestReviewCmd_SyncCloud_MissingKey_FailFast(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_API_KEY", "")
	require.Equal(t, exitAuth, execCmd(t, "review", "--sync-cloud"))
}

// TestReviewCmd_SyncCloud_InvalidEndpoint_ExitsUsage covers AC 04-02 EC4: a
// non-URL --cloud-endpoint with --sync-cloud set is a usage error (2), before any
// request or review work.
func TestReviewCmd_SyncCloud_InvalidEndpoint_ExitsUsage(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_API_KEY", "valid-key")
	require.Equal(t, exitUsage, execCmd(t, "review", "--sync-cloud", "--cloud-endpoint", "not-a-url"))
}

// TestReviewCmd_SyncCloud_PushesAfterRunOnFailure is the 4.2.A MEDIUM regression:
// the review-path push is DEFERRED to run after the fan-out outcome is finalized.
// Even on an all-agents-failed review (exit 1, artifacts preserved), the push
// still fires (AC 04-02 EC2) with run_outcome="failure" (the gate-aware outcome),
// and a non-auth-successful (200) push preserves the run's exit code.
func TestReviewCmd_SyncCloud_PushesAfterRunOnFailure(t *testing.T) {
	isolate(t)
	initGitRepoWithChange(t)
	writeReviewFixtureConfig(t)
	t.Setenv("ATCR_TEST_REVIEW_KEY", "k") // preflight passes; agents then fail on the unreachable URL

	got := false
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = true
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("ATCR_API_KEY", "valid-key")
	code := execCmd(t, "review", "--base", "HEAD^", "--sync-cloud", "--cloud-endpoint", srv.URL)
	require.Equal(t, 1, code, "all agents failed → exit 1; a successful push preserves it")
	require.True(t, got, "review --sync-cloud must push even when the run failed")
	require.Contains(t, string(body), `"run_outcome":"failure"`)
	// Privacy allowlist: CloudSyncRecord must never carry raw reviewer identities
	// or source file paths — those are hashed/omitted at the boundary.
	assert.NotContains(t, string(body), `"reviewer"`)
	assert.NotContains(t, string(body), `"file"`)
	assert.NotContains(t, string(body), `"path"`)
}

// TestReviewCmd_SyncCloud_AuthRejectionOverridesExit covers AC 04-04 on the
// review path: a 401 from the endpoint overrides the run's own exit code with
// exitAuth (3) — the push runs after the run, so bookkeeping is not skipped, but
// the auth failure wins the final code.
// TestReviewCmd_SyncCloud_WarnsWhenNoResult verifies that when --sync-cloud is
// enabled but the run exits before producing a result, the user sees a clear
// stderr notice instead of a silently skipped push.
func TestReviewCmd_SyncCloud_WarnsWhenNoResult(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_API_KEY", "valid-key")
	root := NewRootCmd()
	var stderr bytes.Buffer
	root.SetOut(io.Discard)
	root.SetErr(&stderr)
	root.SetArgs([]string{"review", "--sync-cloud", "--require-verified"})
	_ = root.ExecuteContext(context.Background())
	require.Contains(t, stderr.String(), "--sync-cloud push skipped because the run did not produce a result")
}

func TestReviewCmd_SyncCloud_AuthRejectionOverridesExit(t *testing.T) {
	isolate(t)
	initGitRepoWithChange(t)
	writeReviewFixtureConfig(t)
	t.Setenv("ATCR_TEST_REVIEW_KEY", "k")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	t.Setenv("ATCR_API_KEY", "bad-key")
	require.Equal(t, exitAuth, execCmd(t, "review", "--base", "HEAD^", "--sync-cloud", "--cloud-endpoint", srv.URL))
}

// TestReviewHelpDocumentsPreviewPrecedence pins the --preview precedence
// documentation (TD cli/review.go:runReview): --preview short-circuits at
// the very top of runReview — above the --resume/--force mutual-exclusion check
// and the --auto-fix handling — so combining it with an action flag silently
// ignores that flag. The flag's own usage string lives in flags.go (shared with
// reconcile), so the precedence note must live in the review command's Long help
// text where a user reading `atcr review --help` will see it.
func TestReviewHelpDocumentsPreviewPrecedence(t *testing.T) {
	cmd := newReviewCmd()
	help := cmd.Long + "\n" + cmd.Example

	assert.Contains(t, help, "--preview", "review help must describe --preview")
	assert.Contains(t, help, "precedence", "review help must state that --preview takes precedence")
	for _, action := range []string{"--auto-fix", "--resume", "--force"} {
		assert.Contains(t, help, action, "review help must name the action flag %s that --preview ignores", action)
	}
	assert.Contains(t, help, "ignored", "review help must state the action flags are ignored when --preview is set")
}

// AC 05-01 Scenario 1 / Edge Case 2: `atcr review --fresh` documents BOTH its
// --verify meaning (preserved verbatim) and its new --all/--dir hash-skip-bypass
// meaning, on a single line.
func TestReviewFreshFlag_UsageDocumentsBothMeanings(t *testing.T) {
	usage := newReviewCmd().Flags().Lookup("fresh").Usage
	assert.Contains(t, usage, "with --verify: re-verify findings that already carry a verdict",
		"the preserved --verify clause must remain verbatim")
	assert.Contains(t, usage, "with --all/--dir:", "a new clause must name the baseline hash-skip bypass")
	assert.NotContains(t, usage, "\n", "the usage string must stay single-line (cobra flag-table style)")
}

// AC 05-01 Scenario 2: `--force`'s usage string is unchanged — no baseline-scan
// wording leaks into it (it is never a skip-bypass alias).
func TestReviewForceFlag_UsageUnchanged(t *testing.T) {
	const want = "overwrite an existing review directory, backing it up to <dir>.bak first (applies to --id and --output-dir collisions; mutually exclusive with --resume)"
	assert.Equal(t, want, newReviewCmd().Flags().Lookup("force").Usage)
}

// AC 05-01 Edge Case 1: --fresh is registered exactly once on `atcr review` (a
// duplicate cmd.Flags().Bool("fresh", ...) would panic at construction), and --fresh
// and --force are distinct flag instances.
func TestReviewFreshFlag_SingleRegistrationDistinctFromForce(t *testing.T) {
	assert.NotPanics(t, func() { _ = newReviewCmd() }, "duplicate --fresh registration would panic")
	cmd := newReviewCmd()
	fresh := cmd.Flags().Lookup("fresh")
	force := cmd.Flags().Lookup("force")
	require.NotNil(t, fresh)
	require.NotNil(t, force)
	assert.NotSame(t, fresh, force, "--fresh and --force must be distinct *pflag.Flag instances")
}

// AC 05-01 Edge Case 3: `atcr verify --fresh` is a separate flag instance whose usage
// string is unchanged (the dual-meaning broadening applies only to `atcr review`).
func TestVerifyFreshFlag_UsageUnchangedAndDistinct(t *testing.T) {
	verifyFresh := newVerifyCmd().Flags().Lookup("fresh")
	require.NotNil(t, verifyFresh)
	assert.Equal(t, "re-verify findings that already carry a verdict", verifyFresh.Usage,
		"atcr verify --fresh must keep its original single meaning")
	assert.NotSame(t, verifyFresh, newReviewCmd().Flags().Lookup("fresh"),
		"review and verify --fresh are separate flag instances")
}

// TestRunReview_InvalidConsensusIgnoredWhenNoReconcile: the config-tier
// consensus value is consumed only by the one-shot in-process reconcile, so a
// plain `atcr review` (no --fail-on/--verify/--debate/--auto-fix) must not
// abort on a bad value — the setting cannot influence that run. A reconciling
// run must still fail fast on it, before any paid fan-out. (Errors are
// asserted on the ExecuteContext return value: the root command silences
// error output and main() does the printing.)
func TestRunReview_InvalidConsensusIgnoredWhenNoReconcile(t *testing.T) {
	isolate(t)
	writeProjectConfig(t, "consensus: bogus\n")

	exec := func(args ...string) error {
		root := NewRootCmd()
		root.SetArgs(args)
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		return root.ExecuteContext(context.Background())
	}

	// No reconcile-triggering flag: the run fails later for other reasons in
	// this bare fixture (no git range), but never with the consensus error.
	err := exec("review", "--base", "HEAD^")
	require.Error(t, err, "the bare fixture cannot complete a review")
	require.NotContains(t, err.Error(), "invalid consensus level",
		"a run that never reconciles must not be aborted by the consensus value")

	// A reconciling run (--verify) still fails fast on the bad value, before
	// any review work.
	err = exec("review", "--base", "HEAD^", "--verify")
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err))
	require.Contains(t, err.Error(), "invalid consensus level",
		"a reconciling run must fail fast on the bad config-tier value")
}

// TestReviewCmd_LogsResolvedConsensusLevel is the one-shot-review half of the
// resolve-time consensus log that cli/reconcile.go already emits. The level can
// come from ~/.config/atcr/registry.yaml with nothing local naming it, and
// consensus_filtered == 0 cannot distinguish "off" from "strict with nothing to
// filter" — so a review that reconciles under a non-default level must say so.
// Logged at resolve time, not post-reconcile, so the record survives a run that
// fails after the level was resolved.
func TestReviewCmd_LogsResolvedConsensusLevel(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithWideChange(t)

	srv := perAgentMockProvider(t, map[string]string{
		"m-one": "MEDIUM|wide.go:10|possible nil deref on this path|Guard it|correctness|10|ev",
	})
	liveReviewConfig(t, srv.URL, "one")
	appendProjectConfig(t, "consensus: off\n")

	// --fail-on is what routes the run into the one-shot reconcile block (the
	// only path that resolves a consensus level at all); CRITICAL keeps the
	// MEDIUM-only finding from tripping the gate, so exit stays 0.
	code, _, stderr := execCmdSplit(t, "review", "--base", "HEAD^", "--fail-on", "CRITICAL")
	require.Equal(t, 0, code, stderr)
	require.Contains(t, stderr, "consensus filter level resolved",
		"the one-shot review path must record the level it resolved")
	require.Contains(t, stderr, "off", "the log must name the level that was in effect")
}

// TestRunReview_ResolvesSharedSettingsInOneLoad is the review half of the
// single-load contract: a reconciling `atcr review` needs both fail_on and
// consensus, and resolving them through two independent resolvers re-parses the
// config tiers per setting — two HTTP GETs of the user registry under
// ATCR_REGISTRY_URL, either of which can fail independently because the tier is
// swallowed best-effort.
func TestRunReview_ResolvesSharedSettingsInOneLoad(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithWideChange(t)
	srv := perAgentMockProvider(t, map[string]string{
		"m-one": "MEDIUM|wide.go:10|possible nil deref on this path|Guard it|correctness|10|ev",
	})
	liveReviewConfig(t, srv.URL, "one")

	// The user registry IS the roster source here, so the served copy must be the
	// real one liveReviewConfig wrote, plus the shared setting under test.
	regDir := filepath.Join(os.Getenv("HOME"), ".config", "atcr")
	local, err := os.ReadFile(filepath.Join(regDir, "registry.yaml"))
	require.NoError(t, err)
	remote := string(local) + "consensus: off\n"
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(remote), 0o644))

	var fetches int32
	regSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&fetches, 1)
		_, _ = w.Write([]byte(remote))
	}))
	t.Cleanup(regSrv.Close)

	t.Setenv("ATCR_REGISTRY_URL", regSrv.URL+"/registry.yaml")

	// --fail-on routes the run into the one-shot reconcile, the only branch that
	// needs BOTH settings.
	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^", "--fail-on", "CRITICAL"))
	assert.Equal(t, int32(1), atomic.LoadInt32(&fetches),
		"a reconciling review must load the user registry once for both shared settings")
}
