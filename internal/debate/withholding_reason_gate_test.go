package debate

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/samestrin/atcr/internal/reconcile"
)

// TestCountsTowardWithholding is the gate the three-strike ceiling rests on.
//
// withholdExhausted withholds an item once its recorded attempts reach
// maxUnresolvedAttempts, and priorUnresolvedAttempts floors a withheld record
// back up on every read — so the count only ever rises and withholding is
// permanent. That is correct ONLY if every counted attempt was evidence about
// the ITEM. An environmental failure says nothing about the item: the run was
// interrupted, the harness was down, or the roster could not be cast. Counting
// those meant three Ctrl-C'd runs permanently withheld every disputed item in
// the review, with no flag, no expiry, and no exit but hand-editing debate.json.
//
// A DENY-list, not an allow-list: a reason this gate has never heard of is far
// likelier to be new item evidence than a new environmental failure, and the
// safe default is to COUNT it. An allow-list would silently stop counting any
// reason added later, quietly disabling the ceiling instead of over-applying it.
func TestCountsTowardWithholding(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		reason string
		want   bool
		why    string
	}{
		// Environmental: nothing about the item caused these.
		{ReasonContextCancelled, false, "the operator interrupted the run"},
		{ReasonHarnessUnavailable, false, "the snapshot or dispatcher was down"},
		{ReasonInsufficientModels, false, "the roster could not supply distinct models"},
		{ReasonNoProposer, false, "no proposer could be resolved from the registry"},

		// Item evidence: the debate actually ran and the item defeated it.
		{ReasonSeatHalted, true, "a seat's engine failed on this item"},
		{ReasonSeatSilent, true, "a seat ran clean and said nothing about this item"},
		{ReasonSeatSuppressed, true, "the strip emptied a seat's reply on this item"},
		{ReasonJudgeHalted, true, "the judge halted on this item"},
		{ReasonUnparseableRuling, true, "the judge's ruling on this item was garbled"},
		{ReasonEmptyRuling, true, "the judge returned nothing for this item"},
		{ReasonJudgeThinkMarkup, true, "the judge's reply on this item carried think markup"},
		{ReasonNoClusterDecision, true, "a real ruling that omitted the cluster decision"},

		// The default, stated as a case so the deny-list shape cannot be inverted
		// into an allow-list without this failing.
		{"", true, "an unrecorded reason is not evidence of an environmental failure"},
		{"some_future_reason", true, "an unknown reason defaults to counting, never to silently disabling the ceiling"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			assert.Equal(t, tc.want, countsTowardWithholding(tc.reason), tc.why)
		})
	}
}

// TestCountsTowardWithholding_DenyListIsExactlyTheEnvironmentalFour guards the
// set's size from the other direction: widening it is how a real item-evidence
// reason stops counting and the ceiling quietly stops holding.
func TestCountsTowardWithholding_DenyListIsExactlyTheEnvironmentalFour(t *testing.T) {
	t.Parallel()
	denied := 0
	for _, r := range []string{
		ReasonContextCancelled, ReasonHarnessUnavailable, ReasonInsufficientModels, ReasonNoProposer,
		ReasonSeatHalted, ReasonSeatSilent, ReasonSeatSuppressed, ReasonJudgeHalted,
		ReasonUnparseableRuling, ReasonEmptyRuling, ReasonJudgeThinkMarkup, ReasonNoClusterDecision,
	} {
		if !countsTowardWithholding(r) {
			denied++
		}
	}
	assert.Equal(t, 4, denied, "exactly the four environmental reasons are exempt from the ceiling")
}

