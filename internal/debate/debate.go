package debate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/atomicwrite"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/tools"
)

// ErrNoReconciledFindings is returned when reviewDir has no reconciled
// findings.json — the caller renders "run 'atcr reconcile' first" guidance. It
// wraps os.ErrNotExist so errors.Is keeps working.
var ErrNoReconciledFindings = errors.New("no reconciled findings")

// Options are the debate-stage run controls, set from CLI flags or MCP args.
// SingleModel forces the same-model persona fallback on (the --single-model flag /
// allow_single_model opt-in) for this run regardless of the registry default.
type Options struct {
	SingleModel bool
}

// Result is the debate-stage outcome the CLI and MCP render.
type Result struct {
	Selected   int
	Upheld     int
	Overturned int
	Split      int
	Unresolved int
	Overflow   int
	DurationMs int
}

// Debate runs the cross-examination stage over a review's reconciled findings: it
// rebuilds the disagreement radar (so it includes post-verify verification ties),
// selects the disputed items that match the enabled triggers under the cost cap,
// casts proposer/challenger/judge with the distinct-model rule, drives the bounded
// three-turn debate per item through the Epic 2.0 tool loop, and integrates the
// judge rulings: it re-emits findings.json with the settled verdicts/severities,
// writes reconciled/debate.json, records "debate" in the manifest stages, and
// writes per-item transcripts under debate/. It deliberately does NOT re-emit the
// verify-stage snapshots summary.json (verdictCounts) or verification.json: after
// debate, findings.json together with debate.json is the authoritative record of
// settled verdicts/severities, while those two snapshots remain as-of-verify audit
// artifacts that may legitimately lag findings.json (see the artifacts group below).
//
// It is the single orchestrator shared by `atcr debate`, `atcr review
// --verify --debate`, and the atcr_debate MCP tool. repoRoot is the git repo the
// seats' read-only snapshot is taken from; reviewDir is the review whose
// reconciled/ tree is read and re-emitted.
//
// Failure isolation holds end to end: a seat that errors, times out, or trips a
// budget yields an unresolved item, never a dropped finding or a failed run. The
// only errors returned are setup failures (missing reconciled findings, unreadable
// artifacts).
func Debate(ctx context.Context, repoRoot, reviewDir string, reg *registry.Registry, opts Options) (Result, error) {
	harness := func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		disp, cleanup, err := buildDispatcher(repoRoot, reviewDir)
		if err != nil {
			return nil, nil, nil, err
		}
		return llmclient.New(), disp, cleanup, nil
	}
	return runDebate(ctx, reviewDir, reg, opts, harness)
}

// harnessFunc lazily builds the tool harness (chat completer + dispatcher) the
// seats need. It is the seam that lets tests drive the pipeline with a scripted
// completer and a fake dispatcher; production wires it to llmclient.New() +
// buildDispatcher in Debate.
type harnessFunc func() (fanout.ChatCompleter, Dispatcher, func(), error)

