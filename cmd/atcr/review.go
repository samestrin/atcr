package main

import (
	"fmt"
	"os"
	"strings"
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

	// Run the two review phases separately so build-phase failures (persona
	// resolution, unknown provider, prompt render — configuration errors per
	// AC 03-02) map to exit 2, while an all-agents-failed execution stays the
	// plain exit 1 with artifacts preserved on disk.
	prep, err := fanout.PrepareReview(ctx, cfg, req)
	if err != nil {
		return usageError(err)
	}
	if err := preflightAPIKeys(prep.Slots); err != nil {
		return err // no slot can authenticate → exit 2 before any provider call
	}

	result, err := fanout.ExecuteReview(ctx, llmclient.New(), prep)
	if result != nil {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "review %s: %d/%d agents succeeded (%s)\n",
			result.ID, result.Summary.Succeeded, result.Summary.Total, result.Dir)
	}
	if err != nil {
		return err // all-agents-failed → exit 1, artifacts preserved
	}

	// One-shot mode: reconcile in-process and gate on the threshold. Review
	// artifacts are already on disk, so a reconcile failure (exit 2) preserves
	// them for inspection (AC 03-02 Error Scenario 3).
	if threshold != "" {
		rec, rerr := reconcile.RunReconcile(cmd.Context(), result.Dir, nil, reconcile.Options{
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

// preflightAPIKeys fails fast (exit 2, per AC 03-02 Error Scenario 1) when no
// slot's chain — primary plus fallbacks — has its API key env var set: the
// fan-out cannot possibly produce a single success. Any one keyed agent
// anywhere lets the run proceed, because keys resolve per-invocation and
// partial success (≥1 agent) is a binding exit-0 contract. Runs after
// PrepareReview (slots carry the resolved chains), so a doomed run leaves its
// scaffolded review dir behind — consistent with the artifacts-preserved
// contract, and reconcile/report reject in-progress reviews.
func preflightAPIKeys(slots []fanout.Slot) error {
	seen := map[string]bool{}
	var missing []string
	for _, s := range slots {
		for _, a := range append([]fanout.Agent{s.Primary}, s.Fallbacks...) {
			env := a.Invocation.APIKeyEnv
			if os.Getenv(env) != "" {
				return nil
			}
			if !seen[env] {
				seen[env] = true
				missing = append(missing, env)
			}
		}
	}
	if len(missing) == 0 {
		return nil // empty roster is rejected earlier by PrepareReview
	}
	return usageError(fmt.Errorf("API key env var not set: %s", strings.Join(missing, ", ")))
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
