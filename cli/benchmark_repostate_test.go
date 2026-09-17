package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repoStateMiniPath is the in-repo repo-state fixture suite (1 case, 2 expected
// findings), relative to cli/. Its numbers are known exactly, so these tests do
// not move when the SHIPPED suite gains a case.
const repoStateMiniPath = "../internal/benchmark/testdata/repo-state-mini"

// stubLocatedCompleter raises findings at the fixture's two settling lines, so a
// run scores 1.0 on both halves of the positional metric. app/calc.py:12 is a
// context line (the outside_diff:true expectation) and app/calc.py:8 is an added
// line (the outside_diff:false one).
type stubLocatedCompleter struct{}

func (stubLocatedCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "HIGH|app/calc.py:12|average divides by len(items) with no empty guard|add a guard|correctness|15|return total(items) / len(items)\n" +
		"MEDIUM|app/calc.py:8|safe_total is not None-safe as claimed|handle None explicitly|correctness|15|def safe_total(items):", nil
}

// stubInDiffOnlyCompleter raises ONLY the in-diff finding. It is the reviewer this
// whole tier exists to identify: competent on the diff, blind to unchanged code.
type stubInDiffOnlyCompleter struct{}

func (stubInDiffOnlyCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "MEDIUM|app/calc.py:8|safe_total is not None-safe as claimed|handle None explicitly|correctness|15|def safe_total(items):", nil
}

// T7's headline: a repo-state-v1 invocation runs end to end and reports both
// metrics — the category recall standard-v1 already produced, and the new
// positional recall split by outside_diff.
func TestExecuteRepoStateBenchmarkRun_ReportsBothMetrics(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	rr, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{}, repoStateMiniPath, gen)
	require.NoError(t, err)

	assert.Equal(t, benchmark.FormatRepoStateV1, rr.Suite)
	assert.Equal(t, "1.0.0", rr.SuiteVersion)
	assert.Equal(t, "2026-09-16T12:00:00Z", rr.GeneratedAt, "GeneratedAt is injected, not time.Now")
	assert.Equal(t, []string{"mini-case"}, rr.SuiteCaseIDs)

	require.Len(t, rr.Reviewers, 1, "the category-recall metric is still produced")
	assert.Equal(t, "m-greta", rr.Reviewers[0].Model)
	assert.Equal(t, 1, rr.Reviewers[0].Runs)

	require.Len(t, rr.PositionalRecall, 1, "the new metric is produced alongside it")
	p := rr.PositionalRecall[0]
	assert.Equal(t, "m-greta", p.Model)
	assert.Equal(t, 2, p.ExpectedTotal)
	assert.Equal(t, 2, p.MatchedTotal)
	require.NotNil(t, p.OutsideDiffRecall)
	assert.InDelta(t, 1.0, *p.OutsideDiffRecall, 1e-9)
	require.NotNil(t, p.WithinDiffRecall)
	assert.InDelta(t, 1.0, *p.WithinDiffRecall, 1e-9)
}

// AC4, end to end: a reviewer that reads only the diff scores 1.0 within-diff and
// 0.0 outside it. A blended number would report this reviewer at 0.5 and hide the
// only thing the run measured.
func TestExecuteRepoStateBenchmarkRun_SeparatesTheTwoHalves(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	rr, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubInDiffOnlyCompleter{}, repoStateMiniPath, gen)
	require.NoError(t, err)

	require.Len(t, rr.PositionalRecall, 1)
	p := rr.PositionalRecall[0]
	require.NotNil(t, p.WithinDiffRecall)
	assert.InDelta(t, 1.0, *p.WithinDiffRecall, 1e-9, "the in-diff finding was caught")
	require.NotNil(t, p.OutsideDiffRecall)
	assert.InDelta(t, 0.0, *p.OutsideDiffRecall, 1e-9, "the out-of-diff finding was missed, and says so")
	require.NotNil(t, p.Recall)
	assert.InDelta(t, 0.5, *p.Recall, 1e-9)
}

// AC5 on the produced artifact, not just the type: a real repo-state run-result
// must not carry the new metric on any reviewers[] row.
func TestExecuteRepoStateBenchmarkRun_LeavesPublicRecordAlone(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	rr, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		repoStateMiniPath, time.Unix(0, 0).UTC())
	require.NoError(t, err)

	require.Len(t, rr.Reviewers, 1)
	assert.Equal(t, scorecard.RaisedDenominatorBenchmarkSuite, rr.Reviewers[0].RaisedDenominator,
		"a repo-state row still declares the benchmark denominator, not a production era")
	sub := benchmark.BuildSubmission(*rr, time.Unix(0, 0).UTC())
	assert.Equal(t, benchmark.SourceBenchmarkSuite, sub.Source)
}

// A suite path that is not repo-state-v1 must not reach this runner. Routing is
// the CLI's job; this is the backstop that keeps a mis-route loud.
func TestExecuteRepoStateBenchmarkRun_RefusesAStandardV1Suite(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	_, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, time.Unix(0, 0).UTC())
	require.Error(t, err)
}

