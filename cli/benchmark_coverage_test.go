package cli

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
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
// recomputing it from a different source — the property that keeps a resume across a
// failover boundary honest (AC4).
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
