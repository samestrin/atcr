package scorecard

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

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
		// fanout stamps StatusTimeout on a deadline miss (status.go); it never
		// writes "error" — that literal exercised the same classifier arm
		// (a.Status != StatusOK → failed) through an emitter-impossible value.
		fanout.AgentStatus{Agent: "kai", Status: fanout.StatusTimeout, Error: "context deadline exceeded", Model: "kimi"},
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

// TestEmitForReconcile_RepeatedAgentTakesTheWorstOutcome closes the
// last-write-wins collision in EmitForReconcile's reviewer loop: a pool summary
// listing the same agent twice — first failed, then ok — must record the FAILED
// outcome, not silently overwrite it with clean. The precedence kept is the
// classifier's own (failed > unparseable > truncated > incomplete > findings >
// ungrounded > filtered > clean), in both orders, so a crafted or buggy summary
// cannot hide a failure behind a later clean entry for the same name.
func TestEmitForReconcile_RepeatedAgentTakesTheWorstOutcome(t *testing.T) {
	reviewDir := t.TempDir()
	writePoolSummary(t, reviewDir,
		// Failure first, clean second: clean must not win.
		fanout.AgentStatus{Agent: "bruce", Status: fanout.StatusFailed, Error: "boom", Model: "opus"},
		fanout.AgentStatus{Agent: "bruce", Status: fanout.StatusOK, FindingsCount: 0, Model: "opus"},
		// Reverse order for vera: the worse outcome must win regardless of
		// which entry came later.
		fanout.AgentStatus{Agent: "vera", Status: fanout.StatusOK, FindingsCount: 0, Model: "opus"},
		fanout.AgentStatus{Agent: "vera", Status: fanout.StatusFailed, Error: "boom", Model: "opus"},
	)

	recs := emitAndRead(t, reviewDir, resWith("bruce"))

	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce)
	assert.Equal(t, testOutcomeFailed, bruce.Outcome,
		"a repeated agent must keep the higher-precedence outcome, not the later one")

	vera := findReviewer(recs, "vera")
	require.NotNil(t, vera)
	assert.Equal(t, testOutcomeFailed, vera.Outcome,
		"order in the summary must not decide which outcome survives a collision")
}

// TestEmitForReconcile_CaseFoldedIdentityMintsOneRecord closes the entry-point
// half of the reviewer-identity defect: the pool summary names "Bruce" while
// the findings cell says "bruce", and the two spellings of one persona must
// produce ONE reviewer record, not two. Two records double the run's weight in
// trustPriorsSince — one run buying two credits against DefaultTrustMinRuns —
// and explain.go reports Counted: 2 for a single run.
func TestEmitForReconcile_CaseFoldedIdentityMintsOneRecord(t *testing.T) {
	reviewDir := t.TempDir()
	writePoolSummary(t, reviewDir,
		fanout.AgentStatus{Agent: "Bruce", Status: fanout.StatusOK, FindingsCount: 0, Model: "opus"},
	)

	recs := emitAndRead(t, reviewDir, resWith("bruce"))

	reviewerRecs := make([]Record, 0, 2)
	for _, r := range recs {
		if r.RecordType == RecordTypeReviewer {
			reviewerRecs = append(reviewerRecs, r)
		}
	}
	require.Len(t, reviewerRecs, 1,
		"one persona spelled two ways is one reviewer, not two trust credits")
	assert.Equal(t, "bruce", reviewerRecs[0].Reviewer,
		"Record.Reviewer must be the canonical normalized name")
}

