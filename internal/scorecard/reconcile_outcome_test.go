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

// TestEmitForReconcile_NoPoolSummaryClassifiesFromTheFindings pins AC 02-03
// Edge Case 1's real boundary, after three gate passes disagreed about it.
//
// AC 02-03 prescribes OutcomeUnknown here. Its reasoning is sound for
// OutcomeClean — with no AgentStatus, clean would assert a successful review of
// the diff that nothing witnessed — but a reviewer NAMED ON A SURVIVING FINDING
// demonstrably raised one, which is precisely what the shared classifier means
// by findings. Recording that keeps this path in agreement with what the
// benchmark path reports for the same reviewer.
//
// Gate pass 2 argued the opposite, that withholding the classification stops a
// hand-authored directory forging a trust prior. It does not, and that was
// measured rather than argued: the pool summary that re-enables classification
// lives in the SAME directory, and the identical forgery reproduces unchanged at
// HEAD~4, before this sprint touched the file. The guard cost an attacker one
// extra JSON file and cost every legitimate path-anchored install its trust
// priors permanently. See TD-021.
func TestEmitForReconcile_NoPoolSummaryClassifiesFromTheFindings(t *testing.T) {
	reviewDir := t.TempDir() // no sources/pool/summary.json written at all

	recs := emitAndRead(t, reviewDir, resWith("bruce", "greta"))

	for _, name := range []string{"bruce", "greta"} {
		r := findReviewer(recs, name)
		require.NotNil(t, r, name)
		assert.Equal(t, testOutcomeFindings, r.Outcome, name)
		assert.NotEqual(t, testOutcomeClean, r.Outcome,
			"%s's review of the diff was never witnessed; clean would fabricate it", name)
		assert.Len(t, eligibleOutcomeRuns([]Record{*r}), 1, name)
	}
}

// TestEmitForReconcile_RoutedOnlyReviewerWithNoSummaryIsClassified restores the
// coverage gate pass 3 caught being deleted: the res.Unresolved branch taken
// when there is NO pool summary had no test at all, so a mutant there survived
// the whole suite.
//
// findings, deliberately not ungrounded. The reviewer did raise findings — the
// Tier 4 content check routed them, which is why they are absent from
// res.Findings — and the phantom is charged through FindingsRaised, not through
// the outcome. `ungrounded` already names fanout's DroppedByGrounding gate, so
// reusing it for a second, different gate would make the stored value ambiguous,
// and it is backwards for the doc-shield carve-out, whose definition is that the
// subject WAS named in the tree.
func TestEmitForReconcile_RoutedOnlyReviewerWithNoSummaryIsClassified(t *testing.T) {
	reviewDir := t.TempDir() // no pool summary

	res := reconcile.Result{
		Unresolved: []reconcile.JSONFinding{{
			File: "ghost.go", Line: 1, Problem: "p", Reviewers: []string{"phantom"},
		}},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}
	recs := emitAndRead(t, reviewDir, res)

	r := findReviewer(recs, "phantom")
	require.NotNil(t, r)
	assert.Equal(t, testOutcomeFindings, r.Outcome)
	assert.NotEqual(t, testOutcomeUngrounded, r.Outcome,
		"ungrounded means fanout's grounding gate, not reconcile's Tier 4 check")
	assert.Equal(t, 1, r.FindingsRaised, "the phantom is charged through the denominator")
	assert.Len(t, eligibleOutcomeRuns([]Record{*r}), 1,
		"a phantom-raiser must stay scoreable, not be excused")
}

