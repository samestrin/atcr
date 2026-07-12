package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/debt"
)

// Default locations of the technical-debt store, relative to the repo root.
// Both are overridable via flags so the commands can run against a fixture tree
// in tests or a non-standard checkout.
const (
	defaultTDReadme = ".planning/technical-debt/README.md"
	defaultTDItems  = ".planning/technical-debt/items"
)

// newDebtCmd builds `atcr debt`: query, aggregate, and report on the Epic-12.1
// sharded technical-debt store. It is a thin CLI over internal/debt (which in
// turn reuses internal/tdmigrate), so the whole surface is unit-testable without
// spawning a process. Subcommands: list, add, dashboard.
func newDebtCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debt",
		Short: "Query and report on technical debt",
		Long: "atcr debt reads the sharded technical-debt store under\n" +
			".planning/technical-debt/items/ (Epic 12.1 format) and provides\n" +
			"list/add/dashboard subcommands for querying, capturing, and reporting debt.\n" +
			"The resolve subcommand instead operates on the public, .atcr/-scoped local\n" +
			"TD store (.atcr/debt/) that atcr reconcile populates.",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newDebtListCmd(), newDebtAddCmd(), newDebtDashboardCmd(), newDebtResolveCmd())
	return cmd
}

// addSourceFlags registers the shared --items/--readme/--sync flags used by the
// shard-reading subcommands so list and dashboard resolve their inputs
// identically.
func addSourceFlags(cmd *cobra.Command) {
	cmd.Flags().String("items", defaultTDItems, "path to the sharded technical-debt store")
	cmd.Flags().String("readme", defaultTDReadme, "path to the authoritative technical-debt README")
	cmd.Flags().Bool("sync", false, "regenerate shards from the authoritative README before reading")
}

// loadRecords resolves --items/--readme/--sync and returns the flattened Records.
// When --sync is set it first regenerates the shards from the authoritative
// README (the shards are additive and can lag the table); otherwise it reads the
// shards as-is for speed.
func loadRecords(cmd *cobra.Command) ([]debt.Record, error) {
	items, _ := cmd.Flags().GetString("items")
	readme, _ := cmd.Flags().GetString("readme")
	sync, _ := cmd.Flags().GetBool("sync")

	if sync {
		// --check is a read-only verification mode; combining it with --sync
		// would mutate the working tree while claiming to only verify drift.
		check, _ := cmd.Flags().GetBool("check")
		if !check {
			if err := debt.SyncShards(readme, items, cmd.ErrOrStderr()); err != nil {
				return nil, err
			}
		}
	}
	return debt.Load(items)
}

func newDebtListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List technical-debt items as a table, with filtering and sorting",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runDebtList,
	}
	addSourceFlags(cmd)
	cmd.Flags().String("severity", "", "filter by severity (CRITICAL|HIGH|MEDIUM|LOW)")
	cmd.Flags().String("status", "", "filter by status (open|deferred|resolved)")
	cmd.Flags().String("category", "", "filter by category (substring match)")
	cmd.Flags().String("component", "", "filter by component (path prefix, e.g. internal/autofix)")
	cmd.Flags().String("group", "", "filter by group label")
	cmd.Flags().String("sort", debt.SortSeverity, "sort key: severity|age|est|file")
	return cmd
}

func runDebtList(cmd *cobra.Command, _ []string) error {
	recs, err := loadRecords(cmd)
	if err != nil {
		return err
	}

	f := debt.Filter{
		Severity:  mustFlag(cmd, "severity"),
		Status:    mustFlag(cmd, "status"),
		Category:  mustFlag(cmd, "category"),
		Component: mustFlag(cmd, "component"),
		Group:     mustFlag(cmd, "group"),
	}
	recs = debt.Apply(recs, f)

	sortKey := mustFlag(cmd, "sort")
	if err := debt.Sort(recs, sortKey); err != nil {
		return usageError(err) // a bad --sort value is a usage error (exit 2)
	}

	out := cmd.OutOrStdout()
	if len(recs) == 0 {
		_, _ = fmt.Fprintln(out, "No matching technical-debt items.")
		return nil
	}

	return renderDebtTable(out, recs)
}

// mustFlag reads a string flag, returning "" if it was not registered. The
// error is impossible for flags this command declares, so it is intentionally
// discarded to keep call sites readable.
func mustFlag(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

// renderDebtTable writes an aligned, tab-separated table of records. Problem
// text is truncated so a long finding never wraps the terminal into an
// unreadable block; the full text lives in the shard/README.
func renderDebtTable(w io.Writer, recs []debt.Record) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "SEVERITY\tSTATUS\tGROUP\tEST\tFILE\tCATEGORY\tPROBLEM"); err != nil {
		return err
	}
	for _, r := range recs {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
			r.Severity, r.Status, r.Group, r.EstMinutes, r.File, r.Category, truncate(r.Problem, 60)); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// truncate shortens s to at most n runes, appending an ellipsis when it cut
// anything, and collapses newlines to spaces so a multi-line problem stays on
// one table row.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", " "), "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
