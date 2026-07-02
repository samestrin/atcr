package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingCompleter wraps the stub's single-finding behavior and counts every
// Complete call, so a resumed run can be asserted to make ZERO LLM calls for
// already-checkpointed cases (AC2).
type countingCompleter struct{ calls atomic.Int32 }

func (c *countingCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	c.calls.Add(1)
	return stubCompleter{}.Complete(ctx, inv)
}

// failAfterCompleter succeeds for the first ok calls then errors on every call
// after, simulating a transient mid-suite failure. With a single-agent roster, ok=1
// lets case 0 complete (and checkpoint) while case 1's only agent fails — driving
// the total-roster abort so the checkpoint is asserted to hold exactly the completed
// cases (AC1).
type failAfterCompleter struct {
	ok    int
	calls int
}

func (c *failAfterCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	c.calls++
	if c.calls > c.ok {
		return "", fmt.Errorf("simulated transient failure on call %d", c.calls)
	}
	return stubCompleter{}.Complete(ctx, inv)
}

// AC1: a checkpoint entry is written for each case immediately after it is scored.
func TestExecuteBenchmarkRun_WritesCheckpointPerCase(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "ckpt.json")

	rr, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, path)
	require.NoError(t, err)
	require.Len(t, rr.Reviewers, 1)

	cp, err := loadCheckpoint(path)
	require.NoError(t, err)
	require.NotNil(t, cp)

	assert.Equal(t, "fixture-mini", cp.Suite)
	assert.Equal(t, "1.0.0", cp.SuiteVersion)
	assert.NotEmpty(t, cp.ReproHash, "suite-identity guard key is recorded")

	require.Len(t, cp.Cases, 2, "one checkpoint entry per scored case")
	assert.Equal(t, 0, cp.Cases[0].Index)
	assert.Equal(t, "case-01-nil-deref", cp.Cases[0].CaseID)
	require.Len(t, cp.Cases[0].Reviewers, 1)
	assert.Equal(t, "greta", cp.Cases[0].Reviewers[0].Agent)
	assert.Equal(t, "m-greta", cp.Cases[0].Reviewers[0].Model)
	assert.Equal(t, []string{"correctness"}, cp.Cases[0].Expected)
	assert.Equal(t, []string{"correctness"}, cp.Cases[0].Reviewers[0].Raised)

	assert.Equal(t, 1, cp.Cases[1].Index)
	assert.Equal(t, "case-02-sql-injection", cp.Cases[1].CaseID)
	assert.Equal(t, []string{"security", "correctness"}, cp.Cases[1].Expected)
}

// AC1: a run aborted mid-suite leaves a checkpoint containing exactly the completed
// cases — not the in-flight one, and not a corrupt partial.
func TestExecuteBenchmarkRun_CheckpointHoldsOnlyCompletedCasesOnFailure(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "ckpt.json")

	// ok=1: case 0's single agent succeeds (case scored + checkpointed); case 1's
	// agent fails, triggering the total-roster abort before case 1 is checkpointed.
	_, err := executeBenchmarkRun(context.Background(), cfg, &failAfterCompleter{ok: 1}, suiteValidPath, gen, path)
	require.Error(t, err, "a total-roster case failure still aborts the run (10.2 semantics intact)")

	cp, err := loadCheckpoint(path)
	require.NoError(t, err)
	require.NotNil(t, cp)
	require.Len(t, cp.Cases, 1, "only the case that completed before the failure is checkpointed")
	assert.Equal(t, 0, cp.Cases[0].Index)
	assert.Equal(t, "case-01-nil-deref", cp.Cases[0].CaseID)
}

// AC5: with no checkpoint path, no checkpoint file is written and behavior is the
// 10.2 default (the existing reproducibility tests cover the result itself).
func TestExecuteBenchmarkRun_NoCheckpointPathWritesNothing(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()

	candidate := filepath.Join(dir, "ckpt.json")
	_, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)

	_, err = os.Stat(candidate)
	assert.True(t, os.IsNotExist(err), "empty checkpoint path must not write a file at the candidate location")
}

