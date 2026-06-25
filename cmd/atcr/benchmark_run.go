package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/stream"
)

// executeBenchmarkRun executes a benchmark suite end to end and returns the
// suite-tagged benchmark.RunResult that `atcr benchmark export` consumes. It loads
// + validates the suite (benchmark.Load), then for each case ingests the case diff
// through the EXACT production review path (fanout.PrepareReviewFromDiff →
// fanout.ExecuteReview, the diff-file ingestion entry Epic 10.1 added), reads the
// per-reviewer findings + usage from the review's pool artifacts, scores the
// findings against the case's expected categories (benchmark.Score), and aggregates
// one scorecard.PublicRecord per reviewer.
//
// It lives in cmd/atcr — the composition root — rather than internal/benchmark so
// that package stays the light suite-contract + scorer leaf (no live-LLM
// dependency); the orchestration is the layer that wires the contract to the
// fan-out engine.
//
// The Completer is injected so the CLI passes the real llmclient and tests pass a
// stub (no network). generatedAt is injected (not time.Now) so two runs over the
// same suite + transcript produce a byte-identical RunResult — the reproducibility
// contract export relies on. Each case's review artifacts are written under a temp
// directory that is removed before the function returns; only the scored findings
// flow into the result, so the temp path never affects output.
func executeBenchmarkRun(ctx context.Context, cfg *fanout.ReviewConfig, completer fanout.Completer, suitePath string, generatedAt time.Time) (*benchmark.RunResult, error) {
	m, err := benchmark.Load(suitePath)
	if err != nil {
		return nil, err
	}

	tmp, err := os.MkdirTemp("", "atcr-benchmark-")
	if err != nil {
		return nil, fmt.Errorf("creating benchmark work dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	// reviewerAcc accumulates one reviewer's outcomes across every case.
	type reviewerAcc struct {
		model     string
		persona   string
		cases     []benchmark.CaseScore
		costUSD   float64
		latencies []int64 // per-case wall-clock, recorded only when usage was reported
	}
	accs := map[string]*reviewerAcc{}
	var order []string // reviewer names, sorted for deterministic aggregation

	for i, c := range m.Cases {
		diff, err := os.ReadFile(filepath.Join(suitePath, c.Diff))
		if err != nil {
			return nil, fmt.Errorf("reading case %q diff: %w", c.ID, err)
		}

		// Range-less request writing to an isolated per-case dir: no git range, no
		// .atcr/latest repoint (OutputDir suppresses it). The dir is keyed by case
		// INDEX, not id, so two distinct ids that share a path basename (a case id
		// may legally contain '/') can never collide and overwrite each other's
		// review. The date/suffix only feed the review id, never the RunResult, so
		// fixed values keep the run hermetic.
		req := fanout.ReviewRequest{
			Root:       tmp,
			OutputDir:  filepath.Join(tmp, fmt.Sprintf("case-%d", i)),
			Branch:     "benchmark",
			Date:       "2026-01-01",
			TimeSuffix: "000000",
			StartedAt:  time.Unix(0, 0).UTC(),
		}
		prep, err := fanout.PrepareReviewFromDiff(ctx, cfg, req, string(diff))
		if err != nil {
			return nil, fmt.Errorf("preparing case %q: %w", c.ID, err)
		}
		res, err := fanout.ExecuteReview(ctx, completer, prep)
		if err != nil {
			return nil, fmt.Errorf("executing case %q: %w", c.ID, err)
		}

		summary, err := fanout.ReadPoolSummary(res.Dir)
		if err != nil {
			return nil, fmt.Errorf("reading pool summary for case %q: %w", c.ID, err)
		}
		raisedByReviewer, err := readCaseFindings(res.Dir)
		if err != nil {
			return nil, fmt.Errorf("reading findings for case %q: %w", c.ID, err)
		}

		// Iterate the full agent roster (including failed agents, which raised
		// nothing) so every reviewer is scored on every case — a missed case is
		// recall 0, not an absent record.
		for _, a := range summary.Agents {
			acc := accs[a.Agent]
			if acc == nil {
				acc = &reviewerAcc{model: reviewerModel(cfg, a), persona: reviewerPersona(cfg, a.Agent)}
				accs[a.Agent] = acc
				order = append(order, a.Agent)
			}
			acc.cases = append(acc.cases, benchmark.CaseScore{Expected: c.ExpectedCategories, Raised: raisedByReviewer[a.Agent]})
			// Cost + latency are usage-gated: a completer that reports no token
			// usage (the test stub) contributes neither, keeping the score
			// deterministic. status.json only records Model/tokens when usage > 0.
			if a.TokensIn > 0 || a.TokensOut > 0 {
				acc.costUSD += llmclient.ComputeCostUSD(a.Model, a.TokensIn, a.TokensOut)
				acc.latencies = append(acc.latencies, a.DurationMS)
			}
		}
	}

	sort.Strings(order)
	reviewers := make([]benchmark.ReviewerScore, 0, len(order))
	for _, name := range order {
		acc := accs[name]
		reviewers = append(reviewers, benchmark.ReviewerScore{
			Model:        acc.model,
			Persona:      acc.persona,
			Cases:        acc.cases,
			CostUSD:      acc.costUSD,
			LatencyP50MS: medianInt64(acc.latencies),
		})
	}

	return &benchmark.RunResult{
		Suite:        m.Suite,
		SuiteVersion: m.SuiteVersion,
		GeneratedAt:  generatedAt.UTC().Format(time.RFC3339),
		Reviewers:    benchmark.Score(reviewers),
	}, nil
}

// readCaseFindings parses the merged pool findings.txt for one review and groups
// each finding's category by its REVIEWER (the agent name the engine stamped,
// never a model-supplied value). A pool with no findings yields an empty map.
func readCaseFindings(reviewDir string) (map[string][]string, error) {
	path := filepath.Join(reviewDir, "sources", "pool", "findings.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]string{}, nil
		}
		return nil, err
	}
	parsed, err := stream.ParseSource(data)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(parsed.Findings))
	for _, f := range parsed.Findings {
		out[f.Reviewer] = append(out[f.Reviewer], f.Category)
	}
	return out, nil
}

// reviewerModel resolves a reviewer's model id, preferring the usage-reported
// value in the pool summary and falling back to the configured model when the
// provider reported no usage (e.g. a stub completer leaves AgentStatus.Model empty).
func reviewerModel(cfg *fanout.ReviewConfig, a fanout.AgentStatus) string {
	if a.Model != "" {
		return a.Model
	}
	return cfg.Registry.Agents[a.Agent].Model
}

// reviewerPersona resolves a reviewer's persona from the registry, falling back to
// the agent name when no persona is configured.
func reviewerPersona(cfg *fanout.ReviewConfig, agent string) string {
	if p := cfg.Registry.Agents[agent].Persona; p != "" {
		return p
	}
	return agent
}

// medianInt64 returns the p50 of vs (lower-middle for an even count); 0 for an
// empty slice, so a no-usage run reports a deterministic 0 latency.
func medianInt64(vs []int64) int64 {
	if len(vs) == 0 {
		return 0
	}
	sorted := make([]int64, len(vs))
	copy(sorted, vs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[(len(sorted)-1)/2]
}
