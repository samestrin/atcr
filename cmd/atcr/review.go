package main

import (
	"fmt"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/gitrange"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/spf13/cobra"
)

// newReviewCmd builds `atcr review`: resolve the git range, build payloads,
// create the review directory, and fan out to the persona pool.
func newReviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Fan a code change out to the reviewer pool",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runReview,
	}
	cmd.Flags().String("id", "", "review id (default: <YYYY-MM-DD>_<branch-slug>)")
	cmd.Flags().String("payload", "", "payload mode override: diff, blocks, or files")
	cmd.Flags().Int("timeout", 0, "global timeout in seconds (overrides config)")
	cmd.Flags().String("fail-on", "", "one-shot: review + reconcile, then exit 1 if any finding at/above this severity survives")
	addRangeFlags(cmd)
	return cmd
}

// runReview resolves the range, loads config, and runs the full review flow.
// Range/config problems are usage errors (exit 2); an all-agents-failed review
// is a plain failure (exit 1) with the artifacts preserved on disk.
func runReview(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	base, _ := cmd.Flags().GetString("base")
	head, _ := cmd.Flags().GetString("head")
	mergeCommit, _ := cmd.Flags().GetString("merge-commit")
	idOverride, _ := cmd.Flags().GetString("id")

	// Validate --fail-on before any review work (no wasted API calls on a bad
	// threshold), per AC 03-02 Security.
	threshold, err := failOnThreshold(cmd)
	if err != nil {
		return err
	}

	res, err := gitrange.Resolve(ctx, ".", gitrange.Options{Base: base, Head: head, MergeCommit: mergeCommit})
	if err != nil {
		// A range failure aborts the pipeline before any agent runs — a usage
		// error (exit 2), per AC 03-02 Error Scenario 2 ("review failed: ...").
		return usageError(fmt.Errorf("review failed: %w", err))
	}

	cfg, err := fanout.LoadReviewConfig(".", cliOverrides(cmd))
	if err != nil {
		return usageError(err) // missing/invalid config → exit 2
	}

	now := time.Now()
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
		Branch:     gitrange.CurrentBranch(ctx, "."),
		Date:       now.Format("2006-01-02"),
		TimeSuffix: now.Format("150405"),
		StartedAt:  now,
		IDOverride: idOverride,
	}

	result, err := fanout.RunReview(ctx, llmclient.New(), cfg, req)
	if result != nil {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "review %s: %d/%d agents succeeded (%s)\n",
			result.ID, result.Summary.Succeeded, result.Summary.Total, result.Dir)
	}
	if err != nil {
		return err // all-agents-failed (exit 1) or range/config (exit 2)
	}

	// One-shot mode: reconcile in-process and gate on the threshold. Review
	// artifacts are already on disk, so a reconcile failure (exit 2) preserves
	// them for inspection (AC 03-02 Error Scenario 3).
	if threshold != "" {
		rec, rerr := reconcile.RunReconcile(result.Dir, nil, reconcile.Options{
			ReconciledAt: time.Now(),
			Partial:      result.Summary.Partial,
		})
		if rerr != nil {
			return usageError(fmt.Errorf("review failed: %w", rerr))
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "reconciled %d finding(s)\n", rec.Summary.TotalFindings)
		return gateFindings(rec, threshold)
	}
	return nil
}

// cliOverrides reads the shared-settings flags actually set on cmd.
func cliOverrides(cmd *cobra.Command) registry.CLIOverrides {
	var o registry.CLIOverrides
	if cmd.Flags().Changed("payload") {
		v, _ := cmd.Flags().GetString("payload")
		o.PayloadMode = &v
	}
	if cmd.Flags().Changed("timeout") {
		v, _ := cmd.Flags().GetInt("timeout")
		o.TimeoutSecs = &v
	}
	return o
}
