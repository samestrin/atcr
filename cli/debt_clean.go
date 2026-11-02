package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	goaxi "github.com/samestrin/go-axi"
	"github.com/spf13/cobra"
	toon "github.com/toon-format/toon-go"

	"github.com/samestrin/atcr/internal/localdebt"
)

// debtCleanSeverities is the fixed head of td-clean's Stats table, in its order.
var debtCleanSeverities = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}

// debtCleanSeverityRow is one row of td-clean's regenerated "## Stats" table.
// Resolved is always 0, as it is in the cleaned README: the view holds no
// resolved item. It stays so the table keeps td-clean's columns.
type debtCleanSeverityRow struct {
	Severity       string `json:"severity"`
	Open           int    `json:"open"`
	Deferred       int    `json:"deferred"`
	Resolved       int    `json:"resolved"`
	Unreproducible int    `json:"unreproducible"`
}

// debtCleanSummary is td-clean's --json counts with the README vocabulary
// replaced: removed_rows is excluded_resolved, because nothing is removed;
// removed_sections is gone, because the store has no sections. The two
// excluded_* counters for wontfix and attempts-exhausted are store-only: the
// README legend has no marker for either.
type debtCleanSummary struct {
	Open                      int                    `json:"open"`
	Deferred                  int                    `json:"deferred"`
	Resolved                  int                    `json:"resolved"`
	Unreproducible            int                    `json:"unreproducible"`
	Total                     int                    `json:"total"`
	ExcludedResolved          int                    `json:"excluded_resolved"`
	ExcludedWontfix           int                    `json:"excluded_wontfix"`
	ExcludedAttemptsExhausted int                    `json:"excluded_attempts_exhausted"`
	BySeverity                []debtCleanSeverityRow `json:"by_severity"`
}

type debtCleanDoc struct {
	Items   []localdebt.Record `json:"items"`
	Summary debtCleanSummary   `json:"summary"`
}

// newDebtCleanCmd is td-clean as a view. td-clean rewrote the README without its
// resolved rows and regenerated the Stats section; the store is append-only and
// keeps every event (epic 92), so clean reads it and emits what td-clean would
// have left — the open, deferred and unreproducible items — with the counts it
// would have written. It never writes to the store.
func newDebtCleanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "View the open, deferred and unreproducible technical-debt items with regenerated counts; writes nothing",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runDebtClean,
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().Bool("json", false, "emit {items, summary} as JSON (the default output)")
	cmd.Flags().Bool("axi", false, "emit {items, summary} as a token-dense TOON payload for agent consumption")
	// Registered and rejected for the reason debt filter gives.
	cmd.Flags().Bool("legacy-pipe", false, "not supported by this command; rejected")
	_ = cmd.Flags().MarkHidden("legacy-pipe")
	cmd.Annotations = map[string]string{noLegacyPipeAnnotation: "true"}
	return cmd
}

func runDebtClean(cmd *cobra.Command, _ []string) error {
	if legacy, _ := cmd.Flags().GetBool("legacy-pipe"); legacy {
		return usageError(fmt.Errorf("--legacy-pipe is not supported by debt clean; --axi emits standard TOON"))
	}
	dir := debtStoreDir(cmd)
	opts := localdebt.ReadOpts{Writer: cmd.ErrOrStderr()}

	// The two-pass read selectDebtForList uses: classify and count over folded
	// summaries, then hydrate full records only for the kept ids.
	sums, err := localdebt.ReadSummaries(dir, opts)
	if err != nil {
		return fmt.Errorf("read local debt store: %w", err)
	}
	summary, kept := summarizeDebtClean(localdebt.FoldSummaries(sums))
	sums = nil
	_ = sums
	if err := sortDebtSummaries(kept, sortKeySeverity); err != nil {
		return err
	}
	ids := make([]string, 0, len(kept))
	for _, s := range kept {
		ids = append(ids, s.ID)
	}
	recs, err := hydrateDebtListIDs(dir, ids, opts, nil)
	if err != nil {
		return err
	}
	if recs == nil {
		recs = []localdebt.Record{}
	}
	doc := debtCleanDoc{Items: recs, Summary: summary}

	out := cmd.OutOrStdout()
	if axiFromContext(cmd.Context()) {
		return renderDebtCleanAXI(out, doc)
	}
	return renderDebtDocJSON(out, doc)
}

