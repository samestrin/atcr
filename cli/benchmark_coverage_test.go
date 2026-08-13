package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coverageFor returns the ReviewerCoverage row matching (model, persona), or fails
// the test naming what was actually present — a missing row is otherwise reported as
// a nil-pointer panic several lines later.
func coverageFor(t *testing.T, rr *benchmark.RunResult, model, persona string) benchmark.ReviewerCoverage {
	t.Helper()
	var got []string
	for _, c := range rr.Coverage {
		if c.Model == model && c.Persona == persona {
			return c
		}
		got = append(got, c.Model+"/"+c.Persona)
	}
	require.FailNowf(t, "coverage row not found", "want %s/%s, have %v", model, persona, got)
	return benchmark.ReviewerCoverage{}
}

// The run-result must NAME the cases behind every reviewer row, not merely count
// them. `runs` is a count, and case difficulty on this suite varies enormously, so a
// recall computed over one subset is not comparable to one computed over a different
// subset of the same size. Splitting rows by realized model makes uneven coverage the
// normal case, which is what turns this from a nicety into a requirement.
func TestExecuteBenchmarkRun_RecordsSuiteCaseIDsAndCoverage(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"}, [3]string{"kai", "m-kai", "kai"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)

	assert.Equal(t, []string{"case-01-nil-deref", "case-02-sql-injection"}, rr.SuiteCaseIDs,
		"the suite's case-id list is recorded once, in manifest order — it is the denominator every row is measured against")

	require.Len(t, rr.Coverage, 2, "one coverage row per reviewer row")
	for _, model := range []string{"m-greta", "m-kai"} {
		cov := coverageFor(t, rr, model, map[string]string{"m-greta": "greta", "m-kai": "kai"}[model])
		assert.Equal(t, []string{"case-01-nil-deref", "case-02-sql-injection"}, cov.CaseIDs,
			"a reviewer that served every case covers every case")
	}
}

// Coverage rows are ordered identically to the reviewer rows they describe, so a
// consumer can join them positionally as well as by identity — and so the run-result
// stays byte-identical across runs.
func TestExecuteBenchmarkRun_CoverageOrderMatchesReviewerOrder(t *testing.T) {
	cfg := benchCfg([3]string{"kai", "m-kai", "kai"}, [3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)
	require.Len(t, rr.Coverage, len(rr.Reviewers))
	for i := range rr.Reviewers {
		assert.Equal(t, rr.Reviewers[i].Model, rr.Coverage[i].Model, "row %d identity must line up", i)
		assert.Equal(t, rr.Reviewers[i].Persona, rr.Coverage[i].Persona, "row %d identity must line up", i)
		assert.Equal(t, rr.Reviewers[i].Runs, len(rr.Coverage[i].CaseIDs),
			"row %d: the coverage set must have exactly as many entries as `runs` claims", i)
	}
}

// A resumed run reports the SAME coverage as an uninterrupted one. Coverage is
// accumulated on the shared fold path, so replay reconstructs it rather than
// recomputing it from a different source — the property that keeps a same-model
// resume honest (AC4). The failover boundary itself — replayed entries folding
// under model A while live cases fold under model B — is covered by
// TestExecuteBenchmarkRun_ResumeAcrossFailoverBoundarySplitsCoverage.
func TestExecuteBenchmarkRun_ResumeReportsSameCoverage(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := t.TempDir() + "/ckpt.json"

	baseline, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)
	require.NotEmpty(t, baseline.Coverage, "the baseline must actually carry coverage or the comparison is vacuous")

	// Fail after case 0 so the checkpoint holds a partial run, then resume it.
	_, err = executeBenchmarkRun(context.Background(), cfg, &failAfterCompleter{ok: 1}, suiteValidPath, gen, path)
	require.Error(t, err)
	resumed, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, path)
	require.NoError(t, err)

	assert.Equal(t, mustMarshal(t, baseline.Coverage), mustMarshal(t, resumed.Coverage),
		"a resumed run's coverage is identical to an uninterrupted run's")
	assert.Equal(t, baseline.SuiteCaseIDs, resumed.SuiteCaseIDs)
}

