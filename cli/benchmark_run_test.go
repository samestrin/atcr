package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func ptrF(f float64) *float64 { return &f }

// suiteValidPath is the in-repo fixture suite (2 cases), relative to cmd/atcr.
const suiteValidPath = "../internal/benchmark/testdata/suite-valid"

// benchCfg builds a minimal ReviewConfig (diff mode) from exported registry types
// — the same shape fanout's own tests assemble — for an offline benchmark run. Each
// pair is {agentName, model, persona}.
func benchCfg(agents ...[3]string) *fanout.ReviewConfig {
	regAgents := map[string]registry.AgentConfig{}
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		regAgents[a[0]] = registry.AgentConfig{Provider: "p", Model: a[1], Persona: a[2], Temperature: ptrF(0.7)}
		names = append(names, a[0])
	}
	return &fanout.ReviewConfig{
		Registry: &registry.Registry{
			Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: "http://unused"}},
			Agents:    regAgents,
		},
		Project:     &registry.ProjectConfig{Agents: names},
		Settings:    registry.Settings{PayloadMode: "diff", TimeoutSecs: 600},
		PersonaDirs: registry.PersonaDirs{},
	}
}

// stubCompleter raises a single "correctness" finding for every invocation, no
// network. Against suite-valid that is discriminating:
//
//	case-01 expected {correctness}            -> recall 1.0
//	case-02 expected {security, correctness}  -> recall 0.5 (security missed)
//
// so the macro-averaged corroboration_rate is 0.75 — proving per-case grouping.
type stubCompleter struct{}

func (stubCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "HIGH|x.go:1|planted defect|fix it|correctness|15|evidence", nil
}

// stubCategoryCompleter raises one finding under a caller-chosen category, so a
// test can drive the out-of-vocabulary rate off a word the closed vocabulary does
// not contain.
type stubCategoryCompleter struct{ category string }

func (s stubCategoryCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "HIGH|x.go:1|planted defect|fix it|" + s.category + "|15|evidence", nil
}

// stubNoFindingsCompleter reviews successfully and reports no defect at all — the
// only path that leaves a run with zero findings to measure. Distinct from a failed
// reviewer: the review itself succeeds, so the run is scored rather than aborted.
type stubNoFindingsCompleter struct{}

func (stubNoFindingsCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "No findings.", nil
}

// executeBenchmarkRun loads + validates the suite, executes each case's diff
// through the review pipeline with the injected Completer, scores findings against
// the case's expected categories, and aggregates per-reviewer PublicRecord into a
// suite-tagged RunResult with the injected generatedAt.
func TestExecuteBenchmarkRun_ScoresSuite(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"}, [3]string{"kai", "m-kai", "kai"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)

	assert.Equal(t, "fixture-mini", rr.Suite)
	assert.Equal(t, "1.0.0", rr.SuiteVersion)
	assert.Equal(t, "2026-06-25T12:00:00Z", rr.GeneratedAt, "GeneratedAt is the injected time, not time.Now")
	require.Len(t, rr.Reviewers, 2)

	// Reviewers sorted by (model, persona): m-greta < m-kai.
	greta := rr.Reviewers[0]
	assert.Equal(t, "m-greta", greta.Model)
	assert.Equal(t, "greta", greta.Persona, "persona sourced from registry config, not blank")
	assert.Equal(t, 2, greta.Runs, "one run per case")
	assert.InDelta(t, 0.75, greta.CorroborationRate, 1e-9, "(1.0 + 0.5) / 2 category recall")
	assert.InDelta(t, 1.0, greta.FindingsRaisedAvg, 1e-9, "one finding per case")
	require.NotNil(t, greta.CostPerCorroboratedFindingUSD, "corroborated findings exist -> key present even at 0 cost")
	assert.InDelta(t, 0.0, *greta.CostPerCorroboratedFindingUSD, 1e-9, "stub reports no usage")
	assert.Equal(t, int64(0), greta.LatencyP50MS, "stub reports no usage -> deterministic 0")
	assert.Equal(t, "m-kai", rr.Reviewers[1].Model)

	// The out-of-vocabulary diagnostic must reach the run result, or an operator
	// has no signal that reviewers drifted off the closed vocabulary. Without this
	// assertion the wiring could be deleted and the whole suite would stay green.
	// stubCompleter raises only `correctness`, a taxonomy member, so a measured 0.
	require.NotNil(t, rr.OutOfVocabularyRate, "a run that raised findings must report a rate")
	assert.InDelta(t, 0.0, *rr.OutOfVocabularyRate, 1e-9,
		"every stub finding uses a taxonomy member -> measured 0, not unmeasured")
}

