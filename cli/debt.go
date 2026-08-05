package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/localdebt"
)

// newDebtCmd builds `atcr debt`: query, capture, aggregate, resolve, and compact
// the local technical-debt store. As of Plan 35.13 all five subcommands read and
// write ONE store — the .atcr/-scoped, month-sharded JSONL backlog under
// .atcr/debt/ that `atcr reconcile` populates — resolved through a --dir flag
// with a shared default. The previous .planning/-scoped README+shard store that
// list/add/dashboard used is no longer read or written by any atcr code.
func newDebtCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debt",
		Short: "Query, capture, and report on technical debt",
		Long: "atcr debt reads and writes the local technical-debt store under\n" +
			".atcr/debt/ (month-sharded, append-only JSONL) that atcr reconcile\n" +
			"populates. All five subcommands — list, add, dashboard, resolve, and\n" +
			"compact — operate on that one store, so an item filed by add is visible\n" +
			"to list and closeable by resolve. Use --dir to point them at a store\n" +
			"other than the current repo's.",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newDebtListCmd(), newDebtAddCmd(), newDebtDashboardCmd(), newDebtResolveCmd(), newDebtCompactCmd())
	return cmd
}

// addDebtStoreFlag registers the --dir flag every debt subcommand shares, with
// the one default that makes them a single namespace over a single store.
//
// The DISPLAYED default is cleared after registration: cobra appends
// "(default X)" to --help whenever DefValue is non-zero, and here X would be the
// relative ".atcr/debt" — a path no command actually uses, since an unset --dir
// resolves to <repo root>/.atcr/debt at run time (debtStoreDir). Clearing
// DefValue touches only the help rendering; the flag's actual default value is
// unchanged, so Changed("dir")-based resolution is unaffected.
//
// A set-but-empty --dir is rejected here, at the shared registration point, via
// a chained PreRunE (prev-first, matching addRangeFlags in cli/flags.go): an
// explicit `--dir ""` is the shape an unset shell variable produces, and letting
// it through made `debt list` report an empty backlog (exit 0) and `debt add`
// die on a low-level mkdir error — an invocation mistake masquerading as store
// state. One hook covers every consumer with no change to debtStoreDir's
// signature or its call sites.
func addDebtStoreFlag(cmd *cobra.Command) {
	cmd.Flags().String("dir", defaultDebtResolveDir, "path to the local TD store; unset resolves to <repo root>/.atcr/debt")
	cmd.Flags().Lookup("dir").DefValue = ""
	prev := cmd.PreRunE
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if prev != nil {
			if err := prev(cmd, args); err != nil {
				return err
			}
		}
		if cmd.Flags().Changed("dir") && strings.TrimSpace(mustFlag(cmd, "dir")) == "" {
			return usageError(fmt.Errorf("--dir must not be empty; omit it to resolve <repo root>/.atcr/debt"))
		}
		return nil
	}
}

// debtStoreDir resolves the store directory for a debt subcommand: an explicit
// --dir verbatim, otherwise the store under the repo root that cli/root.go's
// existing .git/.atcr marker walk finds. Without the walk a bare `atcr debt
// list` run from a subdirectory reads a DIFFERENT (usually empty) store than the
// one `atcr reconcile` wrote at the repo root — silently, with no error and no
// hint. That is the reader half of the split T6 opened when it moved the writer
// to a manifest-recorded root.
//
// Resolution happens here, at RUN time, and deliberately not in the package-level
// flag default: localdebt.DefaultDir(".") is a RELATIVE path that re-resolves
// against the working directory at each use, whereas an absolute root computed
// at package-var init would capture the process's start directory and outlive
// any later chdir (which is what the test suite does).
//
// repoRoot falls back to the working directory when it finds no marker, so a
// caller outside a repo keeps exactly today's behavior.
func debtStoreDir(cmd *cobra.Command) string {
	dir := mustFlag(cmd, "dir")
	if cmd.Flags().Changed("dir") {
		return dir
	}
	root, err := debtRepoRoot()
	if err != nil {
		// Getwd failed — there is no better root to offer than the relative
		// default, which is what every caller used before this walk existed.
		return dir
	}
	return localdebt.DefaultDir(root)
}

