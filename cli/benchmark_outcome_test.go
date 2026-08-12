package cli

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/stream"
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
		// Unmapped model: returns empty content, which fanout's truncation
		// failover (engine.go:710, applied unconditionally by ExecuteReview)
		// demotes to StatusFailed — so a typo'd key fails loudly, never
		// silently classifies as a clean review.
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
		// The specified way to report nothing: the literal clean-review sentinel.
		// Note this is NOT free-form prose that happens to say "no findings" — see
		// TestExecuteBenchmarkRun_NearMissProseIsNotACleanReview.
		"m-clean": func() (string, error) { return stream.NoFindingsSentinel, nil },
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

// Prose that merely SOUNDS like a clean review is not one. `stream.IsNoFindings`
// matches the literal sentinel (case- and whitespace-insensitively) and nothing else,
// so "No findings." — a sentence, not the token — is unparseable output.
//
// This is the distinction the whole vocabulary rests on, and it is easy to get
// backwards: a reviewer that ignored the prompt contract and wrote its own sentence
// must not earn the same row as one that followed it. Pinned separately from the
// three-way test above because a near miss is the realistic failure, not an obvious
// wall of prose.
func TestExecuteBenchmarkRun_NearMissProseIsNotACleanReview(t *testing.T) {
	cfg := benchCfg([3]string{"nearly", "m-nearly", "nearly"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := executeBenchmarkRun(context.Background(), cfg, stubNoFindingsCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)

	cov := coverageFor(t, rr, "m-nearly", "nearly")
	assert.Equal(t, 2, cov.Outcomes[benchmark.OutcomeUnparseable],
		`"No findings." is a sentence, not the sentinel — it is unparseable output`)
	assert.Zero(t, cov.Outcomes[benchmark.OutcomeClean],
		"crediting it as a clean review would let a reviewer that ignored the contract publish as if it followed it")
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

	sentinel := perModelCompleter{"m-greta": func() (string, error) { return stream.NoFindingsSentinel, nil }}

	baseline, err := executeBenchmarkRun(context.Background(), cfg, sentinel, suiteValidPath, gen, "")
	require.NoError(t, err)
	require.Equal(t, 2, coverageFor(t, baseline, "m-greta", "greta").Outcomes[benchmark.OutcomeClean],
		"the baseline must actually tally clean reviews or the comparison is vacuous")

	_, err = executeBenchmarkRun(context.Background(), cfg, sentinel, suiteValidPath, gen, path)
	require.NoError(t, err)
	resumed, err := executeBenchmarkRun(context.Background(), cfg, sentinel, suiteValidPath, gen, path)
	require.NoError(t, err)

	assert.Equal(t, mustMarshal(t, baseline.Coverage), mustMarshal(t, resumed.Coverage),
		"a fully-replayed run reports the same outcomes as an uninterrupted one")
}

// failoverThenAbortCompleter serves case 0 from the FALLBACK model (the primary is
// down) and then fails every call, so the run aborts with case 0 checkpointed under
// the backup's realized identity. Paired with a healthy resume it produces the one
// shape the realized-attribution epic exists for: a checkpoint whose lane changed
// model mid-file, resumed live.
type failoverThenAbortCompleter struct{ calls atomic.Int32 }

func (c *failoverThenAbortCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	if c.calls.Add(1) > 2 {
		return "", errors.New("total outage past case 0")
	}
	if inv.Model == "m-primary" {
		return "", errors.New("primary down")
	}
	return stubCompleter{}.Complete(ctx, inv)
}

// AC3/AC4 across the FAILOVER BOUNDARY: a checkpoint whose lane changed realized
// model mid-file, resumed live, must yield one coverage row per realized model,
// the two partitioning the suite. Replayed entries fold under the model the
// checkpoint recorded; live cases fold under the model serving them now. This is
// the case where key-vs-order bugs live — both fold paths must key on the
// REALIZED identity, or case 0 lands under the configured primary.
func TestExecuteBenchmarkRun_ResumeAcrossFailoverBoundarySplitsCoverage(t *testing.T) {
	cfg := benchCfg([3]string{"brad", "m-primary", "brad"})
	agents := cfg.Registry.Agents
	brad := agents["brad"]
	brad.Fallback = "brad-backup"
	agents["brad"] = brad
	agents["brad-backup"] = registry.AgentConfig{Provider: "p", Model: "m-backup", Persona: "brad", Temperature: ptrF(0.7)}

	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := t.TempDir() + "/ckpt.json"

	// First run: the primary is down, so case 0 is served by the backup and
	// checkpointed under ITS model; case 1 then fails outright, aborting the run.
	_, err := executeBenchmarkRun(context.Background(), cfg, &failoverThenAbortCompleter{}, suiteValidPath, gen, path)
	require.Error(t, err, "the total-roster failure on case 1 aborts the run, leaving case 0 checkpointed")

	cp, err := loadCheckpoint(path)
	require.NoError(t, err)
	require.Len(t, cp.Cases, 1, "only case 0 was scored before the abort")
	require.Equal(t, "m-backup", cp.Cases[0].Reviewers[0].Model,
		"the checkpoint records the REALIZED model, not the configured primary")
	assert.True(t, cp.Cases[0].Reviewers[0].FallbackUsed,
		"the checkpoint persists fallback_used — a dropped field or wrong JSON tag turns the resume below into silent misattribution")

	// Resume healthy: case 0 replays (folding under m-backup), case 1 executes
	// live under the primary.
	rr, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, path)
	require.NoError(t, err)

	require.Len(t, rr.Coverage, 2, "one row per realized model across the failover boundary")
	backup := coverageFor(t, rr, "m-backup", "brad")
	primary := coverageFor(t, rr, "m-primary", "brad")
	assert.Equal(t, []string{"case-01-nil-deref"}, backup.CaseIDs,
		"the replayed case stays under the model that actually served it")
	assert.Equal(t, []string{"case-02-sql-injection"}, primary.CaseIDs,
		"the live case folds under the model serving it now — the rows partition the suite")
	assert.Equal(t, 1, backup.FallbackCases, "the replayed case is remembered as fallback-served")
	assert.Zero(t, primary.FallbackCases)
	assert.Contains(t, mustMarshal(t, rr.Coverage), `"fallback_cases":1`,
		"the public run-result wire form carries the fallback_cases key — the in-memory fold alone does not prove the field serializes")
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
	assert.Equal(t, 1, acc.outcomes[benchmark.OutcomeUnknownLabel],
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

// truncatedCompleter answers every call with a parseable finding AND the
// finish_reason=length marker, via MetaCompleter — the interface a real
// *llmclient.Client satisfies — so the engine stamps ResponseTruncated onto the
// AgentStatus exactly as production does. The finding matters: truncated outranks
// findings in reviewerOutcome's precedence, so this is the case that proves the
// tally says "truncated" even when the partial response DID raise something.
type truncatedCompleter struct{}

func (truncatedCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "HIGH|x.go:1|planted defect|fix it|correctness|15|evidence", nil
}

func (truncatedCompleter) CompleteWithMeta(_ context.Context, _ llmclient.Invocation) (llmclient.Completion, error) {
	return llmclient.Completion{
		Content:   "HIGH|x.go:1|planted defect|fix it|correctness|15|evidence",
		Truncated: true,
	}, nil
}

// The truncated outcome must travel the WHOLE path — fanout's ResponseTruncated
// marker → reviewerOutcome → the checkpoint's outcome field → OutcomeTallyKey → the
// reviewer_coverage.outcomes JSON — not just the pure-unit precedence table. It is
// the outcome most likely to appear on a real long-context run, and the only one
// whose serialization a unit test cannot see.
func TestExecuteBenchmarkRun_TruncatedOutcomeReachesCoverageAndSurvivesResume(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := t.TempDir() + "/ckpt.json"

	baseline, err := executeBenchmarkRun(context.Background(), cfg, truncatedCompleter{}, suiteValidPath, gen, path)
	require.NoError(t, err)
	cov := coverageFor(t, baseline, "m-greta", "greta")
	assert.Equal(t, 2, cov.Outcomes[benchmark.OutcomeTruncated],
		"a truncated response tallies as truncated on both cases, even though its partial content raised a finding")
	assert.Zero(t, cov.Outcomes[benchmark.OutcomeFindings],
		"data-integrity signals outrank volume signals: the finding it did raise must not reclassify the case")

	resumed, err := executeBenchmarkRun(context.Background(), cfg, truncatedCompleter{}, suiteValidPath, gen, path)
	require.NoError(t, err)
	assert.Equal(t, mustMarshal(t, baseline.Coverage), mustMarshal(t, resumed.Coverage),
		"the truncated tally survives the checkpoint round trip — a resumed run reports what the interrupted one recorded")
	assert.Contains(t, mustMarshal(t, resumed.Coverage), `"truncated":2`,
		"the wire form itself carries the truncated tally key")
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
