package scorecard

import (
	"encoding/json"
	"math"
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

// TestEmitForReconcile_NoPoolSummaryClassifiesFromTheFindings covers AC 02-03
// Edge Case 1, with the correction the Phase 2 gate forced.
//
// AC 02-03 says every reviewer on this path gets OutcomeUnknown. Its REASON is
// sound — there is no AgentStatus, so OutcomeClean would assert a successful
// review of the diff that nothing witnessed — but applying it to a reviewer
// NAMED ON A SURVIVING FINDING throws away an observation rather than declining
// to guess. The gate proved the cost: an install that only ever reconciles
// path-anchored reviews would write nothing but unclassified records and could
// never accumulate a trust prior at all, permanently disabling trustExempt and
// demoteByTrust with no diagnostic.
//
// So: named on a finding -> findings (observed). Named only on a Tier-4-routed
// finding -> ungrounded (also observed, and it correctly counts AGAINST the
// reviewer). Never clean, which remains the fabricated claim AC 02-03 forbids.
func TestEmitForReconcile_NoPoolSummaryClassifiesFromTheFindings(t *testing.T) {
	reviewDir := t.TempDir() // no sources/pool/summary.json written at all

	recs := emitAndRead(t, reviewDir, resWith("bruce", "greta"))

	for _, name := range []string{"bruce", "greta"} {
		r := findReviewer(recs, name)
		require.NotNil(t, r, name)
		assert.Equal(t, testOutcomeFindings, r.Outcome, name)
		assert.NotEqual(t, testOutcomeClean, r.Outcome,
			"%s's review was never observed; clean would be a fabricated claim", name)
		assert.NotEqual(t, testOutcomeUnknown, r.Outcome,
			"%s is named on a surviving finding, so it is not unclassifiable", name)
	}
}

// TestEmitForReconcile_RoutedOnlyReviewerIsUngrounded is the other half of the
// gate's HIGH fix. A reviewer whose every finding the Tier 4 content check
// routed out cited anchors that are declared nowhere in the tracked tree — which
// is exactly what ungrounded names, and it is on the ELIGIBLE side on purpose.
// Excluding it would hand a phantom-raiser the same protection the gate exists
// to give a lens with a broken proxy.
func TestEmitForReconcile_RoutedOnlyReviewerIsUngrounded(t *testing.T) {
	reviewDir := t.TempDir()

	res := reconcile.Result{
		Unresolved: []reconcile.JSONFinding{{
			File: "ghost.go", Line: 1, Problem: "p", Reviewers: []string{"phantom"},
		}},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}
	recs := emitAndRead(t, reviewDir, res)

	r := findReviewer(recs, "phantom")
	require.NotNil(t, r)
	assert.Equal(t, testOutcomeUngrounded, r.Outcome)

	kept := eligibleOutcomeRuns([]Record{*r})
	assert.Len(t, kept, 1, "a phantom-raiser must stay scoreable, not be excused")
}

// TestEmitForReconcile_BlankAgentNameIsNotRecorded matches the guard the two
// findings loops already had. A summary naming an agent "" would otherwise emit
// a record for a reviewer literally called "" — and since Phase 2 that record
// also carries an eligible outcome and reaches the trust priors.
func TestEmitForReconcile_BlankAgentNameIsNotRecorded(t *testing.T) {
	reviewDir := t.TempDir()
	writePoolSummary(t, reviewDir,
		fanout.AgentStatus{Agent: "", Status: fanout.StatusOK, FindingsCount: 0},
		fanout.AgentStatus{Agent: "bruce", Status: fanout.StatusOK, FindingsCount: 1},
	)

	recs := emitAndRead(t, reviewDir, resWith("bruce"))

	assert.Nil(t, findReviewer(recs, ""), "a blank-named agent must not become a reviewer record")
	require.NotNil(t, findReviewer(recs, "bruce"))
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

// TestEmitForReconcile_HostileFindingsCountDoesNotPanic guards the availability
// hole the 2.2 adversarial review reproduced: FindingsCount is decoded from a
// summary.json on disk, and internal/mcp/handlers.go reaches EmitForReconcile
// with a CALLER-SUPPLIED review directory. Sizing an allocation from that number
// let a count near math.MaxInt panic makeslice and take the process down from
// inside a function contracted never to fail its caller's reconcile.
//
// Both signs are covered: negative exercises the zero-guard, huge-positive is
// the one that actually crashed.
func TestEmitForReconcile_HostileFindingsCountDoesNotPanic(t *testing.T) {
	for name, count := range map[string]string{
		"huge positive": "9223372036854775807",
		"huge negative": "-9223372036854775808",
	} {
		t.Run(name, func(t *testing.T) {
			reviewDir := t.TempDir()
			pool := filepath.Join(reviewDir, "sources", "pool")
			require.NoError(t, os.MkdirAll(pool, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(pool, "summary.json"),
				[]byte(`{"agents":[{"agent":"bruce","status":"ok","findings_count":`+count+`}],"total":1}`),
				0o644))

			recs := emitAndRead(t, reviewDir, resWith("bruce"))

			bruce := findReviewer(recs, "bruce")
			require.NotNil(t, bruce, "the run must still emit, not crash")
			if count[0] == '-' {
				// A negative count is INCOHERENT — atcr's fan-out cannot produce
				// one — so the status is not classifiable and the honest answer
				// is unknown, which is excluded from scoring. Passing it through
				// lands on clean, which is ELIGIBLE: a crafted summary.json in a
				// caller-supplied MCP directory would otherwise mint durable
				// records asserting a successful clean review that never ran.
				assert.Equal(t, testOutcomeUnknown, bruce.Outcome)
				assert.Empty(t, eligibleOutcomeRuns([]Record{*bruce}),
					"an incoherent status must not produce a scoreable record")
			} else {
				// A huge positive count still means "raised something", so the
				// classification is honest; only the allocation was the problem.
				assert.Equal(t, testOutcomeFindings, bruce.Outcome)
			}
		})
	}
}

// TestRaisedSlotsFor_IsBoundedRegardlessOfCount pins the allocation itself, so a
// future edit cannot quietly restore make([]string, a.FindingsCount) while the
// end-to-end test above keeps passing on a machine with enough memory.
func TestRaisedSlotsFor_IsBoundedRegardlessOfCount(t *testing.T) {
	assert.Nil(t, raisedSlotsFor(fanout.AgentStatus{FindingsCount: 0}))
	assert.Nil(t, raisedSlotsFor(fanout.AgentStatus{FindingsCount: -5}))
	for _, n := range []int{1, 7, 1 << 20, math.MaxInt} {
		got := raisedSlotsFor(fanout.AgentStatus{FindingsCount: n})
		assert.Len(t, got, 1, "count %d must not size the slice", n)
		assert.NotEmpty(t, got, "non-empty is the only property the classifier reads")
	}
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
