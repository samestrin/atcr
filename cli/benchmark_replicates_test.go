package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.11.2.2.8 T2: `benchmark run --replicates N`.

var replicatesGen = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

// N=3 records 3 outcomes per pair per case, each a real call: replicates 2..N
// bypass the run's diff cache, so a later replicate is never a replay of the first.
// Only replicate 1 reaches the scored fold, so coverage still names each case once.
func TestExecuteBenchmarkRun_ReplicatesRecordNOutcomesPerPairPerCase(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"}, [3]string{"kai", "m-kai", "kai"})
	c := &countingCompleter{}

	rr, err := executeBenchmarkRunReplicates(context.Background(), cfg, c, suiteValidPath, replicatesGen, "", 3)
	require.NoError(t, err)

	assert.Equal(t, int32(2*3*2), c.calls.Load(), "2 cases x 3 replicates x 2 reviewers, every one a call")

	require.Len(t, rr.Fit, 12, "one fit row per (pair, case, replicate)")
	type pairCase struct{ model, persona, caseID string }
	reps := map[pairCase][]int{}
	for _, f := range rr.Fit {
		k := pairCase{f.Model, f.Persona, f.CaseID}
		reps[k] = append(reps[k], f.Replicate)
		assert.Equal(t, benchmark.OutcomeFindings, f.Outcome)
		assert.Equal(t, 1, f.Findings)
		assert.Equal(t, 1, f.ChunkCount, "a single-shot persona reviews one chunk")
		assert.Zero(t, f.SilentChunks)
	}
	require.Len(t, reps, 4, "2 pairs x 2 cases")
	for k, r := range reps {
		assert.Equal(t, []int{1, 2, 3}, r, "%v: replicates in order", k)
	}
	assert.Equal(t, "m-greta", rr.Fit[0].Model, "rows are ordered by identity")
	assert.Equal(t, "m-kai", rr.Fit[11].Model)

	// The scored fold saw replicate 1 only.
	require.Len(t, rr.Coverage, 2)
	for _, cov := range rr.Coverage {
		assert.Equal(t, []string{"case-01-nil-deref", "case-02-sql-injection"}, cov.CaseIDs)
		assert.Equal(t, map[string]int{benchmark.OutcomeFindings: 2}, cov.Outcomes)
	}
	for _, r := range rr.Reviewers {
		assert.Equal(t, 2, r.Runs, "Runs is the suite size, not suite size x replicates")
	}
}

// writeRunResultFile writes rr exactly as `benchmark run --output` does.
func writeRunResultFile(t *testing.T, rr *benchmark.RunResult) string {
	t.Helper()
	data, err := json.MarshalIndent(rr, "", "  ")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "run.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// `benchmark export` output is byte-identical to a run without replicates: the
// fit rows are run-result-only, and replicate 1 is scored exactly as an N=1 run.
func TestBenchmarkExport_ReplicatesByteIdenticalToSingleRun(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"}, [3]string{"kai", "m-kai", "kai"})

	one, err := executeBenchmarkRunReplicates(context.Background(), cfg, stubCompleter{}, suiteValidPath, replicatesGen, "", 1)
	require.NoError(t, err)
	three, err := executeBenchmarkRunReplicates(context.Background(), cfg, stubCompleter{}, suiteValidPath, replicatesGen, "", 3)
	require.NoError(t, err)
	require.Len(t, one.Fit, 4)
	require.Len(t, three.Fit, 12)

	codeOne, outOne, errOne := execCmdSplit(t, "benchmark", "export", "--in", writeRunResultFile(t, one), "--suite-path", suiteValidPath)
	require.Equal(t, 0, codeOne, errOne)
	codeThree, outThree, errThree := execCmdSplit(t, "benchmark", "export", "--in", writeRunResultFile(t, three), "--suite-path", suiteValidPath)
	require.Equal(t, 0, codeThree, errThree)
	assert.Equal(t, outOne, outThree, "export of an N=3 run must be byte-identical to an N=1 run")
	assert.NotContains(t, outThree, "reviewer_fit")

	// The run-results differ in the fit rows and nothing else.
	one.Fit, three.Fit = nil, nil
	assert.Equal(t, mustJSON(t, one), mustJSON(t, three))
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)
	return string(data)
}

