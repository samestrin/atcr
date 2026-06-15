package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/tools"
)

// thoroughVotes is the skeptic count --thorough forces, overriding the registry
// verify.votes default: three skeptics with the strict-majority rule
// aggregateVerdicts applies (Epic 3.0 / AC 04-01 Scenario 3).
const thoroughVotes = 3

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
// Findings (and the skeptics within a finding) are processed SERIALLY: unlike the
// review fan-out there is no concurrency knob, so a --thorough run is
// findings × votes provider calls back to back. This keeps the stage simple and
// deterministic; parallelizing it across a bounded worker pool is tracked as
// TD-009.
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
			fmt.Fprintln(os.Stderr, "atcr: verify: tool harness unavailable; skeptics degrade to unverifiable")
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
	// run's rich record when available and synthesizing a compact one from the
	// on-disk block for a finding that was skipped this run (TD-007: a verdict
	// whose key matched no finding is surfaced below rather than silently dropped).
	// Load the prior verification.json so a finding skipped this run (already
	// verified, no --fresh) carries its rich audit metadata (Model/DurationMs/
	// TrippedBudgets) forward instead of being re-synthesized as a lossy compact
	// record — the findings.json block lacks those fields (AC4). A missing or
	// unreadable prior degrades to no carry-forward rather than failing the run.
	priorByKey := map[FindingKey]VerificationResult{}
	if prior, perr := ReadVerificationResults(reviewDir); perr != nil {
		fmt.Fprintf(os.Stderr, "atcr: verify: prior verification.json unreadable, skip-path metadata not carried forward: %v\n", perr)
	} else {
		for _, r := range prior {
			priorByKey[FindingKey{File: r.File, Line: r.Line, Problem: r.Problem}] = r
		}
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
		// findings.json block, but carry Model/DurationMs/TrippedBudgets forward from
		// the prior verification.json record (zero values if no prior exists) (AC4).
		prior := priorByKey[key]
		results = append(results, VerificationResult{
			File: f.File, Line: f.Line, Problem: f.Problem,
			Verdict:        f.Verification.Verdict,
			Skeptic:        f.Verification.Skeptic,
			Reasoning:      f.Verification.Notes,
			Model:          prior.Model,
			DurationMs:     prior.DurationMs,
			TrippedBudgets: prior.TrippedBudgets,
		})
	}
	// TD-007: a verdict computed this run whose key matched no re-emitted finding
	// indicates a key-construction mismatch; surface it on stderr rather than
	// dropping it silently. Keys are built from the same findings slice, so this
	// should never fire — but a future merge-text change would make it visible.
	for key := range rich {
		if !matched[key] {
			fmt.Fprintf(os.Stderr, "atcr: verify: verdict for %s:%d matched no finding (dropped)\n", key.File, key.Line)
		}
	}

	// Compute all 4 artifact byte-slices then flush in a single atomic group to
	// minimise the partial-write window (writeGroupAtomic stages every file to a
	// temp before the first rename, then renames them in sequence).
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

	artifacts := []stagingEntry{
		{path: findingsPath, data: findingsData},
		{path: verPath, data: verData},
		{path: sumPath, data: sumData},
	}
	if !mfNoOp {
		artifacts = append(artifacts, stagingEntry{path: mfPath, data: mfData})
	}
	if err := writeGroupAtomic(artifacts); err != nil {
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

// stagingEntry is one artifact in a writeGroupAtomic batch.
type stagingEntry struct {
	path string
	data []byte
}

// writeGroupAtomic stages all entries to temp files then renames them in
// sequence, minimising the partial-write window. All data is flushed before the
// first rename; temps for entries that were not renamed are cleaned up on return.
func writeGroupAtomic(artifacts []stagingEntry) error {
	temps := make([]string, len(artifacts))
	renamed := make([]bool, len(artifacts))
	defer func() {
		for i, t := range temps {
			if t != "" && !renamed[i] {
				_ = os.Remove(t)
			}
		}
	}()
	for i, a := range artifacts {
		dir := filepath.Dir(a.path)
		tmp, err := os.CreateTemp(dir, "."+filepath.Base(a.path)+".tmp-*")
		if err != nil {
			return err
		}
		temps[i] = tmp.Name()
		if _, err := tmp.Write(a.data); err != nil {
			_ = tmp.Close()
			return err
		}
		if err := tmp.Chmod(0o644); err != nil {
			_ = tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
	}
	for i, a := range artifacts {
		if err := os.Rename(temps[i], a.path); err != nil {
			return err
		}
		renamed[i] = true
	}
	return nil
}

// verifyFinding produces the verdict (the compact block for findings.json) and
// the rich record (for verification.json) for one finding. With no eligible
// skeptic it records unverifiable/no_eligible_skeptic; with the tool harness
// unavailable it records unverifiable/tool_harness_unavailable; otherwise it
// drives every selected skeptic through invokeSkeptic and aggregates the votes.
// It never returns an error — failure isolation is the stage's contract.
func verifyFinding(ctx context.Context, f reconcile.JSONFinding, skeptics []Skeptic, cc fanout.ChatCompleter, disp Dispatcher) (*reconcile.Verification, VerificationResult) {
	base := VerificationResult{File: f.File, Line: f.Line, Problem: f.Problem}

	if len(skeptics) == 0 {
		v := &reconcile.Verification{Verdict: verdictUnverifiable, Notes: "no_eligible_skeptic"}
		base.Verdict, base.Reasoning = v.Verdict, v.Notes
		// No skeptic ran, so no model is attributed: Model stays "" by the
		// "attribute only to what executed" rule (AC3 / epic 3.1 clarification). Set
		// explicitly so a future change to base's initializer cannot leak a default.
		base.Model = ""
		return v, base
	}
	if disp == nil {
		v := &reconcile.Verification{Verdict: verdictUnverifiable, Notes: "tool_harness_unavailable"}
		base.Verdict, base.Reasoning = v.Verdict, v.Notes
		// Skeptics were eligible but the tool harness never built, so none executed —
		// Model stays "" (same rule). Recording the candidate models would misattribute
		// the record to models that produced nothing (AC3 / epic 3.1 clarification).
		base.Model = ""
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
			// none can occur here, but never let one drop a finding.
			v = &reconcile.Verification{Verdict: verdictUnverifiable, Notes: ierr.Error(), Skeptic: sk.Name}
			tripped = nil
		}
		perSkeptic = append(perSkeptic, v)
		perTripped = append(perTripped, tripped)
	}
	ver := aggregateVerdicts(perSkeptic)

	base.Verdict = ver.Verdict
	base.Skeptic = ver.Skeptic
	base.Reasoning = ver.Notes
	// Attribute Model/TrippedBudgets to the winning skeptics only — not all of
	// skeptics[0..n] — so a multi-vote verdict records the majority's models, not
	// the losers' (AC2/AC1).
	base.Model, base.TrippedBudgets = winningAttribution(skeptics, perSkeptic, perTripped, ver.Verdict)
	base.DurationMs = int(time.Since(start).Milliseconds())
	return ver, base
}

// winningAttribution derives the audit Model and TrippedBudgets for a finding
// from the skeptics whose verdict matched the aggregated (winning) verdict —
// mirroring how joinSkeptics names contributors, but for the winners only.
//
// perSkeptic[i] and perTripped[i] are the verdict and tripped-budget slice of
// skeptics[i] (verifyFinding builds the three slices together, so indices align).
// Models are deduplicated and joined in selection order. Tripped budgets attach
// only to unverifiable verdicts (a budget trip collapses a skeptic to
// unverifiable), so a confirmed/refuted winner contributes none.
//
// When no per-skeptic verdict matches the aggregate — only possible on a tie that
// aggregates to unverifiable with no unverifiable voter (e.g. 1 confirmed + 1
// refuted) — every candidate model is recorded so a run that actually executed
// never reports an empty Model.
func winningAttribution(skeptics []Skeptic, perSkeptic []*reconcile.Verification, perTripped [][]string, winner string) (string, []string) {
	var models, budgets []string
	seenModel := map[string]bool{}
	seenBudget := map[string]bool{}
	for i, v := range perSkeptic {
		if v == nil || v.Verdict != winner {
			continue
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
	if len(models) == 0 {
		for _, sk := range skeptics {
			if m := sk.Config.Model; m != "" && !seenModel[m] {
				seenModel[m] = true
				models = append(models, m)
			}
		}
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