// summarizeDebtClean sorts each folded item into the view or one excluded
// counter, and builds the Stats table over the kept ones. Severity is folded to
// upper case, as td-clean's table does; a severity outside the four follows
// them, in name order (td-clean appends in README order, which the store lacks).
func summarizeDebtClean(folded []localdebt.Summary) (debtCleanSummary, []localdebt.Summary) {
	var s debtCleanSummary
	rows := map[string]*debtCleanSeverityRow{}
	for _, sev := range debtCleanSeverities {
		rows[sev] = &debtCleanSeverityRow{Severity: sev}
	}
	var extra []string
	var kept []localdebt.Summary
	for _, sum := range folded {
		bucket := debtStatusBucket(sum.Status)
		switch bucket {
		case localdebt.StatusResolved:
			s.ExcludedResolved++
			continue
		case localdebt.StatusWontfix:
			s.ExcludedWontfix++
			continue
		case localdebt.StatusAttemptsExhausted:
			s.ExcludedAttemptsExhausted++
			continue
		}
		sev := strings.ToUpper(strings.TrimSpace(sum.Severity))
		row, ok := rows[sev]
		if !ok {
			row = &debtCleanSeverityRow{Severity: sev}
			rows[sev] = row
			extra = append(extra, sev)
		}
		switch bucket {
		case localdebt.StatusDeferred:
			s.Deferred++
			row.Deferred++
		case localdebt.StatusUnreproducible:
			s.Unreproducible++
			row.Unreproducible++
		default:
			s.Open++
			row.Open++
		}
		s.Total++
		kept = append(kept, sum)
	}
	sort.Strings(extra)
	for _, sev := range append(append([]string{}, debtCleanSeverities...), extra...) {
		s.BySeverity = append(s.BySeverity, *rows[sev])
	}
	return s, kept
}

// renderDebtCleanAXI emits {items, summary} as standard TOON, items in the same
// tabular shape debt filter uses and the Stats table as a tabular array.
func renderDebtCleanAXI(w io.Writer, doc debtCleanDoc) error {
	rows := debtItemAXIRows(doc.Items)
	s := doc.Summary
	bySev := make([]toon.Object, 0, len(s.BySeverity))
	for _, r := range s.BySeverity {
		bySev = append(bySev, toon.NewObject(
			toon.Field{Key: "severity", Value: goaxi.SanitizeString(r.Severity)},
			toon.Field{Key: "open", Value: r.Open},
			toon.Field{Key: "deferred", Value: r.Deferred},
			toon.Field{Key: "resolved", Value: r.Resolved},
			toon.Field{Key: "unreproducible", Value: r.Unreproducible},
		))
	}
	summary := toon.NewObject(
		toon.Field{Key: "open", Value: s.Open},
		toon.Field{Key: "deferred", Value: s.Deferred},
		toon.Field{Key: "resolved", Value: s.Resolved},
		toon.Field{Key: "unreproducible", Value: s.Unreproducible},
		toon.Field{Key: "total", Value: s.Total},
		toon.Field{Key: "excluded_resolved", Value: s.ExcludedResolved},
		toon.Field{Key: "excluded_wontfix", Value: s.ExcludedWontfix},
		toon.Field{Key: "excluded_attempts_exhausted", Value: s.ExcludedAttemptsExhausted},
		toon.Field{Key: "by_severity", Value: bySev},
	)
	payload := toon.NewObject(
		toon.Field{Key: "items", Value: rows},
		toon.Field{Key: "summary", Value: summary},
	)
	if _, err := goaxi.EncodeChecked(w, payload); err != nil {
		return fmt.Errorf("axi encoder: %w", err)
	}
	return nil
}
