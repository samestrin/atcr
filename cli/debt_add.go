package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/localdebt"
)

// debtStdinIsTTY reports whether stdin is an interactive terminal. It is a
// package var so tests can force the interactive path without a real TTY.
var debtStdinIsTTY = func(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// debtAddStatuses is the accepted --status enum, matching the three values the
// deleted .planning/-scoped store accepted. It is validated here because
// localdebt.Append performs no schema validation: that store enforced the enum
// inside tdmigrate.Item.Validate on the way in, and dropping the check during the
// port would let `debt add --status typo` write an unfilterable record.
//
// `wontfix` is deliberately NOT admitted, even though the store carries it.
// Dismissing a false positive is `atcr debt resolve --status wontfix --reason
// <why>`, which requires a justification precisely because wontfix is the one
// permanently-suppressing status: it silences every future re-detection of the
// same file/line/problem. Admitting it here would allow a permanent suppression
// with no recorded rationale, and — since resolve then treats the id as settled —
// no way to attach one afterwards. Filing a finding and dismissing it are also
// not the same act.
var debtAddStatuses = map[string]bool{"open": true, "deferred": true, "resolved": true}

// wizardDefaults seeds the interactive prompts with values already supplied as
// flags, so partial flag input carries into the wizard instead of being
// discarded.
type wizardDefaults struct {
	Severity, File, Problem, Fix, Category, Status string
	Est                                            int
}

func newDebtAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a technical-debt item (flag-driven; interactive when run on a TTY)",
		Long: "atcr debt add files a new item into the local technical-debt store\n" +
			"(.atcr/debt/), the same store list, dashboard, resolve, and compact read,\n" +
			"so the item is immediately listable and closeable. The appended item's id\n" +
			"is echoed on success for `atcr debt resolve <id>`.\n\n" +
			"Provide all required fields as flags for a non-interactive, scriptable add:\n" +
			"  --severity --file --problem --fix --category (--status/--est optional).\n" +
			"Omit them on an interactive terminal to be walked through a prompt instead.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runDebtAdd,
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().String("status", "open", "status: open|deferred|resolved (dismiss a false positive with `debt resolve --status wontfix --reason`)")
	cmd.Flags().String("severity", "", "severity: CRITICAL|HIGH|MEDIUM|LOW (required in flag mode)")
	cmd.Flags().String("file", "", "file:line location (required in flag mode)")
	cmd.Flags().String("problem", "", "problem description (required in flag mode)")
	cmd.Flags().String("fix", "", "recommended fix (required in flag mode)")
	cmd.Flags().String("category", "", "category label (required in flag mode)")
	cmd.Flags().Int("est", 0, "estimated minutes")
	return cmd
}