// AC2 + AC3: re-running the same suite against an existing checkpoint replays every
// checkpointed case (zero Completer calls) and produces a RunResult byte-identical
// to an uninterrupted run.
func TestExecuteBenchmarkRun_FullResumeIsZeroCostAndIdentical(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"}, [3]string{"kai", "m-kai", "kai"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "ckpt.json")

	// Uninterrupted baseline (no checkpoint).
	baseline, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)

	// First checkpointed run fully populates the checkpoint.
	first := &countingCompleter{}
	rr1, err := executeBenchmarkRun(context.Background(), cfg, first, suiteValidPath, gen, path)
	require.NoError(t, err)
	assert.Greater(t, int(first.calls.Load()), 0, "the first run actually executes the cases")

	// Second run resumes entirely from the checkpoint.
	second := &countingCompleter{}
	rr2, err := executeBenchmarkRun(context.Background(), cfg, second, suiteValidPath, gen, path)
	require.NoError(t, err)
	assert.Equal(t, 0, int(second.calls.Load()), "a fully-checkpointed re-run makes zero Completer calls (AC2)")

	jBaseline, err := json.Marshal(baseline)
	require.NoError(t, err)
	j1, err := json.Marshal(rr1)
	require.NoError(t, err)
	j2, err := json.Marshal(rr2)
	require.NoError(t, err)
	assert.Equal(t, string(jBaseline), string(j1), "checkpointed run == uninterrupted run")
	assert.Equal(t, string(jBaseline), string(j2), "resumed run is byte-identical to uninterrupted (AC3)")
}

// AC2 + AC3 (partial): a run that completed only case 0 before failing resumes by
// replaying case 0 (zero calls for it) and executing only case 1, yielding a result
// identical to an uninterrupted run.
func TestExecuteBenchmarkRun_PartialResumeExecutesOnlyRemainder(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "ckpt.json")

	baseline, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)

	// First attempt completes case 0 then fails on case 1.
	_, err = executeBenchmarkRun(context.Background(), cfg, &failAfterCompleter{ok: 1}, suiteValidPath, gen, path)
	require.Error(t, err)

	// Resume: case 0 replayed (no call), only case 1's single agent executes.
	resume := &countingCompleter{}
	rr, err := executeBenchmarkRun(context.Background(), cfg, resume, suiteValidPath, gen, path)
	require.NoError(t, err)
	assert.Equal(t, 1, int(resume.calls.Load()), "only the one unscored case's single agent is executed; case 0 is replayed")

	jBaseline, err := json.Marshal(baseline)
	require.NoError(t, err)
	jResume, err := json.Marshal(rr)
	require.NoError(t, err)
	assert.Equal(t, string(jBaseline), string(jResume), "partial resume is byte-identical to uninterrupted (AC3)")
}

// AC4: a checkpoint whose recorded suite identity differs from the current suite is
// rejected (fail-closed), never silently mixed into the new run.
func TestExecuteBenchmarkRun_RejectsMismatchedCheckpoint(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "ckpt.json")

	// Hand-write a checkpoint with a stale repro hash but the right suite/version.
	stale := &runCheckpoint{ReproHash: "deadbeefdeadbeef", Suite: "fixture-mini", SuiteVersion: "1.0.0"}
	require.NoError(t, saveCheckpoint(path, stale))

	_, err := executeBenchmarkRun(context.Background(), cfg, &countingCompleter{}, suiteValidPath, gen, path)
	require.Error(t, err)
	assert.ErrorIs(t, err, errCheckpointSuiteMismatch, "a changed suite identity aborts the resume (AC4)")
}

// AC5: `benchmark run` exposes an opt-in --checkpoint flag whose default is empty
// (off) — present behavior is unchanged unless the operator passes a path.
func TestBenchmarkRunCmd_HasOptionalCheckpointFlag(t *testing.T) {
	cmd := newBenchmarkRunCmd()
	f := cmd.Flags().Lookup("checkpoint")
	require.NotNil(t, f, "benchmark run exposes a --checkpoint flag")
	assert.Equal(t, "", f.DefValue, "checkpoint is opt-in: default empty = off")
}

