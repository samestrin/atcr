package debate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	reclib "github.com/samestrin/atcr/reconcile"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/atomicwrite"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/hookobs"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/tools"
)

// maxUnresolvedAttempts is how many runs may leave one item unresolved before a
// later run withholds it instead of re-casting three seats for it. An unresolved
// item writes no Verification, so filterAlreadyDebated cannot see it and it
// re-enters the radar on every run; a seat that reasons inline blanks its
// statement on every one of them, so without a ceiling the re-debate loop never
// converges and each pass re-pays three seats plus their tool loops. Withholding
// is disclosed as overflow-style skipped work, never silent.
const maxUnresolvedAttempts = 3

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
// writes per-item transcripts under debate/. It deliberately does NOT re-emit
// summary.json (verdictCounts), and it re-emits verification.json's verdicts on
// one narrow class of record only: after debate, findings.json together with
// debate.json is the authoritative record of settled verdicts/severities, while
// those snapshots remain as-of-verify audit artifacts that may legitimately lag
// findings.json (see the artifacts group below).
//
// The exception covers TWO classes of record, not one.
//
//  1. CLEARED CAVEAT — a record whose tool_budget_bytes caveat a ruling cleared.
//     The stage clears the matching entry in verification.json and rewrites the
//     verdict field that entry describes (with debateJudge/debateReasoning naming
//     who produced it), because the caveat and the verdict describe the SAME
//     recorded outcome and that file is the copy internal/scorecard reads. The
//     isPartialWriteResidue fall-through belongs here: it finishes a drop whose
//     findings.json half already landed.
//
//  2. PRIOR-DEBATE REPAIR — a record a PREVIOUS debate already owns, identified
//     by a non-empty recordedDebateJudge, whose verdict this ruling superseded.
//     Its verdict, debateJudge and debateReasoning are rewritten and any standing
//     modelWithheldReason deleted, so the attribution names the judge the verdict
//     actually came from. This record's own caveat need not have been cleared —
//     the branch is gated on the existing judge and runs BEFORE the trippedBudgets
//     check, so a record with no tool-budget entry at all is in scope. The repair
//     has a ONE-RUN window: its candidates come from the prior debate.json, which
//     each run replaces wholesale.
//
// Every record outside those two classes is left at its as-of-verify value. See
// syncVerificationTruncation for why the correction has to land there rather than
// on findings.json, and the atomic-group scope note below for the full field list.
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
		return hookobs.Wrap(ctx, llmclient.New()), disp, cleanup, nil
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

	// Stage/run identity for audit observers (Epic 35.0). RunID is
	// outermost-wins, so this basename applies only to a standalone
	// `atcr debate`; a chained run keeps the review's id.
	ctx = hookobs.WithCall(ctx, hookobs.Call{Stage: "debate", RunID: filepath.Base(reviewDir)})
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
	var clusterIdx map[FindingKey]reclib.AmbiguousCluster
	if len(grayClusters) > 0 {
		clusterIdx = indexClusters(grayClusters)
	}
	// Idempotency (AC4): drop gray-zone items whose cluster a prior debate already
	// merged inline, so a re-run never re-debates or re-merges an applied cluster.
	df.Items = filterMergedClusters(ctx, df.Items, findings, clusterIdx)
	// Idempotency: drop findings a prior debate already settled (upheld/split mark
	// ChallengeSurvived). An upheld severity-split keeps its Disagreement annotation,
	// so without this guard it re-enters the radar and a re-run re-bills it at three
	// provider calls. Overturned findings are already excluded (refuted) by the radar;
	// unresolved items are intentionally retried (roles may have been configured since).
	df.Items = filterAlreadyDebated(df.Items, findings)
	// Idempotency, the unresolved half: an unresolved item writes no Verification,
	// so the filter above cannot see it and it re-enters the radar on every run.
	// Withhold the ones that already burned maxUnresolvedAttempts and disclose
	// them as skipped work, so a never-converging item stops re-paying three seats
	// instead of looping forever.
	attempts := priorUnresolvedAttempts(ctx, reviewDir)
	var withheld []OverflowItem
	df.Items, withheld = withholdExhausted(ctx, df.Items, attempts)
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
		cluster      reclib.AmbiguousCluster
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
				ir = ItemResult{File: it.File, Line: it.Line, Kind: it.Kind, Problem: it.Problem, OriginalSeverity: it.Severity, Outcome: OutcomeUnresolved, Reason: ReasonHarnessUnavailable}
			case ctx.Err() != nil:
				ir = ItemResult{File: it.File, Line: it.Line, Kind: it.Kind, Problem: it.Problem, OriginalSeverity: it.Severity, Outcome: OutcomeUnresolved, Reason: ReasonContextCancelled}
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
					oc.ir.Reason = ReasonNoClusterDecision
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

	var mergeClusters []reclib.AmbiguousCluster
	for _, oc := range outcomes {
		if oc.ir.Outcome == OutcomeUnresolved {
			// Carry the count forward on the record itself: this is the only place
			// an unresolved item's history is written, and the ceiling is only
			// reachable if each run adds its own attempt to the prior total. The
			// reason gate that decides whether THIS run adds one lives in
			// carryUnresolvedAttempts.
			//
			// Read here, after wg.Wait(), because this is where ir.Reason is final:
			// the per-item goroutine can still reassign it (ReasonNoClusterDecision)
			// up to the point it stores into outcomes[idx].
			oc.ir.UnresolvedAttempts = carryUnresolvedAttempts(
				attempts[FindingKey{File: oc.ir.File, Line: oc.ir.Line, Problem: oc.ir.Problem}],
				oc.ir.Reason)
		}
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
	// Scope note: the atomic group is debate.json + findings.json + manifest.json,
	// plus TWO further entries when syncVerificationTruncation has a correction to
	// publish — verification.json itself and its verification.json.debate.bak
	// pre-rewrite snapshot, which is a group entry published WITH the rewrite
	// rather than a copy taken before it.
	//
	// What that pass rewrites is narrow but is neither a single field nor a single
	// class of record. On a record whose tool_budget_bytes caveat a ruling dropped
	// it writes trippedBudgets, verdict, debateJudge and debateReasoning, and
	// deletes any standing modelWithheldReason. On a record a PRIOR debate owns
	// (non-empty recordedDebateJudge) whose verdict this ruling superseded, it
	// rewrites verdict, debateJudge and debateReasoning and deletes
	// modelWithheldReason — with no caveat of its own required, since that branch
	// precedes the trippedBudgets check. The two classes are enumerated in this
	// file's header; see it before narrowing either one.
	//
	// So verification.json's verdicts are point-in-time verify
	// audit artifacts for every OTHER record, but not for a ruled one. summary.json
	// (verdictCounts) is NOT recomputed here at all and stays point-in-time.
	// findings.json (with debate.json) is the authoritative post-debate record; any
	// consumer needing settled verdict counts must derive them from findings.json,
	// not from the now-stale summary.json.
	debatePath, debateBytes, err := computeDebateBytes(reviewDir, DebateFile{
		SchemaVersion: DebateSchemaVersion,
		Items:         items,
		Overflow:      append(overflowItems(sel.Overflow, attempts), withheld...),
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
	// The set of findings whose truncation caveat a ruling actually cleared. It is
	// NOT "every ruled finding": applyRulings force-clears Verification.Truncated on
	// every ruling it applies, so the post-apply flag cannot distinguish a caveat
	// this run dropped from one that was never there. syncVerificationTruncation
	// needs the former.
	var clearedCaveats map[FindingKey]ruleApply
	if len(rulings) > 0 {
		clearedCaveats = applyRulings(findings, rulings)
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
	// A ruling that cleared a finding's truncation caveat also invalidates the
	// matching tool_budget_bytes entry in the verify snapshot — the same fact
	// about the same verdict, and the one internal/scorecard actually reads (see
	// syncVerificationTruncation). Correcting exactly that entry is not the
	// recompute the scope note above rules out; leaving it is what let report.md
	// and survived_skeptic_rate disagree. It joins the atomic group so the two
	// artifacts can never be published out of step.
	//
	// The full rulings map travels with it for the SECOND-run case: a finding this
	// run ruled again has no caveat left to clear (run 1 cleared it), so the set
	// above is empty for it while the record still names run 1's judge for a verdict
	// run 2 replaced. That correction is gated on the record's existing debateJudge,
	// so it reaches only records a prior debate already owns.
	verPath, verBytes, err := syncVerificationTruncation(reviewDir, findings, clearedCaveats, rulings)
	if err != nil {
		return Result{}, err
	}
	if verBytes != nil {
		// Snapshot before replacing it, the way internal/verify does
		// (backupExistingVerification). This stage's rewrite is the lossier of the
		// two: it round-trips through map[string]any, so the file that comes back
		// is key-sorted rather than in the struct order verify wrote, and is no
		// longer byte-comparable with it even where no value changed. Without a
		// snapshot the pre-debate state was simply gone.
		//
		// It gets its OWN name rather than verification.json.bak. That file belongs
		// to internal/verify (backupExistingVerification), which contracts it as
		// "the generation the last verify replaced" and, via
		// atomicfs.BackupToDotBak, keeps exactly one. debate always runs after
		// verify — cli/review.go runs verify then debate, and standalone `atcr
		// debate` follows a verify too — so a debate snapshot under that name
		// overwrites the pre-verify generation with the post-verify one on every
		// run that clears a caveat, and the pre-verify state becomes unrecoverable.
		// Two stages backing up one file need two names.
		//
		// It also joins the atomic group instead of being copied ahead of it.
		// WriteGroup stages every entry before renaming any, so a publish that
		// fails after staging used to leave verification.json untouched and the
		// snapshot beside it already spent — a backup recording a generation the
		// run never replaced. As a group entry it lands only when the rewrite does.
		// Best-effort in the same spirit as the rest of the stage: an unreadable
		// current file yields no snapshot rather than a lost correction, which is
		// the same outcome the copy-based version produced.
		if prior, rerr := os.ReadFile(verPath); rerr != nil {
			// DELIBERATELY UNCOVERED, and not reachable from a test.
			// syncVerificationTruncation reads this SAME file earlier in the same
			// run (emit.go:431-434) and returns ("", nil, nil) when that read
			// fails, so an unreadable verPath leaves verBytes nil and skips this
			// whole block before the line is reached. A test that makes the path
			// unreadable up front therefore never gets here and would pin the
			// wrong line. Do not write one. (Same treatment as the other
			// unreachable failure arms in this repo: internal/sandbox/oslevel.go,
			// and the ErrEmptyRoster arm after fanout.ExecuteReview in
			// executeRepoStateBenchmarkRun, cli/benchmark_repostate.go, which is
			// likewise kept defensively and deliberately carries no test.
			// Both are named by IDENTIFIER, not by line: a convention claim a
			// reader cannot grep for invites them to add the test this comment
			// forbids, and a line-numbered one silently re-aims on any edit above
			// it. internal/fanout/reviewdir.go used to be cited here and carries
			// no such arm at all — do not restore it.)
			log.FromContext(ctx).Warn("debate: could not snapshot verification.json before rewriting it", "path", verPath, "err", rerr)
		} else {
			artifacts = append(artifacts, atomicwrite.Entry{Path: verPath + debateBakSuffix, Data: prior})
		}
		artifacts = append(artifacts, atomicwrite.Entry{Path: verPath, Data: verBytes})
	}
	if manifestBytes != nil {
		artifacts = append(artifacts, atomicwrite.Entry{Path: manifestPath, Data: manifestBytes})
	}
	if err := atomicwrite.WriteGroup(artifacts); err != nil {
		return Result{}, err
	}

	res.Selected = len(sel.Selected)
	res.Overflow = len(sel.Overflow) + len(withheld)
	res.DurationMs = int(time.Since(start).Milliseconds())
	return res, nil
}

// withholdExhausted splits items into the ones still worth debating and the ones a
// prior run already left unresolved maxUnresolvedAttempts times. The second group
// comes back as overflow records so the run DISCLOSES what it withheld: a silent
// drop would read as "nothing was disputed", which is the opposite of the truth.
//
// It runs before SelectItems so a withheld item does not consume a max_items slot
// that a debatable item could use.
func withholdExhausted(ctx context.Context, items []reconcile.DisagreementItem, attempts map[FindingKey]int) ([]reconcile.DisagreementItem, []OverflowItem) {
	if len(attempts) == 0 {
		return items, nil
	}
	kept := make([]reconcile.DisagreementItem, 0, len(items))
	var withheld []OverflowItem
	for _, it := range items {
		n := attempts[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}]
		if n < maxUnresolvedAttempts {
			kept = append(kept, it)
			continue
		}
		withheld = append(withheld, OverflowItem{
			File: it.File, Line: it.Line, Kind: it.Kind, Severity: it.Severity,
			Problem: it.Problem, Reason: OverflowAttemptsExhausted, UnresolvedAttempts: n,
		})
		log.FromContext(ctx).Warn("debate: item withheld, unresolved attempts exhausted",
			"file", it.File, "line", it.Line, "attempts", n)
	}
	return kept, withheld
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
// halted seat, or an unparseable ruling all degrade to an unresolved ItemResult.
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
		ir.Reason = ReasonJudgeHalted
		tr.RecordRuling(RulingEvent{Outcome: OutcomeUnresolved, Reasoning: "judge halted"})
		return ir
	}
	if silent := silentArguingSeats(rec); len(silent) > 0 {
		// A proposer or challenger with no statement made no case, so the judge
		// ruled on one side only. Recording that as an uphold or overturn would
		// read as a contested ruling that never happened.
		//
		// Two ways to arrive here, kept apart in the reason so an operator
		// reading debate.json is never told a seat halted when it did not: the
		// seat halted on a provider error and returned nothing, or it ran clean
		// and had nothing to say — an empty reply, or one that was entirely think
		// markup that driveSeat stripped. A seat halted by a tripped budget is
		// normally NOT in this set — it still returns its forced final answer, so its
		// statement is non-empty and it made its case — but it IS in the set when that
		// forced answer was itself entirely a leading think run, because then the strip
		// removed a non-empty statement. Suppressed is recorded for it and outranks
		// halted, the same correction TD internal/debate/protocol.go:231 made.
		// ReasonSeatHalted is the stronger claim, so it is reserved for the case
		// where EVERY silent seat really halted. A mixed pair — a halted
		// proposer plus a clean-but-blank challenger — reports the weaker
		// ReasonSeatSilent, which is true of both, rather than asserting a halt
		// that one of them did not have.
		// Three tokens now, and the precedence is "only claim what is true of
		// every silent seat THAT WAS ASKED". seat_halted and seat_suppressed are
		// both stronger claims than seat_silent, so each is reserved for a uniform
		// cause; any mixture falls back to seat_silent, which is true of all three.
		//
		// Scoped to the asked seats because RunDebate short-circuits the remaining
		// turns on a clean-blank proposer: the challenger's blank statement there
		// is "never given a turn", and counting it as a cause would make every
		// suppressed proposer read as a mixture and report seat_silent — the exact
		// collapse this token exists to undo.
		blamed := seatsAsked(rec.Asked, silent)
		ir.Outcome = OutcomeUnresolved
		ir.Reason = ReasonSeatSilent
		// Suppressed outranks halted, because the two are independent facts and the
		// strip is the one that caused the SILENCE. A budget-tripped seat whose forced
		// final answer was entirely think markup halts AND was suppressed, and
		// reporting seat_halted there named a provider/budget problem for a statement
		// the strip had removed — the exact confusion the third token exists to end,
		// on the one input class it was written for (TD internal/debate/protocol.go:231).
		// A seat that halted with genuinely empty content has no Suppressed entry, so
		// this ordering only redirects the cases where the strip really was the cause.
		switch {
		case allSeatsIn(rec.Suppressed, blamed):
			ir.Reason = ReasonSeatSuppressed
		case allSeatsIn(rec.Halted, blamed):
			ir.Reason = ReasonSeatHalted
		}
		// The single reason token cannot describe a mixed pair, so the
		// transcript note labels each seat for itself. Scoped to the SEATS ASKED,
		// the same narrowing the token above just used: on the short-circuit the
		// challenger was never invoked, and rendering "challenger silent" into
		// report.md, the transcript and the operator warn would state a cause for a
		// seat that had no turn to go silent on (TD internal/debate/debate.go:614).
		notes := seatSilenceNotes(rec.Halted, rec.Suppressed, blamed)
		tr.RecordRuling(RulingEvent{Outcome: OutcomeUnresolved, Reasoning: "no statement: " + strings.Join(notes, ", ")})
		// The token in debate.json names one cause for the whole item and goes
		// weak on a mixture; put the per-seat cause next to it and warn, so a seat
		// that blanks every item (an inline-reasoning endpoint, most often) is
		// visible to the operator instead of surfacing as a bare Unresolved count.
		// The notes carry `suppressed` per seat even when the item-level token
		// fell back to seat_silent, which is the only place a mixture's detail
		// survives.
		ir.Reasoning = "no statement: " + strings.Join(notes, ", ")
		log.FromContext(ctx).Warn("debate: silent arguing seat(s), item unresolved", "seats", strings.Join(notes, ", "))
		return ir
	}

	// The strip in driveSeat is leading-only, so a block that sits AFTER the
	// judge's answer reaches here intact. parseRuling takes the FIRST
	// outcome-keyed object it finds, and on a reply whose real answer is prose the
	// draft object inside that block is the ONLY one — so it would become the
	// debate's ruling. Refuse the reply rather than parse it: a wrong ruling is
	// durable (it writes a verdict onto the finding), while an unresolved item
	// leaves the pre-debate verdict standing and is disclosed by its own token.
	//
	// The DETECTION question, asked the way the verify and executor lanes ask it:
	// mask the JSON string values first, then ask the enclosure predicate. Masking
	// means a judge that merely QUOTES a think tag while ruling on think-handling
	// code — the likeliest input in this repo — keeps its ruling instead of being
	// refused as markup. HasThinkMarkup is doctor's detection question,
	// position-blind by design, and reusing it here made the three lanes disagree
	// about what counts as markup in a reply that quotes the tag (TD
	// internal/debate/debate.go:640). HasEnclosingThinkBlock is the enclosure
	// question this site actually asks: is there a BLOCK a discarded draft ruling
	// could sit in, so that parseRuling's first keyed object is the draft rather
	// than the answer.
	if llmclient.HasEnclosingThinkBlock(llmclient.MaskJSONStrings(rec.JudgeRaw)) {
		ir.Outcome = OutcomeUnresolved
		ir.Reason = ReasonJudgeThinkMarkup
		ir.Reasoning = "judge reply carries inline think markup; ruling refused"
		tr.RecordRuling(RulingEvent{Outcome: OutcomeUnresolved, Reasoning: ir.Reasoning})
		log.FromContext(ctx).Warn("debate: judge reply carries inline think markup, ruling refused", "judge", cast.Judge.Agent)
		return ir
	}

	// The one tag shape neither guard above acts on: a </think> no  thinking opened.
	// SplitThink leaves it in place and HasEnclosingThinkBlock does not refuse on
	// it, both deliberately, so the envelope BEFORE it can still be an abandoned
	// draft. llmclient owns the shared rule; this lane supplies the envelope
	// predicate its own parser needs. On an ambiguous pair neither envelope is
	// trusted — a wrong ruling writes a durable verdict onto the finding, while an
	// unresolved item leaves the pre-debate verdict standing (TD
	// internal/debate/debate.go:653).
	judgeText := rec.JudgeRaw
	section, text := llmclient.ClassifyUnopenedCloser(rec.JudgeRaw, carriesRuling)
	switch section {
	case llmclient.SectionAmbiguous:
		ir.Outcome = OutcomeUnresolved
		ir.Reason = ReasonJudgeThinkMarkup
		ir.Reasoning = "judge reply has a ruling envelope on both sides of a </think> no  thinking opened; neither is provably committed"
		tr.RecordRuling(RulingEvent{Outcome: OutcomeUnresolved, Reasoning: ir.Reasoning})
		log.FromContext(ctx).Warn("debate: judge reply ambiguous around an unopened think closer, ruling refused", "judge", cast.Judge.Agent)
		return ir
	case llmclient.SectionAfterCloser:
		judgeText = text
	}

	ruling := parseRuling(judgeText)
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
		// Two distinct failures share the unresolved OUTCOME, so they must not
		// share the operator-facing reason token: a reply that was ABSENT is
		// not one that was unparseable. After the think strip an absent judge
		// reply is the routine outcome on an inline-reasoning endpoint, so
		// collapsing it into unparseable_ruling hid the misconfiguration the
		// token exists to name. The finer diagnosis still travels in
		// ir.Reasoning on both branches.
		ir.Reason = ReasonUnparseableRuling
		if ruling.Reasoning == EmptyRulingReasoning {
			ir.Reason = ReasonEmptyRuling
		}
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

// silentArguingSeats returns the proposer/challenger seats that left no
// statement. A seat halted by a tripped budget still returns its forced final
// answer, which the next seats saw, so only an EMPTY statement means that side
// made no case.
//
// Keyed on the statement, not on rec.Halted: a seat that said nothing made no
// case whether or not the engine halted it. Keying on Halted alone made a
// StatusOK seat with empty content invisible here, so the judge's one-sided
// ruling was recorded as a real outcome — and, being written with
// ChallengeSurvived true, filterAlreadyDebated then skipped that finding on
// every later run, making the fake win durable. driveSeat's think strip added a
// second way to reach the same blank (a reply that was entirely think markup),
// which is how the pre-existing hole was found.
func silentArguingSeats(rec Record) []string {
	var silent []string
	if strings.TrimSpace(rec.ProposerStatement) == "" {
		silent = append(silent, LabelProposer)
	}
	if strings.TrimSpace(rec.ChallengerStatement) == "" {
		silent = append(silent, LabelChallenger)
	}
	return silent
}

// seatsAsked narrows a silent-seat list to the seats that were actually given a
// turn. A seat RunDebate never reached carries no cause — it is not evidence of
// anything, so it must not dilute a uniform one (TD internal/debate/debate.go:524).
// An empty result is unreachable from debateOne's guard: the guard fires only on a
// blank statement, and a statement can only be blank if its seat was asked or the
// proposer short-circuit fired, which leaves the proposer itself asked and blank.
func seatsAsked(asked, seats []string) []string {
	out := make([]string, 0, len(seats))
	for _, s := range seats {
		if slices.Contains(asked, s) {
			out = append(out, s)
		}
	}
	return out
}

// allSeatsIn reports whether EVERY named seat appears in cause. Each reason token
// stronger than seat_silent is keyed on all, not any: on a mixed pair, claiming
// seat_halted would be false of the seat that ran clean, and claiming
// seat_suppressed would be false of the seat that was genuinely empty. Callers
// only pass non-empty seat lists (debateOne invokes it inside its silent-seat
// guard), so an empty list is a programming error rather than a case to answer —
// matching the harness_unavailable arm's documented defensive posture is
// unnecessary here because the loop over an empty list vacuously reports true,
// which only an empty silent set can trigger.
//
// Named for the set membership rather than for one cause because debateOne now
// asks it twice, once per stronger token (TD internal/debate/debate.go:524).
func allSeatsIn(cause, seats []string) bool {
	for _, s := range seats {
		if !slices.Contains(cause, s) {
			return false
		}
	}
	return true
}

// carriesRuling reports whether text parses to a real judge ruling, as opposed to
// parseRuling's "nothing usable here" diagnostics. It is the envelope test
// classifyUnopenedCloser needs for the debate lane: only an outcome-keyed object
// counts, so a closer quoted in prose is not treated as a boundary that splits
// two envelopes.
func carriesRuling(s string) bool {
	return parseRuling(s).Outcome != OutcomeUnresolved
}

// seatSilenceNotes labels each silent seat with its own cause for the transcript,
// which the single reason token cannot do on a mixed pair. Three causes: suppressed
// (something was said and the strip removed all of it), halted (the engine failed),
// and silent (it ran clean and genuinely said nothing).
//
// suppressed outranks halted, matching the token precedence in debateOne and for
// the same reason: the two are independent, a budget-tripped seat can be both, and
// the strip is what explains the absence of a statement. Halted used to win on the
// premise that a halted turn never reached the suppression branch; that premise is
// false — recordTurnCause records both (TD internal/debate/protocol.go:231).
func seatSilenceNotes(halted, suppressed, seats []string) []string {
	notes := make([]string, 0, len(seats))
	for _, s := range seats {
		cause := "silent"
		switch {
		case slices.Contains(suppressed, s):
			cause = "suppressed"
		case slices.Contains(halted, s):
			cause = "halted"
		}
		notes = append(notes, s+" "+cause)
	}
	return notes
}

// carryUnresolvedAttempts returns the attempt total to record on an unresolved
// item: the prior total, plus THIS run's attempt only when the reason is
// evidence about the item (countsTowardWithholding).
//
// The prior total is carried forward unchanged otherwise, so an interrupted run
// neither advances the ceiling nor erases the history earlier real attempts
// earned.
//
// A named function rather than a branch inlined at the one call site, because
// the branch is only testable if it can be CALLED. While it was inline, the test
// that claimed to cover "the writer and the reader" re-implemented this
// arithmetic in its own body — so the assertion was guaranteed by the test's copy
// of the predicate, and replacing the writer's gate with an unconditional
// `prior+1` left `go test ./...` green across the whole repo. The reader's half
// (emit.go's floor) was pinned the whole time; this half was not
// (TD internal/debate/debate.go:308).
func carryUnresolvedAttempts(prior int, reason string) int {
	if countsTowardWithholding(reason) {
		return prior + 1
	}
	return prior
}

// countsTowardWithholding reports whether an unresolved item's reason is
// evidence about the ITEM, and so may consume one of its three attempts toward
// the withholding ceiling.
//
// The ceiling is permanent once reached: withholdExhausted drops the item before
// SelectItems, and priorUnresolvedAttempts floors a withheld record back up on
// every read, so the count never falls. There is no flag, no expiry, and no exit
// but hand-editing debate.json. A count that permanent may only be spent on
// evidence the item itself produced.
//
// The four reasons below are environmental — the debate never ran, for a cause
// outside the item. Counting them meant three interrupted runs permanently
// withheld every disputed item in the review, and it contradicted this stage's
// own stated contract ("unresolved items are intentionally retried — roles may
// have been configured since") for exactly the two roster reasons that contract
// names. Downstream, a withheld item can never earn a confirmed verdict, so
// under --require-verified it can never gate CI again (reconcile/gate.go).
//
// A DENY-list, deliberately, not an allow-list of the item-evidence reasons. The
// two differ only on a reason this function has never heard of, and there the
// defaults are not symmetric: counting an unknown reason over-applies a ceiling
// an operator can see and diagnose, while NOT counting it silently disables the
// ceiling and restores the unbounded re-debate loop. New reasons in this stage
// have overwhelmingly been item evidence (every token added this sprint was), so
// the deny-list is also the likelier-correct default, not merely the safer one.
func countsTowardWithholding(reason string) bool {
	switch reason {
	case ReasonContextCancelled, ReasonHarnessUnavailable, ReasonInsufficientModels, ReasonNoProposer:
		return false
	}
	return true
}

// judgeHalted reports whether the judge seat is among the halted seats. A halted
// judge yields no ruling at all; a proposer/challenger with no statement yields a
// one-sided one, which debateOne also records unresolved — reason seat_halted
// when that seat halted, seat_suppressed when the strip emptied its reply, and
// seat_silent when it ran clean and genuinely said nothing (or on a mixture).
//
// Keyed on Halted alone, with no suppression arm: a judge that replies with only
// a think block leaves JudgeRaw blank, and parseRuling already reports that as
// the distinct empty_ruling token rather than as a halt.
func judgeHalted(halted []string) bool {
	return slices.Contains(halted, LabelJudge)
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