func normalizeSeverity(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
func normalizeStatus(s string) string   { return strings.ToLower(strings.TrimSpace(s)) }

// parseDebtFileLine splits a "path:line" location into the structured File and
// Line that localdebt.Record carries. Only a purely-numeric trailing segment is
// treated as a line number — the same trailing-digits rule the deleted store
// used to strip a line suffix — so a free-text value ("see docs: the thing"), a
// line RANGE ("a.go:1-9", which is not a single line), or a bare path is kept
// verbatim in File with Line 0.
//
// The split never yields an EMPTY File. ":42" is not a location, and a record
// with no File is unreachable through `atcr debt resolve`, which skips records
// it cannot act on (selectOpenDebt) — the item would list but never close. Such
// a value is kept verbatim so the required-field check in finalizeDebtRecord
// sees it and rejects the add outright.
func parseDebtFileLine(v string) (string, int) {
	// Trim first: surrounding whitespace on a flag value would otherwise make the
	// numeric tail test fail ("a.go:1 " is not all digits), silently downgrading a
	// real location to Line 0.
	v = strings.TrimSpace(v)
	i := strings.LastIndex(v, ":")
	if i <= 0 || i == len(v)-1 {
		return v, 0
	}
	tail := v[i+1:]
	for _, r := range tail {
		if r < '0' || r > '9' {
			return v, 0 // not a line suffix; keep verbatim
		}
	}
	n, err := strconv.Atoi(tail)
	if err != nil {
		return v, 0 // a digit run that overflows an int is not a usable line number
	}
	return v[:i], n
}

func missingRequiredFlags(sev, file, problem, fix, category string) []string {
	var missing []string
	if sev == "" {
		missing = append(missing, "--severity")
	}
	if file == "" {
		missing = append(missing, "--file")
	}
	if problem == "" {
		missing = append(missing, "--problem")
	}
	if fix == "" {
		missing = append(missing, "--fix")
	}
	if category == "" {
		missing = append(missing, "--category")
	}
	return missing
}

func runDebtAdd(cmd *cobra.Command, _ []string) error {
	est, _ := cmd.Flags().GetInt("est")
	sev := mustFlag(cmd, "severity")
	file := mustFlag(cmd, "file")
	problem := mustFlag(cmd, "problem")
	fix := mustFlag(cmd, "fix")
	category := mustFlag(cmd, "category")
	def := wizardDefaults{
		Severity: sev, File: file, Problem: problem, Fix: fix, Category: category,
		Status: mustFlag(cmd, "status"), Est: est,
	}

	var rec localdebt.Record
	switch {
	case sev != "" && file != "" && problem != "" && fix != "" && category != "":
		// Flag mode — the scriptable, primary contract.
		rec = localdebt.Record{
			Severity: sev, Problem: problem, Fix: fix, Category: category,
			EstMinutes: est, Status: def.Status,
		}
		rec.File, rec.Line = parseDebtFileLine(file)
	case debtStdinIsTTY(cmd.InOrStdin()):
		// Interactive wizard — only when we can actually prompt a human. Any
		// required flags already supplied were seeded into def above, so partial
		// flag input carries into the prompts instead of being discarded.
		var err error
		rec, err = promptEntry(cmd.InOrStdin(), cmd.OutOrStdout(), def)
		if err != nil {
			return err
		}
	case sev != "" || file != "" || problem != "" || fix != "" || category != "":
		// Some but not all required flags were provided and there is no TTY to
		// finish the rest; name the missing ones.
		missing := missingRequiredFlags(sev, file, problem, fix, category)
		return usageError(fmt.Errorf("missing required flags (%s)", strings.Join(missing, ", ")))
	default:
		missing := missingRequiredFlags(sev, file, problem, fix, category)
		return usageError(fmt.Errorf("missing required flags (%s); provide them or run on an interactive terminal", strings.Join(missing, ", ")))
	}

	if err := finalizeDebtRecord(&rec); err != nil {
		return err
	}
	dir := mustFlag(cmd, "dir")
	if err := localdebt.Append(dir, rec); err != nil {
		return fmt.Errorf("atcr debt add: failed to file the item: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Added %s item %s to %s.\n", rec.Severity, rec.ID, dir)
	return nil
}

// finalizeDebtRecord validates the user-supplied fields and stamps the
// store-owned ones (schema version, synthetic run id, timestamp, origin, id).
// It is shared by the flag and wizard paths so the wizard is never a validation
// bypass, and it runs BEFORE the append so a rejected item writes nothing.
func finalizeDebtRecord(rec *localdebt.Record) error {
	rec.Severity = normalizeSeverity(rec.Severity)
	if !resolveSeverities[rec.Severity] {
		return usageError(fmt.Errorf("invalid severity %q: expected CRITICAL|HIGH|MEDIUM|LOW", rec.Severity))
	}
	// The required-field presence check the deleted tdmigrate.Item.Validate
	// enforced on the way into the old store. The command's own gate is a bare
	// != "" on the flag values, so a whitespace-only answer passes it; without
	// this, `--file "   "` files an item that debt resolve can never act on.
	// Trimming BEFORE StampID also keeps the content-hash id from being computed
	// over incidental padding, so the same finding hashes the same either way.
	rec.File = strings.TrimSpace(rec.File)
	rec.Problem = strings.TrimSpace(rec.Problem)
	rec.Fix = strings.TrimSpace(rec.Fix)
	rec.Category = strings.TrimSpace(rec.Category)
	for _, req := range []struct{ flag, val string }{
		{"--file", rec.File}, {"--problem", rec.Problem}, {"--fix", rec.Fix}, {"--category", rec.Category},
	} {
		if req.val == "" {
			return usageError(fmt.Errorf("%s is required and cannot be blank", req.flag))
		}
	}
	status := normalizeStatus(rec.Status)
	if status == "" {
		status = "open"
	}
	if !debtAddStatuses[status] {
		return usageError(fmt.Errorf("invalid status %q: expected open|deferred|resolved (use `debt resolve --status wontfix --reason <why>` to dismiss a finding)", rec.Status))
	}
	// "open" is spelled as the EMPTY status on disk — the same value the
	// reconcile hook writes — so one finding never folds against two spellings of
	// the same state.
	if status == "open" {
		status = ""
	}
	rec.Status = status
	if rec.EstMinutes < 0 {
		return usageError(fmt.Errorf("invalid --est %d: expected a non-negative number of minutes", rec.EstMinutes))
	}

	// A manual add has no reconcile run behind it, so it carries the synthetic
	// run id whose YYYY-MM prefix resolves the month shard (localdebt.ManualRunID),
	// mirroring the resolution path's construction. time.Now() is always inside
	// ManualRunID's documented four-digit-year precondition.
	ts := time.Now().UTC()
	rec.SchemaVersion = localdebt.SchemaVersion
	rec.RunID = localdebt.ManualRunID(ts)
	rec.Timestamp = ts.Format(time.RFC3339)
	rec.Origin = localdebt.OriginManual
	// A filed item is its own first sighting, recorded as a carrier so the count
	// persists. Without this an item filed with a status (--status deferred) is
	// never a "detection" to the fold's counting rule, so it contributes nothing to
	// carry forward and its first genuine re-detection reports zero regressions
	// (localdebt.aggregateCounters).
	rec.Occurrences = 1
	rec.FirstSeen = rec.Timestamp
	rec.StampID()
	return nil
}

// promptEntry runs the interactive wizard against in/out, returning the record
// to file. An empty answer takes the seeded default; required fields (severity,
// file, problem, fix, category) are re-prompted when left blank and error only
// if the input stream ends first. The returned record carries only the
// user-supplied fields — finalizeDebtRecord validates and stamps the rest.
func promptEntry(in io.Reader, out io.Writer, def wizardDefaults) (localdebt.Record, error) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var perr error
	ask := func(label, dflt string, required bool) string {
		if perr != nil {
			return ""
		}
		for {
			if dflt != "" {
				_, _ = fmt.Fprintf(out, "%s [%s]: ", label, dflt)
			} else {
				_, _ = fmt.Fprintf(out, "%s: ", label)
			}
			if !sc.Scan() {
				if dflt != "" {
					return dflt
				}
				if !required {
					return ""
				}
				perr = fmt.Errorf("input ended before required field %q was provided", label)
				return ""
			}
			v := strings.TrimSpace(sc.Text())
			if v == "" {
				v = dflt
			}
			if v == "" && required {
				_, _ = fmt.Fprintf(out, "  %s is required; please enter a value.\n", label)
				continue
			}
			return v
		}
	}

	sev := ask("Severity (CRITICAL|HIGH|MEDIUM|LOW)", def.Severity, true)
	file := ask("File (file:line)", def.File, true)
	problem := ask("Problem", def.Problem, true)
	fix := ask("Fix", def.Fix, true)
	category := ask("Category", def.Category, true)
	estStr := ask("Est minutes", strconv.Itoa(def.Est), false)
	status := ask("Status (open|deferred|resolved)", def.Status, false)

	if perr != nil {
		return localdebt.Record{}, perr
	}
	if err := sc.Err(); err != nil {
		return localdebt.Record{}, fmt.Errorf("input read error: %w", err)
	}

	est := def.Est
	if n, err := strconv.Atoi(strings.TrimSpace(estStr)); err == nil {
		est = n
	} else if strings.TrimSpace(estStr) != "" {
		_, _ = fmt.Fprintf(out, "  est %q is not an integer; using %d\n", estStr, def.Est)
	}

	rec := localdebt.Record{
		Severity: sev, Problem: problem, Fix: fix, Category: category,
		EstMinutes: est, Status: status,
	}
	rec.File, rec.Line = parseDebtFileLine(file)
	return rec, nil
}