// A run whose reviewers all drift off the vocabulary reports it. Paired with the
// clean case above so the wiring is pinned in both directions rather than by a
// single value that a hardcoded 0 would also satisfy.
func TestExecuteBenchmarkRun_ReportsVocabularyDrift(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := executeBenchmarkRun(context.Background(), cfg,
		stubCategoryCompleter{category: "not-a-taxonomy-word"}, suiteValidPath, gen, "")
	require.NoError(t, err)

	require.NotNil(t, rr.OutOfVocabularyRate)
	assert.InDelta(t, 1.0, *rr.OutOfVocabularyRate, 1e-9,
		"every finding used a non-member word -> full drift")
}

// An EMPTY category counts as drift, exactly like a non-member word — the third of
// vocabulary.go's definitional choices, whose absence would produce a rate that
// IMPROVES when a reviewer stops labelling entirely. Driven through the CLI wiring
// rather than the library because an unlabelled finding also has to survive the
// parser's short-row padding to reach the scorer at all.
func TestExecuteBenchmarkRun_EmptyCategoryCountsAsDrift(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := executeBenchmarkRun(context.Background(), cfg,
		stubCategoryCompleter{category: ""}, suiteValidPath, gen, "")
	require.NoError(t, err)

	require.NotNil(t, rr.OutOfVocabularyRate, "findings were raised -> measured, not unmeasured")
	assert.InDelta(t, 1.0, *rr.OutOfVocabularyRate, 1e-9,
		"an unlabelled finding is drift, not an exemption from the metric")
	require.Len(t, rr.Reviewers, 1)
	assert.InDelta(t, 1.0, rr.Reviewers[0].FindingsRaisedAvg, 1e-9,
		"the unlabelled finding still counts as a finding, so it is in the denominator too")
}

// A run that raised NO findings reports the rate as absent, never 0.0 — the
// nil-vs-zero distinction RunResult.OutOfVocabularyRate's pointer exists to carry.
// Asserted here at the CLI wiring level and against the MARSHALED JSON, because
// omitempty on the nil pointer is what actually drops the key from the run-result
// file an operator reads; the library-level test cannot observe that.
func TestExecuteBenchmarkRun_NoFindingsOmitsVocabularyRate(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := executeBenchmarkRun(context.Background(), cfg, stubNoFindingsCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)
	assert.Nil(t, rr.OutOfVocabularyRate, "no findings -> unmeasured, not a clean 0.0")

	raw, err := json.Marshal(rr)
	require.NoError(t, err)
	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &decoded))
	_, present := decoded["out_of_vocabulary_rate"]
	assert.False(t, present, "a nil rate must drop the key entirely — not emit null, not emit 0")
}

// Two runs over the same suite + transcript are byte-identical (generatedAt is
// injected, no time.Now, no wall-clock latency under the stub).
func TestExecuteBenchmarkRun_Reproducible(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	a, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)
	b, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)

	ja, err := json.Marshal(a)
	require.NoError(t, err)
	jb, err := json.Marshal(b)
	require.NoError(t, err)
	assert.JSONEq(t, string(ja), string(jb), "same suite + transcript -> byte-identical run-result")
}

// An invalid suite path fails before any review executes.
func TestExecuteBenchmarkRun_InvalidSuiteErrors(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	_, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, "testdata/does-not-exist", time.Now(), "")
	require.Error(t, err)
}

