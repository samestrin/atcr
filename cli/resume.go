package cli

import (
	"context"
	"errors"
	"fmt"
	reclib "github.com/samestrin/atcr/reconcile"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/gitrange"
	"github.com/samestrin/atcr/internal/history"
	"github.com/samestrin/atcr/internal/hookobs"
	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/metrics"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/spf13/cobra"
)

// resolveResumeDir maps the --resume anchor to a review directory. The literal
// "latest" (and an empty value) resolve the .atcr/latest pointer, matching the
// documented `atcr review --resume latest` form; any other value is delegated to
// resolveReviewDir, which accepts a bare review id (resolved under
// .atcr/reviews/) or an explicit path used verbatim, and verifies the directory
// holds a sources/ tree (otherwise a clear exit-2 usage error). Note that an
// explicit anchor of "latest" can never be a real id: ReviewID always prefixes
// the date (<YYYY-MM-DD>_<slug>), so reserving the word for the pointer is safe.
func resolveResumeDir(anchor string) (string, error) {
	a := strings.TrimSpace(anchor)
	if a == "" || a == "latest" {
		return resolveReviewDir("") // empty arg → .atcr/latest
	}
	return resolveReviewDir(a)
}

// runResume drives `atcr review --resume <anchor>`: it resolves the target review
// directory, re-resolves the current git range (using any --base/--head/
// --merge-commit flags, exactly as a fresh review would), and hands both to
// PrepareResume, which locks the range and roster against the interrupted run.
// Every pre-fan-out validation problem — a changed range (AC3), a changed roster,
// a missing/corrupt manifest, or a bad config — is a usage error (exit 2). When
// every agent already completed, it re-runs reconciliation and exits clean (AC2);
// otherwise it fans out only the pending agents, then auto-reconciles. A
// SIGINT/SIGTERM during the resumed fan-out preserves the new partial results and
// the interrupted marker (AC7), exiting 1 with the same notice as a fresh review.
func runResume(cmd *cobra.Command, anchor string) error {
	ctx := cmd.Context()
	// --axi gates resume's human-oriented stdout writes and swaps the summary block
	// for the shared token-dense payload, read from the same context value review.go
	// uses so review/resume stay in lockstep with one flag parse (AC 01-04).
	axiMode := axiFromContext(ctx)

	// --resume targets an existing review; --id and --output-dir only make sense
	// when creating a new one, so reject the combination up front (exit 2).
	if cmd.Flags().Changed("id") || cmd.Flags().Changed("output-dir") {
		return usageError(errors.New("--resume cannot be combined with --id or --output-dir"))
	}

	// The one-shot gate/verify flags drive an exit-code gate on a fresh review;
	// --resume always reconciles but deliberately does NOT re-implement that gate
	// (out of scope). Silently ignoring them would let a CI pipeline resume a
	// review and false-PASS despite surviving findings, so reject them fail-closed
	// (exit 2) and point at the standalone gate commands.
	for _, f := range []string{"fail-on", "verify", "require-verified"} {
		if cmd.Flags().Changed(f) {
			return usageError(fmt.Errorf("--resume does not support --%s; resume reconciles automatically — run `atcr reconcile --fail-on <severity>` or `atcr verify` afterward to gate", f))
		}
	}

	// --thorough and --min-severity only apply to the --verify stage; --verify is
	// already rejected above, so silently accepting them would discard the flag
	// without any feedback to the user.
	for _, f := range []string{"thorough", "min-severity"} {
		if cmd.Flags().Changed(f) {
			return usageError(fmt.Errorf("--resume does not support --%s; this flag only applies to --verify, which is not supported with --resume", f))
		}
	}

	// --fresh has two fresh-review meanings — scoping the --verify stage and
	// (since Sprint 35.0) bypassing the baseline file-hash skip (review.go) —
	// and a resume honors neither: it re-reviews the resumed review's pending
	// agents rather than re-scanning the repo. Reject with a reason naming both
	// meanings so a baseline-resume user is not told the flag is verify-only.
	if cmd.Flags().Changed("fresh") {
		return usageError(errors.New("--resume does not support --fresh; --fresh applies to the --verify stage and to fresh baseline scans (bypassing the file-hash skip) — neither is honored on a resume"))
	}

	// --auto-fix, --debate, --single-model, and --exec all parse on the shared
	// review command but are read only by runReview's fresh path: with --resume
	// the write-back remediation, cross-examination stage, and sandbox
	// reproduction would be silently dropped with zero feedback. Reject
	// fail-closed (exit 2), pointing at the standalone command.
	resumeStandalone := map[string]string{
		"auto-fix":     "run `atcr review --auto-fix` on a fresh review instead",
		"debate":       "run `atcr debate` afterward instead",
		"single-model": "this flag only applies to --debate — run `atcr debate` afterward instead",
		"exec":         "this flag only applies to --verify — run `atcr verify --exec` afterward instead",
		// --all selects a fresh full-repository scan; a resume continues whatever mode
		// the original review ran (baseline-ness comes from the manifest, not this flag),
		// so accepting --all here would silently ignore it (Sprint 35.0 phase-2 gate LOW).
		"all": "--all starts a fresh full-repository review; a resume continues the original review's mode",
		// --dir selects a fresh scoped baseline scan; like --all, the scope comes from
		// the resumed review's manifest, not this flag, so accepting it here would
		// silently ignore it (Sprint 35.0, Story 2 — same fresh-only rationale as --all).
		"dir": "--dir starts a fresh scoped baseline review; a resume continues the original review's mode",
	}
	for _, f := range []string{"auto-fix", "debate", "single-model", "exec", "all", "dir"} {
		if cmd.Flags().Changed(f) {
			return usageError(fmt.Errorf("--resume does not support --%s; %s", f, resumeStandalone[f]))
		}
	}

	// Resolve the consensus level after the flag-rejection block (argument errors
	// precede config errors) but still before any resume work: a bad configured
	// value is a usage error (exit 2), and resolving it inside resumeReconcile
	// would surface it only after the resumed fan-out completed. `resume` has no
	// --consensus flag (epic 35.9.1 scope), so this reads the config/registry
	// tiers only.
	consensusLevel, err := resolveConsensusLevel("")
	if err != nil {
		return err
	}
	// Record the effective level for the same reason cli/reconcile.go does: it
	// can come from ~/.config/atcr/registry.yaml with nothing local naming it,
	// and ConsensusFiltered == 0 alone cannot distinguish "off" from "strict with
	// nothing to filter". Logged at resolve time, not post-run, so the level is
	// still recorded when the resumed fan-out or re-reconcile below fails.
	log.FromContext(cmd.Context()).Info("consensus filter level resolved", "consensus", consensusLevel)

	dir, err := resolveResumeDir(anchor)
	if err != nil {
		return usageError(err)
	}

	// Read the manifest before resolving a git range: a baseline (--all/--dir) review
	// has no range, so re-resolving one and validating it against the manifest's empty
	// Base/Head would always fail ErrRangeChanged (Sprint 35.0). For a baseline review
	// resume with a zero Range; PrepareResume rebuilds its payload from the repo walker.
	m, err := fanout.ReadManifest(dir)
	if err != nil {
		return usageError(fmt.Errorf("resume failed: %w", err))
	}
	res := &gitrange.Resolution{}
	if !m.Baseline {
		base, _ := cmd.Flags().GetString("base")
		head, _ := cmd.Flags().GetString("head")
		mergeCommit, _ := cmd.Flags().GetString("merge-commit")
		res, err = gitrange.Resolve(ctx, ".", gitrange.Options{Base: base, Head: head, MergeCommit: mergeCommit})
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return interruptedBeforeFanout(cmd)
			}
			return usageError(fmt.Errorf("resume failed: %w", err))
		}
		if res == nil {
			return usageError(errors.New("resume failed: git range returned no result"))
		}
	} else {
		// A baseline review has no diff range, so the range flags a non-baseline
		// resume consumes and validates (ErrRangeChanged) would be silently
		// ignored here — the one mode-flag class that otherwise slips the
		// fail-closed net above. Reject them like --all/--dir (Sprint 35.0 TD).
		for _, f := range []string{"base", "head", "merge-commit"} {
			if cmd.Flags().Changed(f) {
				return usageError(fmt.Errorf("--resume does not support --%s on a baseline review; a resume continues the original review's range and the baseline review has none", f))
			}
		}
	}

	cfg, err := fanout.LoadReviewConfig(".", cliOverrides(cmd))
	if err != nil {
		return usageError(err)
	}
	if banner := cfg.Registry.ProjectProviderBanner(); banner != "" {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), banner)
	}

	req := fanout.ReviewRequest{
		Repo: ".",
		Root: ".",
		Range: fanout.ReviewRange{
			Base:          res.Base,
			Head:          res.Head,
			DetectionMode: res.DetectionMode,
			DefaultBranch: res.DefaultBranch,
			CommitCount:   res.CommitCount,
		},
		StartedAt: time.Now(),
		PRNumber:  prNumberFromFlags(cmd),
	}

	prep, info, err := fanout.PrepareResume(ctx, cfg, dir, req)
	if err != nil {
		// Range/roster mismatch (AC3) and every other pre-fan-out validation
		// problem is a usage/configuration error (exit 2). The interrupt path is not
		// reachable here: PrepareResume performs no long-running work.
		return usageError(err)
	}

	// Correlate every downstream log line by review id and enforce sink-level
	// redaction (scrub secret-shaped tokens, relativize absolute paths under the
	// repo root) for the resumed fan-out and reconcile — parity with the fresh
	// review path so the resume flow never leaks secrets or absolute paths,
	// including the resolved registry key values (epic 4.9). Shared with runReview
	// via correlateAndRedact so the contract can't drift.
	secrets, secretWarnings := prep.SecretValues()
	ctx = correlateAndRedact(ctx, prep.ID, prep.Repo, secrets...)
	// Audit identity (Epic 35.0), mirroring runReview.
	ctx = hookobs.WithCall(ctx, hookobs.Call{RunID: prep.ID, Stage: "resume"})
	for _, w := range secretWarnings {
		log.FromContext(ctx).Debug(w)
	}

	// Resolve the repo root once so both the AllComplete re-reconcile path and the
	// normal completion path append history to the same repo-root-relative shard
	// the `atcr history` read path uses — never a CWD-relative dir a subdir run
	// writes but never reads back. Fall back to CWD on resolution failure,
	// preserving the prior behavior.
	histRoot, herr := repoRoot()
	if herr != nil {
		histRoot = "."
	}

	// AC2: nothing pending — re-run reconciliation against the complete review and
	// exit clean, never touching a provider. Clear any stale interrupt marker first
	// so a review that was interrupted-but-actually-complete reports completed
	// rather than interrupted (AC6).
	if info.AllComplete() {
		// Gated under --axi (AC 01-04 Edge Case 1): stdout stays payload-only. The
		// human announce is suppressed and the run-summary payload below takes its
		// place so the axi stream is never empty on this path.
		if !axiMode {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "All configured agents already completed. Re-running reconciliation...")
		}
		if err := fanout.ClearInterrupted(dir); err != nil {
			return usageError(fmt.Errorf("resume failed: %w", err))
		}
		reconciledTotal, err := resumeReconcile(ctx, cmd, dir, consensusLevel)
		if err != nil {
			return err
		}
		recordHistory(ctx, histRoot, dir, req.StartedAt)
		if axiMode {
			// This path runs no fan-out, so there is no metrics delta to report: the
			// payload carries the already-complete agent set (all succeeded) and the
			// just-reconciled findings total.
			if werr := writeReviewSummaryAXI(cmd.OutOrStdout(), prep.ID, dir, summarySnapshot{
				agentsSucceeded: int64(len(info.Completed)),
				agentsTotal:     int64(len(info.Completed)),
				findingsTotal:   int64(reconciledTotal),
			}); werr != nil {
				return fmt.Errorf("axi output rendering failed: %w", werr)
			}
		}
		// AllComplete re-reconciles an already-completed review; no new review work
		// was performed, so do not append another audit record.
		return nil
	}

	if err := preflightAPIKeys(prep.Slots); err != nil {
		return err // no pending slot can authenticate → exit 2 before any provider call
	}

	if !axiMode { // gated under --axi: stdout stays payload-only
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "resuming review %s: %d completed, %d pending (%s)\n",
			prep.ID, len(info.Completed), len(info.Pending), strings.Join(info.Pending, ", "))
	}

	// Snapshot the summary counters before the resumed fan-out so the end-of-review
	// summary reports only this resume's contribution, mirroring runReview.
	metricsBaseline := snapshotSummaryMetrics(metrics.DefaultRegistry)

	result, err := fanout.ExecuteResume(ctx, newCompleter(ctx), prep)

	// Graceful interrupt during the resumed fan-out (AC7): the new partial results
	// are already persisted and the manifest is re-marked interrupted, so report
	// what was saved and stop. Checked before err so an interrupted resume is never
	// reported as a clean completion. Exit 1, consistent with a fresh review.
	if errors.Is(ctx.Err(), context.Canceled) {
		return reportInterrupt(cmd, ctx, result, prep)
	}

	// axiWerr captures a broken-stdout run-summary payload write (axi mode only) so
	// the history/audit ledgers below are recorded before the fault is surfaced —
	// the same ledgers-first ordering runReview uses.
	var axiWerr error
	if result != nil {
		// End-of-review metrics summary (Epic 4.4 AC3), mirroring runReview: emitted
		// before the all-agents-failed error guard so a fully-failed resume still
		// reports the breakdown. Under --axi the human outcome + summary lines are
		// replaced by the one shared token-dense payload, byte-identical to
		// review --axi for equivalent data (AC 01-04); a write fault stays unwrapped
		// (exitFailure, AC 02-02 Error Scenario 3).
		summaryDelta := snapshotSummaryMetrics(metrics.DefaultRegistry).sub(metricsBaseline)
		if axiMode {
			// A write failure here (a broken stdout, e.g. a closed pipe) is captured
			// and surfaced only after the history/audit ledgers are written below, so
			// a closed pipe cannot cost the run its compliance record; the fault
			// stays unwrapped → exitFailure (1) (AC 02-02 Error Scenario 3).
			if werr := writeReviewSummaryAXI(cmd.OutOrStdout(), result.ID, result.Dir, summaryDelta); werr != nil {
				axiWerr = fmt.Errorf("axi output rendering failed: %w", werr)
			}
		} else {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "review %s: %d/%d agents succeeded (%s)\n",
				result.ID, result.Summary.Succeeded, result.Summary.Total, result.Dir)
			writeReviewSummary(cmd.OutOrStdout(), summaryDelta, time.Since(req.StartedAt))
		}
	}
	if err != nil {
		// An empty union is a usage/state error (exit 2), consistent with the
		// other pre-fan-out state errors above — not the exit-1 "agents failed"
		// path. Effectively unreachable once there are pending slots, but mapped
		// for consistency.
		if errors.Is(err, fanout.ErrEmptyRoster) {
			return usageError(err)
		}
		return err // every agent (union) failed → exit 1, artifacts preserved
	}

	// TD-011: a resumed BASELINE run persists the incremental file-hash index on
	// successful completion, mirroring the fresh runReview write-back gate so the
	// next --all/--dir skips unchanged files instead of re-scanning everything. The
	// gate is now literally the same function both paths call, so the two can no
	// longer drift (which is what TD-011 was raised for). One resume-specific note:
	// the resume attributes coverage only from the slots IT dispatched (the pending
	// personas), so a file a previously-completed persona reviewed may still be
	// re-scanned — deliberate fail-open, documented at the runEngine seam.
	// The interrupt path returned above, so this never fires on a cancelled run.
	commitBaselineWriteback(ctx, m.Baseline, prep, result)

	// Auto-reconcile on successful completion (epic 4.1.1: a resumed run always
	// produces a fresh reconciliation, mirroring the in-process one-shot path).
	if _, err := resumeReconcile(ctx, cmd, result.Dir, consensusLevel); err != nil {
		return err
	}
	recordHistory(ctx, histRoot, result.Dir, req.StartedAt)
	recordResumeAudit(cmd, ctx, result.Dir, req.StartedAt, req.PRNumber, req.Range.Base, req.Range.Head)
	// Surface a captured broken-stdout payload write only now that the ledgers are
	// recorded: exit 1 with the render fault (never usageError).
	if axiWerr != nil {
		return axiWerr
	}
	return nil
}

