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
func executeBenchmarkRun(ctx context.Context, cfg *fanout.ReviewConfig, completer fanout.Completer, suitePath string, generatedAt time.Time, checkpointPath string) (*benchmark.RunResult, error) {
	m, err := benchmark.Load(suitePath)
	if err != nil {
		return nil, err
	}

	// Opt-in checkpointing (Epic 10.3): when checkpointPath is set, each scored case
	// is persisted before the next begins so a transient failure does not forfeit the
	// paid work of the cases that already completed. An empty path keeps the 10.2
	// behavior verbatim (no read, no write).
	var cp *runCheckpoint
	var done map[int]checkpointCase
	if checkpointPath != "" {
		curHash, herr := benchmark.ReproHashManifest(m, suitePath)
		if herr != nil {
			return nil, fmt.Errorf("hashing suite for checkpoint: %w", herr)
		}
		existing, lerr := loadCheckpoint(checkpointPath)
		if lerr != nil {
			return nil, lerr
		}
		roster := rosterSignature(cfg)
		if existing != nil {
			// Suite-identity guard (AC4): a checkpoint from a different or changed
			// suite is rejected, never silently mixed into this run. The roster guard
			// catches a changed reviewer panel — orthogonal to ReproHash, which hashes
			// only suite content.
			if verr := validateCheckpoint(existing, curHash, m.Suite, m.SuiteVersion); verr != nil {
				return nil, verr
			}
			if verr := validateCheckpointRoster(existing, roster); verr != nil {
				return nil, verr
			}
			cp = existing
		} else {
			cp = &runCheckpoint{ReproHash: curHash, Suite: m.Suite, SuiteVersion: m.SuiteVersion, Roster: roster}
		}
		done = cp.doneIndex()
		if existing != nil {
			replayed := len(done)
			remaining := len(m.Cases) - replayed
			fmt.Fprintf(os.Stderr, "Resuming benchmark: replayed %d case(s), %d remaining to execute\n", replayed, remaining)
		}
	}

	tmp, err := os.MkdirTemp("", "atcr-benchmark-")
	if err != nil {
		return nil, fmt.Errorf("creating benchmark work dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	accs := map[reviewerKey]*reviewerAcc{}
	var order []reviewerKey // realized identities, sorted for deterministic aggregation

	for i, c := range m.Cases {
		// Resume: a case already in the checkpoint is replayed into the accumulator
		// without re-executing (and re-paying for) it (AC2). Replaying in case-index
		// order preserves the deterministic aggregation the reproducibility contract
		// depends on (AC3).
		if entry, ok := done[i]; ok {
			// ReproHash is order-independent (it sorts cases by id), so a reordered
			// suite shares the hash but remaps indices. Guard the per-index identity
			// too: a checkpoint entry whose recorded id no longer matches the suite's
			// case at this index means the suite changed — fail closed rather than
			// replay a score against the wrong case.
			if entry.CaseID != c.ID {
				return nil, fmt.Errorf("%w: checkpoint case at index %d is %q but the suite has %q there; remove the checkpoint to start fresh",
					errCheckpointCaseMismatch, i, entry.CaseID, c.ID)
			}
			replayCheckpointCase(accs, &order, entry, c.ExpectedCategories)
			continue
		}

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
		log.FromContext(ctx).Info("benchmark case executing", "case", c.ID, "reviewers", len(cfg.Project.Agents))
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
		var caseReviewers []checkpointReviewer
		for _, a := range summary.Agents {
			model := reviewerModel(cfg, a)
			persona := reviewerPersona(cfg, a.Agent)
			raised := raisedByReviewer[a.Agent]
			// Cost + latency are usage-gated: a completer that reports no token
			// usage (the test stub) contributes neither, keeping the score
			// deterministic. status.json only records Model/tokens when usage > 0.
			usageReported := a.TokensIn > 0 || a.TokensOut > 0
			var cost float64
			var latency int64
			if usageReported {
				cost = llmclient.ComputeCostUSD(a.Model, a.TokensIn, a.TokensOut)
				latency = a.DurationMS
			}

			applyReviewerOutcome(accs, &order, model, persona, c.ExpectedCategories, raised, usageReported, cost, latency)

			if cp != nil {
				caseReviewers = append(caseReviewers, checkpointReviewer{
					Agent:         a.Agent,
					Model:         model,
					Persona:       persona,
					Raised:        raised,
					UsageReported: usageReported,
					CostUSD:       cost,
					LatencyMS:     latency,
				})
			}
		}

		// Checkpoint the scored case before the loop advances to case i+1 (AC1): the
		// atomic write means a process killed mid-suite leaves a checkpoint holding
		// exactly the cases that completed.
		if cp != nil {
			cp.Cases = append(cp.Cases, checkpointCase{Index: i, CaseID: c.ID, Expected: c.ExpectedCategories, Reviewers: caseReviewers})
			if werr := saveCheckpoint(checkpointPath, cp); werr != nil {
				return nil, fmt.Errorf("writing checkpoint for case %q: %w", c.ID, werr)
			}
		}
	}

	sortReviewerKeys(order)
	reviewers := make([]benchmark.ReviewerScore, 0, len(order))
	for _, k := range order {
		acc := accs[k]
		reviewers = append(reviewers, benchmark.ReviewerScore{
			Model:        k.model,
			Persona:      k.persona,
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
		// Computed from the same accumulated ReviewerScores Score consumes, so a
		// resumed run reports the same rate as an uninterrupted one — replayed cases
		// fold in through applyReviewerOutcome before this point. nil when the run
		// raised no findings at all, which must not read as perfect agreement.
		OutOfVocabularyRate: benchmark.OutOfVocabularyRate(reviewers),
	}, nil
}

// rosterSignature builds the deterministic "agent=model=persona" signature of the
// configured reviewer panel, sorted by agent name. It uses the CONFIGURED values
// (registry), not runtime usage-reported ones, so the same config always yields the
// same signature — the stable identity a resume compares against to reject a changed
// panel (AC4 roster guard). Persona is included because it is a behavioral modifier
// (system prompt) that can change reviewer outputs even when model stays the same.
// An agent with no configured model or persona contributes an empty component,
// which still distinguishes it from a later-configured one.
func rosterSignature(cfg *fanout.ReviewConfig) []string {
	names := append([]string(nil), cfg.Project.Agents...)
	sort.Strings(names)
	sig := make([]string, len(names))
	for i, n := range names {
		sig[i] = n + "=" + cfg.Registry.Agents[n].Model + "=" + cfg.Registry.Agents[n].Persona
	}
	return sig
}

// reviewerKey identifies one leaderboard row by the REALIZED (model, persona) pair
// — the model that actually served the case, not the lane it was configured under.
//
// It is deliberately the same identity benchmark.Score already sorts and publishes
// on (score.go:60-75), so a lane that failed over mid-suite yields one row per model
// actually used instead of crediting every case to whichever model happened to serve
// the first one. Keying by the LANE (the agent name) is what silently attributed 9 of
// Run B's 17 cases to a model that never saw them.
//
// Consequence, accepted: two lanes that realize the same model AND the same persona
// merge into a single row, where the lane-keyed accumulator emitted two rows of
// identical public identity. reviewerPersona falls back to the agent name when no
// persona is configured, so distinct lanes keep distinct personas by default.
type reviewerKey struct {
	model   string
	persona string
}

// sortReviewerKeys orders keys ascending by (model, persona) so aggregation never
// depends on map iteration — determinism is an acceptance criterion here, not a
// nicety (the reproducibility contract requires byte-identical output for identical
// input). It matches Score's own sort, so the accumulator hands Score a slice that
// is already in the order Score will keep.
func sortReviewerKeys(keys []reviewerKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].model != keys[j].model {
			return keys[i].model < keys[j].model
		}
		return keys[i].persona < keys[j].persona
	})
}

// reviewerAcc accumulates one realized identity's outcomes across every case. Model
// and persona are NOT stored here — they are the map key, so there is no second copy
// that could drift from it.
type reviewerAcc struct {
	cases     []benchmark.CaseScore
	costUSD   float64
	latencies []int64 // per-case wall-clock, recorded only when usage was reported
}

// applyReviewerOutcome folds one reviewer's single-case outcome into the
// accumulator, creating the accumulator (and registering the reviewer in order) on
// first sighting. It is the SINGLE fold path shared by fresh execution and
// checkpoint replay, so a resumed run reconstructs accs identically to an
// uninterrupted one (AC3): model/persona are locked at first sighting (matching the
// original first-case-wins behavior), and the usage-gated cost/latency are added in
// the same case order, keeping the float sum and latency median byte-identical.
func applyReviewerOutcome(accs map[reviewerKey]*reviewerAcc, order *[]reviewerKey, model, persona string, expected, raised []string, usageReported bool, costUSD float64, latencyMS int64) {
	key := reviewerKey{persona: persona}
	acc := accs[key]
	if acc == nil {
		acc = &reviewerAcc{}
		accs[key] = acc
		*order = append(*order, key)
	}
	acc.cases = append(acc.cases, benchmark.CaseScore{Expected: expected, Raised: raised})
	if usageReported {
		acc.costUSD += costUSD
		acc.latencies = append(acc.latencies, latencyMS)
	}
}

// replayCheckpointCase folds a checkpointed case's recorded per-reviewer outcomes
// back into the accumulator via the same applyReviewerOutcome path the fresh loop
// uses — no review re-execution and no Completer call (AC2). Expected categories
// are re-read from the suite manifest (passed in as expected) because they are
// identical for every reviewer of a case and are not durable per-reviewer state.
func replayCheckpointCase(accs map[reviewerKey]*reviewerAcc, order *[]reviewerKey, entry checkpointCase, expected []string) {
	for _, r := range entry.Reviewers {
		applyReviewerOutcome(accs, order, r.Model, r.Persona, expected, r.Raised, r.UsageReported, r.CostUSD, r.LatencyMS)
	}
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
	// A SKIPPED row is a finding the reviewer really emitted whose columns the
	// parser could not align (an unescaped pipe in PROBLEM is the usual cause).
	// Dropping it here would shrink the out-of-vocabulary DENOMINATOR, so the
	// reviewer producing the worst-formed output would earn the best drift rate —
	// the metric would reward exactly the behaviour it exists to detect. Fold each
	// one in with an EMPTY category, which counts as drift by the same rule that
	// already governs an empty CATEGORY column.
	//
	// REVIEWER is the engine's last-appended column, so the final field survives an
	// overflow earlier in the row. parse() strips trailing empty fields BEFORE
	// classifying a row as skipped, so that field is non-empty by construction;
	// mirror the strip here to land on the same one. An unrecognized reviewer name
	// keys a map entry no agent reads, exactly as an unrecognized REVIEWER on a
	// well-formed row already does.
	for _, s := range parsed.Skipped {
		fields := strings.Split(s.Content, "|")
		for len(fields) > 1 && fields[len(fields)-1] == "" {
			fields = fields[:len(fields)-1]
		}
		reviewer := fields[len(fields)-1]
		out[reviewer] = append(out[reviewer], "")
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

// medianInt64 returns the p50 of vs; 0 for an empty slice, so a no-usage run
// reports a deterministic 0 latency. It uses the SAME definition as
// scorecard.medianInt64 (odd: the middle element; even: floor of the two middles)
// so the shared public latency_p50_ms column is computed identically for benchmark
// and production rows on the leaderboard. lo + (hi-lo)/2 is the overflow-safe form
// of floor((lo+hi)/2).
func medianInt64(vs []int64) int64 {
	n := len(vs)
	if n == 0 {
		return 0
	}
	sorted := make([]int64, n)
	copy(sorted, vs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if n%2 == 1 {
		return sorted[n/2]
	}
	lo, hi := sorted[n/2-1], sorted[n/2]
	return lo + (hi-lo)/2
}
