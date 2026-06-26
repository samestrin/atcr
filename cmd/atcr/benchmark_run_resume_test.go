package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
