package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/samestrin/atcr/internal/audit"
	"github.com/spf13/cobra"
)

// newAuditReportCmd builds `atcr audit-report --pr <n>`: read the append-only
// audit ledger at .atcr/audit.log.jsonl, select the review runs recorded for the
// given PR, and print a one-page markdown compliance report (Epic 19.1). A PR
// with no recorded runs exits non-zero with a clear message (AC3).
func newAuditReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit-report",
		Short: "Render a one-page compliance report for a PR's review runs",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runAuditReport,
	}
	cmd.Flags().Int("pr", 0, "pull-request number to report on (required)")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}

func runAuditReport(cmd *cobra.Command, _ []string) error {
	pr, _ := cmd.Flags().GetInt("pr")

	root, err := repoRoot()
	if err != nil {
		return usageError(fmt.Errorf("resolving repo root: %w", err))
	}
	auditPath := filepath.Join(root, ".atcr", "audit.log.jsonl")
	recs, err := audit.Load(auditPath)
	if err != nil {
		return usageError(err) // corrupt/unreadable ledger (exit 2)
	}

	forPR := make([]audit.Record, 0, len(recs))
	for _, r := range recs {
		if r.PR == pr {
			forPR = append(forPR, r)
		}
	}
	if len(forPR) == 0 {
		// AC3: an unknown --pr (a PR with no recorded runs, or an absent ledger)
		// exits non-zero with a clear, actionable message.
		return fmt.Errorf("no audit records found for PR #%d — run 'atcr review --pr %d' first, or verify the PR number", pr, pr)
	}

	_, _ = fmt.Fprint(cmd.OutOrStdout(), audit.RenderReport(forPR, pr, time.Now()))
	return nil
}