// AC4 (roster guard): ReproHash covers only suite content, NOT the reviewer roster.
// A roster change (added/removed reviewer) across a resume would silently mix stale
// checkpointed reviewers with freshly-executed ones, so a roster-set drift must fail
// closed — mirroring fanout's ErrRosterChanged precedent.
func TestExecuteBenchmarkRun_RejectsRosterMembershipDrift(t *testing.T) {
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "ckpt.json")

	cfg1 := benchCfg([3]string{"greta", "m-greta", "greta"}, [3]string{"kai", "m-kai", "kai"})
	_, err := executeBenchmarkRun(context.Background(), cfg1, stubCompleter{}, suiteValidPath, gen, path)
	require.NoError(t, err)

	// Resume with kai swapped for zara — same suite, changed roster.
	cfg2 := benchCfg([3]string{"greta", "m-greta", "greta"}, [3]string{"zara", "m-zara", "zara"})
	resume := &countingCompleter{}
	_, err = executeBenchmarkRun(context.Background(), cfg2, resume, suiteValidPath, gen, path)
	require.Error(t, err)
	assert.ErrorIs(t, err, errCheckpointRosterMismatch, "a changed roster aborts the resume")
	assert.Equal(t, 0, int(resume.calls.Load()), "the roster guard fires before any review executes")
}

// AC4 (roster guard): a same-name reviewer whose configured model changed is also a
// roster drift — the checkpoint's recorded model would otherwise be replayed against
// new cases run with the new model.
func TestExecuteBenchmarkRun_RejectsRosterModelDrift(t *testing.T) {
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "ckpt.json")

	cfg1 := benchCfg([3]string{"greta", "m-greta", "greta"})
	_, err := executeBenchmarkRun(context.Background(), cfg1, stubCompleter{}, suiteValidPath, gen, path)
	require.NoError(t, err)

	cfg2 := benchCfg([3]string{"greta", "m-greta-v2", "greta"}) // same agent, new model
	_, err = executeBenchmarkRun(context.Background(), cfg2, &countingCompleter{}, suiteValidPath, gen, path)
	require.Error(t, err)
	assert.ErrorIs(t, err, errCheckpointRosterMismatch, "a changed model aborts the resume")
}

// AC4 (defense in depth): ReproHash is order-independent, so a suite whose cases are
// merely reordered shares the same hash but a different index->case mapping. The
// per-index CaseID guard catches that drift and fails closed rather than silently
// replaying a checkpointed case's score against a different case at the same index.
func TestExecuteBenchmarkRun_RejectsCheckpointCaseIDDrift(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "ckpt.json")

	// Produce a valid checkpoint (correct repro hash / suite / version).
	_, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, path)
	require.NoError(t, err)

	// Tamper only the recorded CaseID at index 0 — hash/suite/version stay valid, so
	// validateCheckpoint passes and the per-index id guard is what must fire.
	cp, err := loadCheckpoint(path)
	require.NoError(t, err)
	cp.Cases[0].CaseID = "some-other-case"
	require.NoError(t, saveCheckpoint(path, cp))

	resume := &countingCompleter{}
	_, err = executeBenchmarkRun(context.Background(), cfg, resume, suiteValidPath, gen, path)
	require.Error(t, err)
	assert.ErrorIs(t, err, errCheckpointCaseMismatch, "a per-index case-id drift aborts the resume")
	assert.Equal(t, 0, int(resume.calls.Load()), "per-index guard fires before any LLM call")
}

// Resume reports how many cases are being replayed from the checkpoint vs
// executed fresh, so an operator can tell the run resumed and gauge progress.
func TestExecuteBenchmarkRun_ResumeReportsReplayedCount(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "ckpt.json")

	// First run completes the suite and writes a checkpoint.
	_, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, path)
	require.NoError(t, err)

	// Capture stderr from the resumed run.
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	_, err = executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, path)
	require.NoError(t, err)

	require.NoError(t, w.Close())
	<-done
	os.Stderr = oldStderr

	out := buf.String()
	assert.Contains(t, out, "Resuming")
	assert.Contains(t, out, "replayed 2")
	assert.Contains(t, out, "0 remaining to execute")
}

