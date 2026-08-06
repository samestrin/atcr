package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/spf13/cobra"
)

// telemetry_report.go implements `atcr quality-report`: the maintainer-facing view
// of the community prompt quality signal (Sprint 30.0, Story 4). It sources
// EXCLUSIVELY from Story 1's content-free localdebt.AggregateQualitySignal — never
// internal/reconcile or the raw reconciled findings (TestTelemetryReport_NoReconcileImport
// locks that at the import-graph level) — so nothing it renders can carry code,
// file paths, or finding text. It is a distinct command from `atcr report` (which
// renders reconciled findings) and never modifies or aliases it.

// qualityReportRow is one rendered row: an aggregated per-(persona, model) pair
// with its computed dismissal rate. Its five fields are the whole allowlist the
// report exposes in either format — persona and model identifiers plus content-free
// counts and the derived rate.
type qualityReportRow struct {
	Persona        string  `json:"persona"`
	Model          string  `json:"model"`
	DismissedCount int     `json:"dismissed_count"`
	ConfirmedCount int     `json:"confirmed_count"`
	DismissalRate  float64 `json:"dismissal_rate"`
}

const qualityReportHeading = "# Prompt Quality Signal\n\n" +
	"Persona+model reviewer prompts ranked by dismissal rate (descending): the prompts " +
	"whose findings maintainers most often dismiss (wontfix) surface first as over-reporting " +
	"candidates; the best-calibrated prompts (lowest dismissal / highest confirmation) sit at " +
	"the bottom. Derived only from local dismissed/confirmed counters — no code, file paths, " +
	"or finding text.\n\n"

const qualityReportNoData = "No quality-signal data yet. Dismissed/confirmed counters accrue " +
	"as findings are resolved or dismissed (see `atcr debt resolve`).\n"

// newQualityReportCmd builds `atcr quality-report`. It is a structurally independent
// command with its own RunE — never a wrapper or alias of newReportCmd/runReport —
// and its help cross-references `atcr report` so the two similarly-named commands
// are not confused (the story's documented name-collision risk).
func newQualityReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quality-report",
		Short: "Render the aggregate persona+model dismissed/confirmed quality signal",
		Long: "Render the community prompt quality signal: per-(persona, model) reviewer prompts\n" +
			"ranked by dismissal rate, aggregated from the local .atcr/debt store's dismissed\n" +
			"and confirmed outcomes. It is content-free — persona, model, and counts only,\n" +
			"never code or finding text.\n\n" +
			"This is DISTINCT from `atcr report`, which renders md/json/checklist/sarif views\n" +
			"over a run's reconciled findings. Use `atcr report` for one run's findings; use\n" +
			"`atcr quality-report` for which prompts over-report across many runs.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runQualityReport,
	}
	// --dir comes from the same helper the debt subcommands use, so every
	// store-reading command scripts identically and tests can point
	// quality-report at a fixture store without chdir.
	addDebtStoreFlag(cmd)
	cmd.Flags().String("format", "md", "output format: md or json")
	return cmd
}