func runDebate(ctx context.Context, reviewDir string, reg *registry.Registry, opts Options, newHarness harnessFunc) (Result, error) {
	start := time.Now()
	cfg := ResolveConfig(reg.Debate)
	if opts.SingleModel {
		cfg.AllowSingleModel = true
	}

	findings, err := reconcile.ReadReconciledFindings(reviewDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Result{}, fmt.Errorf("%w: %s", ErrNoReconciledFindings, reviewDir)
		}
		return Result{}, err
	}

	// Deduplicate findings on the {File,Line,Problem} triple before the radar is
	// built. The reconciler does not guarantee uniqueness of this triple, and a
	// ruling keyed on it would otherwise mutate every matching finding (and
	// idempotency filtering would drop items that were never debated).
	findings = deduplicateFindings(findings)

	// Rebuild the radar from the current findings so the selection includes
	// post-verify verification_disagreement items (absent from the reconcile-time
	// disagreements.json snapshot) alongside severity splits and gray-zone clusters.
	df := reconcile.LoadDisagreements(reviewDir, findings)
	// Gray-zone cluster decisions (Epic 6.1) apply inline: load the clusters so a
	// judge "merge" ruling can union the member findings in findings.json directly
	// (Option A), and drop clusters a prior debate already merged so a re-run is a
	// no-op (AC4). A missing or empty ambiguous.json degrades to no gray-zone work;
	// a present-but-unparseable file is logged so merge rulings are not silently
	// dropped.
	grayClusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
	if err != nil {
		log.FromContext(ctx).Warn("debate: ambiguous.json unreadable; gray-zone merges disabled", "err", err.Error())
	}
	// Avoid allocating an empty cluster index when there are no gray-zone clusters;
	// filterMergedClusters safely handles a nil map as "no clusters to match".
	var clusterIdx map[FindingKey]reconcile.AmbiguousCluster
	if len(grayClusters) > 0 {
		clusterIdx = indexClusters(grayClusters)
	}
	// Idempotency (AC4): drop gray-zone items whose cluster a prior debate already
	// merged inline, so a re-run never re-debates or re-merges an applied cluster.
	df.Items = filterMergedClusters(df.Items, findings, clusterIdx)
	// Idempotency: drop findings a prior debate already settled (upheld/split mark
	// ChallengeSurvived). An upheld severity-split keeps its Disagreement annotation,
	// so without this guard it re-enters the radar and a re-run re-bills it at three
	// provider calls. Overturned findings are already excluded (refuted) by the radar;
	// unresolved items are intentionally retried (roles may have been configured since).
	df.Items = filterAlreadyDebated(df.Items, findings)
	sel := SelectItems(df, cfg)

	// Build the harness only when there is work (mirrors verify): a run with
	// nothing to debate does no git/provider I/O. A harness failure is non-fatal —
	// the seats degrade and the affected items are recorded unresolved.
	var cc fanout.ChatCompleter
	var disp Dispatcher
	harnessFailed := false
	if len(sel.Selected) > 0 {
		c, d, cleanup, herr := newHarness()
		if herr != nil {
			log.FromContext(ctx).Warn("debate: tool harness unavailable; selected items will be unresolved", "err", herr.Error())
			harnessFailed = true
		} else {
			cc, disp = c, d
			if cleanup != nil {
				defer cleanup()
			}
		}
	}

	debateDir := filepath.Join(reviewDir, debateSubdir)
	items := make([]ItemResult, 0, len(sel.Selected))
	// rulings keys on {File, Line, Problem}. This is safe because
	// deduplicateFindings (called near the top of this function) already
	// guarantees that the {File, Line, Problem} triple is unique across the
	// findings slice before any debating happens.
	rulings := map[FindingKey]ruleApply{}
	var res Result

	// Debate items through a bounded worker pool (mirrors verify's sem/maxPar):
	// items run concurrently up to cfg.MaxParallel, while each item's three-turn
	// debate stays sequential inside debateOne. Per-item outcomes land in an
	// index-aligned slice and are merged in selection order after the pool drains,
	// so debate.json item order, the rulings map, and the Result tally stay
	// deterministic and race-free without a lock (the merge is single-threaded).
	maxPar := cfg.MaxParallel
	if maxPar <= 0 {
		maxPar = 4
	}
	type itemOutcome struct {
		ir           ItemResult
		apply        bool
		key          FindingKey
		rule         ruleApply
		clusterMerge bool
		cluster      reconcile.AmbiguousCluster
	}
	outcomes := make([]itemOutcome, len(sel.Selected))
	sem := make(chan struct{}, maxPar)
	var wg sync.WaitGroup
	for i, it := range sel.Selected {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, it reconcile.DisagreementItem) {
			defer wg.Done()
			defer func() { <-sem }()
			var ir ItemResult
			switch {
			case harnessFailed:
				ir = ItemResult{File: it.File, Line: it.Line, Kind: it.Kind, Problem: it.Problem, OriginalSeverity: it.Severity, Outcome: OutcomeUnresolved, Reason: "harness_unavailable"}
			case ctx.Err() != nil:
				ir = ItemResult{File: it.File, Line: it.Line, Kind: it.Kind, Problem: it.Problem, OriginalSeverity: it.Severity, Outcome: OutcomeUnresolved, Reason: "context_cancelled"}
			default:
				ir = debateOne(ctx, debateDir, it, cfg, reg, cc, disp)
			}
			oc := itemOutcome{ir: ir}
			switch {
			case ir.Outcome == OutcomeUnresolved:
				// An unresolved item settles nothing — no application either way.
			case it.Kind == reconcile.KindGrayZone:
				// Epic 6.1: a gray-zone ruling is a cluster-level decision, not a
				// per-finding verdict, so it never enters the single-finding rulings
				// map. A "merge" unions the cluster's members in findings.json inline
				// (Option A); "separate" leaves them unmerged. The cluster is captured
				// here and applied after the pool drains.
				switch ir.ClusterDecision {
				case ClusterMerge:
					if c, ok := clusterIdx[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}]; ok {
						oc.clusterMerge = true
						oc.cluster = c
					}
				case ClusterSeparate:
					// Intentionally separate — no application beyond debate.json.
				default:
					// Empty or unparseable cluster decision on a real gray-zone item:
					// record a distinct reason so the no-decision case is auditable
					// rather than silently treated as separate.
					oc.ir.Reason = "no_cluster_decision"
				}
			default:
				oc.apply = true
				oc.key = FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}
				oc.rule = ruleApply{
					verdict:   ruleVerdict(ir),
					survived:  ir.ChallengeSurvived,
					severity:  splitSeverity(ir),
					judge:     ir.Judge,
					reasoning: ir.Reasoning,
				}
			}
			outcomes[idx] = oc
		}(i, it)
	}
	wg.Wait()

	var mergeClusters []reconcile.AmbiguousCluster
	for _, oc := range outcomes {
		items = append(items, oc.ir)
		tally(&res, oc.ir)
		if oc.apply {
			rulings[oc.key] = oc.rule
		}
		if oc.clusterMerge {
			mergeClusters = append(mergeClusters, oc.cluster)
		}
	}

	// Build all three stage artifacts in memory first, then flush them as one
	// atomic group so a mid-sequence failure cannot leave partial state
	// (e.g. findings.json updated but manifest.json or debate.json missing).
	//
	// Scope note: the atomic group is exactly debate.json + findings.json +
	// manifest.json. The verify-stage snapshots summary.json (verdictCounts) and
	// verification.json are intentionally NOT recomputed here — they are
	// point-in-time verify audit artifacts. findings.json (with debate.json) is the
	// authoritative post-debate record; any consumer needing settled verdict counts
	// must derive them from findings.json, not from the now-stale summary.json.
	debatePath, debateBytes, err := computeDebateBytes(reviewDir, DebateFile{
		SchemaVersion: DebateSchemaVersion,
		Items:         items,
		Overflow:      overflowItems(sel.Overflow),
	})
	if err != nil {
		return Result{}, err
	}

	// Defensive invariant guard (Epic 6.1): gray-zone members are classified into
	// the cluster branch above and never enter the single-finding rulings map, so
	// their locations must be disjoint from the rulings keyspace. If a future
	// radar/selection change broke that, applyRulings (below) and applyClusterMerges
	// would both mutate the same finding — surface it loudly rather than corrupting
	// findings.json silently.
	if loc := firstClusterRulingCollision(rulings, mergeClusters); loc != "" {
		log.FromContext(ctx).Warn("debate: gray-zone cluster member collides with a single-finding ruling key (Epic 6.1 invariant broken)", "location", loc)
	}
	if len(rulings) > 0 {
		applyRulings(findings, rulings)
	}
	if len(mergeClusters) > 0 {
		// Epic 6.1: union gray-zone clusters the judge ruled "merge" directly in the
		// post-verify findings.json (Option A) — never via RunReconcile, which would
		// rebuild from sources/ and erase the verify/debate verdicts above.
		var applied, skipped int
		findings, applied, skipped = applyClusterMerges(findings, mergeClusters)
		if applied < len(mergeClusters)-skipped {
			// A recorded merge ruling that could not be physically applied (its
			// members were not both present in findings.json) is otherwise silent —
			// debate.json still records the ruling, but findings.json is unchanged.
			log.FromContext(ctx).Warn("debate: some gray-zone merge rulings could not be applied to findings.json",
				"ruled", len(mergeClusters)-skipped, "applied", applied)
		}
	}
	findingsPath, findingsBytes, err := computeFindingsBytes(reviewDir, findings)
	if err != nil {
		return Result{}, err
	}

	manifestPath, manifestBytes, err := computeManifestStageBytes(reviewDir)
	if err != nil {
		return Result{}, err
	}

	artifacts := []atomicwrite.Entry{
		{Path: debatePath, Data: debateBytes},
		{Path: findingsPath, Data: findingsBytes},
	}
	if manifestBytes != nil {
		artifacts = append(artifacts, atomicwrite.Entry{Path: manifestPath, Data: manifestBytes})
	}
	if err := atomicwrite.WriteGroup(artifacts); err != nil {
		return Result{}, err
	}

	res.Selected = len(sel.Selected)
	res.Overflow = len(sel.Overflow)
	res.DurationMs = int(time.Since(start).Milliseconds())
	return res, nil
}