// TestExecuteBenchmarkRun_ResumeRestoresStderrOnError guards against the leak in
// TestExecuteBenchmarkRun_ResumeReportsReplayedCount: os.Stderr is swapped for a pipe
// before the resumed executeBenchmarkRun call, but only restored after a
// require.NoError(t, err) that assumes success. If a resumed run ever errors, that
// assertion aborts the goroutine before the restore runs, leaving os.Stderr pointed at
// a dangling pipe for every later test in the process. Reproduced here via a subtest:
// the subtest's own restore never runs because require.NoError(t, err) is expected to
// fail (the resumed call is forced to error), so the parent test observes os.Stderr
// still swapped once the subtest goroutine exits.
func TestExecuteBenchmarkRun_ResumeRestoresStderrOnError(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "ckpt.json")

	// First attempt completes case 0 then fails on case 1, leaving a partial checkpoint.
	_, err := executeBenchmarkRun(context.Background(), cfg, &failAfterCompleter{ok: 1}, suiteValidPath, gen, path)
	require.Error(t, err)

	oldStderr := os.Stderr

	t.Run("resume_fails", func(t *testing.T) {
		r, w, pipeErr := os.Pipe()
		require.NoError(t, pipeErr)
		os.Stderr = w

		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(io.Discard, r)
			close(done)
		}()

		// Only case 1 remains; forcing its single agent call to error mirrors an
		// operational failure mid-resume.
		_, err := executeBenchmarkRun(context.Background(), cfg, &failAfterCompleter{ok: 0}, suiteValidPath, gen, path)
		require.NoError(t, err)

		require.NoError(t, w.Close())
		<-done
		os.Stderr = oldStderr
	})

	assert.Equal(t, oldStderr, os.Stderr, "os.Stderr must be restored even when the resumed run errors")
}

// mustMarshal returns v as compact JSON, failing the test on error. Used to assert
// two RunResults are byte-identical.
func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// suiteOneCasePath is a single-case fixture suite, exercising the doneIndex/replay
// loop at the N=1 boundary (every other resume test uses the 2-case suite-valid).
const suiteOneCasePath = "../../internal/benchmark/testdata/suite-one-case"

// The single-case boundary (TD: doneIndex/replay loop untested at the extremes): a
// checkpointed run completes and records exactly one entry, and a re-run replays it
// (zero Completer calls) byte-identical to an uninterrupted run.
func TestExecuteBenchmarkRun_OneCaseSuiteCheckpointsAndResumes(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "ckpt.json")

	baseline, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteOneCasePath, gen, "")
	require.NoError(t, err)

	first := &countingCompleter{}
	rr1, err := executeBenchmarkRun(context.Background(), cfg, first, suiteOneCasePath, gen, path)
	require.NoError(t, err)
	assert.Greater(t, int(first.calls.Load()), 0, "the first run executes the single case")

	cp, err := loadCheckpoint(path)
	require.NoError(t, err)
	require.NotNil(t, cp)
	require.Len(t, cp.Cases, 1, "exactly one checkpoint entry for a one-case suite")
	assert.Equal(t, 0, cp.Cases[0].Index)

	second := &countingCompleter{}
	rr2, err := executeBenchmarkRun(context.Background(), cfg, second, suiteOneCasePath, gen, path)
	require.NoError(t, err)
	assert.Equal(t, 0, int(second.calls.Load()), "a fully-checkpointed one-case re-run makes zero Completer calls")

	assert.Equal(t, mustMarshal(t, baseline), mustMarshal(t, rr1), "checkpointed one-case run == uninterrupted")
	assert.Equal(t, mustMarshal(t, baseline), mustMarshal(t, rr2), "resumed one-case run is byte-identical to uninterrupted")
}

// The zero-case boundary is unreachable through the public path: benchmark.Validate
// requires "at least one case" (internal/benchmark/benchmark.go:95), so an empty
// suite is rejected at Load before any checkpoint logic runs. The original TD item
// assumed a zero-case run would write an empty-Cases checkpoint and resume to zero
// cost — that can never happen because the suite contract forbids empty suites. The
// honest boundary assertion is therefore that an empty suite fails closed and writes
// no checkpoint file at all.
func TestExecuteBenchmarkRun_EmptySuiteIsRejectedAndWritesNoCheckpoint(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"),
		[]byte(`{"suite":"fixture-empty","suite_version":"1.0.0","cases":[]}`), 0o600))
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	ckpt := filepath.Join(t.TempDir(), "ckpt.json")

	_, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, dir, gen, ckpt)
	require.Error(t, err, "an empty suite is rejected by the suite contract (at least one case)")

	_, statErr := os.Stat(ckpt)
	assert.True(t, os.IsNotExist(statErr), "a rejected empty suite must not write a checkpoint file")
}

