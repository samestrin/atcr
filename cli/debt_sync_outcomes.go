package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/localdebt"
)

// gateMinOutcomes and gateMinPersonas are the scorecard confirmation gate that
// Epic 35.16.11.3.2 T3 waits on: minConfirmationOutcomes counted terminal outcomes
// (internal/scorecard/trust.go) for at least 10 distinct personas, the
// re-measurement trigger written beside that constant. minConfirmationOutcomes is
// unexported, so the value is copied here and
// TestDebtSyncOutcomes_GateFloorMatchesTheScorecard pins it to trust.go's literal.
const (
	gateMinOutcomes = 20
	gateMinPersonas = 10
)

// newDebtSyncOutcomesCmd builds `atcr debt sync-outcomes`: it carries the outcomes
// seats record in a technical-debt README table back into the ledger, and with
// --report says whether the ledger holds enough of them for the scorecard gate.
func newDebtSyncOutcomesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync-outcomes",
		Short: "Record TD README outcomes for rows that carry a debt:<id> token",
		Long: "atcr debt sync-outcomes --td <README> reads the technical-debt table rows that\n" +
			"carry a debt:<16-hex id> token in their Problem or Source cell and records each\n" +
			"row's outcome in the local store: [x] as resolved, [-] as unreproducible. A row\n" +
			"with no token, an id the store does not hold, or an open ([ ]) or deferred ([/])\n" +
			"marker is reported and skipped; a row is never matched any other way. Re-running\n" +
			"appends nothing.\n\n" +
			"--report prints the counted terminal outcomes per persona and whether the\n" +
			"scorecard's confirmation gate is met. It writes nothing and exits 0 either way.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runDebtSyncOutcomes,
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().String("td", "", "path to the technical-debt README to read outcomes from")
	cmd.Flags().Bool("report", false, "print per-persona counted terminal outcomes and the gate status (read-only)")
	return cmd
}

func runDebtSyncOutcomes(cmd *cobra.Command, _ []string) error {
	dir := debtStoreDir(cmd)
	td := strings.TrimSpace(mustFlag(cmd, "td"))
	report, _ := cmd.Flags().GetBool("report")
	switch {
	case report && td != "":
		return usageError(fmt.Errorf("--report is read-only and cannot be combined with --td; run the sync, then the report"))
	case report:
		return reportDebtGate(cmd.OutOrStdout(), dir)
	case td == "":
		return usageError(fmt.Errorf("atcr debt sync-outcomes requires --td <README> (or --report)"))
	}

	f, err := os.Open(td)
	if err != nil {
		return fmt.Errorf("atcr debt sync-outcomes: %w", err)
	}
	rows, err := localdebt.ParseTDRows(f)
	_ = f.Close()
	if err != nil {
		return fmt.Errorf("atcr debt sync-outcomes: reading %s: %w", td, err)
	}
	return syncDebtOutcomes(cmd, dir, td, rows)
}

// syncWant is one id's outcome as the README states it.
type syncWant struct {
	id     string
	status string
	marker string
	lines  []int
	row    localdebt.TDRow
	// conflict is set when two terminal rows name the id with different outcomes.
	conflict bool
}

