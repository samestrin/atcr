package main

import (
	"bytes"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/spf13/cobra"
)

// newLeaderboardCmd builds `atcr leaderboard`: aggregate stored scorecard records
// across runs into a table ranked by corroboration rate, with optional --since,
// --model, and --persona filters.
func newLeaderboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "leaderboard",
		Short: "Aggregate scorecard records across runs, ranked by corroboration rate",
		Long: "Aggregate the local scorecard store across runs into a leaderboard ranked\n" +
			"by corroboration rate. Records are grouped by (reviewer, model). Filters\n" +
			"compose with AND semantics. Read-only.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runLeaderboard,
	}
	cmd.Flags().String("since", "30d", "time window: Nd (days), Nw (weeks), Nm (months)")
	cmd.Flags().String("model", "", "filter to an exact model id")
	cmd.Flags().String("persona", "", "filter to an exact reviewer/persona name")
	return cmd
}

func runLeaderboard(cmd *cobra.Command, _ []string) error {
	since, _ := cmd.Flags().GetString("since")
	model, _ := cmd.Flags().GetString("model")
	persona, _ := cmd.Flags().GetString("persona")

	dir, err := scorecard.DefaultDir()
	if err != nil {
		return fmt.Errorf("cannot determine scorecard store path: %w", err)
	}
	records, err := scorecard.ReadAll(dir)
	if err != nil {
		return fmt.Errorf("failed to read scorecard store: %w", err)
	}

	out := cmd.OutOrStdout()
	if len(records) == 0 {
		// No data at all is a graceful empty state, not an error (exit 0).
		_, err := fmt.Fprintln(out, "No scorecard data found. Run 'atcr reconcile' to generate scorecard records.")
		return err
	}

	filtered, err := scorecard.ApplyFilters(records,
		scorecard.FilterOpts{Since: since, Model: model, Persona: persona}, time.Now())
	if err != nil {
		// A bad --since value parses at runtime (not by cobra); per the sprint
		// contract it is a runtime error (exit 1) carrying actionable guidance.
		return err
	}
	if len(filtered) == 0 {
		// Data exists but no record survived the filters: a real "nothing to
		// show" outcome (exit 1), distinct from the empty-store state above.
		return fmt.Errorf("no records match filters. Try widening --since or removing filters")
	}

	return renderLeaderboard(out, scorecard.Aggregate(filtered))
}

// renderLeaderboard writes the ranked aggregate table via text/tabwriter. Cost
// per corroborated finding renders as a dash for a group with zero corroborated
// findings (undefined). The table is buffered and written once so a flush error
// cannot emit a half table; the single write's error is propagated.
func renderLeaderboard(w io.Writer, rows []scorecard.LeaderboardRow) error {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "REVIEWER\tMODEL\tRUNS\tRAISED\tCORROBORATED\tCORR%\tCOST\tCOST/CORR\tLATENCY")
	for _, r := range rows {
		costPerCorr := "-"
		if r.HasCostPerCorroborated {
			costPerCorr = fmt.Sprintf("$%.4f", r.CostPerCorroborated)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%s\t$%.4f\t%s\t%dms\n",
			sanitizeCell(r.Reviewer), sanitizeCell(r.Model), r.Runs,
			r.FindingsRaised, r.FindingsCorroborated, formatPercent(r.CorroborationRate),
			r.TotalCostUSD, costPerCorr, r.AvgLatencyMS)
	}
	_ = tw.Flush()
	_, err := w.Write(buf.Bytes())
	return err
}