// usageStubCompleter implements fanout.UsageCompleter, reporting a fixed non-zero
// token usage per call so the benchmark's usage-gated cost/latency path runs with
// non-zero values. stubCompleter reports no usage (usageReported stays false), so
// without this CostUSD/LatencyP50MS are always 0 and the AC3 byte-identical claim is
// vacuous. It satisfies fanout.Completer via Complete; the engine selects
// CompleteWithUsage via a type assertion in invokeSingleShot.
type usageStubCompleter struct{}

func (usageStubCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	content, _, _, err := usageStubCompleter{}.CompleteWithUsage(ctx, inv)
	return content, err
}

func (usageStubCompleter) CompleteWithUsage(ctx context.Context, inv llmclient.Invocation) (string, llmclient.UsageData, []llmclient.CallRecord, error) {
	content, err := stubCompleter{}.Complete(ctx, inv)
	return content, llmclient.UsageData{PromptTokens: 1000, CompletionTokens: 500}, nil, err
}

// usageCountingCompleter is usageStubCompleter with a call counter, so a resumed run
// can be asserted to make zero Completer calls while still reporting usage.
type usageCountingCompleter struct{ calls atomic.Int32 }

func (c *usageCountingCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	content, _, _, err := c.CompleteWithUsage(ctx, inv)
	return content, err
}

func (c *usageCountingCompleter) CompleteWithUsage(ctx context.Context, inv llmclient.Invocation) (string, llmclient.UsageData, []llmclient.CallRecord, error) {
	c.calls.Add(1)
	content, err := stubCompleter{}.Complete(ctx, inv)
	return content, llmclient.UsageData{PromptTokens: 1000, CompletionTokens: 500}, nil, err
}

// usageFailAfterCompleter is failAfterCompleter's usage-reporting twin: it reports
// non-zero usage for the first ok calls then errors, driving the partial-resume path
// with the cost/latency replay active. A single-agent roster keeps the call sequence
// deterministic (no concurrent increment of calls).
type usageFailAfterCompleter struct {
	ok    int
	calls int
}

func (c *usageFailAfterCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	content, _, _, err := c.CompleteWithUsage(ctx, inv)
	return content, err
}

func (c *usageFailAfterCompleter) CompleteWithUsage(ctx context.Context, inv llmclient.Invocation) (string, llmclient.UsageData, []llmclient.CallRecord, error) {
	c.calls++
	if c.calls > c.ok {
		return "", llmclient.UsageData{}, nil, fmt.Errorf("simulated transient failure on call %d", c.calls)
	}
	content, err := stubCompleter{}.Complete(ctx, inv)
	return content, llmclient.UsageData{PromptTokens: 1000, CompletionTokens: 500}, nil, err
}