// buildRunResult must not alias accumulator state into the returned RunResult: it
// is documented as the shared fold path for fresh runs and recorded checkpoints, so
// a future caller that keeps folding after building a result would otherwise mutate
// an already-published artifact.
func TestBuildRunResult_DoesNotAliasAccumulatorState(t *testing.T) {
	accs := map[reviewerKey]*reviewerAcc{}
	var order []reviewerKey
	require.NoError(t, applyReviewerOutcome(accs, &order, reviewerCaseOutcome{
		model: "m", persona: "p", caseID: "case-01",
		expected: []string{"correctness"}, raised: []string{"correctness"},
		outcome: benchmark.OutcomeFindings,
	}))
	m := &benchmark.Manifest{Suite: "s", SuiteVersion: "1", Cases: []benchmark.Case{{ID: "case-01", ExpectedCategories: []string{"correctness"}}}}

	rr, err := buildRunResult(accs, order, m, time.Unix(0, 0))
	require.NoError(t, err)
	require.Len(t, rr.Coverage, 1)

	// Mutate the accumulator AFTER the result is built: every mutation must be
	// invisible to the returned artifact.
	acc := accs[reviewerKey{model: "m", persona: "p"}]
	acc.caseIDs[0] = "mutated"
	acc.outcomes[benchmark.OutcomeFindings] = 99

	assert.Equal(t, "case-01", rr.Coverage[0].CaseIDs[0])
	assert.Equal(t, 1, rr.Coverage[0].Outcomes[benchmark.OutcomeFindings])
}

// scrubField is NOT injective: it deletes whole path-, home- and credential-shaped
// tokens, so two distinct realized identities can collapse to the same public one
// (an email-shaped model id strips to empty, matching a genuinely empty one). Left
// undetected, buildRunResult would emit two coverage rows of identical public
// identity — which the export gate rejects as hand-assembled, misdiagnosing a
// legitimate run as tampering. The producer must fail loudly at build time, naming
// the colliding pre-scrub identities.
func TestBuildRunResult_FailsOnPostScrubIdentityCollision(t *testing.T) {
	accs := map[reviewerKey]*reviewerAcc{}
	var order []reviewerKey
	// Distinct raw keys (so the fold's own duplicate guard does not fire), distinct
	// cases, but both scrub to the same public identity.
	for i, model := range []string{"admin@internal.host", ""} {
		require.NoError(t, applyReviewerOutcome(accs, &order, reviewerCaseOutcome{
			model: model, persona: "p", caseID: fmt.Sprintf("case-%02d", i+1),
			expected: []string{"correctness"}, raised: []string{"correctness"},
			outcome: benchmark.OutcomeFindings, agent: fmt.Sprintf("lane-%d", i+1),
		}))
	}
	m := &benchmark.Manifest{Suite: "s", SuiteVersion: "1", Cases: []benchmark.Case{
		{ID: "case-01", ExpectedCategories: []string{"correctness"}},
		{ID: "case-02", ExpectedCategories: []string{"correctness"}},
	}}

	_, err := buildRunResult(accs, order, m, time.Unix(0, 0))
	require.Error(t, err, "two raw identities scrubbing to the same public identity must not publish")
	assert.Contains(t, err.Error(), "admin@internal.host", "the diagnostic names the colliding pre-scrub identities")
}

// medianInt64 returns the lower-middle p50, and 0 for an empty slice (the
// deterministic no-usage path), independent of input order.
func TestMedianInt64(t *testing.T) {
	assert.Equal(t, int64(0), medianInt64(nil))
	assert.Equal(t, int64(5), medianInt64([]int64{5}))
	assert.Equal(t, int64(20), medianInt64([]int64{30, 10, 20}))
	assert.Equal(t, int64(15), medianInt64([]int64{20, 10}), "even count -> floor of two middles, matching scorecard")
}

