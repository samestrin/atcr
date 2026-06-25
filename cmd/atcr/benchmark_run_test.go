package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/require"
)

func ptrF(f float64) *float64 { return &f }

// stubRunCfg builds a minimal two-reviewer ReviewConfig (diff mode) from exported
// registry types, so a benchmark run executes offline with stubRunCompleter.
func stubRunCfg() *fanout.ReviewConfig {
	return &fanout.ReviewConfig{
		Registry: &registry.Registry{
			Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: "http://unused"}},
			Agents: map[string]registry.AgentConfig{
				"greta": {Provider: "p", Model: "m-greta", Persona: "greta", Temperature: ptrF(0.7)},
			},
		},
		Project:     &registry.ProjectConfig{Agents: []string{"greta"}},
		Settings:    registry.Settings{PayloadMode: "diff", TimeoutSecs: 600},
		PersonaDirs: registry.PersonaDirs{},
	}
}

type stubRunCompleter struct{}

func (stubRunCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "HIGH|x.go:1|planted defect|fix it|correctness|15|evidence", nil
}

// AC: the run-result written by `benchmark run` is consumed unchanged by
// `benchmark export --in`, producing a valid suite-tagged Submission. This drives
// benchmark.Run with a stub Completer (no network) over the suite-valid fixture,
// writes the run-result JSON exactly as `run --out` would, then feeds that file to
// the real `benchmark export` command and asserts the round-trip.
func TestBenchmarkRun_RoundTripsThroughExport(t *testing.T) {
	suitePath := filepath.Join("..", "..", "internal", "benchmark", "testdata", "suite-valid")
	gen := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	rr, err := benchmark.Run(context.Background(), stubRunCfg(), stubRunCompleter{}, suitePath, gen)
	require.NoError(t, err)

	data, err := json.MarshalIndent(rr, "", "  ")
	require.NoError(t, err)
	in := filepath.Join(t.TempDir(), "run-result.json")
	require.NoError(t, os.WriteFile(in, data, 0o600))

	// Feed the run output to the real export command — the AC's "consumed unchanged".
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in)
	require.Equal(t, 0, code, out)

	var sub struct {
		SubmissionSchema int    `json:"submission_schema"`
		SubmittedAt      string `json:"submitted_at"`
		Source           string `json:"source"`
		Suite            string `json:"suite"`
		SuiteVersion     string `json:"suite_version"`
		Reviewers        []struct {
			Persona           string  `json:"persona"`
			Model             string  `json:"model"`
			Runs              int     `json:"runs"`
			CorroborationRate float64 `json:"corroboration_rate"`
		} `json:"reviewers"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &sub), "export stdout must be valid JSON: %s", out)
	require.Equal(t, "benchmark-suite", sub.Source, "only suite submissions are board-eligible")
	require.Equal(t, "fixture-mini", sub.Suite)
	require.Equal(t, "1.0.0", sub.SuiteVersion)
	require.Equal(t, "2026-06-25T12:00:00Z", sub.SubmittedAt, "submitted_at uses the run-result's generated_at")
	require.Len(t, sub.Reviewers, 1)
	require.Equal(t, "greta", sub.Reviewers[0].Persona)
	require.Equal(t, "m-greta", sub.Reviewers[0].Model)
	require.Equal(t, 2, sub.Reviewers[0].Runs, "two cases scored")
	require.InDelta(t, 0.75, sub.Reviewers[0].CorroborationRate, 1e-9, "category recall (1.0 + 0.5)/2")
}