// AC3 (usage-gated): the per-reviewer CostUSD and latency median must reproduce
// byte-identical across a full resume with NON-ZERO usage. Every other resume test
// uses stubCompleter (no usage), so CostUSD/LatencyP50MS are 0 and the byte-identical
// claim is vacuous. Two priced reviewers (ComputeCostUSD is non-zero for these model
// ids per rates.go) drive a non-trivial cost sum and a multi-sample latency median.
// LatencyP50MS is 0 across all runs (the in-memory stub call the engine times is
// instantaneous), so cost is the load-bearing non-zero quantity being verified.
func TestExecuteBenchmarkRun_UsageGatedFullResumeReproducesCostAndLatency(t *testing.T) {
	cfg := benchCfg(
		[3]string{"greta", "claude-sonnet-4-6", "greta"},
		[3]string{"kai", "gpt-4o-mini", "kai"},
	)
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "ckpt.json")

	baseline, err := executeBenchmarkRun(context.Background(), cfg, usageStubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)
	require.Len(t, baseline.Reviewers, 2)

	// Guard against silently regressing to the vacuous all-zero state.
	var total float64
	for _, rv := range baseline.Reviewers {
		require.NotNil(t, rv.CostPerCorroboratedFindingUSD, "priced reviewer with corroborated findings must carry the key")
		total += *rv.CostPerCorroboratedFindingUSD
	}
	require.Greater(t, total, 0.0, "priced usage stub must yield a non-zero per-finding cost or the assertion is vacuous")

	first := &usageCountingCompleter{}
	rr1, err := executeBenchmarkRun(context.Background(), cfg, first, suiteValidPath, gen, path)
	require.NoError(t, err)
	assert.Greater(t, int(first.calls.Load()), 0, "the first run executes the cases")

	resume := &usageCountingCompleter{}
	rr2, err := executeBenchmarkRun(context.Background(), cfg, resume, suiteValidPath, gen, path)
	require.NoError(t, err)
	assert.Equal(t, 0, int(resume.calls.Load()), "a fully-checkpointed resume makes zero Completer calls")

	// rr2 resumes from the checkpoint rr1 wrote; it must reproduce rr1 byte-identical.
	// This is the deterministic replay-fidelity check for BOTH the CostUSD sum and the
	// LatencyP50MS median — rr2 folds rr1's stored per-reviewer cost and latency back
	// in case order.
	assert.Equal(t, mustMarshal(t, rr1), mustMarshal(t, rr2),
		"resume reproduces the checkpointed run byte-identical (cost sum + latency median)")

	// Cost is deterministic (fixed tokens + priced models), so the resumed run's
	// per-reviewer cost equals an uninterrupted baseline too. Latency is NOT asserted
	// against a separate baseline: the engine times wall-clock DurationMS, so only a
	// replayed run reproduces stored latency exactly — two independent runs match only
	// when every sample rounds to 0ms, which is not guaranteed.
	require.Len(t, rr2.Reviewers, len(baseline.Reviewers))
	for i := range baseline.Reviewers {
		assert.Equal(t, baseline.Reviewers[i].Model, rr2.Reviewers[i].Model)
		assert.Equal(t, baseline.Reviewers[i].CostPerCorroboratedFindingUSD, rr2.Reviewers[i].CostPerCorroboratedFindingUSD,
			"resumed per-reviewer cost equals uninterrupted (AC3 cost replay)")
	}
}

// AC3 (usage-gated, partial): a run that completed only case 0 (with usage) before
// failing resumes by replaying case 0's non-zero checkpointed cost and executing only
// case 1, yielding a result identical to an uninterrupted run. A single priced
// reviewer keeps the call sequence deterministic.
func TestExecuteBenchmarkRun_UsageGatedPartialResumeReproducesCost(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "claude-sonnet-4-6", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "ckpt.json")

	baseline, err := executeBenchmarkRun(context.Background(), cfg, usageStubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)
	require.Len(t, baseline.Reviewers, 1)
	require.NotNil(t, baseline.Reviewers[0].CostPerCorroboratedFindingUSD, "priced reviewer with corroborated findings must carry the key")
	require.Greater(t, *baseline.Reviewers[0].CostPerCorroboratedFindingUSD, 0.0, "priced usage stub yields a non-zero cost")

	// First attempt completes case 0 (with usage, checkpointed) then fails on case 1.
	_, err = executeBenchmarkRun(context.Background(), cfg, &usageFailAfterCompleter{ok: 1}, suiteValidPath, gen, path)
	require.Error(t, err)

	cp, err := loadCheckpoint(path)
	require.NoError(t, err)
	require.Len(t, cp.Cases, 1, "only case 0 is checkpointed before the failure")
	require.Len(t, cp.Cases[0].Reviewers, 1)
	assert.Greater(t, cp.Cases[0].Reviewers[0].CostUSD, 0.0, "the checkpointed case records a non-zero usage cost to replay")

	// Resume: case 0 replayed (non-zero checkpointed cost folded back in), only case 1
	// executes.
	resume := &usageCountingCompleter{}
	rr, err := executeBenchmarkRun(context.Background(), cfg, resume, suiteValidPath, gen, path)
	require.NoError(t, err)
	assert.Equal(t, 1, int(resume.calls.Load()), "only the one unscored case executes; case 0 is replayed")

	// Cost is deterministic, so the partial resume's per-reviewer cost equals the
	// uninterrupted baseline (case 0's replayed cost + case 1's freshly-computed cost).
	// Latency is wall-clock, so it is not compared against the independent baseline.
	require.Len(t, rr.Reviewers, len(baseline.Reviewers))
	for i := range baseline.Reviewers {
		assert.Equal(t, baseline.Reviewers[i].CostPerCorroboratedFindingUSD, rr.Reviewers[i].CostPerCorroboratedFindingUSD,
			"partial resume per-reviewer cost equals uninterrupted (replayed case 0 + executed case 1)")
	}
}