func runQualityReport(cmd *cobra.Command, _ []string) error {
	// Validate --format against the enum BEFORE any I/O: a bad value is a usage
	// error (exit 2), never conflated with a read failure (exit 1). This mirrors
	// runReport's format-first ordering.
	format, _ := cmd.Flags().GetString("format")
	if format != "md" && format != "json" {
		return usageError(fmt.Errorf("unknown format %q: supported formats are md, json", format))
	}

	// Warnings go to stderr like every other store reader: a malformed or
	// over-long line must never silently under-report the quality signal, and
	// stderr keeps the --format json stdout stream clean.
	records, err := localdebt.ReadAll(debtStoreDir(cmd), localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
	if err != nil {
		// A present-but-unreadable store is a genuine failure (exit 1), distinct
		// from the empty "no data" state below (a missing store reads as nil, nil).
		return &codedError{code: exitFailure, err: fmt.Errorf("reading local debt store: %w", err)}
	}
	rows := localdebt.AggregateQualitySignal(records)
	return renderQualityReport(cmd.OutOrStdout(), rows, format)
}

// renderQualityReport ranks the aggregated rows and writes them in the requested
// format. An empty row set is the "no data" state (exit 0): markdown prints a clear
// message, JSON prints a well-formed [] (never null). format is assumed already
// validated by the caller; any non-json value renders markdown.
func renderQualityReport(w io.Writer, rows []localdebt.QualityRow, format string) error {
	ranked := qualityReportRows(rows)
	if format == "json" {
		b, err := json.MarshalIndent(ranked, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\n", b)
		return err
	}
	return renderQualityReportMarkdown(w, ranked)
}

// qualityReportRows maps each aggregated row to a rendered row with its dismissal
// rate and sorts by rate descending, tie-broken by persona then model ascending
// (matching AggregateQualitySignal's own tie-break style). It always returns a
// non-nil slice so the JSON empty case marshals to [] rather than null.
//
// Persona and Model pass through cell HERE — the single funnel both renderers
// consume — because the report reads the shared public .atcr/debt/ store
// (debtStoreDir): world-appendable via `debt add`, MCP, and third-party tools,
// so these fields are attacker-influenceable text, not catalog-controlled
// slugs. cell flattens CR/LF/TAB to spaces (row structure) and strips the
// remaining control characters through sanitizeCell — covering the json
// renderer too: json.Marshal escapes the C0 range but emits C1
// (U+0080–U+009F) raw.
func qualityReportRows(rows []localdebt.QualityRow) []qualityReportRow {
	out := make([]qualityReportRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, qualityReportRow{
			Persona:        cell(r.Persona),
			Model:          cell(r.Model),
			DismissedCount: r.DismissedCount,
			ConfirmedCount: r.ConfirmedCount,
			DismissalRate:  dismissalRate(r.DismissedCount, r.ConfirmedCount),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DismissalRate != out[j].DismissalRate {
			return out[i].DismissalRate > out[j].DismissalRate // over-reporting first
		}
		if out[i].Persona != out[j].Persona {
			return out[i].Persona < out[j].Persona
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// dismissalRate is dismissed / (dismissed + confirmed). Every emitted QualityRow has
// at least one terminal outcome (Story 1 AC 01-01), so the denominator is non-zero
// in practice; the zero guard is defensive — a 0/0 pair renders 0.0, never a NaN or
// a divide-by-zero panic (AC 04-01 EC1).
func dismissalRate(dismissed, confirmed int) float64 {
	total := dismissed + confirmed
	if total == 0 {
		return 0
	}
	return float64(dismissed) / float64(total)
}

func renderQualityReportMarkdown(w io.Writer, rows []qualityReportRow) error {
	var b strings.Builder
	b.WriteString(qualityReportHeading)
	if len(rows) == 0 {
		b.WriteString(qualityReportNoData)
		_, err := io.WriteString(w, b.String())
		return err
	}
	b.WriteString("| Persona | Model | Dismissed | Confirmed | Dismissal Rate |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %.1f%% |\n",
			escapeMarkdownCell(r.Persona), escapeMarkdownCell(r.Model),
			r.DismissedCount, r.ConfirmedCount, r.DismissalRate*100)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// escapeMarkdownCell makes a persona/model identifier safe to interpolate into a
// markdown table cell: a literal pipe would break the column structure and a
// newline would break the row. Control characters (ESC, C0/C1, DEL, U+2028/9)
// are NOT this function's job — they are stripped upstream in qualityReportRows,
// the funnel every renderer reads, because the report's input is the shared,
// world-appendable .atcr/debt/ store and its text is untrusted. This guard holds
// the remaining markdown-structure line the render layer owns — enforced
// structurally, not left to input hygiene.
func escapeMarkdownCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.ReplaceAll(s, "|", "\\|")
}