// debtRepoRoot finds the repository root for the debt store, using the marker
// rules of the store's WRITER (localdebt's validateRepoRoot): `.git` counts as a
// directory OR a regular file, because a linked worktree and a submodule both
// record their root with a `.git` file; `.atcr` counts only as a directory,
// since that is the only form atcr itself creates. With no marker anywhere up
// the tree it falls back to the working directory.
//
// It deliberately does NOT reuse cli/root.go's repoRoot(), even though the walk
// is the same shape. repoRoot() requires `.git` to be a directory, and its eight
// other consumers (config, telemetry consent, history, audit, resume, review)
// depend on that strictness: widening it there would treat a submodule as its
// own atcr repo and silently relocate their state, including a recorded
// telemetry opt-out. The debt readers are the only callers that must agree with
// localdebt's writer, so the broader rule is scoped to them.
//
// A `.git` SYMLINK is not a marker, which is marginally stricter than the
// writer's os.Stat. That is deliberate and matches repoRoot()'s hardening: a
// link pointing at an arbitrary directory must not pass as a repository root,
// and git never creates one.
func debtRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; ; {
		if info, err := os.Lstat(filepath.Join(dir, ".git")); err == nil &&
			(info.IsDir() || info.Mode().IsRegular()) {
			return dir, nil
		}
		if info, err := os.Lstat(filepath.Join(dir, ".atcr")); err == nil && info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd, nil
		}
		dir = parent
	}
}

// loadLocalDebt reads the store named by --dir and folds it by id, so list and
// dashboard see one effective record per finding — the same precedence rule
// selectOpenDebt, Compact, and AggregateQualitySignal share. Without the fold a
// re-raised finding would render once per append.
//
// Sharing that rule means sharing its lifetimes: only `wontfix` survives a
// re-detection. An id that was resolved or deferred and has since been detected
// again folds to the newer open record, so it renders here as open (and counts as
// open in the dashboard) rather than staying in the terminal bucket. That is the
// point — a re-detection at the same file, line, and problem text is a
// regression, because the line number is part of the finding id.
//
// A missing store is not an error: localdebt.ReadAll reports the "no backlog
// yet" state as an empty result, which renders as the empty-result message.
func loadLocalDebt(cmd *cobra.Command) ([]localdebt.Record, error) {
	dir := debtStoreDir(cmd)
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
	if err != nil {
		return nil, fmt.Errorf("read local debt store: %w", err)
	}
	return localdebt.FoldRecords(recs), nil
}

func newDebtListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List technical-debt items as a table, with filtering and sorting",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runDebtList,
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().String("severity", "", "filter by severity (CRITICAL|HIGH|MEDIUM|LOW)")
	cmd.Flags().String("status", "", "filter by status (open|deferred|resolved|wontfix)")
	cmd.Flags().String("category", "", "filter by category (substring match)")
	cmd.Flags().String("component", "", "filter by component (path prefix, e.g. internal/autofix)")
	cmd.Flags().String("sort", sortKeySeverity, "sort key: severity|age|est|file")
	// Same flag name, type, and help string as `debt resolve --json`, so the two
	// machine-readable surfaces read identically to a caller.
	cmd.Flags().Bool("json", false, "emit the selected items as a JSON array")
	return cmd
}

func runDebtList(cmd *cobra.Command, _ []string) error {
	recs, err := loadLocalDebt(cmd)
	if err != nil {
		return err
	}

	recs = applyDebtFilter(recs, debtFilter{
		Severity:  mustFlag(cmd, "severity"),
		Status:    mustFlag(cmd, "status"),
		Category:  mustFlag(cmd, "category"),
		Component: mustFlag(cmd, "component"),
	})

	if err := sortDebt(recs, mustFlag(cmd, "sort")); err != nil {
		return usageError(err) // a bad --sort value is a usage error (exit 2)
	}

	out := cmd.OutOrStdout()
	// The JSON branch comes before the empty-result message: a consumer parsing
	// the stream must get [] for an empty store, never a human-readable line it
	// would have to special-case.
	if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
		return renderDebtJSON(out, recs)
	}
	if len(recs) == 0 {
		// Name the store the way `debt add` does ("Added ... to <dir>."): an
		// undifferentiated "nothing here" from the WRONG store reads exactly like
		// an empty backlog, with exit code 0 either way — the silent failure
		// debtStoreDir exists to prevent.
		_, _ = fmt.Fprintf(out, "No matching technical-debt items in %s.\n", debtStoreDir(cmd))
		return nil
	}

	return renderDebtTable(out, recs)
}