// The router is what makes `atcr benchmark run --suite-path <repo-state dir>` work
// at all, and what keeps an existing standard-v1 invocation on its own path.
func TestBenchmarkRunRouting_DetectsTheSuiteFormat(t *testing.T) {
	got, err := benchmark.DetectSuiteFormat(repoStateMiniPath)
	require.NoError(t, err)
	assert.Equal(t, benchmark.FormatRepoStateV1, got)

	got, err = benchmark.DetectSuiteFormat(suiteValidPath)
	require.NoError(t, err)
	assert.NotEqual(t, benchmark.FormatRepoStateV1, got)
}

// runBenchmarkRun silently switches between the two tier runners on the suite
// discriminator — an operator who passed --suite-path could not tell from stderr
// which path actually ran. This drives the real command through both arms with
// the config and completer seams swapped and asserts the routing line names the
// discriminator and the chosen runner in each direction.
func TestRunBenchmarkRun_LogsTheChosenTierRunner(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	restoreCfg := benchmarkLoadConfig
	restoreCompleter := benchmarkNewCompleter
	t.Cleanup(func() {
		benchmarkLoadConfig = restoreCfg
		benchmarkNewCompleter = restoreCompleter
	})
	benchmarkLoadConfig = func(string) (*fanout.ReviewConfig, error) { return cfg, nil }
	benchmarkNewCompleter = func(context.Context) fanout.Completer { return stubLocatedCompleter{} }

	// repo-state arm routes to executeRepoStateBenchmarkRun.
	_, _, stderr := execCmdSplit(t, "benchmark", "run", "--suite-path", repoStateMiniPath)
	assert.Contains(t, stderr, benchmark.FormatRepoStateV1,
		"stderr must name the suite discriminator the routing decision was made on")
	assert.Contains(t, stderr, "executeRepoStateBenchmarkRun",
		"stderr must name the chosen runner so the operator can tell which tier ran")

	// standard-v1 arm routes to executeBenchmarkRun.
	_, _, stderr = execCmdSplit(t, "benchmark", "run", "--suite-path", suiteValidPath)
	assert.Contains(t, stderr, "executeBenchmarkRun",
		"the standard-v1 arm must name its runner too — the silence is the defect")
}

// Moving discriminator reading ahead of Load (runBenchmarkRun, cli/benchmark.go)
// changed the operator-visible error for a suite.json whose suite name is absent
// or blank: it used to fail in Manifest.Validate with "suite name is required";
// it now fails in DetectSuiteFormat with "suite manifest <path> declares no suite
// name", and the old arm is unreachable through `benchmark run`. No test pinned
// either message on this path, so the change was invisible to the suite. This
// pins the new wording — and asserts the old one is gone — so the next reorder
// of these calls surfaces here instead of in front of an operator.
func TestRunBenchmarkRun_NoSuiteNameErrorIsPinned(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"),
		[]byte(`{"suite":"","suite_version":"1.0.0","cases":[]}`), 0o600))

	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	restoreCfg := benchmarkLoadConfig
	t.Cleanup(func() { benchmarkLoadConfig = restoreCfg })
	benchmarkLoadConfig = func(string) (*fanout.ReviewConfig, error) { return cfg, nil }

	// Drives the real cobra RunE like execCmdSplit, but keeps the returned error:
	// the root sets SilenceErrors, so the message reaches the operator through
	// main()'s print of the returned error, not through the command's stderr.
	var outBuf, errBuf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs([]string{"benchmark", "run", "--suite-path", dir})
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	runErr := root.ExecuteContext(context.Background())
	require.Error(t, runErr, "a suite manifest with no suite name must fail run")
	combined := outBuf.String() + errBuf.String() + runErr.Error()
	assert.Contains(t, combined, "declares no suite name",
		"DetectSuiteFormat's message is the operator-visible one now that discriminator reading precedes Load")
	assert.NotContains(t, combined, "suite name is required",
		"the old Manifest.Validate wording is unreachable through benchmark run")
}

// --checkpoint is a standard-v1 feature. Silently ignoring it on a repo-state run
// would let an operator believe a long run was resumable when it was not, which is
// the worst moment to find out.
func TestRunBenchmarkRun_RejectsCheckpointForARepoStateSuite(t *testing.T) {
	err := checkRepoStateFlags(benchmark.FormatRepoStateV1, "some/checkpoint.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checkpoint")

	require.NoError(t, checkRepoStateFlags(benchmark.FormatRepoStateV1, ""),
		"no checkpoint requested is fine")
	require.NoError(t, checkRepoStateFlags("standard-v1", "some/checkpoint.json"),
		"standard-v1 checkpointing is untouched")
}