// recordHistory persists a review's pool findings (from dir) to the append-only
// monthly history shard under root, shared by `atcr review` and `atcr resume` so
// the two paths cannot drift on shard location, root resolution, or logging. The
// shard dir is derived from root — the resolved repo root — so writes land where
// `atcr history` reads (the read path resolves the same root via ShardDir),
// regardless of the caller's CWD. A history write failure is non-fatal: it must
// never fail an otherwise-successful review or resume, so it is logged and
// swallowed.
func recordHistory(ctx context.Context, root, dir string, ts time.Time) {
	histPath := history.ShardPath(history.ShardDir(root), ts)
	if n, err := history.RecordReview(histPath, dir, ts); err != nil {
		log.FromContext(ctx).Warn("failed to append finding history", "error", err)
	} else if n > 0 {
		log.FromContext(ctx).Debug("appended finding history", "records", n, "path", histPath)
	}
}

// recordResumeAudit appends a resumed review's audit record to the append-only
// compliance ledger, mirroring the fresh-review hook in review.go so a resumed
// run is recorded exactly once (Epic 19.1 AC1). Like the history write it is
// non-fatal — a compliance write must never fail an otherwise-successful resume,
// so it is logged and swallowed. A write failure is also surfaced on stderr so
// it cannot be missed in log-only output.
func recordResumeAudit(cmd *cobra.Command, ctx context.Context, dir string, ts time.Time, pr int, base, head string) {
	auditPath := filepath.Join(".", ".atcr", "audit.log.jsonl")
	writeAuditRecord(cmd.ErrOrStderr(), ctx, auditPath, dir, ts, pr, base, head)
}