// TestEmitForReconcile_PaddedReviewerNameKeepsItsCounts is the regression test
// for gate pass 3's HIGH, which was a third-generation defect: pass 2 trimmed
// the map key but left Finding.Reviewers untrimmed, and reviewerCounts matches
// the two by exact string. A padded name therefore recorded raised=0,
// corroborated=0, rate=0.00 and no skeptic verdicts — while still carrying a
// model, a cost and an eligible outcome, so the record looked perfectly healthy
// and entered the trust priors with a zeroed denominator.
//
// One whitespace typo in a registry agent name reaches this: fanout copies the
// agent name into the finding verbatim, reconcile's merge strips commas but not
// spaces, and the stream parser assigns its reviewer column untrimmed.
func TestEmitForReconcile_PaddedReviewerNameKeepsItsCounts(t *testing.T) {
	reviewDir := t.TempDir()
	writePoolSummary(t, reviewDir,
		fanout.AgentStatus{Agent: " bruce ", Status: fanout.StatusOK, FindingsCount: 2, Model: "opus"},
	)

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1",
				Reviewers: []string{" bruce ", "greta"}}},
			{Finding: reconcile.Finding{File: "b.go", Line: 2, Problem: "p2",
				Reviewers: []string{" bruce "}}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}
	recs := emitAndRead(t, reviewDir, res)

	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce, "the trimmed name is the record key")
	assert.Nil(t, findReviewer(recs, " bruce "), "the padded name must not be a second key")
	assert.Equal(t, 2, bruce.FindingsRaised, "a padded name must not lose its findings")
	assert.Equal(t, 1, bruce.FindingsCorroborated)
	assert.InDelta(t, 0.5, bruce.CorroborationRate, 1e-9)
	assert.Equal(t, "opus", bruce.Model)
}

// TestEmitForReconcile_RoutedOnlyReviewerIsClassifiedFromTheSummary closes gate
// pass 2's parity finding. The same reviewer, exhibiting the same behaviour,
// must not classify differently depending on which artifacts happen to be on
// disk — that is precisely what AC 02-02 exists to prevent.
//
// phantom raised one finding, which the Tier 4 check routed. With a summary
// present its AgentStatus classifies it, exactly as the benchmark path would.
func TestEmitForReconcile_RoutedOnlyReviewerIsClassifiedFromTheSummary(t *testing.T) {
	reviewDir := t.TempDir()
	status := fanout.AgentStatus{Agent: "phantom", Status: fanout.StatusOK, FindingsCount: 1}
	writePoolSummary(t, reviewDir, status)

	res := reconcile.Result{
		Unresolved: []reconcile.JSONFinding{{
			File: "ghost.go", Line: 1, Problem: "p", Reviewers: []string{"phantom"},
		}},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}
	recs := emitAndRead(t, reviewDir, res)

	r := findReviewer(recs, "phantom")
	require.NotNil(t, r)
	// The recorded value is whatever the SHARED classifier says for this status,
	// asserted against the classifier itself rather than against a literal — so
	// the two paths cannot drift apart without this failing.
	assert.Equal(t, fanout.ReviewerOutcome(status, []string{""}), r.Outcome)
	assert.Len(t, eligibleOutcomeRuns([]Record{*r}), 1,
		"a witnessed phantom-raiser stays scoreable, so the run counts against it")
}

// TestEmitForReconcile_BlankAgentNameIsNotRecorded covers whitespace, not just
// the empty string. The first version of this guard used TrimSpace in the pool
// loop while the findings loops still tested == "", so an agent named "  " was
// skipped in one place and re-registered in the other — promoting a FAILED
// reviewer into the record with its model and usage lost. All three loops now
// trim, and the map key is the trimmed name so " bruce" and "bruce" cannot
// become two distinct trust keys.
func TestEmitForReconcile_BlankAgentNameIsNotRecorded(t *testing.T) {
	reviewDir := t.TempDir()
	writePoolSummary(t, reviewDir,
		fanout.AgentStatus{Agent: "", Status: fanout.StatusOK},
		fanout.AgentStatus{Agent: "   ", Status: "error", Error: "boom"},
		fanout.AgentStatus{Agent: " bruce ", Status: fanout.StatusOK, FindingsCount: 1, Model: "opus"},
	)

	res := resWith("bruce")
	res.Findings[0].Reviewers = []string{"", "   ", " bruce "}
	recs := emitAndRead(t, reviewDir, res)

	for _, blank := range []string{"", "   ", " "} {
		assert.Nil(t, findReviewer(recs, blank),
			"a blank-named agent (%q) must not become a reviewer record", blank)
	}
	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce, "the trimmed name is the key")
	assert.Equal(t, "opus", bruce.Model, "the summary entry must win, not a bare re-registration")
	assert.Nil(t, findReviewer(recs, " bruce "), "the untrimmed name must not be a second key")
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
