package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// thoroughVotes is the skeptic count --thorough forces, overriding the registry
// verify.votes default: three skeptics with the strict-majority rule
// aggregateVerdicts applies (Epic 3.0 / AC 04-01 Scenario 3).
const thoroughVotes = 3

// logPipelineWarning emits a pipeline-level warning (prior load failures, key
// mismatches) through the context logger, so verify-stage diagnostics share the
// single internal/log sink and honor LOG_LEVEL/--log-format instead of writing
// raw to os.Stderr. The class goes to Warn (visible at the default level); the
// detail can carry path-bearing context (e.g. a finding's File:Line) and is held
// to Debug so it does not leak at the default level — mirroring logSkepticFailure's
// path-at-debug discipline. Newlines in detail are flattened so a crafted value
// cannot forge an extra log line.
func logPipelineWarning(logger *slog.Logger, class, detail string) {
	detail = strings.ReplaceAll(detail, "\n", " ")
	logger.Warn("pipeline warning", "class", class)
	logger.Debug("pipeline warning detail", "class", class, "detail", detail)
}

// Options are the verify-stage run controls, set from CLI flags or MCP args.
// MinSeverity is the floor below which a finding keeps its v1 confidence and is
// never sent to a skeptic; "" means "use the registry default" (MEDIUM). Fresh
// re-verifies findings that already carry a verdict; Thorough raises the vote
// count to three with majority rule.
type Options struct {
	Fresh       bool
	Thorough    bool
	MinSeverity string
}

// Result is the verify-stage outcome the CLI and MCP render. VerdictCounts is
// the tally written to verification.json/summary.json; FindingsProcessed is the
// number of findings sent through a live skeptic this run — jobs with at least one
// eligible skeptic AND a live dispatcher (skipped/below-floor/no-eligible-skeptic/
// harness-failed findings are excluded); DurationMs is the wall-clock cost.
type Result struct {
	VerdictCounts     VerdictCounts
	FindingsProcessed int
	DurationMs        int
}

// ErrNoReconciledFindings is returned when reviewDir has no reconciled
// findings.json — the caller renders the "run 'atcr reconcile' first" guidance.
// It wraps os.ErrNotExist so errors.Is keeps working for callers that probe it.
var ErrNoReconciledFindings = errors.New("no reconciled findings")

// Verify runs the adversarial verification stage over a review's reconciled
// findings: it selects an eligible skeptic per finding (different model than any
// crediting reviewer), drives each through the Epic 2.0 tool loop to try to
// refute the finding, aggregates the verdicts, recomputes confidence (confidence
// v2), and re-emits all four artifacts (verification.json, findings.json with
// verification blocks, manifest stages, summary verdictCounts).
//
// It is the single orchestrator shared by `atcr verify`, `atcr review --verify`,
// and the atcr_verify MCP tool, so all three produce identical artifacts for the
// same input (AC 04-03/04-04). repoRoot is the git repo the skeptics' read-only
// snapshot is taken from; reviewDir is the review whose reconciled/ tree is read
// and re-emitted.
//
// Failure isolation holds end to end: a skeptic that errors, times out, or trips
// a budget yields an "unverifiable" verdict, never a dropped finding or a failed
// run. The only errors returned are setup failures (missing reconciled findings,
// unreadable artifacts) before any verdict could be recorded.
//
// Findings are processed concurrently via a bounded worker pool
// (reg.Verify.MaxParallel, default 4). Each finding's skeptics run sequentially
// within their goroutine. The pool bounds peak provider concurrency for cost control.
func Verify(ctx context.Context, repoRoot, reviewDir string, reg *registry.Registry, opts Options) (Result, error) {
	// Production harness: a real chat client plus a read-only snapshot dispatcher
	// of repoRoot at the review's head SHA. Built lazily (only when a skeptic will
	// run) so a no-work or no-eligible-skeptic verify does no git/provider I/O.
	harness := func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		disp, cleanup, err := buildDispatcher(repoRoot, reviewDir)
		if err != nil {
			return nil, nil, nil, err
		}
		return llmclient.New(), disp, cleanup, nil
	}
	return runVerify(ctx, reviewDir, reg, opts, harness)
}

// harnessFunc lazily builds the tool harness (chat completer + dispatcher) a
// skeptic needs. It is the seam that lets tests drive the verify pipeline with a
// scripted completer and a fake dispatcher instead of a real provider + git
// snapshot; production wires it to llmclient.New() + buildDispatcher in Verify.
type harnessFunc func() (fanout.ChatCompleter, Dispatcher, func(), error)

