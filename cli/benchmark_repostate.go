package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/stream"
)

// checkRepoStateFlags rejects flag combinations the repo-state tier cannot honor.
//
// --checkpoint is a standard-v1 feature (epic 10.3) built around that tier's
// per-case diff ingestion. Accepting the flag and ignoring it would let an
// operator start a long panel run believing it was resumable, and discover
// otherwise only when it failed — the worst possible moment, and after the panel
// had already been paid for. Refusing at parse time costs a re-run of a four-case
// suite instead.
func checkRepoStateFlags(suiteFormat, checkpointPath string) error {
	if suiteFormat == benchmark.FormatRepoStateV1 && checkpointPath != "" {
		return fmt.Errorf("--checkpoint is not supported for a %s suite: resumable runs are implemented for the standard-v1 diff path only; re-run without --checkpoint",
			benchmark.FormatRepoStateV1)
	}
	return nil
}

// executeRepoStateBenchmarkRun executes a repo-state-v1 suite end to end and
// returns the same suite-tagged benchmark.RunResult `atcr benchmark export`
// consumes for standard-v1.
//
// THE RANGE PATH, NOT THE DIFF PATH. Each case is materialized into a real git
// repository (base commit, then the change applied as a second commit carrying the
// case's commit message) and reviewed through fanout.PrepareReview over base..head
// — not fanout.PrepareReviewFromDiff, which executeBenchmarkRun uses for
// standard-v1. That is the whole reason this runner exists rather than a branch
// inside the existing one: PrepareReviewFromDiff builds no payload.RangeBuilder,
// and both the claim ledger (epic 35.16.7) and context-aware pre-fetching (epic
// 35.16.8) live there. A tier whose purpose is measuring those two features cannot
// run on the path that omits them.
//
// THE EPIC 14.1 GROUNDING GATE STAYS ON. On the range path it drops a finding whose
// cited file the patch never touched, which is every out-of-diff finding unless
// pre-fetching actually retrieved the cited span (internal/fanout/grounding.go, the
// PrefetchOnly arm). That is the measurement, not an obstacle to it: the tier exists
// to establish whether pre-fetching lets a genuine out-of-diff finding clear the
// shipped anti-hallucination gate, and disabling the gate for benchmark runs would
// hide exactly the thing being measured.
//
// The Completer is injected so the CLI passes the real llmclient and tests pass a
// stub, and generatedAt is injected rather than read from the wall clock, for the
// same reproducibility reason executeBenchmarkRun does both.
func executeRepoStateBenchmarkRun(ctx context.Context, cfg *fanout.ReviewConfig, completer fanout.Completer, suitePath string, generatedAt time.Time) (*benchmark.RunResult, error) {
	m, err := benchmark.LoadRepoState(suitePath)
	if err != nil {
		return nil, err
	}
	// The same two pre-flights executeBenchmarkRun applies, and for the same reason:
	// an unpublishable case id or reviewer identity makes the finished run
	// permanently unexportable, and the export gate would only say so after the
	// whole panel had been paid for.
	if err := validateRepoStatePublishableCaseIDs(m, suitePath); err != nil {
		return nil, err
	}
	if err := validatePublishableReviewerRoster(cfg); err != nil {
		return nil, err
	}

	tmp, err := os.MkdirTemp("", "atcr-repo-state-")
	if err != nil {
		return nil, fmt.Errorf("creating benchmark work dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	// Two accumulators over one pass. Category recall (the standard-v1 quantity
	// CorroborationRate carries on every suite) and positional recall are different
	// measurements of the same findings, so they are folded together rather than by
	// re-reading the pool twice.
	cats := map[reviewerKey]*benchmark.ReviewerScore{}
	positional := map[reviewerKey]*benchmark.RepoStateReviewerScore{}
	var order []reviewerKey

	caseIDs := make([]string, 0, len(m.Cases))
	for i, c := range m.Cases {
		caseIDs = append(caseIDs, c.ID)

		lm, err := loadCaseDiffLineMap(c)
		if err != nil {
			return nil, err
		}
		// Keyed by case INDEX rather than id so two ids sharing a path basename
		// cannot overwrite each other's tree, the same reason executeBenchmarkRun
		// keys its per-case output dir by index.
		repoDir := filepath.Join(tmp, fmt.Sprintf("repo-%d", i))
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			return nil, fmt.Errorf("case %q work dir: %w", c.ID, err)
		}
		mc, err := benchmark.MaterializeCase(ctx, c, repoDir)
		if err != nil {
			return nil, err
		}

		req := fanout.ReviewRequest{
			Repo:       mc.Root,
			Root:       mc.Root,
			Range:      fanout.ReviewRange{Base: mc.BaseSHA, Head: mc.HeadSHA},
			OutputDir:  filepath.Join(tmp, fmt.Sprintf("review-%d", i)),
			Branch:     "benchmark",
			Date:       "2026-01-01",
			TimeSuffix: "000000",
			StartedAt:  time.Unix(0, 0).UTC(),
		}
		prep, err := fanout.PrepareReview(ctx, cfg, req)
		if err != nil {
			return nil, fmt.Errorf("preparing case %q: %w", c.ID, err)
		}
		log.FromContext(ctx).Info("repo-state case executing", "case", c.ID, "reviewers", len(reviewerRoster(cfg)))
		res, err := fanout.ExecuteReview(ctx, completer, prep)
		if err != nil {
			return nil, fmt.Errorf("executing case %q: %w", c.ID, err)
		}

		summary, err := fanout.ReadPoolSummary(res.Dir)
		if err != nil {
			return nil, fmt.Errorf("reading pool summary for case %q: %w", c.ID, err)
		}
		located, err := readCaseFindingsLocated(res.Dir)
		if err != nil {
			return nil, fmt.Errorf("reading findings for case %q: %w", c.ID, err)
		}

		// Iterate the full roster, not just reviewers that raised something: a
		// reviewer that missed the whole case is recall 0, not an absent row.
		for _, a := range summary.Agents {
			key := reviewerKey{model: reviewerModel(cfg, a), persona: reviewerPersona(cfg, a.Agent)}
			if _, ok := cats[key]; !ok {
				cats[key] = &benchmark.ReviewerScore{Model: key.model, Persona: key.persona}
				positional[key] = &benchmark.RepoStateReviewerScore{Model: key.model, Persona: key.persona}
				order = append(order, key)
			}
			reported := located[a.Agent]

			raised := make([]string, 0, len(reported))
			for _, f := range reported {
				raised = append(raised, f.Category)
			}
			cats[key].Cases = append(cats[key].Cases, benchmark.CaseScore{
				Expected: expectedCategories(c),
				Raised:   raised,
			})
			positional[key].Cases = append(positional[key].Cases, benchmark.RepoStateCaseScore{
				CaseID:  c.ID,
				Matches: benchmark.MatchFindings(c.ExpectedFindings, reported, lm),
			})

			// Cost and latency are usage-gated exactly as on the standard path: a
			// stub completer reports no usage, so both stay 0 and the score is
			// deterministic.
			if a.TokensIn > 0 || a.TokensOut > 0 {
				cats[key].CostUSD += llmclient.ComputeCostUSD(a.Model, a.TokensIn, a.TokensOut)
				cats[key].LatencyP50MS = a.DurationMS
			}
		}
	}

	catScores := make([]benchmark.ReviewerScore, 0, len(order))
	posScores := make([]benchmark.RepoStateReviewerScore, 0, len(order))
	for _, k := range order {
		catScores = append(catScores, *cats[k])
		posScores = append(posScores, *positional[k])
	}

	return &benchmark.RunResult{
		Suite:               m.Suite,
		SuiteVersion:        m.SuiteVersion,
		GeneratedAt:         generatedAt.UTC().Format(time.RFC3339),
		Reviewers:           benchmark.Score(catScores),
		OutOfVocabularyRate: benchmark.OutOfVocabularyRate(catScores),
		SuiteCaseIDs:        caseIDs,
		PositionalRecall:    benchmark.ScorePositional(posScores),
	}, nil
}

// expectedCategories projects a case's located expectations onto the bare category
// list CorroborationRate is defined over.
//
// The two metrics deliberately measure the SAME findings under different
// denominators: category recall asks "did any finding carry the right word",
// positional recall asks "did a finding land in the right place". Keeping
// CorroborationRate's meaning identical across both suites is what lets a
// repo-state row sit on the same public board as a standard-v1 one (AC5).
func expectedCategories(c benchmark.RepoStateCase) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(c.ExpectedFindings))
	for _, f := range c.ExpectedFindings {
		if seen[f.Category] {
			continue
		}
		seen[f.Category] = true
		out = append(out, f.Category)
	}
	return out
}

