package scorecard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Outcome values are plain literals here for the import-cycle reason recorded in
// outcome_schema_test.go and sprint 36.0 clarification C5; cli/ holds the pin
// that keeps them equal to internal/benchmark's constants.

// writePoolSummary persists an exact PoolSummary at the path ReadPoolSummary
// expects. The tests below build AgentStatus shapes directly rather than going
// through fanout.WritePool: the point is to pin what EmitForReconcile does with
// a given status, so the status has to be the test's input, not the fan-out
// writer's derived output.
func writePoolSummary(t *testing.T, reviewDir string, agents ...fanout.AgentStatus) {
	t.Helper()
	pool := filepath.Join(reviewDir, "sources", "pool")
	require.NoError(t, os.MkdirAll(pool, 0o755))
	b, err := json.Marshal(fanout.PoolSummary{Agents: agents, Total: len(agents)})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(pool, "summary.json"), b, 0o644))
}

// emitAndRead runs EmitForReconcile against a HOME-overridden store and returns
// the records it wrote.
func emitAndRead(t *testing.T, reviewDir string, res reconcile.Result) []Record {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	EmitForReconcile(reviewDir, res, EmitOpts{})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)
	return recs
}

func resWith(reviewers ...string) reconcile.Result {
	findings := make([]reconcile.Merged, 0, len(reviewers))
	for _, r := range reviewers {
		findings = append(findings, reconcile.Merged{
			Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p", Reviewers: []string{r}},
		})
	}
	return reconcile.Result{
		Findings: findings,
		Summary:  reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}
}

// TestEmitForReconcile_PopulatesOutcomePerReviewer is AC 02-03's happy path.
// Three reviewers, three different reasons for their counts, one run.
func TestEmitForReconcile_PopulatesOutcomePerReviewer(t *testing.T) {
	reviewDir := t.TempDir()
	writePoolSummary(t, reviewDir,
		// Raised something that survived: findings.
		fanout.AgentStatus{Agent: "bruce", Status: fanout.StatusOK, FindingsCount: 2, Model: "opus"},
		// Read the diff, found nothing, nothing went wrong: clean.
		fanout.AgentStatus{Agent: "vera", Status: fanout.StatusOK, FindingsCount: 0, Model: "opus"},
		// The epic's motivating case: a LiteLLM timeout is not a judgment.
		fanout.AgentStatus{Agent: "kai", Status: "error", Error: "context deadline exceeded", Model: "kimi"},
	)

	recs := emitAndRead(t, reviewDir, resWith("bruce"))

	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce)
	assert.Equal(t, testOutcomeFindings, bruce.Outcome)

	vera := findReviewer(recs, "vera")
	require.NotNil(t, vera)
	assert.Equal(t, testOutcomeClean, vera.Outcome,
		"a reviewer that ran and found nothing is clean, never unknown")

	kai := findReviewer(recs, "kai")
	require.NotNil(t, kai)
	assert.Equal(t, testOutcomeFailed, kai.Outcome,
		"the fault is recorded truthfully here; exclusion is the trust filter's job")
}

// TestEmitForReconcile_RaisedIsTheAgentsPostEnforcementCount closes AC 02-02
// Scenario 0, the parity test's stated blind spot.
//
// bruce's own post-enforcement count is 2, but reconcile merged both of his
// findings into clusters credited to other reviewers, so res.Findings names him
// nowhere. Deriving `raised` from res.Findings would classify him CLEAN — "read
// the diff and found nothing" — about a reviewer that raised two findings. The
// count on his AgentStatus is the number the benchmark path feeds the same
// parameter, so it is the one this path must feed too.
func TestEmitForReconcile_RaisedIsTheAgentsPostEnforcementCount(t *testing.T) {
	reviewDir := t.TempDir()
	writePoolSummary(t, reviewDir,
		fanout.AgentStatus{Agent: "bruce", Status: fanout.StatusOK, FindingsCount: 2, Model: "opus"},
	)

	// res credits the merged findings to greta alone; bruce is absent from it.
	recs := emitAndRead(t, reviewDir, resWith("greta"))

	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce)
	assert.Equal(t, testOutcomeFindings, bruce.Outcome)
	assert.NotEqual(t, testOutcomeClean, bruce.Outcome,
		"raised must come from AgentStatus.FindingsCount, not from res.Findings")
}

// TestEmitForReconcile_NoPoolSummaryYieldsUnknownOutcome is AC 02-03 Edge Case
// 1. A path-anchored review has no AgentStatus to classify, and clean would
// assert a successful review nobody observed.
func TestEmitForReconcile_NoPoolSummaryYieldsUnknownOutcome(t *testing.T) {
	reviewDir := t.TempDir() // no sources/pool/summary.json written at all

	recs := emitAndRead(t, reviewDir, resWith("bruce", "greta"))

	for _, name := range []string{"bruce", "greta"} {
		r := findReviewer(recs, name)
		require.NotNil(t, r, name)
		assert.Equal(t, testOutcomeUnknown, r.Outcome, name)
		assert.NotEqual(t, testOutcomeClean, r.Outcome,
			"%s was never classified; clean would be a fabricated claim", name)
	}
}

// TestEmitForReconcile_OutOfVocabularyOutcomeIsCoercedToUnknown is AC 02-03
// Error Scenario 1. The real classifier cannot produce an invalid value, so this
// guards the COERCION, which is what protects the durable store if it ever can.
func TestEmitForReconcile_OutOfVocabularyOutcomeIsCoercedToUnknown(t *testing.T) {
	assert.False(t, fanout.ValidReviewerOutcome("banana"))

	// The guard is expressed as: write the classified value only when it passes
	// ValidReviewerOutcome, else write unknown. Exercised directly because the
	// classifier has no seam to make it lie.
	got := coerceOutcome("banana")
	assert.Equal(t, testOutcomeUnknown, got)
	assert.Equal(t, testOutcomeFiltered, coerceOutcome(testOutcomeFiltered))
	assert.Equal(t, testOutcomeUnknown, coerceOutcome(testOutcomeUnknown))
}

// TestEmitForReconcile_NoScorecardDoesNoOutcomeWork is AC 02-03 Error Scenario
// 2: suppression still short-circuits ahead of the pool-summary read, so the
// new classification work cannot weaken the zero-I/O guarantee.
func TestEmitForReconcile_NoScorecardDoesNoOutcomeWork(t *testing.T) {
	reviewDir := t.TempDir()
	writePoolSummary(t, reviewDir,
		fanout.AgentStatus{Agent: "bruce", Status: fanout.StatusOK, FindingsCount: 2},
	)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	EmitForReconcile(reviewDir, resWith("bruce"), EmitOpts{NoScorecard: true})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(cfg, "atcr", "scorecard"))
	assert.True(t, os.IsNotExist(err), "suppressed run must create no store directory")
}
