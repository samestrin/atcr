package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/debt"
	"github.com/samestrin/atcr/internal/tdmigrate"
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

// wizardDefaults seeds the interactive prompts (and flag-mode section fields)
// with sensible defaults that an empty answer falls back to.
type wizardDefaults struct {
	Date, SourceType, Label string
	Group, Status, Source   string
	Est                     int
}

func newDebtAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a technical-debt item (flag-driven; interactive when run on a TTY)",
		Long: "atcr debt add files a new item into the authoritative README table and\n" +
			"regenerates the shard store so the item is immediately queryable.\n\n" +
			"Provide all required fields as flags for a non-interactive, scriptable add:\n" +
			"  --severity --file --problem --fix --category (\n--group/--status/--est/--source optional).\n" +
			"Omit them on an interactive terminal to be walked through a prompt instead.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runDebtAdd,
	}
	cmd.Flags().String("items", defaultTDItems, "path to the sharded technical-debt store")
	cmd.Flags().String("readme", defaultTDReadme, "path to the authoritative technical-debt README")
	cmd.Flags().String("date", "", "section date YYYY-MM-DD (default: today, UTC)")
	cmd.Flags().String("label", "manual", "section label")
	cmd.Flags().String("source-type", "Sprint", "section source type: Sprint|Review")
	cmd.Flags().String("group", "U", "group label")
	cmd.Flags().String("status", "open", "status: open|deferred|resolved")
	cmd.Flags().String("severity", "", "severity: CRITICAL|HIGH|MEDIUM|LOW (required in flag mode)")
	cmd.Flags().String("file", "", "file:line location (required in flag mode)")
	cmd.Flags().String("problem", "", "problem description (required in flag mode)")
	cmd.Flags().String("fix", "", "recommended fix (required in flag mode)")
	cmd.Flags().String("category", "", "category label (required in flag mode)")
	cmd.Flags().Int("est", 0, "estimated minutes")
	cmd.Flags().String("source", "manual", "capture source")
	return cmd
}

func todayUTC() string { return time.Now().UTC().Format("2006-01-02") }

func validateDate(date string) error {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return usageError(fmt.Errorf("invalid date %q: expected YYYY-MM-DD", date))
	}
	return nil
}

func normalizeSeverity(s string) string { return strings.ToUpper(s) }
func normalizeStatus(s string) string   { return strings.ToLower(s) }
func normalizeSourceType(s string) string {
	switch strings.ToLower(s) {
	case "sprint":
		return tdmigrate.SourceTypeSprint
	case "review":
		return tdmigrate.SourceTypeReview
	default:
		return s
	}
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
	readme := mustFlag(cmd, "readme")
	items := mustFlag(cmd, "items")
	date := mustFlag(cmd, "date")
	if date == "" {
		date = todayUTC()
	}
	if err := validateDate(date); err != nil {
		return err
	}
	est, _ := cmd.Flags().GetInt("est")
	def := wizardDefaults{
		Date: date, SourceType: mustFlag(cmd, "source-type"), Label: mustFlag(cmd, "label"),
		Group: mustFlag(cmd, "group"), Status: mustFlag(cmd, "status"), Source: mustFlag(cmd, "source"),
		Est: est,
	}

	sev := mustFlag(cmd, "severity")
	file := mustFlag(cmd, "file")
	problem := mustFlag(cmd, "problem")
	fix := mustFlag(cmd, "fix")
	category := mustFlag(cmd, "category")

	var (
		sec debt.Section
		it  tdmigrate.Item
	)
	switch {
	case sev != "" && file != "" && problem != "" && fix != "" && category != "":
		// Flag mode — the scriptable, primary contract.
		sec = debt.Section{Date: def.Date, SourceType: normalizeSourceType(def.SourceType), Label: def.Label}
		it = tdmigrate.Item{
			Group: def.Group, Status: normalizeStatus(def.Status), Severity: normalizeSeverity(sev),
			File: file, Problem: problem, Fix: fix, Category: category,
			EstMinutes: est, Source: def.Source,
		}
	case sev != "" || file != "" || problem != "" || fix != "" || category != "":
		// Some but not all required flags were provided; name the missing ones.
		missing := missingRequiredFlags(sev, file, problem, fix, category)
		return usageError(fmt.Errorf("missing required flags (%s)", strings.Join(missing, ", ")))
	case debtStdinIsTTY(cmd.InOrStdin()):
		// Interactive wizard — only when we can actually prompt a human.
		var err error
		sec, it, err = promptEntry(cmd.InOrStdin(), cmd.OutOrStdout(), def)
		if err != nil {
			return err
		}
	default:
		missing := missingRequiredFlags(sev, file, problem, fix, category)
		return usageError(fmt.Errorf("missing required flags (%s); provide them or run on an interactive terminal", strings.Join(missing, ", ")))
	}

	if err := debt.AppendItem(readme, items, sec, it, cmd.ErrOrStderr()); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Added %s item to %s under [%s] From %s: %s.\n",
		it.Severity, readme, sec.Date, sec.SourceType, sec.Label)
	return nil
}

// promptEntry runs the interactive wizard against in/out, returning the Section
// and Item to file. An empty answer takes the seeded default; required fields
// (label, severity, file, problem, fix, category) are re-prompted when left
// blank and error only if the input stream ends first.
func promptEntry(in io.Reader, out io.Writer, def wizardDefaults) (debt.Section, tdmigrate.Item, error) {
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

	date := ask("Date (YYYY-MM-DD)", def.Date, false)
	if err := validateDate(date); err != nil {
		return debt.Section{}, tdmigrate.Item{}, err
	}
	stype := ask("Source type (Sprint|Review)", def.SourceType, false)
	label := ask("Label", def.Label, true)
	group := ask("Group", def.Group, false)
	sev := ask("Severity (CRITICAL|HIGH|MEDIUM|LOW)", "", true)
	file := ask("File (file:line)", "", true)
	problem := ask("Problem", "", true)
	fix := ask("Fix", "", true)
	category := ask("Category", "", true)
	estStr := ask("Est minutes", strconv.Itoa(def.Est), false)
	status := ask("Status (open|deferred|resolved)", def.Status, false)
	source := ask("Source", def.Source, false)

	if perr != nil {
		return debt.Section{}, tdmigrate.Item{}, perr
	}

	est := def.Est
	if n, err := strconv.Atoi(strings.TrimSpace(estStr)); err == nil {
		est = n
	} else if strings.TrimSpace(estStr) != "" {
		_, _ = fmt.Fprintf(out, "  est %q is not an integer; using %d\n", estStr, def.Est)
	}

	sec := debt.Section{Date: date, SourceType: normalizeSourceType(stype), Label: label}
	it := tdmigrate.Item{
		Group: group, Status: normalizeStatus(status), Severity: normalizeSeverity(sev),
		File: file, Problem: problem, Fix: fix, Category: category,
		EstMinutes: est, Source: source,
	}
	return sec, it, nil
}
