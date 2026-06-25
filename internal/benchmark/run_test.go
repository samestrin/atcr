package benchmark

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptrF(f float64) *float64 { return &f }

// twoAgentCfg builds a minimal in-memory ReviewConfig (two reviewers, diff mode)
// from exported registry types — the same shape fanout's own tests assemble — so
// the benchmark run executes offline with a stub Completer.
func twoAgentCfg() *fanout.ReviewConfig {
	return &fanout.ReviewConfig{
		Registry: &registry.Registry{
			Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: "http://unused"}},
			Agents: map[string]registry.AgentConfig{
				"greta": {Provider: "p", Model: "m-greta", Persona: "greta", Temperature: ptrF(0.7)},
				"kai":   {Provider: "p", Model: "m-kai", Persona: "kai", Temperature: ptrF(0.7)},
			},
		},
		Project:     &registry.ProjectConfig{Agents: []string{"greta", "kai"}},
		Settings:    registry.Settings{PayloadMode: "diff", TimeoutSecs: 600},
		PersonaDirs: registry.PersonaDirs{},
	}
}

// stubCompleter raises a single "correctness" finding for every invocation, no
// network. Against testdata/suite-valid that yields a discriminating score:
//
//	case-01 expected {correctness}            -> recall 1.0
//	case-02 expected {security, correctness}  -> recall 0.5  (security missed)
//
// so the macro-averaged corroboration_rate is 0.75 — proving per-case grouping.
type stubCompleter struct{}

func (stubCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "HIGH|x.go:1|planted defect|fix it|correctness|15|evidence", nil
}

// Run loads + validates the suite, executes each case's diff through the review
// pipeline with the injected Completer, scores findings against the case's
// expected categories, and aggregates per-reviewer scorecard.PublicRecord into a
// suite-tagged RunResult with the injected GeneratedAt.
func TestRun_ScoresSuiteEndToEnd(t *testing.T) {
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	rr, err := Run(context.Background(), twoAgentCfg(), stubCompleter{}, "testdata/suite-valid", gen)
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
	assert.InDelta(t, 0.0, greta.CostPerCorroboratedFindingUSD, 1e-9, "stub reports no usage")
	assert.Equal(t, int64(0), greta.LatencyP50MS, "stub reports no usage -> deterministic 0")

	assert.Equal(t, "m-kai", rr.Reviewers[1].Model)
	assert.InDelta(t, 0.75, rr.Reviewers[1].CorroborationRate, 1e-9)
}

// Two runs over the same suite + transcript are byte-identical (GeneratedAt is
// injected, no time.Now, no wall-clock latency under the stub).
func TestRun_Reproducible(t *testing.T) {
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	a, err := Run(context.Background(), twoAgentCfg(), stubCompleter{}, "testdata/suite-valid", gen)
	require.NoError(t, err)
	b, err := Run(context.Background(), twoAgentCfg(), stubCompleter{}, "testdata/suite-valid", gen)
	require.NoError(t, err)

	ja, err := json.Marshal(a)
	require.NoError(t, err)
	jb, err := json.Marshal(b)
	require.NoError(t, err)
	assert.JSONEq(t, string(ja), string(jb), "same suite + transcript -> byte-identical run-result")
}

// An invalid suite path fails before any review executes.
func TestRun_InvalidSuiteErrors(t *testing.T) {
	_, err := Run(context.Background(), twoAgentCfg(), stubCompleter{}, "testdata/does-not-exist", time.Now())
	require.Error(t, err)
}

// medianInt64 returns the lower-middle p50, and 0 for an empty slice (the
// deterministic no-usage path), independent of input order.
func TestMedianInt64(t *testing.T) {
	assert.Equal(t, int64(0), medianInt64(nil))
	assert.Equal(t, int64(5), medianInt64([]int64{5}))
	assert.Equal(t, int64(20), medianInt64([]int64{30, 10, 20}))
	assert.Equal(t, int64(10), medianInt64([]int64{20, 10}), "even count -> lower middle")
}
