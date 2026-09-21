package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
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
//
// WHY THIS IS A REFUSAL AND NOT A TODO. It looks like a one-line flag removal and
// is not. Two blockers sit behind it, and both are decisions rather than
// implementation:
//
//   - There is NO repo-state reproducibility hash. runCheckpoint.ReproHash comes
//     from benchmark.ReproHashManifest, which hashes each case's DIFF BYTES and is
//     standard-v1-only; a repo-state case is a base tree plus a diff plus
//     case.json. validateCheckpoint compares that hash to refuse a resume across a
//     changed suite, so this tier needs either a new hashing contract (which
//     belongs in internal/benchmark beside the standard one — the same reason
//     `benchmark verify` prints "not defined for repo-state-v1" rather than
//     fabricating one) or the suite-identity guard skipped, which would remove the
//     protection that makes resume safe in the first place.
//   - checkpointReviewer carries Raised []string — bare categories, all
//     CorroborationRate needs. This tier also scores ReviewerPositionalRecall over
//     per-case []FindingMatch, none of which survives a checkpoint round-trip, so a
//     resumed run would silently score positional recall over only the
//     un-checkpointed cases.
//
// Removing the refusal without doing both produces exactly the failure the
// paragraph above describes. Both blockers are stated in full above deliberately:
// this comment is the whole record, and cites no planning document. The question it
// answers ("why is --checkpoint refused, and what would it take?") is asked HERE, at
// the function that refuses.
//
// The concern behind the flag is separately half-answered, which is why the refusal
// can stand: a run that fails, and one that completes with cases missing, both
// RETAIN their work dir and report the path, so the completed cases' paid review
// artifacts survive for inspection or manual rescoring. A per-case infrastructure
// failure no longer aborts the run at all — see executeRepoStateBenchmarkRun's
// per-case failure channel.
func checkRepoStateFlags(suiteFormat, checkpointPath string) error {
	// EqualFold for the same reason runBenchmarkRun routes with it: a differently-
	// cased discriminator is the same tier, and the refusal must fire before a run
	// the operator believes is resumable is paid for.
	if strings.EqualFold(suiteFormat, benchmark.FormatRepoStateV1) && checkpointPath != "" {
		// The message names the ALTERNATIVE, not just the refusal. --checkpoint is
		// asked for to avoid forfeiting a long paid run, and that concern is already
		// half-answered: a failed run retains its work dir and names the path, so the
		// completed cases' review artifacts survive. An operator told only "no" has no
		// way to know that.
		return fmt.Errorf("--checkpoint is not supported for a %s suite: resumable runs are implemented for the standard-v1 diff path only. "+
			"Re-run without --checkpoint — a failed run RETAINS its work dir and names the path in the error, so the completed cases' "+
			"review artifacts (raw transcripts, findings.txt, summary.json) survive for inspection or manual rescoring rather than being discarded",
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
// maxConsecutiveFailures is the OPT-IN mid-run cost brake: once that many cases fail
// back to back, the run aborts instead of paying for the rest of the suite. 0 disables
// it, which is the default. It complements the --fail-on-case-failure /
// --max-case-failures exit gates rather than duplicating them — those judge a run that
// has already been paid for in full, and this one stops the bill. Off by default for
// the reason the work-dir arm's comment gives: a general cap would take the abort
// decision away from the operator.
func executeRepoStateBenchmarkRun(ctx context.Context, cfg *fanout.ReviewConfig, completer fanout.Completer, suitePath string, generatedAt time.Time, maxConsecutiveFailures int) (rr *benchmark.RunResult, retainedWorkDir string, err error) {
	m, err := benchmark.LoadRepoState(suitePath)
	if err != nil {
		return nil, "", err
	}
	// The same two pre-flights executeBenchmarkRun applies, and for the same reason:
	// an unpublishable case id or reviewer identity makes the finished run
	// permanently unexportable, and the export gate would only say so after the
	// whole panel had been paid for.
	if err := validateRepoStatePublishableCaseIDs(m, suitePath); err != nil {
		return nil, "", err
	}
	if err := validatePublishableReviewerRoster(cfg); err != nil {
		return nil, "", err
	}

	// This loop builds the LINE MAPS the per-case scoring below needs. It is not the
	// parse gate, and reading it as one is the mistake to avoid: LoadRepoState already
	// parsed every case's diff at load (internal/benchmark/repostate.go:268), so a
	// malformed hunk header failed above, before this function reached its first paid
	// completer call. That guarantee lives in the loader deliberately — it is where
	// `benchmark verify` and `benchmark export --suite-path` inherit it too, which a
	// runner-only pre-flight could never give them.
	//
	// So this is a SECOND parse of the same bytes, and it is redundant only in the
	// narrow sense that its error arm is unreachable in practice. The loader does not
	// cache the map (its doc says why: no consumer outside this runner), so the work is
	// the price of not widening RepoStateCase's public surface for one caller. The err
	// check stays regardless — an unreachable arm that returns is cheaper than one that
	// panics if the two parses ever diverge.
	lineMaps := make([]benchmark.DiffLineMap, len(m.Cases))
	for i := range m.Cases {
		lm, err := loadCaseDiffLineMap(m.Cases[i])
		if err != nil {
			return nil, "", err
		}
		lineMaps[i] = lm
	}

	tmp, err := os.MkdirTemp("", "atcr-repo-state-")
	if err != nil {
		return nil, "", fmt.Errorf("creating benchmark work dir: %w", err)
	}
	// Declared above the defer, not beside the other accumulators, because the
	// retention decision below reads it. The run-result is not built until the loop
	// ends, so the deferred cleanup cannot consult rr.CaseFailures.
	var caseFailures []benchmark.CaseFailure
	// Slot failures share that placement for the same reason: retention now turns on
	// them too. Keyed by the PRE-SCRUB reviewer identity, because that is what the
	// per-agent loop has in hand; the emit tail maps each key through scrubOf so the
	// published array names the same identity as the coverage row it explains.
	slotFailures := map[reviewerKey][]benchmark.SlotFailure{}
	defer func() {
		// The work dir holds the paid review artifacts for every completed case. A
		// FAILED run RETAINS it — and the returned error names the path — so the
		// artifacts survive for inspection or manual rescoring instead of a
		// transient failure destroying everything the panel produced; a clean run
		// still cleans up. Cleanup failures are Warned, never swallowed silently.
		//
		// A PARTIAL run retains it for the same reason and has a harder time saying
		// so. It returns err == nil, so without this second arm the deferred
		// RemoveAll fired on exactly the runs the failure channel exists to rescue —
		// destroying the successful cases' paid artifacts at the moment they became
		// the only copy of work that will not be re-run. There is no error to wrap
		// the path into either, so the log line is the whole report; the run-result's
		// case_failures array names which cases are missing from it.
		if err != nil {
			log.FromContext(ctx).Warn("benchmark work dir retained after a failed run", "path", tmp)
			err = fmt.Errorf("%w (work dir retained at %s)", err, tmp)
			retainedWorkDir = tmp
			return
		}
		// A SLOT failure retains too, not just a whole-case one. This arm used to read
		// len(caseFailures) alone, so a run that lost one reviewer on one case — the
		// case itself reviewed fine by the others — fell through to the RemoveAll
		// below and destroyed every review dir, including the status.json holding the
		// failure the operator would need to diagnose it. The two are one condition:
		// whatever went unmeasured, the paid artifacts are the only record of why.
		if len(caseFailures) > 0 || len(slotFailures) > 0 {
			// The retained BYTES are reported, not just the path. Retention is
			// unbounded and unconditional on a partial run by design — the artifacts
			// are the only copy of a paid panel, so capping or pruning them would
			// destroy what this arm exists to save — which leaves growth as something
			// the operator must SEE rather than something the code may silently
			// reclaim. A scheduled suite losing one case per run accumulates a full
			// work dir every run; a size on the same line that names the path is what
			// makes that visible before the volume fills.
			// An unmeasurable size logs "unknown", never zero: a zero reads as
			// "nothing retained" on the line the operator watches for growth.
			attrs := []any{"path", tmp, "failed_cases", len(caseFailures), "failed_slots", len(slotFailures)}
			if size, measured := dirSizeBytes(tmp); measured {
				attrs = append(attrs, "retained_bytes", size)
			} else {
				attrs = append(attrs, "retained_bytes", "unknown")
			}
			log.FromContext(ctx).Warn("benchmark work dir retained after a partial run", attrs...)
			// Returned to the caller as well as logged. The log line is suppressible —
			// ATCR_LOG_LEVEL=error is a legal setting and drops Warn entirely — and the
			// partial arm has no error to wrap the path into the way the failure arm
			// above does. Without this the only copy of a ten-minute paid panel's
			// artifacts is an unnamed /tmp/atcr-repo-state-* directory the operator has
			// to go hunting for. It is a RETURN VALUE rather than a RunResult field on
			// purpose: the run-result is a file operators hand to other people, and a
			// local $TMPDIR path is machine-local scratch that has no business in it.
			retainedWorkDir = tmp
			return
		}
		if rmErr := os.RemoveAll(tmp); rmErr != nil {
			log.FromContext(ctx).Warn("benchmark work dir cleanup failed", "path", tmp, "err", rmErr)
		}
	}()

	// Two accumulators over one pass. Category recall (the standard-v1 quantity
	// CorroborationRate carries on every suite) and positional recall are different
	// measurements of the same findings, so they are folded together rather than by
	// re-reading the pool twice.
	cats := map[reviewerKey]*benchmark.ReviewerScore{}
	positional := map[reviewerKey]*benchmark.RepoStateReviewerScore{}
	acc := map[reviewerKey]*repoStateAcc{}
	var order []reviewerKey

	// Both cancellation diagnostics print one sentence, so they print it from ONE
	// quantity. The in-loop site used to pass the loop index (cases ATTEMPTED,
	// failures included) and the post-loop site len(m.Cases)-len(caseFailures) (cases
	// scored), so the same interrupt on a run with a failed case produced two
	// different numbers under identical wording. Counted here, after the per-agent
	// loop has folded a case into the accumulators, so the number means "cases this
	// run would have published" at both sites by construction rather than by
	// arithmetic that happens to agree.
	scored := 0
	// consecutiveFailures is reset by a scored case, so it measures a RUN of failures
	// rather than a total. Two failures either side of a scored case are bad luck; two
	// back to back on a 20-case panel are usually one broken provider, and every
	// remaining case is billed at ten minutes to learn the same thing again.
	consecutiveFailures := 0
	caseIDs := make([]string, 0, len(m.Cases))
	// expectedCategories was called inside the per-agent loop — the same case's
	// projection rebuilt once per agent per case. One call per case, above the
	// loop: the projection is a property of the CASE, not of who reviewed it.
	caseExpected := make([][]string, len(m.Cases))
	for i := range m.Cases {
		caseExpected[i] = expectedCategories(m.Cases[i])
	}
	for i, c := range m.Cases {
		// AN OPERATOR INTERRUPT IS NOT AN INFRASTRUCTURE FAILURE. cli/main.go cancels
		// the root context on SIGINT/SIGTERM, and MaterializeCase and PrepareReview
		// both run under it — so without this check Ctrl-C surfaces as a per-case
		// fault, the loop records the same fault for every remaining case, and the
		// command writes a partial run-result and exits 0. An interrupted run would
		// become a publishable artifact whose missing cases were the operator's own
		// decision to stop. Checked here and again after the loop, which together
		// cover an interrupt arriving on any case including the last.
		if cerr := ctx.Err(); cerr != nil {
			return nil, "", fmt.Errorf("benchmark run cancelled after %d of %d case(s): %w", scored, len(m.Cases), cerr)
		}
		// The opt-in cost brake, checked at the TOP of the iteration for the same
		// reason the cancellation check is: this is the last moment before the case
		// is paid for. Checked here rather than at the six recording sites so a
		// future seventh site inherits it, and so the abort always happens with a
		// case still left to save — once the suite is exhausted there is no bill left
		// to stop, and the all-cases-failed guard below already covers that shape.
		if maxConsecutiveFailures > 0 && consecutiveFailures >= maxConsecutiveFailures {
			return nil, "", fmt.Errorf("benchmark run aborted: %d consecutive case(s) failed (%s), reaching --max-consecutive-case-failures=%d; "+
				"the remaining %d case(s) were not run, so the panel was not paid for them",
				consecutiveFailures, summarizeCaseFailureReasons(caseFailures), maxConsecutiveFailures, len(m.Cases)-i)
		}
		caseIDs = append(caseIDs, c.ID)

		lm := lineMaps[i]
		// Keyed by case INDEX rather than id so two ids sharing a path basename
		// cannot overwrite each other's tree, the same reason executeBenchmarkRun
		// keys its per-case output dir by index.
		repoDir := filepath.Join(tmp, fmt.Sprintf("repo-%d", i))
		if err := mkdirAllFn(repoDir, 0o755); err != nil {
			// A HOST-LEVEL work-dir fault aborts instead of joining the failure
			// channel. That channel is for transient, case-specific faults, and these
			// five errnos are neither: the disk is full or over quota, the process or
			// system is out of file descriptors, or the volume is read-only. Every remaining case
			// repeats the identical syscall and fails identically, so a 200-case suite
			// writes 200 entries and still exits 0 — and on EMFILE the same exhaustion
			// then hits PrepareReview and ExecuteReview on any case that DID get a
			// directory, burning the panel while the host has no file handles left.
			// Narrow on purpose: only this site, only these errnos. A general
			// consecutive-failure cap would overlap the opt-in exit gates on
			// `benchmark run` and take the decision away from the operator.
			if isFatalWorkDirError(err) {
				releaseCaseRepo(ctx, repoDir, c.ID)
				return nil, "", fmt.Errorf("creating case work dir for %q: %w "+
					"(host-level fault, not case-specific: every remaining case would fail identically)", c.ID, err)
			}
			recordCaseFailure(ctx, &caseFailures, &consecutiveFailures, c.ID, benchmark.CaseFailureWorkDir, err)
			// Released like every other failure path below, not skipped because the
			// directory "was not created": MkdirAll builds the path element by element
			// and returns on the first element it cannot make, so a partial tree is
			// exactly what a failure here leaves behind. RemoveAll on a path that was
			// never created is a no-op, so the call costs nothing when it really did
			// create nothing — and omitting it is how the accumulation releaseCaseRepo's
			// doc comment is about starts on a long suite.
			releaseCaseRepo(ctx, repoDir, c.ID)
			continue
		}
		mc, err := benchmark.MaterializeCase(ctx, c, repoDir)
		if err != nil {
			recordCaseFailure(ctx, &caseFailures, &consecutiveFailures, c.ID, benchmark.CaseFailureMaterialize, err)
			releaseCaseRepo(ctx, repoDir, c.ID)
			continue
		}
		// The two winnability preconditions LoadRepoState structurally cannot reach:
		// the head state does not exist until the case is materialized. Checked HERE,
		// before this case's first paid completer call, so an unwinnable case costs a
		// materialization rather than a panel — the same fail-early rule the diff
		// pre-parse above follows.
		//
		// STILL ABORTS, and is never recorded into the failure channel. That channel
		// is for transient INFRASTRUCTURE faults, and this is a suite-AUTHORING
		// defect: deterministic, identical on a re-run, and caught before this case
		// costs anything. Recording it would publish a score computed around a suite
		// already known to be broken, which is the same objection that keeps the
		// scored-twice guard below fatal.
		if err := benchmark.ValidateAgainstHead(c, mc.Root); err != nil {
			return nil, "", err
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
			// The empty-roster split belongs HERE, not only on the execute branch
			// below: ErrEmptyRoster is raised by validateReviewRequest, which
			// PrepareReview reaches and ExecuteReview never does. Recorded as a
			// transient per-case fault it repeats on every case and the run dies as
			// "all N case(s) failed ... re-running is the remedy only if the cause was
			// transient" — a config defect reported as bad luck, and with the sentinel
			// buried where errors.Is cannot reach it.
			//
			// ErrNoReviewableContent joins it for the same reason ValidateAgainstHead
			// stays fatal thirty lines up: a case whose materialized range changes no
			// reviewable file is a suite-AUTHORING defect — deterministic, identical
			// on every re-run, and undetectable at load (the NoIgnore note below says
			// so). Recorded as transient it disappears from the paid measurement at
			// exit 0, and the operator is told a re-run might help when it cannot.
			//
			// Classified, not currently reachable, and deliberately untested for that
			// reason. Two properties close every route to it on THIS tier:
			// MaterializeCase's head commit fails rather than producing a commit that
			// changes nothing, and a changed file the payload builder cannot read
			// still yields a one-line marker entry, so ReviewableCount never reaches
			// 0 for a materialized case. The arm states the classification so a future
			// change to either property lands on the correct side by default.
			if errors.Is(err, fanout.ErrEmptyRoster) || errors.Is(err, fanout.ErrNoReviewableContent) {
				releaseCaseRepo(ctx, repoDir, c.ID)
				return nil, "", fmt.Errorf("preparing case %q: %w", c.ID, err)
			}
			recordCaseFailure(ctx, &caseFailures, &consecutiveFailures, c.ID, benchmark.CaseFailurePrepare, err)
			releaseCaseRepo(ctx, repoDir, c.ID)
			continue
		}
		log.FromContext(ctx).Info("repo-state case executing", "case", c.ID, "reviewers", len(reviewerRoster(cfg)))
		res, err := fanout.ExecuteReview(ctx, completer, prep)
		if err != nil {
			// THE ONE EXECUTION FAILURE THAT STILL ABORTS. Every slot in the panel
			// failing is precisely the transient infrastructure failure
			// docs/benchmark.md forbids scoring as a genuine missed defect, and the
			// gate that enforces it is this propagation — recording the case as
			// unmeasured instead would let a whole-roster outage read on the run-result
			// exactly like a local disk fault, which is the distinction the abort
			// exists to keep. Any OTHER execution error is one case's bad luck and
			// joins the failure channel.
			// ErrEmptyRoster rides the same split for the opposite reason: not a
			// transient outage but a deterministic configuration defect, where no slot
			// ran at all. Recorded per-case it would repeat on every case and surface
			// as "all cases failed", burying the real cause under the transient class.
			// The prepare branch above carries the identical arm — that is where a
			// configured empty roster actually lands, since validateReviewRequest
			// raises the sentinel inside PrepareReview. That makes THIS arm currently
			// unreachable through the runner: no traced path reaches ExecuteReview
			// with a roster PrepareReview accepted and Outcome then finds empty. It is
			// kept defensively, not as a live runtime path, and deliberately carries
			// no test — a test would assert a behaviour nothing can produce.
			if errors.Is(err, fanout.ErrAllAgentsFailed) || errors.Is(err, fanout.ErrEmptyRoster) {
				return nil, "", fmt.Errorf("executing case %q: %w", c.ID, err)
			}
			recordCaseFailure(ctx, &caseFailures, &consecutiveFailures, c.ID, benchmark.CaseFailureExecute, err)
			releaseCaseRepo(ctx, repoDir, c.ID)
			continue
		}

		summary, err := readPoolSummaryFn(res.Dir)
		if err != nil {
			recordCaseFailure(ctx, &caseFailures, &consecutiveFailures, c.ID, benchmark.CaseFailurePoolSummary, err)
			releaseCaseRepo(ctx, repoDir, c.ID)
			continue
		}
		// The agent set bounds the unattributed tally: a skipped row whose recovered
		// reviewer is nobody on this panel is counted (and warned) rather than
		// silently keyed under a name no reader will ever look up.
		agentSet := make(map[string]bool, len(summary.Agents))
		for _, a := range summary.Agents {
			agentSet[a.Agent] = true
		}
		located, categorical, unattributed, missingFindingsFile, err := readCaseFindingsLocatedFn(res.Dir, agentSet)
		if err != nil {
			recordCaseFailure(ctx, &caseFailures, &consecutiveFailures, c.ID, benchmark.CaseFailureReadFindings, err)
			releaseCaseRepo(ctx, repoDir, c.ID)
			continue
		}
		if missingFindingsFile {
			log.FromContext(ctx).Warn("case produced no findings file; every reviewer reads as raised-nothing",
				"case", c.ID, "review_dir", res.Dir)
		}
		if unattributed > 0 {
			log.FromContext(ctx).Warn("skipped finding rows name a reviewer not in the panel; counted as unattributed",
				"case", c.ID, "unattributed", unattributed)
		}
		// The materialized repo has no consumer once the findings are read: scoring
		// reads neither the tree nor the .git, and the review dir carries the
		// artifacts. Released HERE rather than at run end, so a large-base-tree
		// suite hands its repos back as it goes instead of accumulating one per
		// case under a $TMPDIR volume a long panel can exhaust mid-run.
		//
		// A cleanup failure is WARNED and NOT recorded as a case failure. The case
		// has already been reviewed and is about to be scored; only the disk reclaim
		// failed. Marking it unmeasured would discard a successful paid measurement
		// over a janitorial fault — and failing the run outright, as this used to,
		// discarded every other case with it. Same Warn-not-fail shape the run-level
		// cleanup above already uses.
		//
		// The MATERIALIZED TREE IS DELIBERATELY NOT PART OF THE RETAINED-ARTIFACT
		// PROMISE. Released here, before the per-agent loop, so the scored-twice
		// identity guard and the post-loop scrub-collision guard both abort with this
		// case's repo already gone. Those paths retain the work dir so "the artifacts
		// survive for inspection", and what survives is the REVIEW dir — which carries
		// the findings the guards are about. The tree is reconstructible from the
		// suite; the panel's output is not.
		releaseCaseRepo(ctx, repoDir, c.ID)

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
			// THE UNMEASURED-NOT-MISSED RULE, AT SLOT GRANULARITY. The whole-case
			// failure channel covers the all-reviewers outage; one slot down, a
			// provider timeout on 1 of N reviewers still produced a CaseScore with
			// Raised nil and a positional row matched against nothing — charging that
			// reviewer full recall-0 for a case it was never shown. That is the exact
			// conflation docs/benchmark.md forbids for this tier ("a transient
			// infrastructure failure recorded as a genuine missed defect"), surviving
			// one level below where the whole-case channel can see it.
			//
			// The row is still EMITTED — the identity was registered just above — so a
			// reviewer whose every slot failed appears with an empty covered set and no
			// score, reading as "measured nothing" rather than vanishing or, worse,
			// "missed everything".
			//
			// Skipped BEFORE the duplicate-identity guard on purpose: a failed slot must
			// not claim the case, or a second lane sharing that identity would trip the
			// scored-twice abort for a case only one of them actually reviewed.
			//
			// ALL THREE of score, covered set and outcome tally are skipped together,
			// and that is load-bearing rather than tidy. checkCoverage enforces
			// runs == len(case_ids) == sum(outcomes) as a tamper check, so recording the
			// failed slot in the tally while omitting it from the covered set would make
			// every run with a failed slot fail the export gate as "malformed" — after
			// the panel had been paid for.
			//
			// The CAUSE is recorded instead, on its own axis (benchmark.SlotFailure),
			// which is what keeps the skip from being silent. It cannot go on the
			// case-level channel: validateCaseFailures rejects a case some reviewer
			// scored, and the surviving reviewers did score this one. Keyed by the
			// PRE-SCRUB identity here and mapped through scrubOf at emit, so the
			// published record joins to the coverage row it explains.
			//
			// The predicate tests Status only, NOT a.Error beside it — deliberately,
			// unlike ReviewerOutcome's failed arm (internal/fanout/revieweroutcome.go),
			// which reads Status != StatusOK || a.Error != "". No live fanout producer
			// emits StatusOK with a non-empty Error, so the pair is latent; and a skip
			// that fired on it would need a slot-failure REASON for an OK status, which
			// the vocabulary has no entry for (SlotFailureReasonForStatus documents that
			// an OK slot is never skipped). If a producer of that pair ever appears, the
			// predicate here and that reason mapping must move together — unify both
			// sides in the same change, never this one alone.
			if a.Status != fanout.StatusOK {
				slotFailures[key] = append(slotFailures[key], benchmark.SlotFailure{
					CaseID: c.ID,
					Reason: benchmark.SlotFailureReasonForStatus(a.Status),
				})
				log.FromContext(ctx).Warn("reviewer slot failed; recorded as unmeasured for this case",
					"case", c.ID, "agent", a.Agent, "status", a.Status, "err", a.Error)
				continue
			}

			// Two lanes can realize the SAME (model, persona) — a parallel and a
			// serial slot pointing at one registry entry, or a fallback converging on
			// another agent's model. Both then append a CaseScore for this case,
			// silently doubling Runs and re-weighting CorroborationRate. The standard
			// tier fails closed on exactly this, and so must this one: a merged
			// identity is only meaningful when the two lanes PARTITION the suite.
			if prior, dup := acc[key].scored[c.ID]; dup {
				return nil, "", fmt.Errorf("case %q scored twice under realized identity %q/%q (agents %q and %q); "+
					"two lanes sharing one identity must partition the suite, not both score it",
					c.ID, key.model, key.persona, prior, a.Agent)
			}
			acc[key].scored[c.ID] = a.Agent

			cats[key].Cases = append(cats[key].Cases, benchmark.CaseScore{
				Expected: caseExpected[i],
				// The CATEGORICAL projection, which folds unparseable rows back in
				// with an empty category. Driving this off the positional projection
				// instead would shrink the out-of-vocabulary denominator for a
				// reviewer emitting malformed output — rewarding exactly the
				// behaviour that metric exists to detect.
				Raised: categorical[a.Agent],
			})
			positional[key].Cases = append(positional[key].Cases, benchmark.RepoStateCaseScore{
				Matches: benchmark.MatchFindings(c.ExpectedFindings, located[a.Agent], lm),
			})

			acc[key].caseIDs = append(acc[key].caseIDs, c.ID)
			acc[key].outcomes[benchmark.OutcomeTallyKey(fanout.ReviewerOutcome(a, categorical[a.Agent]))]++
			if a.FallbackUsed {
				acc[key].fallbackCases++
			}
			// Fold the gate state across this row's cases, requiring UNANIMITY rather
			// than overwriting: a row whose categories came from a mix of gated and
			// ungated cases measured a mixed population, and claiming either state for
			// it would be the overstatement the tag exists to prevent. A disagreement
			// between cases, or a nil from any case (a rebuilt summary cannot know),
			// makes the whole row nil — unmeasured, not false.
			acc[key].groundingEnabled = foldGroundingEnabled(
				acc[key].groundingEnabled, summary.GroundingEnabled, len(acc[key].caseIDs) == 1)

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
		scored++
		consecutiveFailures = 0
	}

	// The loop's cancellation check again, for an interrupt that arrived on the LAST
	// case: that iteration has no successor to catch it, so without this the run
	// would return a partial result whose missing case was the operator's own Ctrl-C.
	if cerr := ctx.Err(); cerr != nil {
		return nil, "", fmt.Errorf("benchmark run cancelled after %d of %d case(s): %w", scored, len(m.Cases), cerr)
	}

	// A run that scored NOTHING measured nothing, so there is no partial result to
	// salvage and nothing the caller can do with a run-result built from it.
	// Returned as an error rather than an empty artifact because the alternative
	// misdiagnoses itself downstream: a run-result carrying suite_case_ids with zero
	// reviewer rows is what checkCoverage calls "malformed", which would blame the
	// file for a runner that behaved exactly as designed.
	//
	// Keyed on the reviewer accumulator being EMPTY rather than on every case having
	// recorded a failure. The two coincide today, but they are different claims: the
	// failure count answers "did each case fail", and what the guard must prevent is
	// the empty artifact, which any future path that skips a case without recording
	// one would also produce.
	//
	// The shipped all-agents-failed abort does NOT cover this shape. That one fires
	// inside a single case's review call and cannot see a suite-wide outcome; this
	// one is reachable only now that a case failure stops aborting.
	if len(order) == 0 {
		// One return, not two: the fallback that named a no-rows-no-failures shape
		// was a branch no test could reach (the empty case list is rejected at load
		// and an all-roster failure aborts per case), and an untestable arm is dead
		// weight the file carries for nothing. The folded message still reads at
		// zero failures — the tally renders as "no failure was recorded" — so the
		// guard keeps preventing the empty artifact whatever future path reaches it.
		reasons := summarizeCaseFailureReasons(caseFailures)
		if reasons == "" {
			reasons = "no failure was recorded"
		}
		return nil, "", fmt.Errorf("no case could be scored: %d of %d case(s) failed (%s); "+
			"re-running is the remedy only if the cause was transient",
			len(caseFailures), len(m.Cases), reasons)
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
		// The REALIZED printability guard, shared with buildRunResult. This loop
		// carried only the collision half: validatePublishableReviewerRoster at the
		// top of this function covers the CONFIGURED registry values, but reviewerModel
		// prefers the usage-reported and fallback models over the registry, so a Cc/Cf
		// rune from a provider's own usage payload arrived here ungated and survived
		// the scrub into the published identity. Failing now costs this run; failing at
		// export costs the whole paid panel again, because --checkpoint is refused on
		// this tier and a re-run re-derives the same rune.
		if err := checkRealizedIdentityPrintable(k); err != nil {
			return nil, "", err
		}
		s := scorecard.ScrubPublicRecord(scorecard.PublicRecord{Model: k.model, Persona: k.persona})
		id := reviewerKey{model: s.Model, persona: s.Persona}
		if prev, dup := public[id]; dup {
			return nil, "", fmt.Errorf("distinct reviewer identities %q/%q and %q/%q scrub to the same public identity %q/%q: "+
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
			Model:   pub.model,
			Persona: pub.persona,
			// COPIED, not aliased — buildRunResult carries an explicit comment for
			// exactly this: the returned artifact must not change if a caller keeps
			// folding into the accumulator afterwards. The acc here is
			// function-local today, so the copy is defense against a future reader
			// rather than a live bug; carrying it keeps the two runners' emit tails
			// interchangeable without re-deriving that fact.
			CaseIDs:          append([]string(nil), acc[k].caseIDs...),
			Outcomes:         maps.Clone(acc[k].outcomes),
			FallbackCases:    acc[k].fallbackCases,
			GroundingEnabled: acc[k].groundingEnabled,
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
		CaseFailures:        caseFailures,
		SlotFailures:        publicSlotFailures(ctx, slotFailures, order, scrubOf),
		// retainedWorkDir is set by the deferred cleanup above, which runs after this
		// return and is the only place that knows whether the dir survived.
	}, "", nil
}

// publicSlotFailures flattens the per-identity slot-failure map into the emitted
// array, translating each key to the identity the coverage rows carry.
//
// The TRANSLATION is the point. The per-agent loop holds the pre-scrub key, and the
// emitted rows are scrubbed, so publishing the raw keys would produce an array naming
// identities that appear nowhere else in the document — unjoinable by the export
// diagnostic that exists to read it.
//
// Iterated over `order` rather than over the map, for the reason the coverage rows
// are: `order` is already sorted by the scrubbed pair, so two runs with identical
// logical content produce byte-identical arrays. Ranging a Go map here would make the
// output order random per run.
//
// A key absent from scrubOf is skipped rather than emitted raw. The only way to reach
// that is a reviewer whose every slot failed on every case before the identity was
// registered, which the loop's registration order rules out — but emitting an
// untranslated identity would be worse than emitting nothing, since it would look
// joinable and not be. The skip is WARNED, not silent: this is the channel built to
// end silent drops, and an unreachable case becoming a silent one would repeat the
// exact failure the channel exists to prevent.
func publicSlotFailures(ctx context.Context, byKey map[reviewerKey][]benchmark.SlotFailure, order []reviewerKey, scrubOf map[reviewerKey]reviewerKey) []benchmark.SlotFailure {
	if len(byKey) == 0 {
		return nil
	}
	var out []benchmark.SlotFailure
	for _, k := range order {
		id, ok := scrubOf[k]
		if !ok {
			log.FromContext(ctx).Warn("slot failure dropped: identity missing from the scrub map",
				"model", k.model, "persona", k.persona, "slot_failures", len(byKey[k]))
			continue
		}
		for _, sf := range byKey[k] {
			out = append(out, benchmark.SlotFailure{
				Model:   id.model,
				Persona: id.persona,
				CaseID:  sf.CaseID,
				Reason:  sf.Reason,
			})
		}
	}
	return out
}

// recordCaseFailure marks one case unmeasured and lets the run continue.
//
// The full error goes to the LOG and the run-result gets the case id and the reason
// only. That split is deliberate: a Go error on this path routinely embeds the run's
// $TMPDIR path or a provider's message, and the run-result is a file operators hand
// to other people, while the log stays on the machine that produced it. The reason
// is the part a downstream reader can act on.
//
// The pointer-to-slice is what makes this one helper serve all six recording sites
// with only the reason constant differing. Six inlined append-and-log pairs is how a
// classification drifts: one of them eventually logs at a different level, or omits
// the case id, and the difference reads as meaningful when it is not.
func recordCaseFailure(ctx context.Context, into *[]benchmark.CaseFailure, consecutive *int, caseID, reason string, cause error) {
	log.FromContext(ctx).Warn("repo-state case failed; recorded as unmeasured and skipped",
		"case", caseID, "reason", reason, "err", cause)
	*into = append(*into, benchmark.CaseFailure{CaseID: caseID, Reason: reason})
	// Counted HERE so the opt-in consecutive-failure cap cannot miss a site: the
	// same property that makes one helper serve all six recording sites makes this
	// the one place the run of failures can be tracked without relying on every
	// future site remembering to.
	*consecutive++
}

// readPoolSummaryFn and readCaseFindingsLocatedFn are the two POST-PAYMENT read-back
// seams, indirected through package vars (like resolveAutoFixSandboxFn) so a test can
// fault them. Production points at the real functions.
//
// They exist because these two record-and-continue sites are otherwise untestable.
// Every other fault a test can inject rides a completer call, and a completer call
// happens strictly BEFORE writePool — so anything planted there fails the WRITE and
// the case is recorded at `execute`, never at `pool_summary` or `read_findings`. Those
// are exactly the two reasons whose doc comments promise "the panel ran and was paid
// for", which is the claim most worth proving and the one nothing could reach.
var (
	readPoolSummaryFn         = fanout.ReadPoolSummary
	readCaseFindingsLocatedFn = readCaseFindingsLocated
)

// summarizeCaseFailureReasons renders the failure channel as a per-reason tally,
// sorted so the same run always produces the same string.
//
// The all-cases-failed diagnostic used to name caseFailures[len-1] — "the last
// reason". On a mixed systemic failure that is one arbitrary reason out of N, and the
// sentence beside it tells the operator that re-running helps "only if the cause was
// transient" while withholding what the causes were. The whole list is in hand at
// that point and is published nowhere else on this path, since the run returns no
// run-result.
func summarizeCaseFailureReasons(failures []benchmark.CaseFailure) string {
	tally := map[string]int{}
	for _, f := range failures {
		tally[f.Reason]++
	}
	reasons := make([]string, 0, len(tally))
	for r := range tally {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	parts := make([]string, 0, len(reasons))
	for _, r := range reasons {
		parts = append(parts, fmt.Sprintf("%s x%d", r, tally[r]))
	}
	return strings.Join(parts, ", ")
}

// dirSizeBytes totals the regular-file bytes under root, best-effort, and reports
// whether the total was measured at all.
//
// Reported beside the retained path so unbounded retention is VISIBLE rather than
// merely documented. It is still warn-never-fail — this runs inside a deferred
// cleanup, and a run must not change its outcome because a size could not be
// measured — but a walk that cannot even read the ROOT is reported as unmeasured
// rather than as zero: the caller logs "unknown" for it, because the doc tells the
// operator to watch exactly this number for growth, and a zero reads as "nothing
// retained", hiding the very growth the warning exists to surface. A mid-walk
// failure leaves the partial total, which is still a signal, and reports measured.
func dirSizeBytes(root string) (int64, bool) {
	var total int64
	rootErr := error(nil)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				rootErr = err
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil {
			total += info.Size()
		}
		return nil
	})
	return total, rootErr == nil
}

// maxNamedFailedCases bounds the per-case list in warnCaseFailures, matching
// maxNamedMissingCases' role for the coverage shortfall. Larger than that one because
// this list is the run's own failure report rather than a clause inside a sentence,
// and an operator triaging a partial run wants a few concrete case ids to start from.
const maxNamedFailedCases = 10

// mkdirAllFn is the per-case work-dir creation seam, indirected through a package
// var like readPoolSummaryFn and resolveAutoFixSandboxFn. Production points at
// os.MkdirAll.
//
// It exists because the host-level abort below is otherwise untestable, and was
// measurably untested: the run's work dir is created INSIDE the runner by
// os.MkdirTemp, so it has no name a test could chmod before the run starts, and the
// only fault a test could previously stage there was EACCES — which is
// path-specific and deliberately NOT in the fatal set, so it exercised the
// record-and-continue arm instead. `if false && isFatalWorkDirError(err)` left the
// whole cli suite green.
var mkdirAllFn = os.MkdirAll

// isFatalWorkDirError reports whether a work-dir creation error is a property of the
// HOST rather than of the case.
//
// The five here are the ones that cannot clear on their own within a run: no space
// left (ENOSPC), a volume over its quota (EDQUOT), the process or the system out of
// file descriptors (EMFILE/ENFILE), and a read-only filesystem (EROFS). EDQUOT is
// grouped with ENOSPC rather than left out because a quota-exhausted volume behaves
// identically at the call site — every remaining case repeats the identical failing
// syscall — so recording it as one case's bad luck invites the loop to fail the
// whole suite the same way an out-of-space volume does.
//
// Deliberately NOT included: EACCES/EPERM and ENOTDIR. Those can be specific to the
// path being created, so the per-case classification is the honest one for them.
func isFatalWorkDirError(err error) bool {
	return errors.Is(err, syscall.ENOSPC) ||
		errors.Is(err, syscall.EDQUOT) ||
		errors.Is(err, syscall.EMFILE) ||
		errors.Is(err, syscall.ENFILE) ||
		errors.Is(err, syscall.EROFS)
}

// releaseCaseRepo hands one case's materialized repository back, warning rather than
// failing if it cannot.
//
// Called on the failure paths too, not just after a scored case. A case that died at
// materialization or prepare still left a partial tree behind, and on a long suite
// those accumulate under the same $TMPDIR volume the run-end cleanup was already
// moved off to protect. The paid artifacts a partial run retains live in the
// per-case review dir, which this never touches.
func releaseCaseRepo(ctx context.Context, repoDir, caseID string) {
	if err := os.RemoveAll(repoDir); err != nil {
		log.FromContext(ctx).Warn("case work repo cleanup failed", "case", caseID, "path", repoDir, "err", err)
	}
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
	// groundingEnabled is the run's Epic 14.1 gate state, carried up from each
	// case's PoolSummary so the emitted coverage row can state which population its
	// CorroborationRate measured. Folded by UNANIMITY: the tag may only claim a state
	// if every case this row scored reported that same state, since one disagreeing
	// case is enough to make the row's categories a mixed population.
	groundingEnabled *bool
}

// expectedCategories projects a case's located expectations onto the bare category
// list CorroborationRate is defined over.
//
// The dedupe NORMALIZES first, matching every other consumer of the same field
// (Score's normalizeDistinct, validateCategoryEquivalence's normalize): a case
// carrying 'Correctness' beside 'correctness' is ONE expected category. Score
// re-normalizes downstream, so the raw-string dedupe this used to be was
// harmless today — but it was a silent second dedupe rule, one fix away from
// diverging, and normalize is two words. The raw spelling is kept in out so the
// score sees the case's own words.
//
// The two metrics deliberately measure the SAME findings under different
// denominators: category recall asks "did any finding carry the right word",
// positional recall asks "did a finding land in the right place". Keeping
// CorroborationRate's DEFINITION identical across both suites is what lets a
// repo-state row sit on the same public board as a standard-v1 one (AC5).
//
// Identical definition, not identical comparability — this comment used to claim
// the second and only supported the first. The findings fed to the category scorer
// are read from the post-grounding findings.txt, and the Epic 14.1 gate is live on
// this tier and fails open on standard-v1, so the two tiers' rates are computed
// over different populations. The row states which via
// benchmark.ReviewerCoverage.GroundingEnabled rather than adjusting the rate, so
// nothing already published changes value.
func expectedCategories(c benchmark.RepoStateCase) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(c.ExpectedFindings))
	for _, f := range c.ExpectedFindings {
		n := strings.ToLower(strings.TrimSpace(f.Category))
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, f.Category)
	}
	return out
}

// foldGroundingEnabled combines one case's grounding-gate state into a reviewer
// row's running tag. first marks the row's opening case, where there is no prior
// value to fold against.
//
// The fold is UNANIMITY over three-valued logic, with nil ABSORBING: a row keeps a
// state only when every case it scored reported that same state, and anything else —
// a nil from a rebuilt summary that cannot know, or two cases that disagree — makes
// the whole row nil. Every direction fails toward "unmeasured" rather than toward a
// claim, because the tag's only job is to say which population the row's
// CorroborationRate came from, and an overstated tag is worse than an absent one.
//
// It is NOT a boolean AND, and the difference is the whole point. AND folded a
// disagreement to false, which a consumer cannot distinguish from "every case this
// row scored was ungated" — and only the second is comparable with a standard-v1 row.
// A row mixing gated and ungated cases measured a mixed population and is comparable
// with neither, so it must say so. The mixed case is reachable per case:
// internal/fanout/review.go stamps &false for a case whose changed-lines computation
// failed or came back empty, while its siblings in the same run are gated.
//
// It also kept the run-result self-contradictory: a row could tally `ungrounded` from
// a gated case 1 and fold to false on a fail-open case 2, publishing a gate-driven
// outcome beside a claim that the gate was off — which the export gate in
// cli/benchmark_coverage.go now rejects outright.
func foldGroundingEnabled(prior, caseState *bool, first bool) *bool {
	if first {
		if caseState == nil {
			return nil
		}
		// A FRESH pointer, not caseState itself: the accumulator must not end up
		// sharing storage with a PoolSummary the caller still holds. The copy below
		// and this one enforce the same invariant for every row length — the
		// one-case row reaches this arm, the multi-case row the one further down.
		agreed := *caseState
		return &agreed
	}
	if prior == nil || caseState == nil {
		return nil
	}
	if *prior != *caseState {
		return nil
	}
	// A FRESH pointer, not prior itself: the accumulator must not end up sharing
	// storage with a PoolSummary the caller still holds.
	agreed := *prior
	return &agreed
}

// loadCaseDiffLineMap reads and parses a case's own diff, which is what makes
// outside_diff a measurement rather than a label (AC3b).
//
// The read is size-capped before it happens, mirroring MaxDiffBytes on the
// standard tier (ReproHash rejects an oversized diff there): this is the same
// class of untrusted third-party input, and a multi-gigabyte change.diff would
// otherwise OOM the process at read/parse time.
//
// The LOAD-TIME cap in benchmark.loadRepoStateCase is the one that actually bounds
// memory — it sits at the first read of this file, and every LoadRepoState caller
// inherits it. This check is kept as a cheap assertion for the same reason the
// runner re-validates other load-time invariants: it costs one Stat, it keeps this
// function safe for any future caller that did not come through the loader, and a
// file that grew between load and here is a real (if unlikely) shape.
func loadCaseDiffLineMap(c benchmark.RepoStateCase) (benchmark.DiffLineMap, error) {
	path := filepath.Join(c.Dir, c.Diff)
	if fi, err := os.Stat(path); err != nil {
		return benchmark.DiffLineMap{}, fmt.Errorf("reading case %q diff: %w", c.ID, err)
	} else if fi.Size() > benchmark.MaxDiffBytes {
		return benchmark.DiffLineMap{}, fmt.Errorf("case %q diff is %d bytes, exceeding the %d-byte cap: a repo-state case's change.diff is bounded exactly like a standard-v1 one",
			c.ID, fi.Size(), benchmark.MaxDiffBytes)
	}
	raw, err := os.ReadFile(path)
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
	for _, f := range publishableSuiteIdentityArms(m.Suite, m.SuiteVersion) {
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
//
// The skipped-row fold carries the SAME caveat its standard-v1 original states
// outright: an unrecognized reviewer name keys a map entry no agent reads, exactly
// as an unrecognized REVIEWER on a well-formed row already does — so such a row is
// effectively dropped from every denominator. This copy deleted that sentence and
// kept the promise; the sentence is restored AND the drop is measured: every
// skipped row whose recovered reviewer is not in the caller's agent set is
// counted into the returned unattributed tally, which the runner surfaces as a
// warning, instead of vanishing without a trace.
func readCaseFindingsLocated(reviewDir string, agents map[string]bool) (located map[string][]benchmark.ReportedFinding, categorical map[string][]string, unattributed int, missingFindingsFile bool, err error) {
	located = map[string][]benchmark.ReportedFinding{}
	categorical = map[string][]string{}

	path := filepath.Join(reviewDir, "sources", "pool", "findings.txt")
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			// A missing findings file means the review produced NOTHING — but a
			// caller reading only the returned maps cannot distinguish that from
			// reviewers that wrote nothing. Surfaced as a flag so the runner can
			// warn; a per-reviewer diagnostics FIELD would need a RunResult schema
			// change (internal/benchmark), which this tier's file does not own.
			return located, categorical, 0, true, nil
		}
		return nil, nil, 0, false, rerr
	}
	parsed, perr := stream.ParseSource(data)
	if perr != nil {
		return nil, nil, 0, false, perr
	}
	for _, f := range parsed.Findings {
		located[f.Reviewer] = append(located[f.Reviewer], benchmark.ReportedFinding{
			File:     f.File,
			Line:     f.Line,
			Category: f.Category,
		})
		categorical[f.Reviewer] = append(categorical[f.Reviewer], f.Category)
	}
	// See skippedRowReviewer: the recovery lives in ONE place, shared with
	// readCaseFindings, so the two projections cannot attribute the same skipped
	// row to different reviewers. An unrecognized reviewer name keys a map entry
	// no agent reads — counted into unattributed rather than left silent.
	for _, s := range parsed.Skipped {
		reviewer := skippedRowReviewer(s.Content)
		if !agents[reviewer] {
			unattributed++
		}
		categorical[reviewer] = append(categorical[reviewer], "")
	}
	return located, categorical, unattributed, false, nil
}

// warnCaseFailures gives a PARTIAL run an operator surface, beside the recall
// summary whose numbers it qualifies.
//
// Without it the terminal shows a recall figure that reads exactly like a
// full-suite measurement: the failure channel is in the run-result, but an operator
// watching a ten-minute panel finish does not open the JSON to check whether the
// number covers the suite. That is the one misreading this whole feature makes
// possible, so it is reported where the number is.
//
// It writes to the passed stderr rather than the context logger for the same reason
// warnVocabularyDiagnostics does: a shortfall an operator must act on should not be
// suppressible by a log level. That applies to the retained work dir's PATH too, which
// is why retainedWorkDir is a parameter: it used to reach the operator ONLY through
// the runner's Warn line, and ATCR_LOG_LEVEL=error — a legal setting — drops that
// line, leaving the artifacts in an unnamed directory. An empty retainedWorkDir keeps
// the old wording, so a caller that has no path to offer still says the artifacts
// exist.
//
// Silent on a clean run, like every sibling summary on a suite that carries none of
// its signal.
//
// The per-case list is CAPPED, like summarizeMissing's and warnDriftingReviewers'. It
// was not, and a suite losing 200 cases to one systemic fault wrote 200 lines ahead of
// the recall summary this warning exists to qualify — scrolling the number the
// operator needs off the terminal, which is the misreading it was added to prevent.
// The scale line above the list already carries the true total, so the cap costs no
// information that matters at a glance.
func warnCaseFailures(w io.Writer, rr *benchmark.RunResult, retainedWorkDir string) {
	if rr == nil || (len(rr.CaseFailures) == 0 && len(rr.SlotFailures) == 0) {
		return
	}
	var msg strings.Builder
	if len(rr.CaseFailures) > 0 {
		fmt.Fprintf(&msg, "warning: %d of %d case(s) were UNMEASURED — an infrastructure failure stopped them being reviewed, "+
			"so they are excluded from every recall denominator rather than scored as misses:\n",
			len(rr.CaseFailures), len(rr.SuiteCaseIDs))
		named := rr.CaseFailures
		if len(named) > maxNamedFailedCases {
			named = named[:maxNamedFailedCases]
		}
		for _, f := range named {
			fmt.Fprintf(&msg, "  %s: failed at %s\n",
				stripTerminalControlRunes(f.CaseID), stripTerminalControlRunes(f.Reason))
		}
		if overflow := len(rr.CaseFailures) - len(named); overflow > 0 {
			fmt.Fprintf(&msg, "  ... and %d more\n", overflow)
		}
	}
	// The SLOT half, reported separately because it means something different: the
	// case ran and is in the other reviewers' covered sets, and only this reviewer's
	// row is short by it. Folding the two lists together would tell an operator a case
	// went unmeasured when it was measured by everyone else on the panel.
	//
	// Capped on the same terms as the case list above, and for the same reason — one
	// dead provider on a 200-case suite produces 200 slot failures, which would scroll
	// the recall summary this warning exists to qualify off the terminal.
	if len(rr.SlotFailures) > 0 {
		fmt.Fprintf(&msg, "warning: %d reviewer slot(s) were UNMEASURED — the case ran, but one reviewer could not be "+
			"shown it, so that reviewer's row is short by it rather than scored a miss:\n",
			len(rr.SlotFailures))
		namedSlots := rr.SlotFailures
		if len(namedSlots) > maxNamedFailedCases {
			namedSlots = namedSlots[:maxNamedFailedCases]
		}
		for _, sf := range namedSlots {
			fmt.Fprintf(&msg, "  %s/%s on %s: %s\n",
				stripTerminalControlRunes(sf.Model), stripTerminalControlRunes(sf.Persona),
				stripTerminalControlRunes(sf.CaseID), stripTerminalControlRunes(sf.Reason))
		}
		if overflow := len(rr.SlotFailures) - len(namedSlots); overflow > 0 {
			fmt.Fprintf(&msg, "  ... and %d more\n", overflow)
		}
	}
	if retainedWorkDir != "" {
		fmt.Fprintf(&msg, "  The work dir is retained at %s — the scored cases' review artifacts "+
			"survive there for inspection or manual rescoring.\n", retainedWorkDir)
	} else {
		// Stated as a CHECK, not as a fact. With no path in hand this function cannot
		// observe whether retention happened: the deferred cleanup's own RemoveAll
		// failure is warned rather than acted on, and a second caller — or a future
		// path building a RunResult from a checkpoint-like source — would otherwise
		// print a guarantee nobody verified.
		msg.WriteString("  If the run log reports a retained work dir, the scored cases' review artifacts " +
			"are there for inspection or manual rescoring.\n")
	}
	_, _ = io.WriteString(w, msg.String())
}
