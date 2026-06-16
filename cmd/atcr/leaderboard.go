package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
			"compose with AND semantics. Read-only.\n\n" +
			"With --export, emit an anonymized, versioned public submission JSON\n" +
			"document instead of the table (run_id and any path/host/key strings are\n" +
			"stripped); --output writes it to a file instead of stdout.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runLeaderboard,
	}
	cmd.Flags().String("since", "30d", "time window: Nd (days), Nw (weeks), Nm (months)")
	cmd.Flags().String("model", "", "filter to an exact model id")
	cmd.Flags().String("persona", "", "filter to an exact reviewer/persona name")
	cmd.Flags().Bool("export", false, "emit anonymized public submission JSON instead of the table")
	cmd.Flags().String("output", "", "with --export: write JSON to this file instead of stdout")
	return cmd
}

func runLeaderboard(cmd *cobra.Command, _ []string) error {
	since, _ := cmd.Flags().GetString("since")
	model, _ := cmd.Flags().GetString("model")
	persona, _ := cmd.Flags().GetString("persona")
	export, _ := cmd.Flags().GetBool("export")
	output, _ := cmd.Flags().GetString("output")

	dir, err := scorecard.DefaultDir()
	if err != nil {
		return fmt.Errorf("cannot determine scorecard store path: %w", err)
	}
	records, err := scorecard.ReadAll(dir)
	if err != nil {
		return fmt.Errorf("failed to read scorecard store: %w", err)
	}

	out := cmd.OutOrStdout()
	filters := scorecard.FilterOpts{Since: since, Model: model, Persona: persona}

	// --export takes its own path: it emits anonymized JSON and treats an empty
	// store or a no-match filter as a failure (exit 1) — unlike the table view,
	// where an empty store is a graceful exit-0 state.
	if export {
		return runLeaderboardExport(out, records, filters, output)
	}

	if len(records) == 0 {
		// No data at all is a graceful empty state, not an error (exit 0).
		_, err := fmt.Fprintln(out, "No scorecard data found. Run 'atcr reconcile' to generate scorecard records.")
		return err
	}

	filtered, err := scorecard.ApplyFilters(records, filters, time.Now())
	if err != nil {
		// A bad --since value parses at runtime (not by cobra); per the sprint
		// contract it is a runtime error (exit 1) carrying actionable guidance.
		return err
	}
	if len(filtered) == 0 {
		// Data exists but no record survived the filters: a real "nothing to
		// show" outcome (exit 1), distinct from the empty-store state above. The
		// active window is named so data hidden purely by the default 30d --since
		// is not mistaken for a bad --model/--persona.
		return fmt.Errorf("no records match filters (window: last %s). Try a wider --since or removing --model/--persona", since)
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

// runLeaderboardExport builds the anonymized v1 submission JSON and routes it to
// stdout or, when output is set, to a file. A no-match/empty result surfaces the
// canonical message (exit 1) and writes nothing. time.Now().UTC() is the
// envelope timestamp and the --since anchor.
func runLeaderboardExport(out io.Writer, records []scorecard.Record, filters scorecard.FilterOpts, output string) error {
	data, err := scorecard.Export(records, filters, time.Now().UTC())
	if err != nil {
		// ErrNoExportRecords already carries the exact user-facing message; a bad
		// --since carries its own actionable text. Both are exit-1 runtime errors.
		return err
	}
	if output == "" {
		_, werr := out.Write(append(data, '\n'))
		return werr
	}
	return writeExportFile(output, data)
}

// writeExportFile atomically writes the export to path: it creates parent
// directories, writes a sibling temp file (0600), then renames it over the
// target, so a crash never leaves a partial file and an existing file is
// replaced whole. A directory target is rejected up front with a clear message.
func writeExportFile(path string, data []byte) error {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return fmt.Errorf("--output path %s is a directory, not a file", path)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".scorecard-export-*.tmp")
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	tmpName := tmp.Name()
	// Remove the temp file if anything below fails; a no-op after a successful
	// rename (the path no longer exists under tmpName).
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting output permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing export: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing export: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("finalizing output file: %w", err)
	}
	return nil
}
