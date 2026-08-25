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
		Long: "atcr debt backfill-justifications re-derives each LIVE record's justification\n" +
			"from the review.md it was originally stamped from, and rewrites the ones that\n" +
			"changed. Live means open or deferred: a resolved or wontfix record is settled,\n" +
			"and its justification may be the operator's --reason rather than a review\n" +
			"excerpt, which nothing can replay.\n\n" +
			"It exists because a record's id excludes its justification: a re-detected\n" +
			"finding hashes to the same id and is deduped away, so an improvement to the\n" +
			"extractor reaches only records persisted after it. Excerpts already in the\n" +
			"store keep their original text — including the marker-free ones that\n" +
			"`debt resolve --status wontfix` accepts as a complete audit trail.\n\n" +
			"A record is repaired only when exactly one surviving review.md anchors it.\n" +
			"source_report paths are review-dir-relative, so several reviews hold the same\n" +
			"relative path; a record whose candidates disagree is left alone rather than\n" +
			"rewritten from a guess. Within a repaired id only the LINES still carrying the\n" +
			"stale excerpt are written, so a resolution trail's --reason is left intact.\n" +
			"Run --dry-run first: it prints the before and after of every line it would\n" +
			"touch.",
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
	// The rewritten counter names LINES as well as records. They differ whenever an
	// id carries a resolution trail, and the line count is the one that describes
	// what was written to an append-only store.
	// The settled counter is printed for the same reason: the fold suppresses a
	// settled id per-record, so a store whose ids are all settled reports "0 scanned,
	// 0 rewritten" — byte-identical to a store that needs no repair. Naming the
	// suppression is what lets an operator tell those apart.
	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"%s%d scanned, %d rewritten (%d %s), %d unchanged, %d unresolved (no review.md yielded the excerpt), %d ambiguous (candidates disagreed), %d skipped (settled: resolved or wontfix)\n",
		prefix, res.Scanned, res.Rewritten, res.RewrittenLines, pluralLines(res.RewrittenLines),
		res.Unchanged, res.Unresolved, res.Ambiguous, res.SkippedSettled)

	// A dry run shows the text, not just the count. It is documented as the step to
	// run FIRST on the one subcommand that rewrites the store in place, and a bare
	// counter cannot reveal WHICH line would change — the difference between a stale
	// review excerpt and an operator's typed --reason is only visible in the text.
	// %q keeps a multi-line excerpt on one line and, more importantly, escapes it:
	// the store is world-appendable, so its text is untrusted input that must never
	// be echoed verbatim to a terminal.
	//
	// That rule covers EVERY field of the line, not just the excerpt. The id reaches
	// JustificationChange as an unvalidated `m["id"].(string)` (internal/localdebt/
	// backfill.go), and the shard is a store filename, so both are untrusted for the
	// same reason. Printing them under %s let an ANSI CSI or a bidi override through to
	// the terminal on the one surface an operator consults to decide whether to let the
	// in-place rewrite proceed - where reordering WHICH line is named is the whole
	// attack. The sibling listing `atcr debt list` already strips them (cli/debt.go ->
	// cell -> sanitizeCell).
	//
	// The two get different treatment on purpose. The id takes %q, which escapes the
	// FORMAT runes (Cf) sanitizeCell deliberately keeps - a bidi override is not a C0/C1
	// control, and it is the rune that reorders which line appears to be named. The
	// shard takes sanitizeCell so the `<shard>:<line>` locator stays copy-pasteable;
	// quoting it would put the line number outside the name.
	if dryRun {
		for _, c := range res.Changes {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s:%d %q\n    before: %q\n    after:  %q\n",
				sanitizeCell(c.Shard), c.Line, c.ID, c.Before, c.After)
		}
	}
	return nil
}

func pluralLines(n int) string {
	if n == 1 {
		return "line"
	}
	return "lines"
}
