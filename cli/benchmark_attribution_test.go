package cli

import (
	"fmt"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// foldCase is a test-local shorthand for folding one reviewer's single-case outcome
// into a fresh accumulator pair, so a table of (model, persona) sightings reads as
// the sequence of cases it represents rather than as nine positional arguments.
type foldCase struct {
	model   string
	persona string
	raised  []string
}

func foldAll(folds []foldCase) (map[reviewerKey]*reviewerAcc, []reviewerKey) {
	accs := map[reviewerKey]*reviewerAcc{}
	var order []reviewerKey
	for i, f := range folds {
		applyReviewerOutcome(accs, &order, reviewerCaseOutcome{
			model:    f.model,
			persona:  f.persona,
			caseID:   fmt.Sprintf("case-%02d", i+1),
			expected: []string{"correctness"},
			raised:   f.raised,
		})
	}
	return accs, order
}

// A lane that fails over mid-suite must yield ONE ROW PER REALIZED MODEL, each
// carrying only the cases that model actually served.
//
// This is the defect Run B exposed: brad's primary exhausted its token plan partway
// through the suite, 9 of 17 cases were served by the backup, and the run-result
// credited all 17 to the primary. Keying the accumulator by the LANE folds every
// later case into whichever model happened to serve the first one.
func TestApplyReviewerOutcome_SplitsRowsByRealizedModel(t *testing.T) {
	// One lane (persona "brad"), two realized models across four cases: the primary
	// served cases 1-2, then the lane failed over and the backup served cases 3-4.
	accs, order := foldAll([]foldCase{
		{model: "qwen3.8-max", persona: "brad", raised: []string{"correctness"}},
		{model: "qwen3.8-max", persona: "brad", raised: []string{"security"}},
		{model: "llm-large", persona: "brad", raised: []string{"correctness"}},
		{model: "llm-large", persona: "brad", raised: nil},
	})

	require.Len(t, order, 2, "a lane that failed over yields one row per realized model, not one row for the lane")

	primary := accs[reviewerKey{model: "qwen3.8-max", persona: "brad"}]
	backup := accs[reviewerKey{model: "llm-large", persona: "brad"}]
	require.NotNil(t, primary, "the primary must key on the model that served its cases")
	require.NotNil(t, backup, "the backup must key on ITS OWN model, not the lane's configured one")

	assert.Len(t, primary.cases, 2, "the primary is credited with exactly the cases it served")
	assert.Len(t, backup.cases, 2, "the backup is credited with exactly the cases it served")
	assert.Equal(t, []string{"case-01", "case-02"}, primary.caseIDs,
		"the primary holds exactly the cases it served")
	assert.Equal(t, []string{"case-03", "case-04"}, backup.caseIDs,
		"the backup holds exactly the cases it served — disjoint from the primary's, summing to the suite size")
}

// A run with NO failover is unchanged: one realized model per lane means one row per
// lane, exactly as the lane-keyed accumulator produced. Paired with the test above so
// the re-keying is pinned in both directions rather than by a split-only assertion
// that a key of "always split" would also satisfy.
func TestApplyReviewerOutcome_NoFailoverKeepsOneRowPerLane(t *testing.T) {
	accs, order := foldAll([]foldCase{
		{model: "m-greta", persona: "greta", raised: []string{"correctness"}},
		{model: "m-kai", persona: "kai", raised: []string{"correctness"}},
		{model: "m-greta", persona: "greta", raised: []string{"security"}},
		{model: "m-kai", persona: "kai", raised: []string{"security"}},
	})

	require.Len(t, order, 2, "two lanes, neither failed over -> two rows")
	assert.Len(t, accs[reviewerKey{model: "m-greta", persona: "greta"}].cases, 2)
	assert.Len(t, accs[reviewerKey{model: "m-kai", persona: "kai"}].cases, 2)
}

// Two lanes that realize the SAME model under DIFFERENT personas stay separate rows:
// persona is a behavioral modifier (system prompt) that changes reviewer output, so
// collapsing on model alone would merge two genuinely different reviewers. This is
// the case that a model-only key would silently get wrong.
func TestApplyReviewerOutcome_SameModelDifferentPersonasStaySeparate(t *testing.T) {
	_, order := foldAll([]foldCase{
		{model: "llm-large", persona: "brad", raised: []string{"correctness"}},
		{model: "llm-large", persona: "kai", raised: []string{"correctness"}},
	})
	require.Len(t, order, 2, "persona is part of the row identity, not a label on it")
}

// A case that FAILED after its slot had already failed over must be attributed to
// the model that actually attempted it, not to the configured primary.
//
// fanout stamps AgentStatus.Model only when the provider reported token usage, so a
// failed call reports no model at all — and resolving straight to the registry would
// credit that case to a primary which by definition did not serve it. AC1 forbids
// exactly that. FallbackModel is populated unconditionally and is the honest answer.
func TestReviewerModel_FailedFallbackCaseIsNotCreditedToThePrimary(t *testing.T) {
	cfg := benchCfg([3]string{"brad", "qwen3.8-max", "brad"})

	failedAfterFailover := fanout.AgentStatus{
		Agent:         "brad",
		Status:        fanout.StatusFailed,
		FallbackUsed:  true,
		FallbackFrom:  "brad",
		FallbackModel: "llm-large",
		// No Model: statusFor stamps it only when tokens were reported.
	}
	assert.Equal(t, "llm-large", reviewerModel(cfg, failedAfterFailover),
		"the fallback attempted this case; the primary did not")

	// Every non-failover path is unchanged.
	assert.Equal(t, "qwen3.8-max", reviewerModel(cfg, fanout.AgentStatus{Agent: "brad", Status: fanout.StatusFailed}),
		"no failover -> the configured model is still the right answer")
	assert.Equal(t, "kimi-k3", reviewerModel(cfg, fanout.AgentStatus{Agent: "brad", Model: "kimi-k3"}),
		"a usage-reported model still wins over everything")
	assert.Equal(t, "kimi-k3", reviewerModel(cfg, fanout.AgentStatus{
		Agent: "brad", Model: "kimi-k3", FallbackUsed: true, FallbackModel: "llm-large",
	}), "usage-reported beats FallbackModel — it is the model that actually produced the tokens")
}

// Aggregation order is produced by an explicit sort, never by map iteration or by
// arrival order — the reproducibility contract requires identical input to yield
// byte-identical output.
func TestSortScoredRows_DeterministicByModelThenPersona(t *testing.T) {
	rows := []scoredRow{
		{id: reviewerKey{model: "m-kai", persona: "kai"}},
		{id: reviewerKey{model: "m-greta", persona: "zeta"}},
		{id: reviewerKey{model: "m-greta", persona: "alpha"}},
	}
	sortScoredRows(rows)
	assert.Equal(t, []reviewerKey{
		{model: "m-greta", persona: "alpha"},
		{model: "m-greta", persona: "zeta"},
		{model: "m-kai", persona: "kai"},
	}, []reviewerKey{rows[0].id, rows[1].id, rows[2].id}, "ascending by model, then persona")
}
