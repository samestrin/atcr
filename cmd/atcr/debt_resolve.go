package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/localdebt"
)

// defaultDebtResolveDir is the .atcr/-scoped local TD store, rooted at the current
// working directory (localdebt's Root: "." convention). Unlike list/add/dashboard,
// resolve never reads the private .planning/-scoped store — it operates only on the
// public store the reconcile persistence hook (Story 2) populates.
var defaultDebtResolveDir = localdebt.DefaultDir(".")

// resolveSeverities is the validated --severity enum, matching the set used
// elsewhere in cmd/atcr/debt.go.
var resolveSeverities = map[string]bool{"CRITICAL": true, "HIGH": true, "MEDIUM": true, "LOW": true}

// resolveStatuses is the validated --status enum for a mark action. Both values are
// terminal (isClosedStatus folds them out): "resolved" means the code was actually
// fixed; "wontfix" (Epic 24.0) dismisses a false-positive/accepted pattern so agents
// stop re-surfacing it. "deferred" is intentionally excluded — it is written by other
// paths, not by an explicit resolve.
var resolveStatuses = map[string]bool{"resolved": true, "wontfix": true}

// newDebtResolveCmd builds `atcr debt resolve`: the .atcr/-scoped resolver surface
// the debt-resolve skill route shells out to. It lists open items from the local TD
// store (deterministically sorted for the skill's selection rule) and records
// resolution outcomes as append-only status records. The actual fix cycle
// (RED→GREEN→ADVERSARIAL→REFACTOR) is agent-driven in skill/debt-resolve/SKILL.md;
// this subcommand is the store's read/mark-resolved contract, never a code editor.
func newDebtResolveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve",
		Short: "List and mark-resolve items in the .atcr/-scoped local TD store (public store)",
		Long: "atcr debt resolve reads the public, .atcr/-scoped local technical-debt store\n" +
			"(.atcr/debt/, populated by atcr reconcile) and lists open items for the\n" +
			"debt-resolve skill route to fix. Unlike list/add/dashboard, it never touches\n" +
			"the private .planning/ store. Use --resolve <id> to record a resolution.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runDebtResolve,
	}
	cmd.Flags().String("dir", defaultDebtResolveDir, "path to the local TD store (.atcr/debt)")
	cmd.Flags().Bool("list", false, "list open items (default when no other action is given)")
	cmd.Flags().Bool("json", false, "emit the selected items as a JSON array")
	cmd.Flags().String("severity", "", "filter by severity (CRITICAL|HIGH|MEDIUM|LOW)")
	cmd.Flags().Int("max", 10, "cap the number of selected items (0 = no cap)")
	cmd.Flags().String("resolve", "", "mark the item with this id resolved (append-only)")
	cmd.Flags().String("status", "resolved", "terminal status to record for --resolve (resolved|wontfix)")
	cmd.Flags().String("reason", "", "justification recorded on the --resolve record (e.g. why a finding is wontfix)")
	return cmd
}

func runDebtResolve(cmd *cobra.Command, _ []string) error {
	dir := mustFlag(cmd, "dir")

	id := mustFlag(cmd, "resolve")
	// --status/--reason only mean something for a mark action; without --resolve they
	// would be silently ignored (dropping the user's dismissal intent and skipping
	// --status validation). Reject that combination rather than fall through to list.
	if id == "" {
		var provided []string
		if cmd.Flags().Changed("status") {
			provided = append(provided, "--status")
		}
		if cmd.Flags().Changed("reason") {
			provided = append(provided, "--reason")
		}
		if len(provided) == 1 {
			return usageError(fmt.Errorf("%s requires --resolve <id>", provided[0]))
		}
		if len(provided) > 1 {
			return usageError(fmt.Errorf("%s require --resolve <id>", strings.Join(provided, ", ")))
		}
	}
	if id != "" {
		status := strings.ToLower(strings.TrimSpace(mustFlag(cmd, "status")))
		if !resolveStatuses[status] {
			return usageError(fmt.Errorf("invalid --status %q: expected resolved|wontfix", status))
		}
		return markDebtResolved(cmd, dir, id, status, mustFlag(cmd, "reason"))
	}

	sev := strings.ToUpper(mustFlag(cmd, "severity"))
	if sev != "" && !resolveSeverities[sev] {
		return usageError(fmt.Errorf("invalid --severity %q: expected CRITICAL|HIGH|MEDIUM|LOW", mustFlag(cmd, "severity")))
	}
	max, _ := cmd.Flags().GetInt("max")

	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
	if err != nil {
		return fmt.Errorf("atcr debt resolve: failed to read local debt store: %w", err)
	}
	open := selectOpenDebt(recs, sev, max)

	if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
		return renderResolveJSON(cmd.OutOrStdout(), open)
	}
	return renderResolveList(cmd.OutOrStdout(), open)
}