// renderDebtJSON writes records as a JSON array (never null, so an empty store
// yields [] for a scripting consumer). It is the ONE encoder behind both
// `debt list --json` and `debt resolve --json`: a second encoder is how the two
// surfaces would drift apart on field ordering or indent, breaking a downstream
// consumer that reads one and is tested against the other.
func renderDebtJSON(w io.Writer, recs []localdebt.Record) error {
	if recs == nil {
		recs = []localdebt.Record{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(recs)
}

// mustFlag reads a string flag, returning "" if it was not registered. The
// error is impossible for flags this command declares, so it is intentionally
// discarded to keep call sites readable.
func mustFlag(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

// debtFilter selects records by exact/substring field matches. A zero-value
// field (empty string) matches anything, so a zero debtFilter is a pass-through.
//
// There is deliberately no Group field: group is cadence workflow vocabulary,
// excluded from the record schema by the atcr<->cadence seam.
type debtFilter struct {
	Severity  string // exact, case-insensitive (CRITICAL|HIGH|MEDIUM|LOW)
	Status    string // exact (open|deferred|resolved|wontfix); "open" matches an empty status
	Category  string // substring, case-insensitive
	Component string // path-prefix match against the record's File
}

// match reports whether r satisfies every non-empty field of f.
func (f debtFilter) match(r localdebt.Record) bool {
	if f.Severity != "" && !strings.EqualFold(r.Severity, f.Severity) {
		return false
	}
	// The store's canonical open record carries an EMPTY status, so a literal
	// --status open must match it; comparing the raw fields would silently return
	// nothing for the most common query in the namespace. The comparison is
	// bucket-vs-LITERAL, not bucket-vs-bucket: bucketing the filter value too
	// would map an unrecognized --status onto "open" and quietly return the open
	// backlog instead of nothing. It also keeps the filter agreeing with the
	// STATUS column the table just rendered.
	if f.Status != "" && debtStatusBucket(r.Status) != strings.ToLower(strings.TrimSpace(f.Status)) {
		return false
	}
	if f.Category != "" && !strings.Contains(strings.ToLower(r.Category), strings.ToLower(f.Category)) {
		return false
	}
	if f.Component != "" {
		// Require an exact component match or a path-segment prefix so that
		// "cmd" does not also match "cmder/...". The record side is cleaned
		// before comparing (the filter side is cleaned once in applyDebtFilter):
		// an uncleaned "./internal/foo.go" record would miss a plain "internal"
		// filter, and an uncleaned "internal/" filter — the shape shell
		// tab-completion produces — would miss every record, with exit code 0
		// either way, indistinguishable from an empty backlog.
		file := filepath.ToSlash(filepath.Clean(r.File))
		if file != f.Component && !strings.HasPrefix(file, f.Component+"/") {
			return false
		}
	}
	return true
}

// applyDebtFilter returns the subset of recs matching f, preserving order. It
// always returns a non-nil slice so callers can range/marshal without nil checks.
//
// The component filter is normalized ONCE here rather than per record:
// filepath.Clean strips the trailing slash shell tab-completion appends
// ("internal/" -> "internal") and a leading ./, so both spellings match the
// plain component. The empty-string guard is load-bearing — filepath.Clean("")
// returns ".", which must never become an active filter.
func applyDebtFilter(recs []localdebt.Record, f debtFilter) []localdebt.Record {
	if f.Component != "" {
		f.Component = filepath.ToSlash(filepath.Clean(f.Component))
	}
	out := make([]localdebt.Record, 0, len(recs))
	for _, r := range recs {
		if f.match(r) {
			out = append(out, r)
		}
	}
	return out
}

// Sort keys accepted by sortDebt.
const (
	sortKeySeverity = "severity" // CRITICAL first, then age within a severity
	sortKeyAge      = "age"      // oldest first
	sortKeyEst      = "est"      // largest est_minutes first
	sortKeyFile     = "file"     // lexicographic by file:line
)

// sortDebt orders recs in place by the given key. An unknown key is a hard error
// so a typo'd --sort flag fails loudly instead of silently returning unsorted
// data. Every ordering is total so output is deterministic across runs; the
// tiebreak chain is key-specific: severity breaks ties by date then location,
// age by date then location, est by location, and file by location then date.
//
// Location (file, then line) rather than file alone is what keeps the orderings
// total now that the line number is a separate field — two findings in one file
// would otherwise tie.
func sortDebt(recs []localdebt.Record, key string) error {
	byLocation := func(a, b localdebt.Record) (bool, bool) {
		if a.File != b.File {
			return a.File < b.File, true
		}
		if a.Line != b.Line {
			return a.Line < b.Line, true
		}
		return false, false
	}

	var less func(a, b localdebt.Record) bool
	switch key {
	case sortKeySeverity:
		less = func(a, b localdebt.Record) bool {
			// severityRank is most-severe-HIGHEST (cli/debt_resolve.go), so the
			// most-severe-first ordering is a descending compare.
			if ra, rb := severityRank(a.Severity), severityRank(b.Severity); ra != rb {
				return ra > rb
			}
			if da, db := debtRecordDate(a), debtRecordDate(b); da != db {
				return da < db // older first within a severity
			}
			ok, decided := byLocation(a, b)
			return decided && ok
		}
	case sortKeyAge:
		less = func(a, b localdebt.Record) bool {
			if da, db := debtRecordDate(a), debtRecordDate(b); da != db {
				return da < db // older first
			}
			ok, decided := byLocation(a, b)
			return decided && ok
		}
	case sortKeyEst:
		less = func(a, b localdebt.Record) bool {
			if a.EstMinutes != b.EstMinutes {
				return a.EstMinutes > b.EstMinutes // largest first
			}
			ok, decided := byLocation(a, b)
			return decided && ok
		}
	case sortKeyFile:
		less = func(a, b localdebt.Record) bool {
			if ok, decided := byLocation(a, b); decided {
				return ok
			}
			return debtRecordDate(a) < debtRecordDate(b)
		}
	default:
		return fmt.Errorf("unknown sort key %q (want severity|age|est|file)", key)
	}
	sort.SliceStable(recs, func(i, j int) bool { return less(recs[i], recs[j]) })
	return nil
}

// renderDebtTable writes an aligned, tab-separated table of records. The id is
// the leading column and is never truncated, so a listed item is directly
// copy-pasteable into `atcr debt resolve <id>` — the visible half of "an item
// filed by add is closeable by resolve". Problem text is truncated so a long
// finding never wraps the terminal into an unreadable block; the full text lives
// in the store. Every cell passes through cell so a stray tab or newline in a
// free-text field cannot tear a row or misalign the tabwriter block.
func renderDebtTable(w io.Writer, recs []localdebt.Record) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tSEVERITY\tSTATUS\tEST\tFILE\tCATEGORY\tPROBLEM"); err != nil {
		return err
	}
	for _, r := range recs {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
			debtIDCell(r.ID), cell(r.Severity), debtStatusBucket(r.Status), r.EstMinutes,
			cell(debtLocation(r)), cell(r.Category), cell(truncate(r.Problem, 60))); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// debtIDCell renders a record's id for a table or dashboard row. Every writer
// stamps the id (Record.StampID, persistLocalDebt, and markDebtResolved copying
// it forward), so an empty id only reaches a renderer from a hand-edited store;
// it renders as a literal "-" rather than a blank cell that would misalign the
// column and read as a missing value.
//
// It deliberately does NOT fall back to recomputing FindingID: markDebtResolved
// matches on the STORED id, so a computed display id would print a value
// `atcr debt resolve` cannot match. A copy-pasteable lie is worse than a
// visible gap.
func debtIDCell(id string) string {
	if id == "" {
		return "-"
	}
	return cell(id)
}

// cell makes a raw store field safe to interpolate into the tab-separated
// table: a literal newline would break the row and a literal tab would tear a
// column, so both collapse to spaces. The render layer enforces this
// structurally — the same line escapeMarkdownCell holds for the telemetry
// report — rather than trusting the fields to be single-line.
func cell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.ReplaceAll(s, "\t", " ")
}

// truncate shortens s to at most n runes, appending an ellipsis when it cut
// anything, and collapses newlines to spaces so a multi-line problem stays on
// one table row.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", " "), "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