// A run-result produced before coverage existed unmarshals with both fields nil, and
// a coverage-free RunResult marshals without the keys at all. This is what lets
// `benchmark export` distinguish "measured and short" from "never measured" rather
// than reading an absent field as a violation.
func TestRunResult_CoverageAbsentWhenUnmeasured(t *testing.T) {
	var rr benchmark.RunResult
	require.NoError(t, json.Unmarshal([]byte(
		`{"suite":"mini","suite_version":"1.0.0","generated_at":"2026-06-24T12:00:00Z","reviewers":[]}`), &rr))
	assert.Nil(t, rr.SuiteCaseIDs, "a pre-coverage run-result reports unmeasured, not empty")
	assert.Nil(t, rr.Coverage)

	raw, err := json.Marshal(rr)
	require.NoError(t, err)
	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.NotContains(t, decoded, "suite_case_ids", "nil coverage drops the key entirely — not null, not []")
	assert.NotContains(t, decoded, "reviewer_coverage")
}

// The shortfall message caps missing case ids per row at maxNamedMissingCases,
// but deliberately names EVERY short row: each row is a distinct reviewer
// identity whose re-runs the operator must schedule, so an overflow count on the
// outer list would hide who owes work — the actionable unit of the message.
func TestCheckCoverage_EveryShortRowIsNamed(t *testing.T) {
	rr := benchmark.RunResult{SuiteCaseIDs: []string{"case-01"}}
	for i := 0; i < 5; i++ {
		model := fmt.Sprintf("m-%d", i)
		rr.Reviewers = append(rr.Reviewers, scorecard.PublicRecord{Model: model, Persona: "p", Runs: 0})
		rr.Coverage = append(rr.Coverage, benchmark.ReviewerCoverage{Model: model, Persona: "p"})
	}

	err := checkCoverage(io.Discard, rr, "rr.json", false)
	require.Error(t, err)
	msg := err.Error()
	for i := 0; i < 5; i++ {
		assert.Contains(t, msg, fmt.Sprintf("m-%d/p", i), "every short row is named, past any cap")
	}
}

// The outcomes tally is untrusted input at the export boundary, same as
// out_of_vocabulary_rate at load: the producer writes one closed-vocabulary outcome
// per case, so a NEGATIVE count or a key outside the vocabulary can only come from a
// hand-assembled file. The sum check alone catches neither — a -1 paired with an
// inflated positive still sums to the covered-set size, and a fabricated key simply
// adds to the tally.
func TestCheckCoverage_RejectsMalformedOutcomeTallies(t *testing.T) {
	base := func(outcomes map[string]int) benchmark.RunResult {
		return benchmark.RunResult{
			SuiteCaseIDs: []string{"case-01"},
			Reviewers:    []scorecard.PublicRecord{{Model: "m", Persona: "p", Runs: 1}},
			Coverage: []benchmark.ReviewerCoverage{{
				Model: "m", Persona: "p", CaseIDs: []string{"case-01"}, Outcomes: outcomes,
			}},
		}
	}

	for name, outcomes := range map[string]map[string]int{
		"negative count":        {"clean": -1, "findings": 2}, // sums to 1: the sum check passes it
		"out-of-vocabulary key": {"fabricated": 1},            // sums to 1: also passes
	} {
		err := checkCoverage(io.Discard, base(outcomes), "rr.json", false)
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "malformed", name)
	}

	// The legitimate vocabulary — including the "unknown" tally label — stays legal.
	err := checkCoverage(io.Discard, base(map[string]int{"unknown": 1}), "rr.json", false)
	require.NoError(t, err, "the unknown tally label is a legitimate key")
}