func runVerify(ctx context.Context, reviewDir string, reg *registry.Registry, opts Options, newHarness harnessFunc) (Result, error) {
	start := time.Now()

	findings, err := reconcile.ReadReconciledFindings(reviewDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Result{}, fmt.Errorf("%w: %s", ErrNoReconciledFindings, reviewDir)
		}
		return Result{}, err
	}

	minSev := opts.MinSeverity
	if minSev == "" {
		minSev = reg.Verify.MinSeverity
	}
	if minSev == "" {
		minSev = registry.DefaultVerifyMinSeverity
	}
	votes := reg.Verify.Votes
	if votes <= 0 {
		votes = registry.DefaultVerifyVotes
	}
	if opts.Thorough {
		votes = thoroughVotes
	}

	// Plan the work first: which findings need a skeptic, and who is eligible.
	// Doing this before any provider/snapshot setup means a run with nothing to
	// verify (all skipped or below floor) does no I/O against git or a provider.
	type job struct {
		finding  reconcile.JSONFinding
		key      FindingKey
		skeptics []Skeptic
	}
	var jobs []job
	needTool := false
	for _, f := range findings {
		if !opts.Fresh && hasTrustedVerdict(f.Verification) {
			continue // skip-already-verified (AC 04-05); --fresh forces re-verification
		}
		if !meetsSeverityFloor(f.Severity, minSev) {
			continue // below floor: keep v1 confidence, no skeptic (cost control)
		}
		sk := SelectEligibleSkeptics(reg, f, votes)
		if len(sk) > 0 {
			needTool = true
		}
		jobs = append(jobs, job{finding: f, key: FindingKey{File: f.File, Line: f.Line, Problem: f.Problem}, skeptics: sk})
	}

	// Build the tool harness (snapshot → jail → dispatcher) only when at least
	// one finding has an eligible skeptic. A snapshot failure is non-fatal: the
	// affected findings degrade to unverifiable rather than failing the run.
	var disp Dispatcher
	var cc fanout.ChatCompleter
	if needTool {
		c, d, cleanup, herr := newHarness()
		if herr != nil {
			log.FromContext(ctx).Warn("tool harness unavailable; skeptics degrade to unverifiable")
		} else {
			cc = c
			disp = d
			if cleanup != nil {
				defer cleanup()
			}
		}
	}

	type jobResult struct {
		ver *reconcile.Verification
		vr  VerificationResult
	}
	jobResults := make([]jobResult, len(jobs))

	maxPar := reg.Verify.MaxParallel
	if maxPar <= 0 {
		maxPar = 4
	}
	sem := make(chan struct{}, maxPar)
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, jb job) {
			defer wg.Done()
			defer func() { <-sem }()
			ver, vr := verifyFinding(ctx, jb.finding, jb.skeptics, cc, disp)
			jobResults[idx] = jobResult{ver: ver, vr: vr}
		}(i, j)
	}
	wg.Wait()

	verdicts := make(map[FindingKey]*reconcile.Verification, len(jobs))
	rich := make(map[FindingKey]VerificationResult, len(jobs))
	for i, j := range jobs {
		verdicts[j.key] = jobResults[i].ver
		rich[j.key] = jobResults[i].vr
	}

	// Apply the VERIFIED guard then mutate findings in-memory: update verdicts and
	// recompute confidence. Skipped and below-floor findings keep their existing
	// on-disk blocks because they are not in the verdicts map.
	if err := checkVERIFIEDGuard(findings, verdicts); err != nil {
		return Result{}, err
	}
	for i := range findings {
		key := FindingKey{File: findings[i].File, Line: findings[i].Line, Problem: findings[i].Problem}
		v, ok := verdicts[key]
		if !ok || v == nil {
			continue
		}
		findings[i].Verification = v
		findings[i].Confidence = confidenceV2(findings[i].Confidence, v.Verdict)
	}

	// Build the complete verification.json from in-memory findings (no disk
	// round-trip) so a no-op re-run reproduces the same snapshot rather than an
	// empty file: every finding that carries a verdict is recorded, using this
	// run's rich record when available and, for a finding skipped this run,
	// rebuilding from the on-disk findings.json block enriched with the prior
	// run's audit metadata (TD-007: a verdict whose key matched no finding is
	// surfaced below rather than silently dropped).
	// Load the prior verification.json LAZILY: only when at least one finding is
	// skipped this run (already verified, no --fresh). A skipped finding carries
	// its rich audit metadata (Model/DurationMs/TrippedBudgets) forward from the
	// prior instead of being re-synthesized as a lossy compact record — the
	// findings.json block lacks those fields (AC4). A missing or unreadable prior
	// degrades to no carry-forward rather than failing the run. Loading eagerly
	// caused a spurious stderr warning when the prior was corrupt but no finding
	// was skipped (the prior data was never consulted).
	var priorByKey map[FindingKey]VerificationResult
	var priorLoaded, priorLoadFailed bool
	// loadPrior builds an in-memory map from verification.json. A streaming decoder
	// is not warranted at current scale: one record per finding, < 1 MB for any
	// realistic review run (10–500 findings). The closure is lazy — only fires when
	// at least one finding is skipped — so the memory cost is deferred and bounded.
	loadPrior := func() map[FindingKey]VerificationResult {
		if priorLoaded {
			return priorByKey
		}
		priorLoaded = true
		prior, perr := ReadVerificationResults(reviewDir)
		if perr != nil {
			priorLoadFailed = true
			logPipelineWarning(log.FromContext(ctx), "prior_unreadable", fmt.Sprintf("skip-path metadata not carried forward: %v", perr))
			return nil
		}
		priorByKey = make(map[FindingKey]VerificationResult, len(prior))
		for _, r := range prior {
			priorByKey[FindingKey{File: r.File, Line: r.Line, Problem: r.Problem}] = r
		}
		return priorByKey
	}

	matched := make(map[FindingKey]bool, len(rich))
	results := make([]VerificationResult, 0, len(findings))
	for _, f := range findings {
		if f.Verification == nil {
			continue
		}
		key := FindingKey{File: f.File, Line: f.Line, Problem: f.Problem}
		if r, ok := rich[key]; ok {
			matched[key] = true
			results = append(results, r)
			continue
		}
		// Skipped this run: keep the authoritative verdict/skeptic/reasoning from the
		// findings.json block (AC4).
		rec := VerificationResult{
			File: f.File, Line: f.Line, Problem: f.Problem,
			Verdict:   f.Verification.Verdict,
			Skeptic:   f.Verification.Skeptic,
			Reasoning: f.Verification.Notes,
		}
		// Carry Model/DurationMs/TrippedBudgets forward from the prior
		// verification.json — but only when the prior verdict still matches the
		// current block. A stale or hand-edited prior with a different verdict must
		// not lend its audit metadata to a now-different outcome (zero values left
		// if no prior, or a mismatched one, exists). The EqualFold comparison is
		// intentional: parseVerdict normalizes verdicts to lowercase on write, so
		// EqualFold is harmless for the normal path and protective for hand-edited
		// verification.json files where a human might write "Confirmed" or "CONFIRMED".
		pk := loadPrior()
		var prior VerificationResult
		if pk != nil {
			prior = pk[key]
		}
		if !priorLoadFailed && strings.EqualFold(strings.TrimSpace(prior.Verdict), strings.TrimSpace(f.Verification.Verdict)) {
			rec.Model = prior.Model
			rec.DurationMs = prior.DurationMs
			rec.TrippedBudgets = prior.TrippedBudgets
		}
		// Coerce nil TrippedBudgets to empty slice to avoid null in JSON output.
		if rec.TrippedBudgets == nil {
			rec.TrippedBudgets = []string{}
		}
		results = append(results, rec)
	}
	// TD-007: a verdict computed this run whose key matched no re-emitted finding
	// indicates a key-construction mismatch; surface it on stderr rather than
	// dropping it silently. Keys are built from the same findings slice, so this
	// should never fire — but a future merge-text change would make it visible.
	for key := range rich {
		if !matched[key] {
			logPipelineWarning(log.FromContext(ctx), "orphan_verdict", fmt.Sprintf("%s:%d matched no finding (dropped)", key.File, key.Line))
		}
	}

	// Compute all 4 artifact byte-slices then flush in a single atomic group to
	// minimise the partial-write window (atomicwrite.WriteGroup stages every file
	// to a temp before the first rename, then renames them in sequence).
	counts := CountVerdicts(results)

	findingsPath, findingsData, err := computeFindingsBytes(findings, reviewDir)
	if err != nil {
		return Result{}, err
	}
	verPath, verData, err := computeVerificationBytes(reviewDir, results, counts)
	if err != nil {
		return Result{}, err
	}
	mfPath, mfData, mfNoOp, err := computeManifestStageBytes(reviewDir)
	if err != nil {
		return Result{}, err
	}
	sumPath, sumData, err := computeSummaryVerdictsBytes(reviewDir, counts)
	if err != nil {
		return Result{}, err
	}

	artifacts := []atomicwrite.Entry{
		{Path: findingsPath, Data: findingsData},
		{Path: verPath, Data: verData},
		{Path: sumPath, Data: sumData},
	}
	if !mfNoOp {
		artifacts = append(artifacts, atomicwrite.Entry{Path: mfPath, Data: mfData})
	}
	// Idempotency (Epic 4.7 AC5): this flush overwrites verification.json AND
	// re-writes the reconcile-owned findings.json/summary.json as one atomic group.
	// By deliberate design the verify stage snapshots only its own verification.json
	// to verification.json.bak before the flush; findings.json/summary.json are
	// reconcile-owned and are overwritten in place here with no verify-stage backup
	// (their prior state is captured in reconciled.bak/ only by RunReconcile, never
	// here). A first-ever verify has no prior verification.json and this is a no-op.
	if err := backupExistingVerification(reviewDir); err != nil {
		return Result{}, err
	}
	if err := atomicwrite.WriteGroup(artifacts); err != nil {
		return Result{}, err
	}

	processed := 0
	for _, j := range jobs {
		if len(j.skeptics) > 0 && disp != nil {
			processed++
		}
	}
	return Result{
		VerdictCounts:     counts,
		FindingsProcessed: processed,
		DurationMs:        int(time.Since(start).Milliseconds()),
	}, nil
}