// isClosedStatus reports whether a record's status takes an item out of the open
// backlog. The reconcile hook writes records with an empty status (open); a
// resolution/deferral/dismissal record carries an explicit terminal status.
// wontfix (Epic 24.0) folds a finding out exactly like resolved — it marks a
// false-positive/accepted pattern that agents must stop re-surfacing.
func isClosedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "resolved", "deferred", "wontfix":
		return true
	default:
		return false
	}
}

// selectOpenDebt folds the append-only record stream by id into the open backlog.
// An id is open unless any record for it carries a terminal status; the displayed
// record is the first open (non-terminal) occurrence, so a later resolution record
// folds the item out. Results are sorted severity DESC then ts ASC (oldest first)
// and capped at max — the deterministic selection rule the skill route documents.
func selectOpenDebt(recs []localdebt.Record, severity string, max int) []localdebt.Record {
	type agg struct {
		rec      localdebt.Record
		resolved bool
		hasRec   bool
	}
	order := make([]string, 0, len(recs))
	m := make(map[string]*agg, len(recs))
	for _, r := range recs {
		a := m[r.ID]
		if a == nil {
			a = &agg{}
			m[r.ID] = a
			order = append(order, r.ID)
		}
		if isClosedStatus(r.Status) {
			a.resolved = true
			continue
		}
		if !a.hasRec && r.File != "" {
			a.rec = r
			a.hasRec = true
		}
	}

	open := make([]localdebt.Record, 0, len(order))
	for _, id := range order {
		a := m[id]
		if a.resolved || !a.hasRec {
			continue
		}
		if severity != "" && strings.ToUpper(a.rec.Severity) != severity {
			continue
		}
		open = append(open, a.rec)
	}

	sort.SliceStable(open, func(i, j int) bool {
		ri, rj := severityRank(open[i].Severity), severityRank(open[j].Severity)
		if ri != rj {
			return ri > rj
		}
		return open[i].Timestamp < open[j].Timestamp
	})
	if max > 0 && len(open) > max {
		open = open[:max]
	}
	return open
}

// severityRank orders severities for selection: CRITICAL > HIGH > MEDIUM > LOW.
// Unknown severities sort last (rank 0).
func severityRank(s string) int {
	switch strings.ToUpper(s) {
	case "CRITICAL":
		return 4
	case "HIGH":
		return 3
	case "MEDIUM":
		return 2
	case "LOW":
		return 1
	default:
		return 0
	}
}

// renderResolveJSON writes the selected records as a JSON array (never null, so an
// empty store yields [] for a scripting consumer).
func renderResolveJSON(w io.Writer, recs []localdebt.Record) error {
	if recs == nil {
		recs = []localdebt.Record{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(recs)
}

// renderResolveList writes an aligned table of open items for the skill route to
// select from. An empty selection prints a clear "no items" line and exits 0.
func renderResolveList(w io.Writer, recs []localdebt.Record) error {
	if len(recs) == 0 {
		_, err := fmt.Fprintln(w, "No items to resolve (the local TD store has no open items).")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tSEVERITY\tFILE\tLINE\tPROBLEM"); err != nil {
		return err
	}
	for _, r := range recs {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n",
			r.ID, r.Severity, r.File, r.Line, truncate(r.Problem, 60)); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// markDebtResolved records an append-only resolution for id: it copies the item's
// open record, stamps a terminal status/timestamp, and appends it so the fold in
// selectOpenDebt drops the item from the open list. The stable id is preserved
// (never re-stamped) so the resolution lines up with the original finding.
func markDebtResolved(cmd *cobra.Command, dir, id, status, reason string) error {
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
	if err != nil {
		return fmt.Errorf("atcr debt resolve: failed to read local debt store: %w", err)
	}

	var orig *localdebt.Record
	var alreadyClosed bool
	for i := range recs {
		if recs[i].ID != id {
			continue
		}
		if isClosedStatus(recs[i].Status) {
			alreadyClosed = true
			continue
		}
		if orig == nil && recs[i].File != "" {
			r := recs[i]
			orig = &r
		}
	}
	// Concurrency-tolerant, not lock-protected: a terminal record for this id already
	// exists, so this invocation reports and no-ops instead of appending a duplicate
	// resolution record. Two concurrent invocations can each pass this check before
	// either appends (the accepted TD-004 no-lock stance); selectOpenDebt's append-only
	// fold treats any extra resolution record for an already-closed id as redundant, so
	// the result is harmless duplicate bloat, not corruption.
	if alreadyClosed {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is already resolved; nothing to do.\n", id)
		return nil
	}
	if orig == nil {
		return fmt.Errorf("no open technical-debt item with id %q in the local store", id)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	rec := *orig
	rec.RunID = now + "-" + status
	rec.Timestamp = now
	rec.Status = status
	rec.ResolvedAt = now
	// A supplied --reason records why the finding was dismissed/resolved; an empty
	// reason preserves any justification the item already carried (e.g. reconcile
	// enrichment), never blanking it.
	if r := strings.TrimSpace(reason); r != "" {
		rec.Justification = r
	}
	if err := localdebt.Append(dir, rec); err != nil {
		return fmt.Errorf("atcr debt resolve: failed to record resolution: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Marked %s %s.\n", id, status)
	return nil
}