// A checkpoint written before the replicate and fit fields existed decodes them
// absent and replays byte-identically: it folds the score as before and adds no
// reviewer_fit rows, since it never measured them.
func TestExecuteBenchmarkRun_LegacyCheckpointReplaysByteIdentical(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	path := filepath.Join(t.TempDir(), "ckpt.json")

	fresh, err := executeBenchmarkRun(context.Background(), cfg, usageStubCompleter{}, suiteValidPath, replicatesGen, path)
	require.NoError(t, err)
	require.NotEmpty(t, fresh.Fit)

	// Rewrite the checkpoint in the shape the previous binary wrote: no replicate on
	// a case, no fit fields on a reviewer.
	cp, err := loadCheckpoint(path)
	require.NoError(t, err)
	for i := range cp.Cases {
		require.Equal(t, 1, cp.Cases[i].Replicate, "this binary stamps replicate 1 on the scored run")
		cp.Cases[i].Replicate = 0
		for j := range cp.Cases[i].Reviewers {
			r := &cp.Cases[i].Reviewers[j]
			r.TokensOut, r.ChunkCount, r.SilentChunks, r.TimedOut = 0, 0, 0, false
		}
	}
	require.NoError(t, saveCheckpoint(path, cp))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, key := range []string{`"replicate"`, `"tokens_out"`, `"chunk_count"`, `"silent_chunks"`, `"timed_out"`} {
		require.NotContains(t, string(raw), key, "the legacy file must not carry %s", key)
	}

	c := &usageCountingCompleter{}
	replayed, err := executeBenchmarkRun(context.Background(), cfg, c, suiteValidPath, replicatesGen, path)
	require.NoError(t, err)
	assert.Zero(t, c.calls.Load(), "a fully checkpointed run makes no call")
	assert.Nil(t, replayed.Fit, "a legacy entry recorded no fit data, so it replays none")

	fresh.Fit = nil
	assert.Equal(t, mustJSON(t, fresh), mustJSON(t, replayed),
		"the legacy replay must be byte-identical to the run that wrote it, fit rows aside")
}

// A --replicates run interrupted mid-suite resumes without re-paying for any
// completed replicate and reproduces the same fit rows, with the recorded tokens.
func TestExecuteBenchmarkRun_ReplicatesResumeFromCheckpoint(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	path := filepath.Join(t.TempDir(), "ckpt.json")

	// Case 0's three replicates succeed; case 1 replicate 1 fails and aborts the run.
	_, err := executeBenchmarkRunReplicates(context.Background(), cfg, &usageFailAfterCompleter{ok: 3}, suiteValidPath, replicatesGen, path, 3)
	require.Error(t, err)
	cp, err := loadCheckpoint(path)
	require.NoError(t, err)
	require.Len(t, cp.Cases, 3, "every completed replicate is checkpointed")
	for i, e := range cp.Cases {
		assert.Equal(t, 0, e.Index)
		assert.Equal(t, i+1, e.Replicate)
		require.Len(t, e.Reviewers, 1)
		assert.Equal(t, 500, e.Reviewers[0].TokensOut)
		assert.Equal(t, 1, e.Reviewers[0].ChunkCount)
	}

	c := &usageCountingCompleter{}
	resumed, err := executeBenchmarkRunReplicates(context.Background(), cfg, c, suiteValidPath, replicatesGen, path, 3)
	require.NoError(t, err)
	assert.Equal(t, int32(3), c.calls.Load(), "only case 1's three replicates execute")
	require.Len(t, resumed.Fit, 6)
	for _, f := range resumed.Fit {
		assert.Equal(t, 500, f.TokensOut)
	}

	// A full replay of the finished checkpoint reproduces the resumed run exactly.
	again := &usageCountingCompleter{}
	replayed, err := executeBenchmarkRunReplicates(context.Background(), cfg, again, suiteValidPath, replicatesGen, path, 3)
	require.NoError(t, err)
	assert.Zero(t, again.calls.Load())
	assert.Equal(t, mustJSON(t, resumed), mustJSON(t, replayed))
}

// A checkpoint holding replicates beyond this run's N is refused, naming the value
// that keeps them, rather than dropping paid fit data in silence.
func TestExecuteBenchmarkRun_RefusesCheckpointWithMoreReplicates(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	path := filepath.Join(t.TempDir(), "ckpt.json")
	_, err := executeBenchmarkRunReplicates(context.Background(), cfg, stubCompleter{}, suiteValidPath, replicatesGen, path, 3)
	require.NoError(t, err)

	_, err = executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, replicatesGen, path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "asks for --replicates 1")
	assert.Contains(t, err.Error(), "resume with --replicates 3 or more")
}

// A legacy checkpoint resumed with --replicates 2 replays its scored run and
// executes only the new replicate, which alone carries fit rows.
func TestExecuteBenchmarkRun_LegacyCheckpointGainsReplicates(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	path := filepath.Join(t.TempDir(), "ckpt.json")
	_, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, replicatesGen, path)
	require.NoError(t, err)
	cp, err := loadCheckpoint(path)
	require.NoError(t, err)
	for i := range cp.Cases {
		cp.Cases[i].Replicate = 0
	}
	require.NoError(t, saveCheckpoint(path, cp))

	c := &countingCompleter{}
	rr, err := executeBenchmarkRunReplicates(context.Background(), cfg, c, suiteValidPath, replicatesGen, path, 2)
	require.NoError(t, err)
	assert.Equal(t, int32(2), c.calls.Load(), "one new replicate per case")
	require.Len(t, rr.Fit, 2)
	for _, f := range rr.Fit {
		assert.Equal(t, 2, f.Replicate)
	}
}

