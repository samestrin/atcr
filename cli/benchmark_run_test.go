package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	assert.Contains(t, got, "0.20", "the warning must name the ceiling")
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