// verifyFinding produces the verdict (the compact block for findings.json) and
// the rich record (for verification.json) for one finding. With no eligible
// skeptic it records unverifiable/no_eligible_skeptic; with the tool harness
// unavailable it records unverifiable/tool_harness_unavailable; otherwise it
// drives every selected skeptic through invokeSkeptic and aggregates the votes.
// It never returns an error — failure isolation is the stage's contract.
func verifyFinding(ctx context.Context, f reconcile.JSONFinding, skeptics []Skeptic, cc fanout.ChatCompleter, disp Dispatcher) (*reconcile.Verification, VerificationResult) {
	base := VerificationResult{File: f.File, Line: f.Line, Problem: f.Problem}
	// Initialize TrippedBudgets to empty slice to avoid null in JSON output.
	base.TrippedBudgets = []string{}

	if len(skeptics) == 0 {
		v := &reconcile.Verification{Verdict: verdictUnverifiable, Notes: "no_eligible_skeptic"}
		base.Verdict, base.Reasoning = v.Verdict, v.Notes
		return v, base
	}
	if disp == nil {
		v := &reconcile.Verification{Verdict: verdictUnverifiable, Notes: "tool_harness_unavailable"}
		base.Verdict, base.Reasoning = v.Verdict, v.Notes
		return v, base
	}

	prompt := buildSkepticPrompt(f, nil) // nil entries: the skeptic reads code via the tool loop
	start := time.Now()
	perSkeptic := make([]*reconcile.Verification, 0, len(skeptics))
	perTripped := make([][]string, 0, len(skeptics))
	for _, sk := range skeptics {
		v, tripped, ierr := invokeSkeptic(ctx, sk, prompt, cc, disp)
		if ierr != nil {
			// invokeSkeptic errors only on programming faults (nil ctx/cc/disp);
			// none can occur here, but log it so the impossible case is visible if it
			// ever fires, then synthesize an unverifiable verdict rather than dropping
			// the finding.
			logSkepticFailure(log.FromContext(ctx), sk.Name, "programming_fault", ierr.Error())
			v = &reconcile.Verification{Verdict: verdictUnverifiable, Notes: ierr.Error(), Skeptic: sk.Name}
			tripped = []string{}
		}
		perSkeptic = append(perSkeptic, v)
		perTripped = append(perTripped, tripped)
	}
	ver := aggregateVerdicts(perSkeptic)

	base.Verdict = ver.Verdict
	base.Skeptic = ver.Skeptic
	base.Reasoning = ver.Notes
	// Attribute Model/TrippedBudgets to the skeptics that produced the recorded
	// verdict: on a decisive vote, only the winners; on a tie (unverifiable), every
	// participant — never a blind skeptics[0..n] (see winningAttribution) (AC2/AC1).
	base.Model, base.TrippedBudgets = winningAttribution(skeptics, perSkeptic, perTripped, ver.Verdict)
	if base.TrippedBudgets == nil {
		base.TrippedBudgets = []string{}
	}
	base.DurationMs = int(time.Since(start).Milliseconds())
	return ver, base
}

