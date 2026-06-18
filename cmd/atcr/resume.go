package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/gitrange"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
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

	dir, err := resolveResumeDir(anchor)
	if err != nil {
		return usageError(err)
	}

	base, _ := cmd.Flags().GetString("base")
	head, _ := cmd.Flags().GetString("head")
	mergeCommit, _ := cmd.Flags().GetString("merge-commit")
	res, err := gitrange.Resolve(ctx, ".", gitrange.Options{Base: base, Head: head, MergeCommit: mergeCommit})
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return interruptedBeforeFanout(cmd)
		}
		return usageError(fmt.Errorf("resume failed: %w", err))
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
	// review path so the resume flow never leaks secrets or absolute paths.
	ctx = correlateReviewID(ctx, prep.ID)
	ctx = log.NewContext(ctx, log.WithRedactor(log.FromContext(ctx), log.NewRedactor(resolveRedactRoot(ctx, prep.Repo))))

	// AC2: nothing pending — re-run reconciliation against the complete review and
	// exit clean, never touching a provider. Clear any stale interrupt marker first
	// so a review that was interrupted-but-actually-complete reports completed
	// rather than interrupted (AC6).
	if info.AllComplete() {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "All configured agents already completed. Re-running reconciliation...")
		if err := fanout.ClearInterrupted(dir); err != nil {
			return usageError(fmt.Errorf("resume failed: %w", err))
		}
		return resumeReconcile(ctx, cmd, dir)
	}

	if err := preflightAPIKeys(prep.Slots); err != nil {
		return err // no pending slot can authenticate → exit 2 before any provider call
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "resuming review %s: %d completed, %d pending (%s)\n",
		prep.ID, len(info.Completed), len(info.Pending), strings.Join(info.Pending, ", "))

	result, err := fanout.ExecuteResume(ctx, llmclient.New(), prep)

	// Graceful interrupt during the resumed fan-out (AC7): the new partial results
	// are already persisted and the manifest is re-marked interrupted, so report
	// what was saved and stop. Checked before err so an interrupted resume is never
	// reported as a clean completion. Exit 1, consistent with a fresh review.
	if errors.Is(ctx.Err(), context.Canceled) {
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), interruptMessage(result, prep))
		return &codedError{code: exitFailure, err: errors.New("review interrupted")}
	}

	if result != nil {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "review %s: %d/%d agents succeeded (%s)\n",
			result.ID, result.Summary.Succeeded, result.Summary.Total, result.Dir)
	}
	if err != nil {
		return err // every agent (union) failed → exit 1, artifacts preserved
	}

	// Auto-reconcile on successful completion (epic 4.1.1: a resumed run always
	// produces a fresh reconciliation, mirroring the in-process one-shot path).
	return resumeReconcile(ctx, cmd, result.Dir)
}

// resumeReconcile runs the deterministic reconcile pipeline against dir and prints
// the merged finding count, mapping a reconcile failure to a usage error (exit 2)
// with the on-disk review preserved for inspection. The partial flag is read from
// the just-finalized review so reconcile records the run's partial provenance.
func resumeReconcile(ctx context.Context, cmd *cobra.Command, dir string) error {
	rec, err := reconcile.RunReconcile(ctx, dir, nil, reconcile.Options{
		ReconciledAt: time.Now(),
		Partial:      fanout.ReadManifestPartial(dir),
	})
	if err != nil {
		return usageError(fmt.Errorf("resume failed: %w", err))
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "reconciled %d finding(s)\n", rec.Summary.TotalFindings)
	return nil
}