// TestRunDebate_InterruptedRunSpendsNoAttempt is the end-to-end proof: the
// defect's headline consequence was that three Ctrl-C'd runs permanently
// withheld every disputed item. cli/main.go cancels the root context on SIGINT
// and runDebate has no ctx.Err() check between wg.Wait() and the artifact write,
// so a cancelled run still persists a record for every selected item. That
// record must now carry no attempt.
func TestRunDebate_InterruptedRunSpendsNoAttempt(t *testing.T) {
	// Three consecutive interrupted runs, each reading the previous one's record.
	//
	// The arithmetic runs through carryUnresolvedAttempts — the SAME function
	// runDebate's writer arm calls. An earlier version of this test re-implemented
	// the `if countsTowardWithholding(...)` branch in its own body, so its
	// assertion was guaranteed by the test's copy rather than by the production
	// line: the writer's gate could be replaced with an unconditional `prior+1`
	// and the whole repo stayed green. Mutation-checked after this change
	// (TD internal/debate/debate.go:308).
	prior := map[FindingKey]int{}
	key := FindingKey{File: "a.go", Line: 7, Problem: "disputed finding"}
	for round := 1; round <= 3; round++ {
		ir := ItemResult{
			File: key.File, Line: key.Line, Problem: key.Problem,
			Outcome: OutcomeUnresolved, Reason: ReasonContextCancelled,
		}
		ir.UnresolvedAttempts = carryUnresolvedAttempts(prior[key], ir.Reason)
		prior[key] = ir.UnresolvedAttempts
	}
	assert.Zero(t, prior[key], "three interrupted runs must leave the item with no attempts spent")

	kept, withheld := withholdExhausted(t.Context(), []reconcile.DisagreementItem{
		{File: key.File, Line: key.Line, Problem: key.Problem},
	}, prior)
	assert.Len(t, kept, 1, "the item is still debatable after three interruptions")
	assert.Empty(t, withheld, "and nothing was withheld")
}

// The complement, so the ceiling is proved to still WORK: three runs the item
// itself defeated do withhold it.
func TestRunDebate_ThreeItemEvidenceFailuresStillWithhold(t *testing.T) {
	prior := map[FindingKey]int{}
	key := FindingKey{File: "a.go", Line: 7, Problem: "disputed finding"}
	for round := 1; round <= maxUnresolvedAttempts; round++ {
		prior[key] = carryUnresolvedAttempts(prior[key], ReasonSeatSilent)
	}
	assert.Equal(t, maxUnresolvedAttempts, prior[key])

	kept, withheld := withholdExhausted(t.Context(), []reconcile.DisagreementItem{
		{File: key.File, Line: key.Line, Problem: key.Problem},
	}, prior)
	assert.Empty(t, kept, "three real failures still reach the ceiling")
	assert.Len(t, withheld, 1)
	assert.Equal(t, OverflowAttemptsExhausted, withheld[0].Reason)
}

// TestCarryUnresolvedAttempts is the writer arm's arithmetic, pinned directly.
//
// runDebate's unresolved branch is the only place an item's attempt history is
// written, and the three-strike ceiling is permanent once reached — so the gate
// deciding whether THIS run spends one is as load-bearing as the reader's floor
// in emit.go. The reader half was already pinned; this half was not, and the
// test that claimed to cover "the writer and the reader" re-implemented the
// writer's branch instead of calling it.
func TestCarryUnresolvedAttempts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		prior  int
		reason string
		want   int
	}{
		// Environmental: the prior total is carried forward UNCHANGED, so an
		// interrupted run neither advances the ceiling nor erases the history
		// earlier real attempts earned.
		{"interruption spends nothing", 0, ReasonContextCancelled, 0},
		{"interruption preserves earned history", 2, ReasonHarnessUnavailable, 2},
		{"roster failure spends nothing", 1, ReasonInsufficientModels, 1},
		{"no proposer spends nothing", 1, ReasonNoProposer, 1},

		// Item evidence: this run's attempt is added to the prior total.
		{"a silent seat spends one", 0, ReasonSeatSilent, 1},
		{"a halted judge spends one", 1, ReasonJudgeHalted, 2},
		{"the last real failure reaches the ceiling", 2, ReasonSeatSuppressed, 3},

		// An unrecorded reason counts, matching the deny-list's stated default:
		// over-applying a ceiling is visible and diagnosable, silently disabling
		// it is not.
		{"an unrecorded reason counts", 0, "", 1},
		{"an unknown reason counts", 1, "some_future_reason", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, carryUnresolvedAttempts(tc.prior, tc.reason))
		})
	}
}
