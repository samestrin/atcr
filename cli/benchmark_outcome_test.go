package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// perModelCompleter dispatches on the invocation's model id, so a single run can
// drive three reviewers down three different response shapes — the only way to
// assert that a clean review, unparseable prose, and a failed call are told apart
// within one run-result.
type perModelCompleter map[string]func() (string, error)

func (p perModelCompleter) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	fn, ok := p[inv.Model]
	if !ok {
		return "", nil
	}
	return fn()
}

// AC7: a zero-finding result is no longer ambiguous. A reviewer that emitted the
// clean-review sentinel, one that emitted prose parsing to zero findings, and one
// whose call failed all record zero raised categories and therefore score
// IDENTICALLY. Before the outcome vocabulary, the run-result could not tell them
// apart — which on a leaderboard means a model that reviewed cleanly and a model that
// emitted garbage publish the same row.
func TestExecuteBenchmarkRun_DistinguishesZeroFindingOutcomes(t *testing.T) {
	cfg := benchCfg(
		[3]string{"cleaner", "m-clean", "cleaner"},
		[3]string{"rambler", "m-prose", "rambler"},
		[3]string{"broken", "m-fail", "broken"},
	)
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := executeBenchmarkRun(context.Background(), cfg, perModelCompleter{
		// The specified way to report nothing: a clean review.
		"m-clean": func() (string, error) { return "No findings.", nil },
		// Content present, nothing parseable, and NOT the sentinel.
		"m-prose": func() (string, error) {
			return "I looked over the diff and honestly it seems fine to me overall.", nil
		},
		// The call itself never produced a reviewable response.
		"m-fail": func() (string, error) { return "", errors.New("simulated provider failure") },
	}, suiteValidPath, gen, "")
	require.NoError(t, err)

	clean := coverageFor(t, rr, "m-clean", "cleaner")
	prose := coverageFor(t, rr, "m-prose", "rambler")
	broken := coverageFor(t, rr, "m-fail", "broken")

	assert.Equal(t, 2, clean.Outcomes[benchmark.OutcomeClean],
		"the sentinel is a clean review on both cases")
	assert.Zero(t, clean.Outcomes[benchmark.OutcomeUnparseable],
		"a clean review must never be tallied as unparseable — flagging the sentinel would destroy the distinction")

	assert.Equal(t, 2, prose.Outcomes[benchmark.OutcomeUnparseable],
		"content that parses to zero findings and is not the sentinel is unparseable, not clean")
	assert.Zero(t, prose.Outcomes[benchmark.OutcomeClean])

	assert.Equal(t, 2, broken.Outcomes[benchmark.OutcomeFailed],
		"a failed call is neither a clean review nor unparseable output")
	assert.Zero(t, broken.Outcomes[benchmark.OutcomeClean])

	// The load-bearing claim, stated directly: all three score identically and must
	// still be distinguishable.
	for _, rev := range rr.Reviewers {
		require.Zero(t, rev.FindingsRaisedAvg, "the premise is that all three raised nothing")
	}
	assert.NotEqual(t, clean.Outcomes, prose.Outcomes, "clean must not read as unparseable")
	assert.NotEqual(t, clean.Outcomes, broken.Outcomes, "clean must not read as failed")
	assert.NotEqual(t, prose.Outcomes, broken.Outcomes, "unparseable must not read as failed")
}

// A reviewer that raised findings tallies as findings — the positive control, without
// which every assertion above is satisfied by a tally that is always "clean".
func TestExecuteBenchmarkRun_TalliesFindingsOutcome(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)

	cov := coverageFor(t, rr, "m-greta", "greta")
	assert.Equal(t, 2, cov.Outcomes[benchmark.OutcomeFindings], "both cases raised a finding")
	assert.Zero(t, cov.Outcomes[benchmark.OutcomeClean])
}

// The outcome must SURVIVE a checkpoint round trip. Recording it only in memory would
// make a resumed run report different outcomes from an uninterrupted one — the same
// class of defect as losing the realized model.
func TestExecuteBenchmarkRun_OutcomeSurvivesResume(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := t.TempDir() + "/ckpt.json"

	baseline, err := executeBenchmarkRun(context.Background(), cfg, stubNoFindingsCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)
	require.Equal(t, 2, coverageFor(t, baseline, "m-greta", "greta").Outcomes[benchmark.OutcomeClean],
		"the baseline must actually tally clean reviews or the comparison is vacuous")

	_, err = executeBenchmarkRun(context.Background(), cfg, stubNoFindingsCompleter{}, suiteValidPath, gen, path)
	require.NoError(t, err)
	resumed, err := executeBenchmarkRun(context.Background(), cfg, stubNoFindingsCompleter{}, suiteValidPath, gen, path)
	require.NoError(t, err)

	assert.Equal(t, mustMarshal(t, baseline.Coverage), mustMarshal(t, resumed.Coverage),
		"a fully-replayed run reports the same outcomes as an uninterrupted one")
}

