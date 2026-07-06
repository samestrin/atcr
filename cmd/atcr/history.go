package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/history"
	"github.com/spf13/cobra"
)

// defaultHistorySince is the query window used when --since is omitted: wide
// enough (90 days) to be useful by default, while still bounding the table.
const defaultHistorySince = 90 * 24 * time.Hour

// newHistoryCmd builds `atcr history`: read the append-only finding history —
// the monthly shards under .planning/history plus the legacy .atcr flat ledger
// (Epic 19.4) — filter it by a time window (--since) and package prefix
// (--package), and print a markdown table of counts by severity per package. An
// absent or fully-filtered history is not an error — it exits 0 with a "no
// history" notice (Epic 19.0 AC3).
func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show finding history over time as a markdown table",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runHistory,
	}
	cmd.Flags().String("since", "", "only include findings within this window: h/m/s or d/w units (e.g. 30d, 2w, 48h); default 90d")
	cmd.Flags().String("package", "", "only include findings whose package is at or under this path prefix (e.g. internal/registry)")
	return cmd
}

func runHistory(cmd *cobra.Command, _ []string) error {
	since := defaultHistorySince
	if raw, _ := cmd.Flags().GetString("since"); strings.TrimSpace(raw) != "" {
		d, err := history.ParseSince(raw)
		if err != nil {
			return usageError(err) // bad --since is a usage error (exit 2)
		}
		since = d
	}
	pkg, _ := cmd.Flags().GetString("package")

	root, err := repoRoot()
	if err != nil {
		return usageError(fmt.Errorf("resolving repo root: %w", err))
	}
	shardDir := filepath.Join(root, ".planning", "history")
	legacyPath := filepath.Join(root, ".atcr", "findings-history.jsonl")
	recs, err := history.LoadAll(shardDir, legacyPath)
	if err != nil {
		return usageError(err) // corrupt/unreadable ledger (exit 2)
	}

	out := cmd.OutOrStdout()
	if len(recs) == 0 {
		_, _ = fmt.Fprintln(out, "no history recorded yet — run 'atcr review' first")
		return nil
	}

	filtered := history.Filter(recs, since, pkg, time.Now())
	if len(filtered) == 0 {
		scope := "the selected window"
		if strings.TrimSpace(pkg) != "" {
			scope = fmt.Sprintf("package %q within the selected window", strings.TrimSpace(pkg))
		}
		_, _ = fmt.Fprintf(out, "no history for %s\n", scope)
		return nil
	}

	_, _ = fmt.Fprint(out, history.RenderTable(filtered))
	return nil
}