// AC: the run-result written by `benchmark run` is consumed unchanged by
// `benchmark export --in`, producing a valid suite-tagged Submission. This drives
// the orchestrator with a stub Completer (no network) over the suite-valid fixture,
// writes the run-result JSON exactly as `run --out` would, then feeds that file to
// the real `benchmark export` command and asserts the round-trip.
func TestBenchmarkRun_RoundTripsThroughExport(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)

	data, err := json.MarshalIndent(rr, "", "  ")
	require.NoError(t, err)
	in := filepath.Join(t.TempDir(), "run-result.json")
	require.NoError(t, os.WriteFile(in, data, 0o600))

	// Feed the run output to the real export command — the AC's "consumed unchanged".
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.Equal(t, 0, code, out)

	var sub struct {
		SubmittedAt  string `json:"submitted_at"`
		Source       string `json:"source"`
		Suite        string `json:"suite"`
		SuiteVersion string `json:"suite_version"`
		Reviewers    []struct {
			Persona                       string   `json:"persona"`
			Model                         string   `json:"model"`
			Runs                          int      `json:"runs"`
			CorroborationRate             float64  `json:"corroboration_rate"`
			CostPerCorroboratedFindingUSD *float64 `json:"cost_per_corroborated_finding_usd"`
		} `json:"reviewers"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &sub), "export stdout must be valid JSON: %s", out)
	require.Equal(t, "benchmark-suite", sub.Source, "only suite submissions are board-eligible")
	require.Equal(t, "fixture-mini", sub.Suite)
	require.Equal(t, "1.0.0", sub.SuiteVersion)
	require.Equal(t, "2026-06-25T12:00:00Z", sub.SubmittedAt, "submitted_at uses the run-result's generated_at")
	require.Len(t, sub.Reviewers, 1)
	require.Equal(t, "greta", sub.Reviewers[0].Persona)
	require.NotNil(t, sub.Reviewers[0].CostPerCorroboratedFindingUSD, "cost_per_corroborated_finding_usd must round-trip through the real CLI/export boundary")
	assert.Contains(t, out, "cost_per_corroborated_finding_usd", "the key itself must be present in the raw export output")
	require.Equal(t, "m-greta", sub.Reviewers[0].Model)
	require.Equal(t, 2, sub.Reviewers[0].Runs, "two cases scored")
	require.InDelta(t, 0.75, sub.Reviewers[0].CorroborationRate, 1e-9, "category recall (1.0 + 0.5)/2")
}

// A pool row whose PROBLEM leaked an unescaped pipe is recorded by the parser as
// SKIPPED, not as a finding. Discarding it shrinks the out-of-vocabulary
// DENOMINATOR, so the reviewer emitting the worst-formed output earns the best
// drift rate — the metric rewards exactly the behaviour it exists to detect. A
// skipped row must fold into its reviewer's raised categories as "" and count as
// drift, by the same rule that already makes an empty CATEGORY column drift.
//
// REVIEWER is the engine's last-appended column, so the final field survives an
// overflow earlier in the row; parse() strips trailing empty fields BEFORE
// classifying a row as skipped, so that field is non-empty by construction.
func TestReadCaseFindings_SkippedRowsCountAsDrift(t *testing.T) {
	dir := t.TempDir()
	poolDir := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))

	body := stream.Version + "\n" +
		// well-formed, in-vocabulary
		"HIGH|a.go:1|clean problem|clean fix|correctness|15|evidence|greta\n" +
		// unescaped pipe in PROBLEM -> 9 columns -> parser records it as skipped
		"HIGH|b.go:2|leaked | pipe here|some fix|security|15|evidence|greta\n"
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "findings.txt"), []byte(body), 0o600))

	got, err := readCaseFindings(dir)
	require.NoError(t, err)
	require.Contains(t, got, "greta", "the skipped row's REVIEWER must still be recovered")
	assert.ElementsMatch(t, []string{"correctness", ""}, got["greta"],
		"the malformed row joins the denominator as an unlabelled finding")

	// The recipe from the TD row: one in-vocabulary row + one malformed row is a
	// rate of 0.5, not 0.0.
	rate := benchmark.OutOfVocabularyRate([]benchmark.ReviewerScore{
		{Model: "m", Persona: "p", Cases: []benchmark.CaseScore{
			{Expected: []string{"correctness"}, Raised: got["greta"]},
		}},
	})
	require.NotNil(t, rate)
	assert.InDelta(t, 0.5, *rate, 1e-9, "1 drifted of 2 findings, not 0 of 1")
}

// A run whose reviewers breached the vocabulary ceiling must SAY SO to the
// operator. Before this, MaxOutOfVocabularyRate had no non-test consumer at all: a
// run measuring 0.72 drift wrote the number to JSON and exited 0 silently, while
// the constant's doc and the CHANGELOG both announced enforcement that did not
// exist. The warning names both the ceiling and the measured value, and goes to
// stderr so `--output <path>` (which prints nothing to stdout) still surfaces it.
func TestBenchmarkRun_WarnsWhenVocabularyCeilingExceeded(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := executeBenchmarkRun(context.Background(), cfg,
		stubCategoryCompleter{category: "not-a-taxonomy-word"}, suiteValidPath, gen, "")
	require.NoError(t, err)
	require.NotNil(t, rr.OutOfVocabularyRate)

	var buf bytes.Buffer
	warnIfVocabularyCeilingExceeded(&buf, rr.OutOfVocabularyRate)
	got := buf.String()
	assert.Contains(t, got, "out_of_vocabulary_rate", "the warning must name the metric")
	assert.Contains(t, got, "0.05", "the warning must name the ceiling")
	assert.Contains(t, got, "1.00", "the warning must name the measured value")
}

// The clean and unmeasured paths stay silent — a warning on every run is a warning
// nobody reads.
func TestBenchmarkRun_NoVocabularyWarningWhenCleanOrUnmeasured(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	clean, err := executeBenchmarkRun(context.Background(), cfg, stubCompleter{}, suiteValidPath, gen, "")
	require.NoError(t, err)
	require.NotNil(t, clean.OutOfVocabularyRate)

	var buf bytes.Buffer
	warnIfVocabularyCeilingExceeded(&buf, clean.OutOfVocabularyRate)
	assert.Empty(t, buf.String(), "a measured-clean run must not warn")

	buf.Reset()
	warnIfVocabularyCeilingExceeded(&buf, nil)
	assert.Empty(t, buf.String(), "an unmeasured run is not a breach")
}

// AC2, the defect this epic exists to close at the operator surface. The run-level
// warning is silent here — correctly, since 12 drifted findings pooled against 300
// clean ones is 0.038, under the ceiling — and one of the two models nevertheless
// ignored the enumeration on every single finding it raised. The operator must be
// told WHICH.
func TestWarnDriftingReviewers_NamesTheReviewerTheRunLevelWarningMisses(t *testing.T) {
	threeHundredClean := make([]string, 300)
	for i := range threeHundredClean {
		threeHundredClean[i] = "correctness"
	}
	twelveDrifted := make([]string, 12)
	for i := range twelveDrifted {
		twelveDrifted[i] = "bug"
	}
	reviewers := []benchmark.ReviewerScore{
		{Model: "clean-model", Persona: "alice", Cases: []benchmark.CaseScore{{
			Expected: []string{"correctness"}, Raised: threeHundredClean,
		}}},
		{Model: "drifted-model", Persona: "bob", Cases: []benchmark.CaseScore{{
			Expected: []string{"correctness"}, Raised: twelveDrifted,
		}}},
	}

	// Precondition: the EXISTING signal says nothing about this run.
	var runLevel bytes.Buffer
	warnIfVocabularyCeilingExceeded(&runLevel, benchmark.OutOfVocabularyRate(reviewers))
	require.Empty(t, runLevel.String(),
		"precondition: the pooled rate clears the ceiling, so the run-level warning stays silent")

	var buf bytes.Buffer
	warnDriftingReviewers(&buf, benchmark.PerReviewerVocabulary(reviewers))
	got := buf.String()

	assert.Contains(t, got, "drifted-model", "the warning must name the drifting reviewer's model")
	assert.Contains(t, got, "bob", "...and its persona, since the two together are the row identity")
	assert.Contains(t, got, "12/12", "the counts must be quoted so a small-n row is visibly small-n")
	assert.NotContains(t, got, "clean-model", "a clean reviewer must not be named")
}

// The threshold is deliberately NOT the run-level ceiling. Only one valid run exists
// under this metric (V1, n=1), so variance is unmeasured, and a per-reviewer signal
// set at the run-level number would assume a tightness a single observation cannot
// support — reproducing the defect warnIfVocabularyCeilingExceeded's own doc names:
// a warning printed on every run is a warning nobody reads.
func TestWarnDriftingReviewers_FiresOnlyAtTheMajorityThreshold(t *testing.T) {
	row := func(model string, drifted, findings int) benchmark.ReviewerVocabulary {
		rate := float64(drifted) / float64(findings)
		return benchmark.ReviewerVocabulary{
			Model: model, Persona: "p", Findings: findings, Drifted: drifted, Rate: &rate,
		}
	}

	var buf bytes.Buffer
	warnDriftingReviewers(&buf, []benchmark.ReviewerVocabulary{
		row("just-under", 49, 100),   // 0.49
		row("exactly-at", 50, 100),   // 0.50 — the threshold is inclusive
		row("well-over", 90, 100),    // 0.90
		row("above-ceiling", 6, 100), // 0.06 — over the RUN-level ceiling, under this one
	})
	got := buf.String()

	assert.Contains(t, got, "exactly-at", "the threshold is inclusive: a reviewer AT it is named")
	assert.Contains(t, got, "well-over")
	assert.NotContains(t, got, "just-under", "0.49 is not a majority")
	assert.NotContains(t, got, "above-ceiling",
		"the per-reviewer threshold is deliberately looser than the run-level ceiling")
}

// An unmeasured row is not a drifted one. A reviewer that raised nothing carries a nil
// rate, and treating that as drift would name the failed reviewers of a total-failure
// run as the vocabulary problem — the same nil-vs-zero collapse the pointer prevents.
func TestWarnDriftingReviewers_SilentOnCleanAndUnmeasuredRows(t *testing.T) {
	clean := 0.0
	var buf bytes.Buffer
	warnDriftingReviewers(&buf, []benchmark.ReviewerVocabulary{
		{Model: "unmeasured", Persona: "p"},                                    // nil rate
		{Model: "clean", Persona: "p", Findings: 40, Drifted: 0, Rate: &clean}, // 0.0
	})
	assert.Empty(t, buf.String(), "no row drifted, so there is nothing to say")

	buf.Reset()
	warnDriftingReviewers(&buf, nil)
	assert.Empty(t, buf.String(), "a run with no breakdown is not a run with a problem")
}

// The two signals are independent: a run can breach the ceiling AND have the breach
// concentrated in one reviewer, and the operator needs both facts. This pins that the
// per-reviewer warning does not go quiet just because the run-level one fired.
func TestWarnDriftingReviewers_FiresAlongsideTheRunLevelWarning(t *testing.T) {
	reviewers := []benchmark.ReviewerScore{
		{Model: "totally-drifted", Persona: "p", Cases: []benchmark.CaseScore{{
			Expected: []string{"correctness"}, Raised: []string{"bug", "clarity", "input"},
		}}},
	}
	rate := benchmark.OutOfVocabularyRate(reviewers)
	require.NotNil(t, rate)
	require.True(t, benchmark.ExceedsVocabularyCeiling(rate), "precondition: the run also breaches")

	var buf bytes.Buffer
	warnIfVocabularyCeilingExceeded(&buf, rate)
	warnDriftingReviewers(&buf, benchmark.PerReviewerVocabulary(reviewers))
	got := buf.String()

	assert.Contains(t, got, "out_of_vocabulary_rate", "the run-level warning still fires")
	assert.Contains(t, got, "totally-drifted", "and the per-reviewer one names who")
}

// AC2 end-to-end from a RunResult, and the reason the two signals share one call site:
// the scenario that matters is the one where the ceiling warning is SILENT and a
// reviewer is drifting anyway. A test that invokes each helper directly cannot observe
// that BOTH are reached from the command — delete either call and this fails.
func TestWarnVocabularyDiagnostics_SilentCeilingStillNamesTheDriftingReviewer(t *testing.T) {
	threeHundredClean := make([]string, 300)
	for i := range threeHundredClean {
		threeHundredClean[i] = "correctness"
	}
	twelveDrifted := make([]string, 12)
	for i := range twelveDrifted {
		twelveDrifted[i] = "bug"
	}
	reviewers := []benchmark.ReviewerScore{
		{Model: "clean-model", Persona: "alice", Cases: []benchmark.CaseScore{{Raised: threeHundredClean}}},
		{Model: "drifted-model", Persona: "bob", Cases: []benchmark.CaseScore{{Raised: twelveDrifted}}},
	}
	rr := &benchmark.RunResult{
		Suite:               "s",
		SuiteVersion:        "1.0.0",
		Reviewers:           benchmark.Score(reviewers),
		OutOfVocabularyRate: benchmark.OutOfVocabularyRate(reviewers),
		Vocabulary:          benchmark.PerReviewerVocabulary(reviewers),
	}
	require.False(t, benchmark.ExceedsVocabularyCeiling(rr.OutOfVocabularyRate),
		"precondition: the pooled rate passes, so only the per-reviewer signal can speak")

	var buf bytes.Buffer
	warnVocabularyDiagnostics(&buf, rr)
	got := buf.String()

	assert.NotContains(t, got, "is at or above", "the run-level ceiling was not breached")
	assert.Contains(t, got, "drifted-model/bob", "the drifting reviewer is named anyway")
	assert.Contains(t, got, "12/12")
}

// A breaching run emits BOTH signals from that same call site.
func TestWarnVocabularyDiagnostics_EmitsBothSignalsWhenBothApply(t *testing.T) {
	reviewers := []benchmark.ReviewerScore{
		{Model: "totally-drifted", Persona: "p", Cases: []benchmark.CaseScore{{
			Raised: []string{"bug", "clarity", "input"},
		}}},
	}
	rr := &benchmark.RunResult{
		OutOfVocabularyRate: benchmark.OutOfVocabularyRate(reviewers),
		Vocabulary:          benchmark.PerReviewerVocabulary(reviewers),
	}

	var buf bytes.Buffer
	warnVocabularyDiagnostics(&buf, rr)
	got := buf.String()

	// "is at or above" is unique to the ceiling warning. Asserting on
	// "out_of_vocabulary_rate" would NOT distinguish the two signals — the
	// per-reviewer warning names that metric in its own body, so the weaker
	// assertion passes even with the run-level call deleted.
	assert.Contains(t, got, "is at or above", "run-level breach still reported")
	assert.Contains(t, got, "totally-drifted/p", "and the row responsible is named")

	// A clean run says nothing at all, and a nil result is not a crash.
	buf.Reset()
	warnVocabularyDiagnostics(&buf, &benchmark.RunResult{})
	warnVocabularyDiagnostics(&buf, nil)
	assert.Empty(t, buf.String(), "an unmeasured run has nothing to report")
}

// The all-`other` blind spot, closed at the operator surface. Both routing values are
// taxonomy members, so a reviewer that labels EVERY finding `other` reports drift 0.0 —
// invisible to warnDriftingReviewers and no ceiling breach — while conveying no
// categorical information at all. warnVocabularyDiagnostics must still name that row,
// through its routing arm.
func TestWarnVocabularyDiagnostics_NamesTheAllRoutingReviewer(t *testing.T) {
	reviewers := []benchmark.ReviewerScore{
		{Model: "routing-only", Persona: "p", Cases: []benchmark.CaseScore{{
			Expected: []string{"correctness"}, Raised: []string{"other", "other", "other"},
		}}},
	}
	rr := &benchmark.RunResult{
		OutOfVocabularyRate: benchmark.OutOfVocabularyRate(reviewers),
		Vocabulary:          benchmark.PerReviewerVocabulary(reviewers),
	}
	require.Len(t, rr.Vocabulary, 1)
	require.Equal(t, rr.Vocabulary[0].Findings, rr.Vocabulary[0].RoutingValues,
		"precondition: every finding is a routing value")

	var buf bytes.Buffer
	warnVocabularyDiagnostics(&buf, rr)
	got := buf.String()

	assert.Contains(t, got, "routing-only/p", "an all-`other` reviewer must be named")
	assert.NotContains(t, got, "is at or above", "routing values are taxonomy members — no ceiling breach")
	assert.NotContains(t, got, "out of vocabulary", "drift is 0.0 — the drift warning must stay silent")
}

// A PARTIALLY-routing reviewer is visible too: the drift warning's per-row line quotes
// RoutingValues alongside the drift counts, so a reviewer hiding half its findings
// behind `other` is readable from the same line that names its drift.
func TestWarnDriftingReviewers_RowQuotesRoutingValues(t *testing.T) {
	reviewers := []benchmark.ReviewerScore{
		{Model: "half-routing", Persona: "p", Cases: []benchmark.CaseScore{{
			Expected: []string{"correctness"},
			Raised:   []string{"bug", "clarity", "other", "out-of-scope"},
		}}},
	}
	rows := benchmark.PerReviewerVocabulary(reviewers)
	require.Len(t, rows, 1)
	require.Equal(t, 2, rows[0].RoutingValues, "precondition: two of four findings are routing values")

	var buf bytes.Buffer
	warnDriftingReviewers(&buf, rows)
	got := buf.String()

	assert.Contains(t, got, "half-routing/p")
	assert.Contains(t, got, "2/4", "the drift counts are still quoted")
	assert.Contains(t, got, "routing", "the row must surface the routing-value count")
}

// A findings-parser regression drifts EVERY reviewer at once — on a 27-model roster an
// uncapped, alphabetized listing prints 27 rows with the worst drifter at an arbitrary
// position: the "warning nobody reads" outcome the threshold doc argues against. The
// listing must be sorted by descending rate and capped, with the remainder summarized.
func TestWarnDriftingReviewers_SortsByRateAndCapsTheListing(t *testing.T) {
	rows := make([]benchmark.ReviewerVocabulary, 0, 30)
	for i := 0; i < 30; i++ {
		rate := 0.50 + float64(i)*0.01 // 0.50 … 0.79
		rows = append(rows, benchmark.ReviewerVocabulary{
			Model: fmt.Sprintf("model-%02d", i), Persona: "p",
			Findings: 100, Drifted: int(rate * 100), Rate: &rate,
		})
	}

	var buf bytes.Buffer
	warnDriftingReviewers(&buf, rows)
	got := buf.String()

	worst := strings.Index(got, "model-29")
	middle := strings.Index(got, "model-14")
	require.NotEqual(t, -1, worst, "the worst drifter must appear")
	require.NotEqual(t, -1, middle)
	assert.Less(t, worst, middle, "rows must sort by descending rate, not alphabetically")
	assert.Contains(t, got, "more", "the tail must summarize the capped remainder")

	rowLines := 0
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "  model-") {
			rowLines++
		}
	}
	assert.Less(t, rowLines, 30, "the listing must be capped, not one line per drifting row")
}

// The breakdown a REAL run produces is what the warning reads — not a hand-built
// slice. This drives executeBenchmarkRun end to end and feeds its own rr.Vocabulary
// to the warning, so a defect anywhere in the producer chain (fold, sort, scrub,
// buildRunResult wiring) surfaces here rather than being papered over by a fixture.
//
// It deliberately does NOT claim to pin stderr ROUTING: it passes its own buffer
// rather than the command's writer. Pinning that requires driving the cobra command,
// which runBenchmarkRun cannot do offline (it resolves a real registry and completer)
// — tracked as tech debt rather than asserted falsely here.
func TestBenchmarkRun_RealRunBreakdownFeedsTheWarning(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := executeBenchmarkRun(context.Background(), cfg,
		stubCategoryCompleter{category: "not-a-taxonomy-word"}, suiteValidPath, gen, "")
	require.NoError(t, err)
	require.NotEmpty(t, rr.Vocabulary, "the run result must carry the breakdown the warning reads")

	var buf bytes.Buffer
	warnDriftingReviewers(&buf, rr.Vocabulary)
	assert.Contains(t, buf.String(), "m-greta",
		"a reviewer that drifted on every finding must be named from a real run's breakdown")
}
