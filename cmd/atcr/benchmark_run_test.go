package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptrF(f float64) *float64 { return &f }

// suiteValidPath is the in-repo fixture suite (2 cases), relative to cmd/atcr.
const suiteValidPath = "../../internal/benchmark/testdata/suite-valid"

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