// filterAlreadyDebated removes radar items whose finding a prior debate already
// upheld or split (Verification.ChallengeSurvived). It keys on the same
// File+Line+Problem triple rulings are applied by, so only single-finding items
// (severity splits, verification disagreements) are filtered; gray-zone cluster
// items never carry the marker and are unaffected. Returns items unchanged when no
// finding is marked, so a first-ever debate run does no extra work.
func filterAlreadyDebated(items []reconcile.DisagreementItem, findings []reconcile.JSONFinding) []reconcile.DisagreementItem {
	debated := map[FindingKey]bool{}
	for _, f := range findings {
		if f.Verification != nil && f.Verification.ChallengeSurvived {
			debated[FindingKey{File: f.File, Line: f.Line, Problem: f.Problem}] = true
		}
	}
	if len(debated) == 0 {
		return items
	}
	out := make([]reconcile.DisagreementItem, 0, len(items))
	for _, it := range items {
		if debated[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}] {
			continue
		}
		out = append(out, it)
	}
	return out
}

// deduplicateFindings returns a copy of findings with only the first occurrence of
// each {File,Line,Problem} triple retained. This keeps rulings from silently
// mutating multiple findings that happen to share the same location and problem
// text.
func deduplicateFindings(findings []reconcile.JSONFinding) []reconcile.JSONFinding {
	seen := make(map[FindingKey]bool, len(findings))
	out := make([]reconcile.JSONFinding, 0, len(findings))
	for _, f := range findings {
		key := FindingKey{File: f.File, Line: f.Line, Problem: f.Problem}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}

// debateOne casts and runs the debate for a single item, records its transcript,
// and returns the recorded outcome. It never returns an error: an uncast item, a
// halted judge, or an unparseable ruling all degrade to an unresolved ItemResult.
func debateOne(ctx context.Context, debateDir string, item reconcile.DisagreementItem, cfg Config, reg *registry.Registry, cc fanout.ChatCompleter, disp Dispatcher) ItemResult {
	ir := ItemResult{File: item.File, Line: item.Line, Kind: item.Kind, Problem: item.Problem, OriginalSeverity: item.Severity}

	cast, ok, reason := CastRoles(reg, item, cfg)
	if !ok {
		ir.Outcome = OutcomeUnresolved
		ir.Reason = reason
		return ir
	}
	ir.SingleModel = cast.SingleModel
	ir.Proposer = cast.Proposer.Agent
	ir.Challenger = cast.Challenger.Agent
	ir.Judge = cast.Judge.Agent

	id := itemID(item)
	if err := os.MkdirAll(filepath.Join(debateDir, id), 0o755); err != nil {
		log.FromContext(ctx).Warn("debate: cannot create transcript dir", "err", err.Error())
	}
	tr := OpenTranscript(filepath.Join(debateDir, id, "transcript.jsonl"))
	defer func() { _ = tr.Close() }()
	ir.Transcript = filepath.Join(debateSubdir, id, "transcript.jsonl")

	rec := RunDebate(ctx, item, cast, cc, disp, tr)

	if judgeHalted(rec.Halted) {
		ir.Outcome = OutcomeUnresolved
		ir.Reason = "judge_halted"
		tr.RecordRuling(RulingEvent{Outcome: OutcomeUnresolved, Reasoning: "judge halted"})
		return ir
	}

	ruling := parseRuling(rec.JudgeRaw)
	tr.RecordRuling(RulingEvent{
		Outcome:         ruling.Outcome,
		SettledSeverity: ruling.SettledSeverity,
		ClusterDecision: ruling.ClusterDecision,
		Reasoning:       ruling.Reasoning,
	})

	ir.Outcome = ruling.Outcome
	ir.Reasoning = ruling.Reasoning
	ir.ClusterDecision = ruling.ClusterDecision
	ir.ChallengeSurvived = ruling.ChallengeSurvived()
	if ruling.Outcome == OutcomeUnresolved {
		ir.Reason = "unparseable_ruling"
	}
	if ruling.Outcome == OutcomeSplit {
		// A split with no settled_severity settles nothing: record no settled
		// severity rather than backfilling item.Severity. Backfilling would echo
		// the original severity as if the judge had adjusted to it (masking a
		// no-op ruling) and — should the radar ever score item.Severity above the
		// finding's own — silently bump the finding up. Empty SettledSeverity →
		// splitSeverity returns "" → applyRulings leaves findings[i].Severity as-is.
		ir.SettledSeverity = ruling.SettledSeverity
	}
	return ir
}

// ruleVerdict maps a recorded outcome to the verdict written into the finding.
func ruleVerdict(ir ItemResult) string {
	return Ruling{Outcome: ir.Outcome}.Verdict()
}

// splitSeverity returns the settled severity to write only for a split ruling
// (the value that replaces severity-max); "" for any other outcome leaves the
// finding's severity unchanged.
func splitSeverity(ir ItemResult) string {
	if ir.Outcome == OutcomeSplit {
		return ir.SettledSeverity
	}
	return ""
}

// judgeHalted reports whether the judge seat is among the halted seats — the only
// halt that invalidates the ruling (a halted proposer/challenger still leaves the
// judge a verdict to render on the available statements).
func judgeHalted(halted []string) bool {
	for _, h := range halted {
		if h == LabelJudge {
			return true
		}
	}
	return false
}

// tally accumulates per-outcome counts into the run Result.
func tally(res *Result, ir ItemResult) {
	switch ir.Outcome {
	case OutcomeUphold:
		res.Upheld++
	case OutcomeOverturn:
		res.Overturned++
	case OutcomeSplit:
		res.Split++
	default:
		res.Unresolved++
	}
}

// buildDispatcher reconstructs the read-only tool harness the seats use to inspect
// the code, mirroring verify.buildDispatcher: a snapshot of repoRoot at the
// review's head SHA (read from the manifest), a path jail rooted at it, and a
// dispatcher with the default limits. The returned cleanup removes the snapshot.
func buildDispatcher(repoRoot, reviewDir string) (Dispatcher, func(), error) {
	head, err := readManifestHead(reviewDir)
	if err != nil {
		return nil, nil, err
	}
	if head == "" {
		return nil, nil, errors.New("manifest has no head SHA")
	}
	root, cleanup, err := tools.NewSnapshotManager(repoRoot).SnapshotFor(head)
	if err != nil {
		return nil, nil, err
	}
	jail, err := tools.NewJail(root)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return tools.NewDispatcher(jail, tools.DefaultLimits()), cleanup, nil
}

// readManifestHead reads reviewDir/manifest.json and returns its head SHA.
func readManifestHead(reviewDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(reviewDir, manifestFile))
	if err != nil {
		return "", err
	}
	var m payload.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return "", fmt.Errorf("parsing manifest.json: %w", err)
	}
	return m.Head, nil
}