// silentUsageCompleter answers NO FINDINGS in 10 tokens on a ~158k-token payload:
// the pace-shaped silent lane T1 counts.
type silentUsageCompleter struct{}

func (silentUsageCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	content, _, _, err := silentUsageCompleter{}.CompleteWithUsage(ctx, inv)
	return content, err
}

func (silentUsageCompleter) CompleteWithUsage(context.Context, llmclient.Invocation) (string, llmclient.UsageData, []llmclient.CallRecord, error) {
	return "NO FINDINGS", llmclient.UsageData{PromptTokens: 158304, CompletionTokens: 10}, nil, nil
}

// The fit row carries the per-replicate silent-chunk count and output tokens from
// the reviewer's AgentStatus, on every replicate.
func TestExecuteBenchmarkRun_FitRowsCarrySilentChunks(t *testing.T) {
	cfg := benchCfg([3]string{"pace", "m-pace", "pace"})
	a := cfg.Registry.Agents["pace"]
	a.Thinking = registry.ThinkingOn
	cfg.Registry.Agents["pace"] = a

	rr, err := executeBenchmarkRunReplicates(context.Background(), cfg, silentUsageCompleter{}, suiteValidPath, replicatesGen, "", 2)
	require.NoError(t, err)
	require.Len(t, rr.Fit, 4)
	for _, f := range rr.Fit {
		assert.Equal(t, benchmark.OutcomeClean, f.Outcome, "silence is a fit signal, not an outcome")
		assert.Equal(t, 0, f.Findings)
		assert.Equal(t, 10, f.TokensOut)
		assert.Equal(t, 1, f.ChunkCount)
		assert.Equal(t, 1, f.SilentChunks, "wholly silent: silent_chunks >= chunk_count")
		assert.False(t, f.TimedOut)
	}
}

func TestExecuteBenchmarkRun_RejectsReplicatesBelowOne(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	_, err := executeBenchmarkRunReplicates(context.Background(), cfg, stubCompleter{}, suiteValidPath, replicatesGen, "", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--replicates must be at least 1, got 0")
}

// The flag reaches the runner through the real command: --replicates 2 on a
// two-case suite makes four calls per reviewer and emits the fit rows on stdout.
func TestBenchmarkRunCmd_ReplicatesFlag(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	c := &countingCompleter{}
	restoreCfg, restoreCompleter := benchmarkLoadConfig, benchmarkNewCompleter
	t.Cleanup(func() { benchmarkLoadConfig, benchmarkNewCompleter = restoreCfg, restoreCompleter })
	benchmarkLoadConfig = func(string) (*fanout.ReviewConfig, error) { return cfg, nil }
	benchmarkNewCompleter = func(context.Context) fanout.Completer { return c }

	code, stdout, stderr := execCmdSplit(t, "benchmark", "run", "--suite-path", suiteValidPath, "--replicates", "2")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, int32(4), c.calls.Load())
	var rr benchmark.RunResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &rr))
	assert.Len(t, rr.Fit, 4)

	err := execBenchmarkCmd("benchmark", "run", "--suite-path", suiteValidPath, "--replicates", "0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--replicates must be at least 1, got 0")
}

// execBenchmarkCmd runs the root command and returns its RunE error, which
// execCmdSplit reduces to an exit code.
func execBenchmarkCmd(args ...string) error {
	var outBuf, errBuf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	return root.ExecuteContext(context.Background())
}

// Replicates are standard-v1 only; a repo-state suite refuses the flag before any
// reviewer runs.
func TestBenchmarkRunCmd_RejectsReplicatesForARepoStateSuite(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	var calls atomic.Int32
	restoreCfg, restoreCompleter := benchmarkLoadConfig, benchmarkNewCompleter
	t.Cleanup(func() { benchmarkLoadConfig, benchmarkNewCompleter = restoreCfg, restoreCompleter })
	benchmarkLoadConfig = func(string) (*fanout.ReviewConfig, error) { return cfg, nil }
	benchmarkNewCompleter = func(context.Context) fanout.Completer {
		calls.Add(1)
		return stubCompleter{}
	}

	err := execBenchmarkCmd("benchmark", "run", "--suite-path", repoStateMiniPath, "--replicates", "2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--replicates is not supported for a repo-state-v1 suite")
	assert.Zero(t, calls.Load(), "refused before a completer is built")
}
