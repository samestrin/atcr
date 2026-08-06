package cli

import (
	"bufio"
	"encoding/json"
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
	// No backticks in this usage string: cobra's UnquoteUsage reads the first
	// backtick-quoted span as the flag's VALUE PLACEHOLDER, so `debt resolve
	// --status wontfix --reason` rendered in --help as if --status took four
	// arguments.
	cmd.Flags().String("status", "open", "status: open|deferred|resolved (dismiss a false positive with 'debt resolve --status wontfix --reason')")
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
// A numeric tail of 0 ("a.go:0", "a.go:00") is rejected: line numbers are
// 1-based, and Line 0 is the sentinel localdebt.Record carries for "no line
// recorded". Accepting it would make "a.go:0" indistinguishable from "a.go" —
// StampID hashes file+line+problem, so the two spellings would collide onto one
// id and a second `debt add` with the other spelling would silently reuse an
// existing record's id.
//
// The split never yields an EMPTY File. ":42" is not a location, and a record
// with no File is unreachable through `atcr debt resolve`, which skips records
// it cannot act on (selectOpenDebt) — the item would list but never close. Such
// a value is kept verbatim so the required-field check in finalizeDebtRecord
// sees it and rejects the add outright.
func parseDebtFileLine(v string) (string, int, error) {
	// Trim first: surrounding whitespace on a flag value would otherwise make the
	// numeric tail test fail ("a.go:1 " is not all digits), silently downgrading a
	// real location to Line 0.
	v = strings.TrimSpace(v)
	i := strings.LastIndex(v, ":")
	if i <= 0 || i == len(v)-1 {
		return v, 0, nil
	}
	tail := v[i+1:]
	for _, r := range tail {
		if r < '0' || r > '9' {
			return v, 0, nil // not a line suffix; keep verbatim
		}
	}
	n, err := strconv.Atoi(tail)
	if err != nil {
		return v, 0, nil // a digit run that overflows an int is not a usable line number
	}
	if n == 0 {
		return "", 0, fmt.Errorf("invalid location %q: line numbers are 1-based (a :0 suffix records no line)", v)
	}
	return v[:i], n, nil
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
		f, line, err := parseDebtFileLine(file)
		if err != nil {
			return usageError(err)
		}
		rec.File, rec.Line = f, line
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
	dir := debtStoreDir(cmd)
	if err := localdebt.Append(dir, rec); err != nil {
		return fmt.Errorf("atcr debt add: failed to file the item: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Added %s item %s to %s.\n", rec.Severity, rec.ID, dir)
	warnDebtAddSuperseded(cmd, dir, rec)
	return nil
}

// warnDebtAddSuperseded reports, on stderr, that the record just appended is NOT
// the effective one for its id.
//
// StampID hashes file+line+problem, so re-filing an identical finding reuses the
// id of an existing record. When that id already carries a terminal status the
// fold can keep the OLD record — always for `wontfix`, which survives
// re-detection by design, and on a same-second timestamp tie where
// ClosedStatusRank lets the terminal record win. Both cases printed
// "Added <id>" and exited 0 while every reader still showed the terminal status:
// a silent no-op wearing a success message.
//
// It is a warning, not an error, and the exit code is unchanged: the append
// really did happen (the store is append-only) and a script that files findings
// in bulk must not start failing. It reads SUMMARIES rather than full records —
// the id and status are all it needs — so the check costs one streaming pass and
// no record materialization. Any read failure is silently ignored: this is
// advisory output on an already-successful write.
func warnDebtAddSuperseded(cmd *cobra.Command, dir string, rec localdebt.Record) {
	sums, err := localdebt.ReadSummaries(dir, localdebt.ReadOpts{})
	if err != nil {
		return
	}
	for _, s := range localdebt.FoldSummaries(sums) {
		if s.ID != rec.ID {
			continue
		}
		// Only a DIFFERENT effective status means the add was superseded. Filing
		// `--status resolved` onto an already-resolved id changes nothing a reader
		// can see, so there is nothing to warn about.
		if !localdebt.IsClosedStatus(s.Status) || strings.EqualFold(s.Status, rec.Status) {
			return
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"warning: %s already carries status %q, which wins the fold — the item stays %s in `atcr debt list` and is not on the `atcr debt resolve` worklist\n",
			rec.ID, s.Status, s.Status)
		return
	}
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
	// mirroring the resolution path's construction. time.Now() can never trip
	// ManualRunID's year validation; the error check keeps the validated
	// (string, error) contract honest rather than discarding it.
	ts := time.Now().UTC()
	rec.SchemaVersion = localdebt.SchemaVersion
	runID, err := localdebt.ManualRunID(ts)
	if err != nil {
		return fmt.Errorf("stamping the item's run id: %w", err)
	}
	rec.RunID = runID
	rec.Timestamp = ts.Format(time.RFC3339)
	rec.Origin = localdebt.OriginManual
	// Occurrences/FirstSeen are deliberately left ZERO. A filed item IS its own
	// first sighting, but stamping it as a counting carrier here would make it the
	// boundary for every earlier detection of the same id and silently DECREASE the
	// count when a user files something that hashes to an id the store already
	// holds. localdebt.aggregateCounters instead recognises a manual filing as a
	// sighting from Origin + an empty ResolvedAt, which needs no stamp and cannot
	// erase history.
	rec.StampID()

	// Bound the ENCODED record, not the individual fields. Every read path skips a
	// JSONL line longer than localdebt.MaxRecordBytes, so a record over that cap is
	// written, echoed as "Added <id>", and then invisible to every reader forever —
	// while permanently protecting its shard from compaction. `debt resolve
	// --reason` bounds its justification for exactly this reason; the add path
	// bounded nothing, and the wizard's scanner accepts 1 MiB per answer, so two
	// pasted fields reach the cap. Checking the marshaled form (plus the newline
	// Append adds) is what makes the bound exact rather than a per-field guess.
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encoding the item: %w", err)
	}
	if len(line)+1 > localdebt.MaxRecordBytes {
		return usageError(fmt.Errorf(
			"item is too large to store: %d bytes encoded, cap is %d — shorten --problem/--fix (the store skips longer lines on read, so it would be filed and never seen)",
			len(line)+1, localdebt.MaxRecordBytes))
	}
	return nil
}

// validDebtSeverity, validDebtStatus and validDebtEst are the wizard's prompt-time
// validators. They check exactly what finalizeDebtRecord checks — the same
// package-level enum maps, the same non-negative est — so the wizard cannot admit
// a value the finalizer would then reject, and the error names the accepted set
// so the re-prompt tells the user what to type.
func validDebtSeverity(v string) error {
	if !resolveSeverities[normalizeSeverity(v)] {
		return fmt.Errorf("invalid severity %q: expected CRITICAL|HIGH|MEDIUM|LOW", v)
	}
	return nil
}

func validDebtStatus(v string) error {
	if !debtAddStatuses[normalizeStatus(v)] {
		return fmt.Errorf("invalid status %q: expected open|deferred|resolved (dismiss a finding with `debt resolve --status wontfix --reason <why>`)", v)
	}
	return nil
}

// validDebtEst rejects only a NEGATIVE number of minutes. A non-numeric answer is
// deliberately left to the caller's existing fall-back-to-the-default warning:
// est is optional, and that behavior is the documented one.
func validDebtEst(v string) error {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n < 0 {
		return fmt.Errorf("invalid est %d: expected a non-negative number of minutes", n)
	}
	return nil
}

// promptEntry runs the interactive wizard against in/out, returning the record
// to file. An empty answer takes the seeded default; required fields (severity,
// file, problem, fix, category) are re-prompted when left blank and error if the
// input stream ends first. A mid-stream read failure is NOT end-of-input: it
// aborts the wizard with the underlying cause ("input read error: ...") rather
// than letting later prompts silently accept seeded defaults off a dead scanner.
// The returned record carries only the user-supplied fields — finalizeDebtRecord
// validates and stamps the rest.
func promptEntry(in io.Reader, out io.Writer, def wizardDefaults) (localdebt.Record, error) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var perr error
	// ask takes an optional validator so a bad VALUE is caught at the prompt, where
	// the user can retype it, instead of by finalizeDebtRecord after all seven
	// prompts — which failed the command and discarded every answer already typed.
	// A nil validator accepts anything, which is the free-text fields' behavior.
	ask := func(label, dflt string, required bool, valid func(string) error) string {
		if perr != nil {
			return ""
		}
		// The last value this prompt rejected, if any. When the stream ends while a
		// field has only ever been given invalid values, the failure is the VALUE,
		// not the missing input: it stays a usage error (exit 2), the same class
		// finalizeDebtRecord returned before the check moved to prompt time.
		var lastInvalid error
		for {
			if dflt != "" {
				_, _ = fmt.Fprintf(out, "%s [%s]: ", label, dflt)
			} else {
				_, _ = fmt.Fprintf(out, "%s: ", label)
			}
			if !sc.Scan() {
				// A false Scan is not always EOF: a scanner failure
				// (bufio.ErrTooLong, an I/O error) must surface its real cause
				// rather than masquerade as end-of-input. Latching it into perr
				// also stops every later ask from silently accepting a seeded
				// default off a dead scanner.
				if err := sc.Err(); err != nil {
					perr = fmt.Errorf("input read error: %w", err)
					return ""
				}
				if dflt != "" {
					// A seeded default the validator rejects must not be accepted
					// silently at end-of-input: it would file exactly the invalid
					// value the prompt was re-asking for.
					if valid != nil {
						if err := valid(dflt); err != nil {
							perr = usageError(err)
							return ""
						}
					}
					return dflt
				}
				if lastInvalid != nil {
					perr = usageError(lastInvalid)
					return ""
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
			if v != "" && valid != nil {
				if err := valid(v); err != nil {
					lastInvalid = err
					_, _ = fmt.Fprintf(out, "  %v\n", err)
					continue
				}
			}
			return v
		}
	}

	sev := ask("Severity (CRITICAL|HIGH|MEDIUM|LOW)", def.Severity, true, validDebtSeverity)
	file := ask("File (file:line)", def.File, true, nil)
	problem := ask("Problem", def.Problem, true, nil)
	fix := ask("Fix", def.Fix, true, nil)
	category := ask("Category", def.Category, true, nil)
	estStr := ask("Est minutes", strconv.Itoa(def.Est), false, validDebtEst)
	status := ask("Status (open|deferred|resolved)", def.Status, false, validDebtStatus)

	if perr != nil {
		return localdebt.Record{}, perr
	}
	// Redundant for the ask path — a scanner failure already latched into perr —
	// but kept as a guard in case the prompt flow ever changes.
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
	f, line, err := parseDebtFileLine(file)
	if err != nil {
		// A rejected location is user-input validation, classified like every
		// other wizard answer validation (finalizeDebtRecord's usageError) and
		// like the flag path above — NOT like the wizard's exit-1 stream
		// failures ("input ended" / "input read error"), which are I/O.
		return localdebt.Record{}, usageError(err)
	}
	rec.File, rec.Line = f, line
	return rec, nil
}