// loadCaseDiffLineMap reads and parses a case's own diff, which is what makes
// outside_diff a measurement rather than a label (AC3b).
func loadCaseDiffLineMap(c benchmark.RepoStateCase) (benchmark.DiffLineMap, error) {
	raw, err := os.ReadFile(filepath.Join(c.Dir, c.Diff))
	if err != nil {
		return benchmark.DiffLineMap{}, fmt.Errorf("reading case %q diff: %w", c.ID, err)
	}
	lm, err := benchmark.ParseDiffLineMap(raw)
	if err != nil {
		return benchmark.DiffLineMap{}, fmt.Errorf("case %q: %w", c.ID, err)
	}
	return lm, nil
}

// validateRepoStatePublishableCaseIDs is validateSuitePublishableCaseIDs for the
// repo-state manifest shape. It applies the identical three-arm publication rule
// through the same checkPublishable helper, so the two tiers cannot drift on what
// a publishable suite is; only the manifest type differs.
func validateRepoStatePublishableCaseIDs(m *benchmark.RepoStateManifest, suitePath string) error {
	for _, f := range []struct{ noun, published, value, consequence, remedy string }{
		{"suite name", "the envelope's suite name", m.Suite,
			"the published envelope must name the same suite the manifest does",
			"rename the suite in the suite manifest"},
		{"suite_version", "the envelope's suite_version", m.SuiteVersion,
			"the published envelope must name the same suite_version the manifest does",
			"change suite_version in the suite manifest"},
	} {
		if err := checkPublishable(suitePath, "declares "+f.noun, f.value, f.published, f.consequence, f.remedy); err != nil {
			return err
		}
	}
	for _, c := range m.Cases {
		if err := checkPublishable(suitePath, "declares case", c.ID, "suite_case_ids",
			"the published suite_case_ids must name the same cases as the manifest",
			"rename the case in the suite manifest"); err != nil {
			return err
		}
	}
	return nil
}

// readCaseFindingsLocated is readCaseFindings with the POSITION kept. The
// standard-v1 reader discards file and line because category recall has no use for
// them; positional matching is nothing but those two fields.
//
// Unparseable ("skipped") rows are deliberately NOT folded in here, which is the
// one place this reader differs from its category sibling beyond the projection.
// That sibling folds them in with an empty category so the out-of-vocabulary
// DENOMINATOR is not shrunk by malformed output. A skipped row has no recoverable
// file or line, so it cannot match any expectation, and adding it as an
// unmatchable entry would change no positional outcome while inviting a reader to
// think it might.
func readCaseFindingsLocated(reviewDir string) (map[string][]benchmark.ReportedFinding, error) {
	path := filepath.Join(reviewDir, "sources", "pool", "findings.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]benchmark.ReportedFinding{}, nil
		}
		return nil, err
	}
	parsed, err := stream.ParseSource(data)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]benchmark.ReportedFinding, len(parsed.Findings))
	for _, f := range parsed.Findings {
		out[f.Reviewer] = append(out[f.Reviewer], benchmark.ReportedFinding{
			File:     f.File,
			Line:     f.Line,
			Category: f.Category,
		})
	}
	return out, nil
}