// TestEmitForReconcile_RunIDCannotCollideAcrossSiblingDirectories closes the
// RunID collision: RunID used to be ReconciledAt + filepath.Base(reviewDir), so
// two reviews in DIFFERENT repositories whose review directories share a leaf
// name and land in the same second produced the same id — merging their
// opportunity-category unions (opportunityUnions keys on RunID, so one broad
// run permanently widens every lens's remit) and max-collapsing their pair
// evidence (pairTallies keys runsByPersona and evidence on it). The absolute
// review-directory path must discriminate.
func TestEmitForReconcile_RunIDCannotCollideAcrossSiblingDirectories(t *testing.T) {
	// Two sibling directories with the IDENTICAL basename "review" under
	// different roots — the shape two side-by-side checkouts produce.
	reviewA := filepath.Join(t.TempDir(), "review")
	reviewB := filepath.Join(t.TempDir(), "review")
	for _, d := range []string{reviewA, reviewB} {
		require.NoError(t, os.MkdirAll(filepath.Join(d, "reconciled"), 0o755))
	}

	recsA := emitAndRead(t, reviewA, resWith("bruce"))
	recsB := emitAndRead(t, reviewB, resWith("bruce"))
	require.NotEmpty(t, recsA)
	require.NotEmpty(t, recsB)

	assert.NotEqual(t, recsA[0].RunID, recsB[0].RunID,
		"same-second runs from same-basename directories in different repos must not share a RunID")
	// The extended id must still satisfy the store's shape contract.
	assert.True(t, IsRunID(recsA[0].RunID),
		"the discriminated RunID must still parse as a run_id (month prefix + T separator)")
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
// 3aad9a7d — yielding map[attacker:1 sockpuppet:1] — before this sprint
// touched the file. That sha is the branch merge-base and is already on main,
// so the measurement stays re-runnable after this branch is squash-merged;
// a sha reachable only from the branch becomes "bad object" the day it lands. The guard cost an attacker one
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

	// normalizedReviewers must return a FRESH slice, never trim in place. res is
	// not this package's to mutate and it is consumed after this call: the CLI
	// hands the same Result to persistLocalDebt, and internal/localdebt reads
	// f.Reviewers off it. An in-place trim would silently rewrite localdebt
	// records, and nothing else in the suite would notice.
	assert.Equal(t, []string{" bruce ", "greta"}, res.Findings[0].Reviewers,
		"EmitForReconcile must not mutate the caller's Result")
	assert.Equal(t, []string{" bruce "}, res.Findings[1].Reviewers)
}

