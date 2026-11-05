package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/registry"
)

// importRunSuffix ends the run id of every imported record, so an import's
// records are legible in the raw store beside reconcile's and add's.
const importRunSuffix = "-import"

// importSectionDate reads the date of a `### [<date>] From: ...` heading.
var importSectionDate = regexp.MustCompile(`^###\s+\[([^\]]+)\]`)

// importDateLayouts are the section-date spellings in use: ISO, and the long
// form a few older sections carry ("September 17, 2026").
var importDateLayouts = []string{"2006-01-02", "January 2, 2006"}

func newDebtImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <README>",
		Short: "Import a technical-debt README table into the local TD store",
		Long: "atcr debt import reads every item row of a technical-debt README table\n" +
			"and writes it to the local TD store (either layout) with its status\n" +
			"([ ] open, [/] deferred, [x] resolved, [-] unreproducible) and attempts.\n\n" +
			"A record's time and run id come from the date of the row's\n" +
			"`### [date]` section, never the clock, so importing the same table\n" +
			"again writes nothing. Rows that hash to one id are merged (the newest\n" +
			"section's row is kept); rows that cannot be imported as written are\n" +
			"rejected and listed. imported + merged + rejected is every item row.\n" +
			"Control characters in a cell are stripped. --dry-run writes nothing.",
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: runDebtImport,
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().Bool("dry-run", false, "report what would be imported without changing the store")
	return cmd
}

// debtImportRow is one parsed item row and what the import made of it.
type debtImportRow struct {
	Item localdebt.TDItem
	// Record is the record the row imports as (Imported and Merged rows).
	Record localdebt.Record
	// Reason says why a Rejected row was rejected.
	Reason string
	// KeptLine is the README line of the row a Merged row was merged into.
	KeptLine int
}

// debtImportPlan sorts every parsed item row into exactly one of three lists.
type debtImportPlan struct {
	Imported, Merged, Rejected []debtImportRow
}

func runDebtImport(cmd *cobra.Command, args []string) error {
	readme := args[0]
	f, err := os.Open(readme)
	if err != nil {
		return fmt.Errorf("atcr debt import: %w", localdebt.RedactPathErr(err))
	}
	items, err := localdebt.ParseTDItems(f)
	_ = f.Close()
	if err != nil {
		return fmt.Errorf("atcr debt import: reading %s: %w", filepath.Base(readme), err)
	}
	dir, err := debtStoreDir(cmd)
	if err != nil {
		return err
	}
	plan, err := planDebtImport(items, importSourcePath(readme))
	if err != nil {
		return err
	}

	// Skip every record the store already holds, so a rerun (or the rest of an
	// interrupted run) writes only what is missing and changes no file.
	have, err := storedImportKeys(dir, cmd.ErrOrStderr())
	if err != nil {
		return fmt.Errorf("atcr debt import: reading the store: %w", err)
	}
	var pending []localdebt.Record
	for _, row := range plan.Imported {
		if !have[importKey(row.Record)] {
			pending = append(pending, row.Record)
		}
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	written := 0
	if !dryRun {
		for _, rec := range pending {
			if err := localdebt.Append(dir, rec); err != nil {
				return fmt.Errorf("atcr debt import: wrote %d of %d records: %w", written, len(pending), err)
			}
			written++
		}
	}
	writeDebtImportReport(cmd.OutOrStdout(), plan, len(items), len(pending), written, dryRun)
	return nil
}

// planDebtImport turns parsed rows into records and sorts each row into
// Imported, Merged or Rejected. It reads no clock and touches no file: a row's
// time comes from its section, so the same table always plans the same records.
func planDebtImport(items []localdebt.TDItem, source string) (debtImportPlan, error) {
	var plan debtImportPlan
	var valid []debtImportRow
	for _, it := range items {
		rec, reason, err := importRecord(it, source)
		if err != nil {
			return plan, err
		}
		if reason != "" {
			plan.Rejected = append(plan.Rejected, debtImportRow{Item: it, Reason: reason})
			continue
		}
		valid = append(valid, debtImportRow{Item: it, Record: rec})
	}

	// Rows hashing to one id are one finding. The newest section's row is kept,
	// the later row in the file on a tie, so the kept row is its latest state.
	kept := map[string]int{} // id -> index into valid
	for i, row := range valid {
		j, seen := kept[row.Record.ID]
		if !seen || row.Record.Timestamp >= valid[j].Record.Timestamp {
			kept[row.Record.ID] = i
		}
	}
	for i, row := range valid {
		j := kept[row.Record.ID]
		if i == j {
			plan.Imported = append(plan.Imported, row)
			continue
		}
		row.KeptLine = valid[j].Item.Line
		plan.Merged = append(plan.Merged, row)
	}
	return plan, nil
}

// importRecord builds the record a row imports as, or says why it cannot be
// imported. Every text cell is stripped of control characters first, so the id
// is a hash of the text the store keeps.
func importRecord(it localdebt.TDItem, source string) (localdebt.Record, string, error) {
	if it.Invalid != "" {
		return localdebt.Record{}, it.Invalid, nil
	}
	status, _, known := localdebt.TDMarkerStatus(it.Marker)
	if !known {
		return localdebt.Record{}, fmt.Sprintf("unknown marker %q", it.Marker), nil
	}
	ts, reason := importSectionTime(it.Section)
	if reason != "" {
		return localdebt.Record{}, reason, nil
	}
	clean := func(s string) string { return strings.TrimSpace(stripTerminalDrivers(s)) }
	rec := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion,
		RunID:         ts + importRunSuffix,
		Timestamp:     ts,
		Severity:      normalizeSeverity(clean(it.Severity)),
		File:          clean(it.File),
		Line:          it.FileLine,
		Problem:       clean(it.Problem),
		Fix:           clean(it.Fix),
		Category:      clean(it.Category),
		EstMinutes:    it.EstMinutes,
		Confidence:    clean(it.Confidence),
		Origin:        localdebt.OriginManual,
		Attempts:      it.Attempts,
		Status:        status,
		// The README path and section are kept so a report can group by section.
		// The row's line is not: inserting a row above it would change every
		// record below and defeat the rerun check.
		SourceReport: &localdebt.SourceReport{Path: source, Section: clean(it.Section)},
	}
	for _, r := range it.Reviewers {
		if r = clean(r); r != "" {
			rec.Reviewers = append(rec.Reviewers, r)
		}
	}
	if localdebt.IsClosedStatus(status) && status != localdebt.StatusDeferred {
		rec.ResolvedAt = ts
	}
	switch {
	case !resolveSeverities[rec.Severity]:
		return localdebt.Record{}, fmt.Sprintf("severity %q is not CRITICAL, HIGH, MEDIUM or LOW", rec.Severity), nil
	case rec.File == "":
		return localdebt.Record{}, "the File cell is empty", nil
	case rec.Problem == "":
		return localdebt.Record{}, "the Problem cell is empty", nil
	case rec.EstMinutes > registry.MaxExecutorEstimatedMinutes:
		return localdebt.Record{}, fmt.Sprintf("Est Minutes %d is over %d", rec.EstMinutes, registry.MaxExecutorEstimatedMinutes), nil
	}
	rec.StampID()
	line, err := json.Marshal(rec)
	if err != nil {
		return localdebt.Record{}, "", fmt.Errorf("atcr debt import: encoding line %d: %w", it.Line, err)
	}
	if len(line)+1 > localdebt.MaxRecordBytes {
		return localdebt.Record{}, fmt.Sprintf("the record is %d bytes encoded, over the store's %d", len(line)+1, localdebt.MaxRecordBytes), nil
	}
	return rec, "", nil
}