// resumeReconcile runs the deterministic reconcile pipeline against dir, prints
// the merged finding count (gated under --axi), and returns that count so the
// AllComplete path can carry it in the run-summary payload. A reconcile failure
// maps to a usage error (exit 2) with the on-disk review preserved for inspection.
// The partial flag is read from the just-finalized review so reconcile records the
// run's partial provenance.
func resumeReconcile(ctx context.Context, cmd *cobra.Command, dir, consensusLevel string) (int, error) {
	// Config/registry tiers only — `resume` has no --consensus flag (epic 35.9.1
	// scope). It reconciles and persists like `atcr reconcile` does, so leaving
	// Consensus unresolved here would silently ignore a configured level. The
	// level was validated up front in runResume, so this cannot fail late.
	rec, err := reconcile.RunReconcile(ctx, dir, nil, reclib.Options{
		ReconciledAt: time.Now(),
		Partial:      fanout.ReadManifestPartial(dir),
		Root:         ".", // repo root = CWD; validate finding file paths (Epic 5.0)
		TrustPriors:  scorecard.ResolveTrustPriors(),
		Consensus:    consensusLevel,
	})
	if err != nil {
		return 0, usageError(fmt.Errorf("resume failed: %w", err))
	}
	// Mirrors the `atcr reconcile` line: the priors come from a 180d windowed
	// store read (epic 35.11), so a reviewer with no runs inside the window drops
	// out of the map and silently loses trust exemption/demotion. A drop in this
	// count between runs is the signal. Logged (not printed) so the --axi
	// stdout contract is untouched.
	log.FromContext(ctx).Info("trust priors resolved", "reviewers", rec.Summary.TrustPriorsResolved)
	// Persist the resumed reconcile's findings to the local TD store, mirroring
	// `atcr reconcile`'s persistLocalDebt through the same shared bridge (TD
	// cli/resume.go:382): this auto-reconcile used to persist nothing, so a
	// resumed review's findings never reached the backlog through `atcr resume`
	// at all — the resume-time root backfill's stated payoff was only reachable
	// via a separate `atcr reconcile`. No suppression flag exists on this
	// command (the same asymmetry as the MCP handler); AutoCompact stays zero.
	// Best-effort: persistence never fails the resume or changes its exit code.
	if root, ok := localdebt.ResolveStoreRoot(localdebt.RootOpts{
		ReviewDir: dir, AllowCWD: true, Diag: cmd.ErrOrStderr(),
	}); ok {
		localdebt.PersistForReconcile(dir, rec, localdebt.PersistOpts{Root: root, Diag: cmd.ErrOrStderr()})
	}
	// Shared by the AllComplete and pending paths; gated under --axi (read from the
	// same context value) so both resume routes keep stdout payload-only (AC 01-04).
	if !axiFromContext(ctx) {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "reconciled %d finding(s)\n", rec.Summary.TotalFindings)
	}
	return rec.Summary.TotalFindings, nil
}
