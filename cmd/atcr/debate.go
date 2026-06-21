package main

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/samestrin/atcr/internal/debate"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/spf13/cobra"
)

// newDebateCmd builds `atcr debate`: cross-examine a review's disputed findings
// (severity splits, gray-zone clusters, verification disagreements) through a
// bounded proposer/challenger/judge debate and integrate the rulings. It is the
// standalone counterpart to `atcr review --verify --debate` and shares the same
// internal/debate.Debate orchestration as the atcr_debate MCP tool. Runs after
// `atcr reconcile` (and, for verification disagreements, after `atcr verify`).
func newDebateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debate [id-or-path]",
		Short: "Cross-examine disputed findings (proposer/challenger/judge)",
		Long: "Resolve reviewer disagreements through a bounded cross-examination: where reviewers " +
			"dispute severity, the reconciler leaves a gray-zone cluster, or a skeptic vote ties, a " +
			"proposer defends the finding, a challenger (a different model) attacks, and a judge (a third " +
			"distinct model) rules. The ruling replaces severity-max and settles the finding; survivors " +
			"are marked challenge-survived. Reads reconciled/findings.json; writes reconciled/debate.json " +
			"and per-item transcripts under debate/.",
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: runDebateCmd,
	}
	cmd.Flags().Bool("single-model", false,
		"allow the same-model persona fallback when fewer than 3 distinct models are available across the proposer/challenger/judge roles (default: skip and record unresolved)")
	return cmd
}

func runDebateCmd(cmd *cobra.Command, args []string) error {
	arg := ""
	if len(args) == 1 {
		arg = args[0]
	}
	reviewDir, err := resolveReviewDir(arg)
	if err != nil {
		return debateFailureError(err) // missing/incomplete review → exit 2
	}

	cfg, err := fanout.LoadReviewConfig(".", registry.CLIOverrides{})
	if err != nil {
		return usageError(err) // missing/invalid registry → exit 2
	}

	res, err := debate.Debate(cmd.Context(), ".", reviewDir, cfg.Registry, debate.Options{
		SingleModel: boolFlag(cmd, "single-model"),
	})
	if err != nil {
		if errors.Is(err, debate.ErrNoReconciledFindings) {
			return fmt.Errorf("no reconciled findings found in %s — run 'atcr reconcile' first", reviewDir)
		}
		return usageError(err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"debated %d item(s): %d upheld, %d overturned, %d split, %d unresolved (%d overflow) -> %s\n",
		res.Selected, res.Upheld, res.Overturned, res.Split, res.Unresolved, res.Overflow,
		filepath.Join(reviewDir, "reconciled"))
	return nil
}

// debateFailureError wraps a non-ErrNoReconciledFindings error from debate.Debate
// with a consistent "debate failed:" prefix so `atcr debate` and
// `atcr review --debate` produce identical stderr shapes. Both map to exit 2.
func debateFailureError(err error) error {
	return usageError(fmt.Errorf("debate failed: %w", err))
}