func syncDebtOutcomes(cmd *cobra.Command, dir, td string, rows []localdebt.TDRow) error {
	out := cmd.OutOrStdout()
	var skips []string
	skip := func(format string, a ...any) { skips = append(skips, fmt.Sprintf(format, a...)) }
	untokened := 0

	wants := map[string]*syncWant{}
	var order []string
	for _, r := range rows {
		switch {
		case len(r.BadTokens) > 0:
			skip("line %d: malformed token %s (an id is 16 hex characters)", r.Line, r.BadTokens[0])
			continue
		case len(r.IDs) == 0:
			untokened++
			continue
		case len(r.IDs) > 1:
			skip("line %d: names more than one id (debt:%s); a row records one outcome", r.Line, strings.Join(r.IDs, ", debt:"))
			continue
		}
		id := r.IDs[0]
		status, terminal, known := localdebt.TDMarkerStatus(r.Marker)
		if !known {
			skip("line %d: unknown marker %s for debt:%s", r.Line, cell(r.Marker), id)
			continue
		}
		if !terminal {
			skip("line %d: debt:%s marker %s is not a terminal outcome", r.Line, id, r.Marker)
			continue
		}
		w, ok := wants[id]
		if !ok {
			wants[id] = &syncWant{id: id, status: status, marker: r.Marker, lines: []int{r.Line}, row: r}
			order = append(order, id)
			continue
		}
		w.lines = append(w.lines, r.Line)
		if w.status != status {
			w.conflict = true
		}
	}

	recs, err := collectDebtIDRecords(dir, order, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
	if err != nil {
		return fmt.Errorf("atcr debt sync-outcomes: failed to read local debt store: %w", err)
	}
	byID := map[string][]localdebt.Record{}
	for _, r := range recs {
		byID[r.ID] = append(byID[r.ID], r)
	}

	var appended, unchanged int
	var marked []string
	for _, id := range order {
		w := wants[id]
		if w.conflict {
			skip("debt:%s rows disagree (lines %s); fix the README so one outcome stands", id, joinInts(w.lines))
			continue
		}
		hist := byID[id]
		if len(hist) == 0 {
			skip("line %d: debt:%s is not in the store", w.lines[0], id)
			continue
		}
		// The idempotence guard reads HISTORY, not only the folded record. A
		// finding resolved and then re-detected folds to an open record, and the
		// README row that recorded the resolution is still [x]: re-appending it
		// would close a regression on the strength of the old fix. Only the folded
		// record carrying the status is a clean no-op; any other recorded instance
		// means the row is stale.
		if recordedStatus(hist, w.status) {
			if effectiveStatus(hist, id) == w.status {
				unchanged++
				continue
			}
			skip("line %d: debt:%s was re-detected after it was recorded %s; the row is stale (reopen it, or run atcr debt resolve %s)", w.lines[0], id, w.status, id)
			continue
		}
		rec, closedAs, err := buildDebtResolution(hist, id, w.status, syncReason(td, w))
		if err != nil {
			skip("line %d: debt:%s: %v", w.lines[0], id, err)
			continue
		}
		if closedAs != "" {
			skip("line %d: debt:%s is already closed as %s; the row says %s", w.lines[0], id, closedAs, w.status)
			continue
		}
		if err := appendDebtResolution(dir, rec, "atcr debt sync-outcomes"); err != nil {
			// A usage error is this row's record being refused (too large to
			// store); anything else is the store failing, which stops the run.
			if exitCode(err) != exitUsage {
				return err
			}
			skip("line %d: debt:%s: %v", w.lines[0], id, err)
			continue
		}
		appended++
		marked = append(marked, fmt.Sprintf("  recorded debt:%s %s (line %d)", id, w.status, w.lines[0]))
	}

	_, _ = fmt.Fprintf(out, "synced %s into %s: appended %d, already recorded %d, skipped %d\n",
		td, dir, appended, unchanged, len(skips)+untokened)
	for _, m := range marked {
		_, _ = fmt.Fprintln(out, m)
	}
	for _, s := range skips {
		_, _ = fmt.Fprintln(out, "  skip "+s)
	}
	if untokened > 0 {
		_, _ = fmt.Fprintf(out, "  skip %d row(s) carry no debt:<id> token\n", untokened)
	}
	return nil
}

// syncReason is the --reason a synced record carries. resolved needs none (the
// fix is the explanation) and keeps the finding's existing justification, exactly
// as `atcr debt resolve <id>` does; every other status needs one, and the row's
// Problem cell is where seats write why they closed it.
func syncReason(td string, w *syncWant) string {
	if w.status == localdebt.StatusResolved {
		return ""
	}
	r := fmt.Sprintf("Recorded by atcr debt sync-outcomes from %s line %d (%s row): %s",
		td, w.lines[0], w.marker, w.row.Problem)
	if len(r) > maxReasonBytes {
		r = strings.ToValidUTF8(r[:maxReasonBytes], "")
	}
	return r
}

func recordedStatus(hist []localdebt.Record, status string) bool {
	for _, r := range hist {
		if strings.ToLower(strings.TrimSpace(r.Status)) == status {
			return true
		}
	}
	return false
}

func effectiveStatus(hist []localdebt.Record, id string) string {
	for _, r := range localdebt.FoldRecords(hist) {
		if r.ID == id {
			return strings.ToLower(strings.TrimSpace(r.Status))
		}
	}
	return ""
}

func joinInts(xs []int) string {
	s := make([]string, len(xs))
	for i, x := range xs {
		s[i] = fmt.Sprint(x)
	}
	return strings.Join(s, ", ")
}

// personaOutcomes is one persona's counted terminal outcomes, folded across
// models.
type personaOutcomes struct {
	persona                                                     string
	total, resolved, wontfix, unreproducible, attemptsExhausted int
}

// reportDebtGate prints each persona's counted terminal outcomes and whether the
// scorecard's confirmation gate is met. The counts are the ones T3 will read:
// ReadAll then AggregateQualitySignal, as buildQualitySignalPayload does, folded
// across models by persona. The persona key is trimmed and lower-cased, the
// GroundTruthLookup key convention (internal/scorecard/trust.go), so case
// variants of one persona count as one.
func reportDebtGate(w io.Writer, dir string) error {
	records, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: io.Discard})
	if err != nil {
		return fmt.Errorf("atcr debt sync-outcomes: failed to read local debt store: %w", err)
	}
	byPersona := map[string]*personaOutcomes{}
	for _, row := range localdebt.AggregateQualitySignal(records) {
		key := strings.ToLower(strings.TrimSpace(row.Persona))
		p, ok := byPersona[key]
		if !ok {
			p = &personaOutcomes{persona: key}
			byPersona[key] = p
		}
		p.total += row.TerminalOutcomes
		p.resolved += row.ConfirmedCount
		p.wontfix += row.DismissedCount
		p.unreproducible += row.UnreproducibleCount
		p.attemptsExhausted += row.AttemptsExhaustedCount
	}
	ps := make([]*personaOutcomes, 0, len(byPersona))
	for _, p := range byPersona {
		ps = append(ps, p)
	}
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].total != ps[j].total {
			return ps[i].total > ps[j].total
		}
		return ps[i].persona < ps[j].persona
	})

	_, _ = fmt.Fprintf(w, "Counted terminal outcomes per persona in %s:\n", dir)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PERSONA\tOUTCOMES\tRESOLVED\tWONTFIX\tUNREPRODUCIBLE\tATTEMPTS-EXHAUSTED")
	atFloor := 0
	for _, p := range ps {
		if p.total >= gateMinOutcomes {
			atFloor++
		}
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\n",
			cell(p.persona), p.total, p.resolved, p.wontfix, p.unreproducible, p.attemptsExhausted)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if atFloor >= gateMinPersonas {
		_, err = fmt.Fprintf(w, "GATE MET: %d personas at %d (need %d)\n", atFloor, gateMinOutcomes, gateMinPersonas)
	} else {
		_, err = fmt.Fprintf(w, "GATE NOT MET: %d of %d personas at %d\n", atFloor, gateMinPersonas, gateMinOutcomes)
	}
	return err
}
