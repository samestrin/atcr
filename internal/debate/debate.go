package debate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
// writes per-item transcripts under debate/.
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

	// Rebuild the radar from the current findings so the selection includes
	// post-verify verification_disagreement items (absent from the reconcile-time
	// disagreements.json snapshot) alongside severity splits and gray-zone clusters.
	df := reconcile.LoadDisagreements(reviewDir, findings)
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
	if len(sel.Selected) > 0 {
		c, d, cleanup, herr := newHarness()
		if herr != nil {
			log.FromContext(ctx).Warn("debate: tool harness unavailable; selected items will be unresolved")
		} else {
			cc, disp = c, d
			if cleanup != nil {
				defer cleanup()
			}
		}
	}

	debateDir := filepath.Join(reviewDir, debateSubdir)
	items := make([]ItemResult, 0, len(sel.Selected))
	rulings := map[FindingKey]ruleApply{}
	var res Result

	for _, it := range sel.Selected {
		if ctx.Err() != nil {
			ir := ItemResult{File: it.File, Line: it.Line, Kind: it.Kind, Problem: it.Problem, OriginalSeverity: it.Severity, Outcome: OutcomeUnresolved, Reason: "context_cancelled"}
			items = append(items, ir)
			tally(&res, ir)
			continue
		}
		ir := debateOne(ctx, debateDir, it, cfg, reg, cc, disp)
		items = append(items, ir)
		tally(&res, ir)
		// Apply verdicts only to single-finding items. A gray-zone ruling is a
		// cluster-level merge/separate decision recorded for the existing
		// adjudication path, not a per-finding verdict (see Clarifications).
		if ir.Outcome != OutcomeUnresolved && it.Kind != reconcile.KindGrayZone {
			rulings[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}] = ruleApply{
				verdict:   ruleVerdict(ir),
				survived:  ir.ChallengeSurvived,
				severity:  splitSeverity(ir),
				judge:     ir.Judge,
				reasoning: ir.Reasoning,
			}
		}
	}

	if len(rulings) > 0 {
		applyRulings(findings, rulings)
		if err := writeFindings(reviewDir, findings); err != nil {
			return Result{}, err
		}
	}
	if err := writeDebateFile(reviewDir, DebateFile{
		SchemaVersion: DebateSchemaVersion,
		Items:         items,
		Overflow:      overflowItems(sel.Overflow),
	}); err != nil {
		return Result{}, err
	}
	if err := updateManifestStage(reviewDir); err != nil {
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
		sev := ruling.SettledSeverity
		if sev == "" {
			sev = item.Severity // judge gave no severity: keep the original
		}
		ir.SettledSeverity = sev
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
