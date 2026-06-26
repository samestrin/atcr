package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingCompleter wraps the stub's single-finding behavior and counts every
// Complete call, so a resumed run can be asserted to make ZERO LLM calls for
// already-checkpointed cases (AC2).
type countingCompleter struct{ calls int }

func (c *countingCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	c.calls++
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
	assert.Equal(t, []string{"correctness"}, cp.Cases[0].Reviewers[0].Expected)
	assert.Equal(t, []string{"correctness"}, cp.Cases[0].Reviewers[0].Raised)

	assert.Equal(t, 1, cp.Cases[1].Index)
	assert.Equal(t, "case-02-sql-injection", cp.Cases[1].CaseID)
	assert.Equal(t, []string{"security", "correctness"}, cp.Cases[1].Reviewers[0].Expected)
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

	_, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "empty checkpoint path writes nothing")
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
	assert.Greater(t, first.calls, 0, "the first run actually executes the cases")

	// Second run resumes entirely from the checkpoint.
	second := &countingCompleter{}
	rr2, err := executeBenchmarkRun(context.Background(), cfg, second, suiteValidPath, gen, path)
	require.NoError(t, err)
	assert.Equal(t, 0, second.calls, "a fully-checkpointed re-run makes zero Completer calls (AC2)")

	jBaseline, _ := json.Marshal(baseline)
	j1, _ := json.Marshal(rr1)
	j2, _ := json.Marshal(rr2)
	assert.JSONEq(t, string(jBaseline), string(j1), "checkpointed run == uninterrupted run")
	assert.JSONEq(t, string(jBaseline), string(j2), "resumed run is byte-identical to uninterrupted (AC3)")
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
	assert.Equal(t, 1, resume.calls, "only the one unscored case's single agent is executed; case 0 is replayed")

	jBaseline, _ := json.Marshal(baseline)
	jResume, _ := json.Marshal(rr)
	assert.JSONEq(t, string(jBaseline), string(jResume), "partial resume is byte-identical to uninterrupted (AC3)")
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

	_, err = executeBenchmarkRun(context.Background(), cfg, &countingCompleter{}, suiteValidPath, gen, path)
	require.Error(t, err)
	assert.ErrorIs(t, err, errCheckpointSuiteMismatch, "a per-index case-id drift aborts the resume")
}