// winningAttribution derives the audit Model and TrippedBudgets for a finding
// from the skeptics that produced the recorded verdict — mirroring how
// joinSkeptics names contributors, but resolving who "won".
//
// perSkeptic[i] and perTripped[i] are the verdict and tripped-budget slice of
// skeptics[i] (verifyFinding builds the three slices together, so indices align).
// Models are deduplicated and joined in selection order. Tripped budgets attach
// only to unverifiable verdicts (a budget trip collapses a skeptic to
// unverifiable), so a confirmed/refuted winner contributes none.
//
// Attribution depends on whether `winner` was decisive or a tie sentinel:
//   - Decisive (a strict plurality, e.g. 2 confirmed > 1 refuted → confirmed):
//     record only the skeptics whose verdict equals `winner` (AC2) — the losers'
//     models are dropped.
//   - Tie (aggregateVerdicts emits unverifiable with Skeptic naming every voter,
//     e.g. 1 confirmed + 1 refuted, or 1+1+1): no skeptic "won", so record every
//     participant's model so Model stays consistent with Skeptic rather than
//     misattributing the outcome to the lone unverifiable voter.
func winningAttribution(skeptics []Skeptic, perSkeptic []*reconcile.Verification, perTripped [][]string, winner string) (string, []string) {
	// A verdict is decisive when no other verdict matches or exceeds its count;
	// equality anywhere means aggregateVerdicts resolved a tie to unverifiable.
	// Precondition: every perSkeptic verdict is already canonical — parseVerdict
	// maps unknown verdicts to unverifiable and the error/early exits hardcode
	// canonical verdicts — so counting them directly stays consistent with
	// aggregateVerdicts' canonical-only fold; no separate filter is needed here.
	counts := map[string]int{}
	for _, v := range perSkeptic {
		if v != nil {
			counts[v.Verdict]++
		}
	}
	decisive := true
	for verdict, n := range counts {
		if verdict != winner && n >= counts[winner] {
			decisive = false
			break
		}
	}

	var models, budgets []string
	seenModel := map[string]bool{}
	seenBudget := map[string]bool{}
	for i, v := range perSkeptic {
		if v == nil {
			continue
		}
		if i >= len(skeptics) || i >= len(perTripped) {
			continue
		}
		if decisive && v.Verdict != winner {
			continue // decisive vote: skip the losers
		}
		if m := skeptics[i].Config.Model; m != "" && !seenModel[m] {
			seenModel[m] = true
			models = append(models, m)
		}
		for _, b := range perTripped[i] {
			if !seenBudget[b] {
				seenBudget[b] = true
				budgets = append(budgets, b)
			}
		}
	}
	// Coerce nil budgets to empty slice to avoid null in JSON output.
	if budgets == nil {
		budgets = []string{}
	}
	return strings.Join(models, ", "), budgets
}

// hasTrustedVerdict reports whether a finding's existing verification block is a
// cached result safe to skip (AC 04-05). Only the three canonical verdicts are
// trusted; nil, empty, or an unknown verdict is treated as unverified so the
// finding is re-processed rather than trusting a corrupt block.
func hasTrustedVerdict(v *reconcile.Verification) bool {
	if v == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v.Verdict)) {
	case verdictConfirmed, verdictRefuted, verdictUnverifiable:
		return true
	default:
		return false
	}
}

// buildDispatcher reconstructs the read-only tool harness the skeptics use to
// inspect the code, mirroring fanout.ExecuteReview: a snapshot of repoRoot at the
// review's head SHA (read from the manifest), a path jail rooted at it, and a
// dispatcher with the default limits. The returned cleanup removes the snapshot;
// the caller defers it. Any failure (missing/headless manifest, snapshot or jail
// error) is returned so the caller can degrade affected findings to unverifiable.
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

// readManifestHead reads reviewDir/manifest.json and returns its head SHA. A
// missing or malformed manifest is an error (the snapshot cannot be taken).
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