// TestDistinctCount_IgnoresWhitespaceAndCollapsesPaddedDuplicates covers the
// corroboration counter directly. It is defensive rather than reachable through
// EmitForReconcile, which pre-trims — but Emit is exported, so a caller that
// builds its own Finding reaches it, and both halves matter there: a
// whitespace-only name must not corroborate anything, and a padded duplicate of
// a real name must not let a reviewer corroborate itself.
func TestDistinctCount_IgnoresWhitespaceAndCollapsesPaddedDuplicates(t *testing.T) {
	assert.Equal(t, 0, distinctCount([]string{"", "  ", "\t"}))
	assert.Equal(t, 1, distinctCount([]string{"bruce", "   "}))
	assert.Equal(t, 1, distinctCount([]string{" bruce ", "bruce"}),
		"a padded duplicate is the same reviewer, not a second corroborator")
	assert.Equal(t, 2, distinctCount([]string{"bruce", "greta"}))
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
	assert.Equal(t, fanout.ReviewerOutcome(status, 1), r.Outcome)
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
		fanout.AgentStatus{Agent: "   ", Status: fanout.StatusFailed, Error: "boom"},
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
	got := coerceOutcome("banana", nil)
	assert.Equal(t, testOutcomeUnknown, got)
	assert.Equal(t, testOutcomeFiltered, coerceOutcome(testOutcomeFiltered, nil))
	assert.Equal(t, testOutcomeUnknown, coerceOutcome(testOutcomeUnknown, nil))
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

// TestEmitForReconcile_TwoReviewerGrayZoneClusterChargesItsPair pins the
// bridge half of the AC 05-01 gray-zone rule: an ambiguous cluster whose
// distinct reviewers number exactly TWO becomes ONE canonical pair key in
// EmitInput.GrayZonePairs, which the pair fold charges as one disagreement.
// Singleton clusters (one reviewer) and 3+-reviewer clusters contribute
// nothing — the same unattributable rule the severity-split fold applies.
func TestEmitForReconcile_TwoReviewerGrayZoneClusterChargesItsPair(t *testing.T) {
	reviewDir := t.TempDir()
	writePoolSummary(t, reviewDir,
		fanout.AgentStatus{Agent: "otto", Status: fanout.StatusOK, FindingsCount: 1, Model: "opus"},
		fanout.AgentStatus{Agent: "sasha", Status: fanout.StatusOK, FindingsCount: 1, Model: "opus"},
		fanout.AgentStatus{Agent: "bruce", Status: fanout.StatusOK, FindingsCount: 1, Model: "opus"},
	)
	res := resWith("otto")
	res.Ambiguous = []reconcile.AmbiguousCluster{
		{ID: "gz-pair", Findings: []reconcile.Finding{
			{File: "a.go", Line: 1, Problem: "p", Reviewer: "otto"},
			{File: "a.go", Line: 2, Problem: "q", Reviewer: "sasha"},
		}},
		// Singleton: contributes nothing.
		{ID: "gz-single", Findings: []reconcile.Finding{
			{File: "b.go", Line: 1, Problem: "p", Reviewer: "bruce"},
		}},
		// Three distinct reviewers: unattributable, contributes nothing.
		{ID: "gz-trio", Findings: []reconcile.Finding{
			{File: "c.go", Line: 1, Problem: "p", Reviewer: "otto"},
			{File: "c.go", Line: 2, Problem: "q", Reviewer: "sasha"},
			{File: "c.go", Line: 3, Problem: "r", Reviewer: "bruce"},
		}},
	}

	recs := emitAndRead(t, reviewDir, res)
	byName := map[string]Record{}
	for _, r := range recs {
		if r.RecordType == RecordTypeReviewer {
			byName[r.Reviewer] = r
		}
	}
	otto, ok := byName["otto"]
	require.True(t, ok)
	require.NotEmpty(t, otto.PairSignals, "the two-reviewer gray-zone cluster must charge its pair")
	for _, s := range otto.PairSignals {
		assert.Equal(t, 1, s.Disagreed, "exactly one gray-zone pair charged, exactly one item")
		assert.Zero(t, s.Agreed)
	}
	bruce, ok := byName["bruce"]
	require.True(t, ok)
	assert.Empty(t, bruce.PairSignals,
		"a reviewer appearing only in singleton/trio clusters is charged nothing")
}

// TestCoerceOutcome_EmitsMsgOutcomeCoerced pins the diagnostic the 180-day
// silent drop now carries: a value coerceOutcome rejects still becomes unknown,
// but the rejection is written to the injected diag writer as a
// MsgOutcomeCoerced substring, matching the MsgMalformedSkip/MsgWriteFailed
// convention so wiring tests can pin the literal.
func TestCoerceOutcome_EmitsMsgOutcomeCoerced(t *testing.T) {
	var buf bytes.Buffer
	got := coerceOutcome("banana", &buf)
	assert.Equal(t, testOutcomeUnknown, got, "the fail-neutral result is unchanged")
	assert.Contains(t, buf.String(), MsgOutcomeCoerced,
		"the silent 180-day drop must leave a trace in the diag channel")
	assert.Contains(t, buf.String(), "banana", "the rejected value is named so it can be traced")

	// A valid value emits nothing.
	var clean bytes.Buffer
	assert.Equal(t, testOutcomeFindings, coerceOutcome(testOutcomeFindings, &clean))
	assert.Empty(t, clean.String(), "a valid outcome must not log a coercion")
}

// TestEmit_RejectsAnInvalidOutcomeAtTheWriteBoundary pins the write-boundary
// guard: Emit is exported and used to copy meta.Outcome into the record
// unvalidated, so a caller that builds its own ReviewerMeta — any path other
// than EmitForReconcile — could persist a value outside the vocabulary. The
// guard at the Outcome assignment coerces it to unknown, the same fail-neutral
// result coerceOutcome produces one frame up on the reconcile path.
func TestEmit_RejectsAnInvalidOutcomeAtTheWriteBoundary(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Emit(EmitInput{
		RunID: pairRunID("write-boundary-guard"),
		Reviewers: map[string]ReviewerMeta{
			"bruce": {Model: "opus", Outcome: "banana"}, // not a vocabulary member
		},
	}, EmitOpts{Dir: dir}))

	recs, err := ReadSince(dir, 0, time.Now(), ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	r := findReviewer(recs, "bruce")
	require.NotNil(t, r)
	assert.Equal(t, testOutcomeUnknown, r.Outcome,
		"a garbage outcome must be coerced to unknown at the write boundary, never persisted raw")
}

// TestOutcomeRank_FollowsTheDocumentedPrecedence pins every arm of outcomeRank
// against the classifier's precedence. A misspelled literal ranks 0 and lets a
// later, lower-precedence entry overwrite a repeated agent's outcome, so each
// value must rank strictly above the next and every known value above an
// unknown one. Literals, not benchmark constants: scorecard cannot import
// benchmark (cycle); the cli/ parity test pins the spellings.
func TestOutcomeRank_FollowsTheDocumentedPrecedence(t *testing.T) {
	precedence := []string{
		"failed", "unparseable", "truncated", "incomplete",
		"findings", "ungrounded", "filtered", "clean",
	}
	for i := 1; i < len(precedence); i++ {
		assert.Greater(t, outcomeRank(precedence[i-1]), outcomeRank(precedence[i]),
			"%s must outrank %s", precedence[i-1], precedence[i])
	}
	for _, unknown := range []string{"", "not-an-outcome"} {
		assert.Greater(t, outcomeRank("clean"), outcomeRank(unknown),
			"%q must rank below every known outcome", unknown)
	}
}

// TestRunIDForReviewDir_AbsFailureHashesTheGivenPath covers the fallback: when
// the absolute path cannot be resolved, the id still hashes the path it was
// given rather than failing or hashing an empty string.
func TestRunIDForReviewDir_AbsFailureHashesTheGivenPath(t *testing.T) {
	orig := absPath
	t.Cleanup(func() { absPath = orig })
	absPath = func(string) (string, error) { return "", errors.New("getwd failed") }

	sum := sha256.Sum256([]byte("rel/review"))
	assert.Equal(t, "2026-06-14T10:00:00Z-review-"+hex.EncodeToString(sum[:4]),
		RunIDForReviewDir("2026-06-14T10:00:00Z", "rel/review"))
}

// TestEmitForReconcile_AmbiguousSingularReviewerIsFolded: a mis-cased, padded
// singular Reviewer on an ambiguous finding is normalized exactly as the
// reviewers map and the pair keys are, so its category and its gray-zone pair
// land on the one record for that persona (atcr review 2026-09-23,
// reconcile.go:246).
func TestEmitForReconcile_AmbiguousSingularReviewerIsFolded(t *testing.T) {
	reviewDir := t.TempDir()
	writePoolSummary(t, reviewDir,
		fanout.AgentStatus{Agent: "otto", Status: fanout.StatusOK, FindingsCount: 1, Model: "opus"},
		fanout.AgentStatus{Agent: "sasha", Status: fanout.StatusOK, FindingsCount: 1, Model: "opus"},
	)
	res := resWith("otto")
	res.Ambiguous = []reconcile.AmbiguousCluster{
		{ID: "gz", Findings: []reconcile.Finding{
			{File: "a.go", Line: 1, Problem: "p", Category: "security", Reviewer: " Otto "},
			{File: "a.go", Line: 2, Problem: "q", Reviewer: "SASHA"},
		}},
	}

	recs := emitAndRead(t, reviewDir, res)
	otto := findReviewer(recs, "otto")
	require.NotNil(t, otto)
	assert.Contains(t, otto.CategoriesRaised, "security")
	assert.Equal(t, []PairSignal{{Peer: "sasha", Disagreed: 1}}, otto.PairSignals)
	reviewers := 0
	for _, r := range recs {
		if r.RecordType == RecordTypeReviewer {
			reviewers++
		}
	}
	assert.Equal(t, 2, reviewers, "no record is minted for a spelling variant")
}
