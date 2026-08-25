package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/localdebt"
)

// newDebtBackfillCmd builds `atcr debt backfill-justifications`: a ONE-OFF repair for
// the justifications already on disk.
//
// It is a separate command rather than a step inside reconcile because the repair and
// the write path have opposite growth characteristics. Record.StampID excludes
// Justification, so a re-detected finding keeps its id and PersistForReconcile skips
// the append — which is exactly what bounds store size by finding count instead of by
// review count. Refreshing excerpts on every reconcile would give that up to catch a
// reviewer rewording its narrative. Running the repair once, deliberately, does not.
func newDebtBackfillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backfill-justifications",
		Short: "Replay stored justifications from their source review.md (one-off repair)",
		Long: "atcr debt backfill-justifications re-derives each open or wontfix record's\n" +
			"justification from the review.md it was originally stamped from, and rewrites\n" +
			"the ones that changed.\n\n" +
			"It exists because a record's id excludes its justification: a re-detected\n" +
			"finding hashes to the same id and is deduped away, so an improvement to the\n" +
			"extractor reaches only records persisted after it. Excerpts already in the\n" +
			"store keep their original text — including the marker-free ones that\n" +
			"`debt resolve --status wontfix` accepts as a complete audit trail.\n\n" +
			"A record is repaired only when exactly one surviving review.md anchors it.\n" +
			"source_report paths are review-dir-relative, so several reviews hold the same\n" +
			"relative path; a record whose candidates disagree is left alone rather than\n" +
			"rewritten from a guess. Run --dry-run first.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runDebtBackfill,
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().String("review-root", "", "directory to search for source review.md files; unset resolves to the repo root")
	cmd.Flags().Bool("dry-run", false, "report what would be rewritten without touching the store")
	return cmd
}

func runDebtBackfill(cmd *cobra.Command, _ []string) error {
	dir := debtStoreDir(cmd)
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	reviewRoot, _ := cmd.Flags().GetString("review-root")
	if reviewRoot == "" {
		root, err := repoRoot()
		if err != nil {
			return fmt.Errorf("backfill-justifications: no --review-root given and the repo root could not be resolved: %w", err)
		}
		reviewRoot = root
	}

	res, err := localdebt.BackfillJustifications(dir, reviewRoot, dryRun)
	if err != nil {
		return fmt.Errorf("backfill-justifications: %w", err)
	}

	prefix := ""
	if dryRun {
		prefix = "dry run: "
	}
	// Every counter is printed, including the zeros. Unresolved and Ambiguous are
	// the two the operator may need to act on — a pruned review tree, or a repo
	// holding several reviews that anchor one finding — and a counter that appears
	// only when non-zero reads as "not checked" rather than "checked, none found".
	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"%s%d scanned, %d rewritten, %d unchanged, %d unresolved (no surviving review.md), %d ambiguous (candidates disagreed)\n",
		prefix, res.Scanned, res.Rewritten, res.Unchanged, res.Unresolved, res.Ambiguous)
	return nil
}
