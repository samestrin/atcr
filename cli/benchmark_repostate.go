package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/scorecard"
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
	// EqualFold for the same reason runBenchmarkRun routes with it: a differently-
	// cased discriminator is the same tier, and the refusal must fire before a run
	// the operator believes is resumable is paid for.
	if strings.EqualFold(suiteFormat, benchmark.FormatRepoStateV1) && checkpointPath != "" {
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

	// Every case's diff is parsed BEFORE the first paid completer call. The parse
	// used to run inside the per-case loop, so case N's malformed hunk header
	// surfaced only after cases 1..N-1 had driven the whole reviewer panel — the
	// exact fail-late shape LoadRepoState's eager-load contract names one level up
	// ("a defective case fails at load, where the remedy is free, instead of
	// part-way through a paid panel run"). Folding it into LoadRepoState itself
	// would also put it behind `benchmark verify`; that is internal/benchmark's
	// file, so this pre-flight is the in-runner guarantee.
	lineMaps := make([]benchmark.DiffLineMap, len(m.Cases))
	for i := range m.Cases {
		lm, err := loadCaseDiffLineMap(m.Cases[i])
		if err != nil {
			return nil, err
		}
		lineMaps[i] = lm
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
	acc := map[reviewerKey]*repoStateAcc{}
	var order []reviewerKey

	caseIDs := make([]string, 0, len(m.Cases))
	for i, c := range m.Cases {
		caseIDs = append(caseIDs, c.ID)

		lm := lineMaps[i]
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

		// Fixed Branch/Date/TimeSuffix and a zero StartedAt are carried verbatim from
		// executeBenchmarkRun's request: the date and suffix only feed the review id,
		// never the RunResult, so fixed values keep the run hermetic rather than
		// tying it to the wall clock. Deliberate, not placeholder — see the matching
		// comment at cli/benchmark_run.go's request construction.
		//
		// NoIgnore, because a repo-state case's reviewable set is its DIFF — the
		// planted change — not the ignore policy of the tree it ships. A base tree
		// carrying an .atcrignore that excludes the changed paths would otherwise
		// resolve the range to zero reviewable files and abort the run mid-panel,
		// after earlier cases were paid for; nothing at load detects that shape, and
		// the standard-v1 diff path cannot hit it at all (its reviewable set is the
		// diff bytes). The materialized tree's .gitignore and the host excludes file
		// are already closed off at materialization; this closes the last ignore
		// source on the only tier where the reviewed range is author-planted.
		req := fanout.ReviewRequest{
			Repo:       mc.Root,
			Root:       mc.Root,
			Range:      fanout.ReviewRange{Base: mc.BaseSHA, Head: mc.HeadSHA},
			OutputDir:  filepath.Join(tmp, fmt.Sprintf("review-%d", i)),
			NoIgnore:   true,
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
		located, categorical, err := readCaseFindingsLocated(res.Dir)
		if err != nil {
			return nil, fmt.Errorf("reading findings for case %q: %w", c.ID, err)
		}
		// The materialized repo has no consumer once the findings are read: scoring
		// reads neither the tree nor the .git, and the review dir carries the
		// artifacts. Released HERE rather than at run end, so a large-base-tree
		// suite hands its repos back as it goes instead of accumulating one per
		// case under a $TMPDIR volume a long panel can exhaust mid-run.
		if err := os.RemoveAll(repoDir); err != nil {
			return nil, fmt.Errorf("case %q: removing work repo: %w", c.ID, err)
		}

		// Iterate the full roster, not just reviewers that raised something: a
		// reviewer that missed the whole case is recall 0, not an absent row.
		for _, a := range summary.Agents {
			key := reviewerKey{model: reviewerModel(cfg, a), persona: reviewerPersona(cfg, a.Agent)}
			if _, ok := cats[key]; !ok {
				cats[key] = &benchmark.ReviewerScore{Model: key.model, Persona: key.persona}
				positional[key] = &benchmark.RepoStateReviewerScore{Model: key.model, Persona: key.persona}
				acc[key] = &repoStateAcc{scored: map[string]string{}, outcomes: map[string]int{}}
				order = append(order, key)
			}
			// Two lanes can realize the SAME (model, persona) — a parallel and a
			// serial slot pointing at one registry entry, or a fallback converging on
			// another agent's model. Both then append a CaseScore for this case,
			// silently doubling Runs and re-weighting CorroborationRate. The standard
			// tier fails closed on exactly this, and so must this one: a merged
			// identity is only meaningful when the two lanes PARTITION the suite.
			if prior, dup := acc[key].scored[c.ID]; dup {
				return nil, fmt.Errorf("case %q scored twice under realized identity %q/%q (agents %q and %q); "+
					"two lanes sharing one identity must partition the suite, not both score it",
					c.ID, key.model, key.persona, prior, a.Agent)
			}
			acc[key].scored[c.ID] = a.Agent

			cats[key].Cases = append(cats[key].Cases, benchmark.CaseScore{
				Expected: expectedCategories(c),
				// The CATEGORICAL projection, which folds unparseable rows back in
				// with an empty category. Driving this off the positional projection
				// instead would shrink the out-of-vocabulary denominator for a
				// reviewer emitting malformed output — rewarding exactly the
				// behaviour that metric exists to detect.
				Raised: categorical[a.Agent],
			})
			positional[key].Cases = append(positional[key].Cases, benchmark.RepoStateCaseScore{
				CaseID:  c.ID,
				Matches: benchmark.MatchFindings(c.ExpectedFindings, located[a.Agent], lm),
			})

			acc[key].caseIDs = append(acc[key].caseIDs, c.ID)
			acc[key].outcomes[benchmark.OutcomeTallyKey(reviewerOutcome(a, categorical[a.Agent]))]++
			if a.FallbackUsed {
				acc[key].fallbackCases++
			}

			// Cost and latency are usage-gated exactly as on the standard path: a
			// stub completer reports no usage, so both stay 0 and the score is
			// deterministic.
			if a.TokensIn > 0 || a.TokensOut > 0 {
				cats[key].CostUSD += llmclient.ComputeCostUSD(a.Model, a.TokensIn, a.TokensOut)
				// COLLECTED, not overwritten. LatencyP50MS is a median on the frozen
				// public row, and assigning each case in turn published the LAST
				// case's wall clock under a column that means something else on every
				// standard-v1 row.
				acc[key].latencies = append(acc[key].latencies, a.DurationMS)
			}
		}
	}

	// The post-scrub identity collision guard buildRunResult carries: scrubField
	// is not injective (it deletes path-, home- and credential-shaped tokens), so
	// two DISTINCT raw identities can fold into one public one. This runner folds
	// per raw key, so both would emit their own Reviewers row under the same
	// public identity — which checkCoverage then rejects as a hand-assembled file
	// after the whole panel was paid for, with a diagnostic that cannot see the
	// raw strings. The producer names both pre-scrub identities instead. Checked
	// BEFORE the sort and the emit, so the four arrays below are built from the
	// same public identities that just passed the collision gate.
	public := make(map[reviewerKey]reviewerKey, len(order))  // public identity -> pre-scrub key (collision naming)
	scrubOf := make(map[reviewerKey]reviewerKey, len(order)) // pre-scrub key -> public identity (emit)
	for _, k := range order {
		s := scorecard.ScrubPublicRecord(scorecard.PublicRecord{Model: k.model, Persona: k.persona})
		id := reviewerKey{model: s.Model, persona: s.Persona}
		if prev, dup := public[id]; dup {
			return nil, fmt.Errorf("distinct reviewer identities %q/%q and %q/%q scrub to the same public identity %q/%q: "+
				"scorecard's path/credential scrub is not injective, so publishing would emit two reviewer rows under one identity",
				prev.model, prev.persona, k.model, k.persona, id.model, id.persona)
		}
		public[id] = k
		scrubOf[k] = id
	}

	// All four emitted arrays share ONE order. The coverage array used to be
	// emitted in `order` — first-sighting slot order — while Reviewers, Vocabulary
	// and PositionalRecall each come back re-sorted on the scrubbed identity, so
	// on any panel with more than one identity the documented positional join
	// (coverage[i] describes reviewers[i]) was false for every repo-state
	// run-result. Sorting `order` by the same scrubbed pair makes the alignment a
	// property of the code; Score's own re-sort is then idempotent on it.
	sort.SliceStable(order, func(i, j int) bool {
		if scrubOf[order[i]].model != scrubOf[order[j]].model {
			return scrubOf[order[i]].model < scrubOf[order[j]].model
		}
		return scrubOf[order[i]].persona < scrubOf[order[j]].persona
	})

	catScores := make([]benchmark.ReviewerScore, 0, len(order))
	posScores := make([]benchmark.RepoStateReviewerScore, 0, len(order))
	coverage := make([]benchmark.ReviewerCoverage, 0, len(order))
	for _, k := range order {
		// The PUBLIC identity — the same scrubbed pair the collision gate above
		// computed — goes into the coverage row. Writing the RAW key here shipped
		// credential- and path-shaped model ids verbatim inside reviewer_coverage[],
		// the exact identity leak the sibling arrays refuse; the export join only
		// survived because coverageKey re-scrubbed on read. Both sides now carry
		// the scrubbed value by construction.
		pub := scrubOf[k]
		cats[k].LatencyP50MS = medianInt64(acc[k].latencies)
		catScores = append(catScores, *cats[k])
		posScores = append(posScores, *positional[k])
		// Coverage is not optional decoration: `benchmark export` hard-rejects a
		// run-result that names a suite but records no reviewer coverage, calling the
		// FILE malformed. Omitting it made every repo-state run unexportable by
		// construction, and the operator would learn so only after paying for a full
		// panel.
		coverage = append(coverage, benchmark.ReviewerCoverage{
			Model:         pub.model,
			Persona:       pub.persona,
			CaseIDs:       acc[k].caseIDs,
			Outcomes:      acc[k].outcomes,
			FallbackCases: acc[k].fallbackCases,
		})
	}

	return &benchmark.RunResult{
		Suite:               m.Suite,
		SuiteVersion:        m.SuiteVersion,
		GeneratedAt:         generatedAt.UTC().Format(time.RFC3339),
		Reviewers:           benchmark.Score(catScores),
		OutOfVocabularyRate: benchmark.OutOfVocabularyRate(catScores),
		SuiteCaseIDs:        caseIDs,
		Coverage:            coverage,
		Vocabulary:          benchmark.PerReviewerVocabulary(catScores),
		PositionalRecall:    benchmark.ScorePositional(posScores),
	}, nil
}

// repoStateAcc is the per-identity bookkeeping the run-result needs beyond the two
// score accumulators: which cases this identity actually scored (the coverage
// denominator), how each one turned out, and the per-case latencies a median is
// taken over. It mirrors reviewerAcc's role on the standard path.
type repoStateAcc struct {
	// scored maps a case id to the AGENT that scored it, so the duplicate-case
	// diagnostic can name both colliding lanes rather than just reporting a count.
	scored        map[string]string
	caseIDs       []string
	outcomes      map[string]int
	fallbackCases int
	latencies     []int64
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

// readCaseFindingsLocated reads one case's pool findings and returns BOTH
// projections the two metrics need: `located` carries file+line+category for
// positional matching, and `categorical` carries the bare category list the
// category-recall scorer and the out-of-vocabulary rate are defined over.
//
// Two projections rather than one, because they must treat unparseable ("skipped")
// rows DIFFERENTLY:
//
//   - `categorical` folds each skipped row in with an EMPTY category, exactly as
//     readCaseFindings does. Dropping them would shrink the out-of-vocabulary
//     denominator, so the reviewer producing the worst-formed output would earn the
//     best drift rate — the metric would reward the behaviour it exists to detect.
//   - `located` omits them. A skipped row has no recoverable file or line, so it can
//     match no expectation; carrying it as a permanently unmatchable entry would
//     change no outcome while inviting a reader to think it might.
//
// Returning both from ONE read is what keeps that asymmetry deliberate. Deriving
// the category list from the located slice — which is what this function used to
// invite — silently gave the positional rule's drop to a metric that must not have
// it.
func readCaseFindingsLocated(reviewDir string) (located map[string][]benchmark.ReportedFinding, categorical map[string][]string, err error) {
	located = map[string][]benchmark.ReportedFinding{}
	categorical = map[string][]string{}

	path := filepath.Join(reviewDir, "sources", "pool", "findings.txt")
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return located, categorical, nil
		}
		return nil, nil, rerr
	}
	parsed, perr := stream.ParseSource(data)
	if perr != nil {
		return nil, nil, perr
	}
	for _, f := range parsed.Findings {
		located[f.Reviewer] = append(located[f.Reviewer], benchmark.ReportedFinding{
			File:     f.File,
			Line:     f.Line,
			Category: f.Category,
		})
		categorical[f.Reviewer] = append(categorical[f.Reviewer], f.Category)
	}
	// REVIEWER is the engine's last-appended column, so the final field survives an
	// overflow earlier in the row. parse() strips trailing empty fields before
	// classifying a row as skipped, so mirror that strip to land on the same one.
	for _, s := range parsed.Skipped {
		fields := strings.Split(s.Content, "|")
		for len(fields) > 1 && fields[len(fields)-1] == "" {
			fields = fields[:len(fields)-1]
		}
		reviewer := fields[len(fields)-1]
		categorical[reviewer] = append(categorical[reviewer], "")
	}
	return located, categorical, nil
}