// Neither the new suite-format routing arm nor the --checkpoint rejection had a
// command-level test: the suite tested DetectSuiteFormat and checkRepoStateFlags
// directly, and the only test driving the real cobra RunE used the standard-v1
// arm — so deleting the routing branch (and the checkpoint refusal) left
// `go test ./cli/...` green while `benchmark run --suite-path <repo-state dir>`
// silently fell back to the standard loader and died. These drive the actual
// command through both arms, per the documented benchmarkLoadConfig /
// benchmarkNewCompleter seams.
func TestRunBenchmarkRun_RepoStateArmReachesStdout(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	restoreCfg := benchmarkLoadConfig
	restoreCompleter := benchmarkNewCompleter
	t.Cleanup(func() {
		benchmarkLoadConfig = restoreCfg
		benchmarkNewCompleter = restoreCompleter
	})
	benchmarkLoadConfig = func(string) (*fanout.ReviewConfig, error) { return cfg, nil }
	benchmarkNewCompleter = func(context.Context) fanout.Completer { return stubLocatedCompleter{} }

	// Deleting the repo-state routing branch makes this fall through to
	// executeBenchmarkRun, whose loader hard-rejects the discriminator — so a
	// silent mis-route fails here instead of in front of an operator.
	code, stdout, _ := execCmdSplit(t, "benchmark", "run", "--suite-path", repoStateMiniPath)
	require.Equal(t, 0, code, stdout)
	assert.Contains(t, stdout, "reviewer_positional_recall",
		"the cobra wiring must actually reach the repo-state runner — its headline metric must reach stdout")
	assert.NotContains(t, stdout, "outside the offered vocabulary",
		"sanity: a clean-vocabulary stub run emits no drift diagnostic on the repo-state arm")
}

func TestRunBenchmarkRun_RejectsCheckpointForARepoStateSuiteThroughTheCommand(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	restoreCfg := benchmarkLoadConfig
	t.Cleanup(func() { benchmarkLoadConfig = restoreCfg })
	benchmarkLoadConfig = func(string) (*fanout.ReviewConfig, error) { return cfg, nil }

	// Drives the real cobra RunE (the helper-level test only calls
	// checkRepoStateFlags): the refusal must fire at the command boundary,
	// before any loader runs.
	var outBuf, errBuf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs([]string{"benchmark", "run", "--suite-path", repoStateMiniPath, "--checkpoint", "cp.json"})
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	runErr := root.ExecuteContext(context.Background())
	require.Error(t, runErr, "the command must refuse --checkpoint on a repo-state suite")
	combined := outBuf.String() + errBuf.String() + runErr.Error()
	assert.Contains(t, combined, "--checkpoint is not supported for a repo-state-v1 suite")
}

// The routing comparison was an exact case-sensitive string match, so a manifest
// declaring "Repo-State-V1" fell through to the standard-v1 arm, missed the
// known-other-format guard, and died on "diff path is required" — the exact
// misleading message the discriminator check exists to prevent (the repo-state
// manifest has no `diff` field at all). Case-insensitive ROUTING keeps that
// message unreachable: the cased discriminator now reaches the repo-state arm,
// where the loader's own exact tier check produces a precise, actionable error.
// (Full case-insensitivity — accepting the cased name at load — needs a change
// in internal/benchmark/repostate.go, outside this session's group scope, so the
// TD row stays open and this test pins the routing half only.)
func TestRunBenchmarkRun_CasedDiscriminatorRoutesToTheRepoStateArm(t *testing.T) {
	dir := t.TempDir()
	// os.CopyFS clones the whole case directory (base/, head files, case.json)
	// into the temp suite so only the manifest's discriminator spelling differs.
	require.NoError(t, os.CopyFS(dir, os.DirFS(repoStateMiniPath)))
	manifest, err := os.ReadFile(filepath.Join(repoStateMiniPath, "suite.json"))
	require.NoError(t, err)
	cased := strings.Replace(string(manifest), `"repo-state-v1"`, `"Repo-State-V1"`, 1)
	require.NotEqual(t, string(manifest), cased, "fixture rewrite must actually change the discriminator")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"), []byte(cased), 0o600))

	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	restoreCfg := benchmarkLoadConfig
	restoreCompleter := benchmarkNewCompleter
	t.Cleanup(func() {
		benchmarkLoadConfig = restoreCfg
		benchmarkNewCompleter = restoreCompleter
	})
	benchmarkLoadConfig = func(string) (*fanout.ReviewConfig, error) { return cfg, nil }
	benchmarkNewCompleter = func(context.Context) fanout.Completer { return stubLocatedCompleter{} }

	// Drives the real cobra RunE: the routing arm must pick the repo-state runner
	// for a cased discriminator (the stderr routing line names it), and the
	// standard loader's misleading message must stay out of the error path.
	_, _, stderr := execCmdSplit(t, "benchmark", "run", "--suite-path", dir)
	assert.Contains(t, stderr, "executeRepoStateBenchmarkRun",
		"a cased repo-state discriminator must route to the repo-state arm, not fall through to the standard loader")
	assert.NotContains(t, stderr, "diff path is required",
		"the misleading standard-v1 message must stay unreachable through benchmark run")
}