// importSectionTime reads a section heading's date as midnight UTC in RFC3339,
// or says why the row cannot be dated from it.
func importSectionTime(section string) (ts, reason string) {
	const undated = "the row is under no dated `### [date]` section"
	m := importSectionDate.FindStringSubmatch(section)
	if m == nil {
		return "", undated
	}
	date := strings.TrimSpace(m[1])
	for _, layout := range importDateLayouts {
		if t, err := time.Parse(layout, date); err == nil {
			// time.Parse accepts year 0000, which the store cannot shard; the row
			// is rejected here rather than failing the whole import at Append.
			if _, err := localdebt.ManualRunID(t); err != nil {
				return "", fmt.Sprintf("section date %q is outside the years the store can file (1-9999)", date)
			}
			return t.UTC().Format(time.RFC3339), ""
		}
	}
	return "", undated
}

// importSourcePath names the README in a record: relative to the repo root when
// it is inside it, its base name otherwise, so no absolute path (and the user
// name in it) is written to a shared store.
func importSourcePath(readme string) string {
	abs, err := filepath.Abs(readme)
	if err != nil {
		return filepath.Base(readme)
	}
	if root, err := debtRepoRoot(); err == nil {
		if rel, err := filepath.Rel(root, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Base(readme)
}

// importKey is the identity of a record for the rerun check: its encoding with
// the counters a fold or compaction stamps zeroed, so a compacted copy of an
// imported record still matches the record the row imports as.
func importKey(rec localdebt.Record) string {
	rec.Occurrences, rec.CountedThrough, rec.FirstSeen = 0, "", ""
	b, err := json.Marshal(rec)
	if err != nil {
		return ""
	}
	return string(b)
}

// storedImportKeys reads the store's import keys. A store that does not exist yet
// holds none.
func storedImportKeys(dir string, diag io.Writer) (map[string]bool, error) {
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: diag})
	if err != nil && !os.IsNotExist(err) {
		return nil, localdebt.RedactPathErr(err)
	}
	have := make(map[string]bool, len(recs))
	for _, r := range recs {
		have[importKey(r)] = true
	}
	return have, nil
}

// writeDebtImportReport prints the counts, then each merged and rejected row.
func writeDebtImportReport(w io.Writer, plan debtImportPlan, parsed, pending, written int, dryRun bool) {
	_, _ = fmt.Fprintf(w, "%d item rows: %d imported, %d merged, %d rejected.\n",
		parsed, len(plan.Imported), len(plan.Merged), len(plan.Rejected))
	already := len(plan.Imported) - pending
	if dryRun {
		_, _ = fmt.Fprintf(w, "Dry run: would write %d records (%d already in the store).\n", pending, already)
	} else {
		_, _ = fmt.Fprintf(w, "Import: wrote %d records (%d already in the store).\n", written, already)
	}
	for _, row := range plan.Merged {
		_, _ = fmt.Fprintf(w, "  merged   line %d: id %s, kept line %d\n", row.Item.Line, row.Record.ID, row.KeptLine)
	}
	for _, row := range plan.Rejected {
		_, _ = fmt.Fprintf(w, "  rejected line %d: %s\n", row.Item.Line, sanitizeCell(row.Reason))
	}
}