// AC4/AC8: a checkpoint written BEFORE the outcome field existed replays as
// `unknown`, never as `clean`. This is the whole reason the vocabulary is a string
// enum rather than a pair of booleans — two bools both default to false, which would
// silently assert "reviewed and found nothing" about cases nobody recorded an outcome
// for.
func TestReplayCheckpointCase_PreOutcomeCheckpointReplaysAsUnknown(t *testing.T) {
	accs := map[reviewerKey]*reviewerAcc{}
	var order []reviewerKey

	// A checkpoint entry as an older binary wrote it: no Outcome field at all.
	replayCheckpointCase(accs, &order, checkpointCase{
		Index:  0,
		CaseID: "case-01",
		Reviewers: []checkpointReviewer{
			{Agent: "greta", Model: "m-greta", Persona: "greta", Raised: nil},
		},
	}, []string{"correctness"})

	acc := accs[reviewerKey{model: "m-greta", persona: "greta"}]
	require.NotNil(t, acc)
	assert.Equal(t, 1, acc.outcomes[benchmark.OutcomeUnknown],
		"an unrecorded outcome replays as unknown")
	assert.Zero(t, acc.outcomes[benchmark.OutcomeClean],
		"absence of a recorded outcome must NEVER be read as a clean review")
}

// reviewerOutcome's precedence, stated as a table. The signals are not mutually
// exclusive on the wire, so the ordering is a decision that has to be pinned rather
// than inferred: data-integrity signals outrank volume signals.
func TestReviewerOutcome_Precedence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status fanout.AgentStatus
		raised []string
		want   string
	}{
		{name: "failed status", status: fanout.AgentStatus{Status: fanout.StatusFailed}, want: benchmark.OutcomeFailed},
		{
			name:   "failed outranks unparseable",
			status: fanout.AgentStatus{Status: fanout.StatusFailed, UnparseableResponse: true},
			want:   benchmark.OutcomeFailed,
		},
		{
			name:   "error recorded on an ok status is still failed",
			status: fanout.AgentStatus{Status: fanout.StatusOK, Error: "boom"},
			want:   benchmark.OutcomeFailed,
		},
		{
			name:   "unparseable outranks truncated",
			status: fanout.AgentStatus{Status: fanout.StatusOK, UnparseableResponse: true, ResponseTruncated: true},
			want:   benchmark.OutcomeUnparseable,
		},
		{
			name:   "truncated outranks findings",
			status: fanout.AgentStatus{Status: fanout.StatusOK, ResponseTruncated: true},
			raised: []string{"correctness"},
			want:   benchmark.OutcomeTruncated,
		},
		{
			name:   "findings",
			status: fanout.AgentStatus{Status: fanout.StatusOK},
			raised: []string{"correctness"},
			want:   benchmark.OutcomeFindings,
		},
		{name: "clean", status: fanout.AgentStatus{Status: fanout.StatusOK}, want: benchmark.OutcomeClean},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, reviewerOutcome(tc.status, tc.raised))
		})
	}
}

// FallbackUsed is tallied SEPARATELY from the outcome, not folded into the enum: a
// fallback-served case is independently clean, unparseable, or failed, so making it
// an enum member would admit exactly the impossible combined states the enum exists
// to prevent.
func TestApplyReviewerOutcome_TalliesFallbackSeparatelyFromOutcome(t *testing.T) {
	accs := map[reviewerKey]*reviewerAcc{}
	var order []reviewerKey
	applyReviewerOutcome(accs, &order, reviewerCaseOutcome{
		model: "llm-large", persona: "brad", caseID: "case-01",
		expected: []string{"correctness"}, outcome: benchmark.OutcomeClean, fallbackUsed: true,
	})

	acc := accs[reviewerKey{model: "llm-large", persona: "brad"}]
	require.NotNil(t, acc)
	assert.Equal(t, 1, acc.fallbackCases, "the substitution is counted")
	assert.Equal(t, 1, acc.outcomes[benchmark.OutcomeClean],
		"and the review it served is still described by its own outcome")
}
