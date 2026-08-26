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
	"github.com/samestrin/atcr/internal/validation"
)

// newDebtCmd builds `atcr debt`: query, capture, aggregate, resolve, and compact
// the local technical-debt store. As of Plan 35.13 all five subcommands read and
// write ONE store — the .atcr/-scoped, month-sharded JSONL backlog under
// .atcr/debt/ that `atcr reconcile` populates — resolved through a --dir flag
// with a shared default. backfill-justifications joins them as a one-off repair
// for excerpts persisted before the extractor that produced them was corrected. The previous .planning/-scoped README+shard store that
// list/add/dashboard used is no longer read or written by any atcr code.
func newDebtCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debt",
		Short: "Query, capture, and report on technical debt",
		Long: "atcr debt reads and writes the local technical-debt store under\n" +
			".atcr/debt/ (month-sharded, append-only JSONL) that atcr reconcile\n" +
			"populates. Its subcommands — list, add, dashboard, resolve, compact, and\n" +
			"backfill-justifications — operate on that one store, so an item filed by add is visible\n" +
			"to list and closeable by resolve. Use --dir to point them at a store\n" +
			"other than the current repo's.",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newDebtListCmd(), newDebtAddCmd(), newDebtDashboardCmd(), newDebtResolveCmd(), newDebtCompactCmd(), newDebtBackfillCmd())
	return cmd
}

// addDebtStoreFlag registers the --store flag every debt subcommand shares, with
// the one default that makes them a single namespace over a single store.
// --dir is a deprecated hidden alias resolving identically.
//
// The DISPLAYED default is cleared after registration: cobra appends
// "(default X)" to --help whenever DefValue is non-zero, and here X would be the
// relative ".atcr/debt" — a path no command actually uses, since an unset
// --store resolves to <repo root>/.atcr/debt at run time (debtStoreDir). Clearing
// DefValue touches only the help rendering; the flag's actual default value is
// unchanged, so Changed("store")-based resolution is unaffected.
//
// A set-but-empty --store is rejected here, at the shared registration point, via
// a chained PreRunE (prev-first, matching addRangeFlags in cli/flags.go): an
// explicit `--store ""` is the shape an unset shell variable produces, and letting
// it through made `debt list` report an empty backlog (exit 0) and `debt add`
// die on a low-level mkdir error — an invocation mistake masquerading as store
// state. One hook covers every consumer with no change to debtStoreDir's
// signature or its call sites.
//
// The same hook runs the value through validation.FilePath, which is what the
// sibling --output on this command family already does. Without it --store went
// verbatim to localdebt.Append, which MkdirAlls it: `--store <repo>/../../escaped`
// silently created and wrote a store outside the repo, and a system directory
// failed only as a raw mkdir permission error. Both the raw value (so a `..`
// segment is caught as typed, before absolutization collapses it) and its
// absolute form (so a relative path that lands in /etc is caught too) are
// checked. Containment inside the repo root is deliberately NOT required:
// pointing at another repo's store is --store's documented purpose.
func addDebtStoreFlag(cmd *cobra.Command) {
	cmd.Flags().String("store", defaultDebtResolveDir, "path to the local TD store; unset resolves to <repo root>/.atcr/debt")
	cmd.Flags().Lookup("store").DefValue = ""
	cmd.Flags().String("dir", defaultDebtResolveDir, "deprecated alias for --store")
	cmd.Flags().Lookup("dir").DefValue = ""
	// Hidden rather than MarkDeprecated: --dir has hundreds of live call sites
	// (including this repo's own tooling), and pflag's parse-time deprecation
	// warning pollutes captured stdout in merged-stream harnesses. The alias
	// stays invisible in --help; docs carry the deprecation. Resolution keys on
	// Changed(), so the alias's default value is inert.
	_ = cmd.Flags().MarkHidden("dir")
	prev := cmd.PreRunE
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if prev != nil {
			if err := prev(cmd, args); err != nil {
				return err
			}
		}
		name, dir := debtStoreFlagValue(cmd)
		if name == "" {
			return nil
		}
		if strings.TrimSpace(dir) == "" {
			return usageError(fmt.Errorf("--%s must not be empty; omit it to resolve <repo root>/.atcr/debt", name))
		}
		if err := validation.FilePath(dir); err != nil {
			return usageError(fmt.Errorf("--%s %q: %w", name, dir, err))
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return usageError(fmt.Errorf("--%s %q: %w", name, dir, err))
		}
		if err := validation.FilePath(abs); err != nil {
			return usageError(fmt.Errorf("--%s %q: %w", name, dir, err))
		}
		return nil
	}
}

// debtStoreFlagValue reports the explicitly set store selector: the canonical
// --store wins over the deprecated --dir alias when both are given. It returns
// ("", "") when neither flag was supplied.
func debtStoreFlagValue(cmd *cobra.Command) (string, string) {
	if cmd.Flags().Changed("store") {
		return "store", mustFlag(cmd, "store")
	}
	if cmd.Flags().Changed("dir") {
		return "dir", mustFlag(cmd, "dir")
	}
	return "", ""
}

// debtStoreDir resolves the store directory for a debt subcommand: an explicit
// --store (or its deprecated --dir alias) verbatim, otherwise the store under
// the repo root that cli/root.go's existing .git/.atcr marker walk finds.
// Without the walk a bare `atcr debt
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
	_, dir := debtStoreFlagValue(cmd)
	if cmd.Flags().Changed("store") || cmd.Flags().Changed("dir") {
		return dir
	}
	root, err := debtRepoRoot()
	if err != nil {
		// Getwd failed — there is no better root to offer than the relative
		// default, which is what every caller used before this walk existed.
		return defaultDebtResolveDir
	}
	return localdebt.DefaultDir(root)
}

// debtRepoRoot finds the repository root for the debt store, using the marker
// rules of the store's WRITER — literally, by calling the same predicate the
// writer's validateRepoRoot calls (localdebt.HasRepoRootMarker): `.git` counts
// as a directory OR a regular file, because a linked worktree and a submodule
// both record their root with a `.git` file; `.atcr` counts only as a directory,
// since that is the only form atcr itself creates; a SYMLINK is neither. With no
// marker anywhere up the tree it falls back to the working directory.
//
// It deliberately does NOT reuse cli/root.go's repoRoot(), even though the walk
// is the same shape. repoRoot() requires `.git` to be a directory, and its eight
// other consumers (config, telemetry consent, history, audit, resume, review)
// depend on that strictness: widening it there would treat a submodule as its
// own atcr repo and silently relocate their state, including a recorded
// telemetry opt-out. The debt readers are the only callers that must agree with
// localdebt's writer, so the broader rule is scoped to them.
//
// A `.git` SYMLINK is not a marker on either side — the shared predicate uses
// Lstat, matching repoRoot()'s hardening: a link pointing at an arbitrary
// directory must not pass as a repository root, and git never creates one.
func debtRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// The walk itself lives in localdebt (FindRepoRoot), shared with the store's
	// WRITER: a second copy of the loop here is exactly how the reader and writer
	// halves came to resolve different roots (TD internal/localdebt/root.go:89).
	if root, found := localdebt.FindRepoRoot(cwd); found {
		return root, nil
	}
	return cwd, nil
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

// selectDebtForList is `debt list`'s two-pass read, the one `debt resolve`
// already uses (cli/debt_resolve.go's selectOpenDebt) and the one
// cli/debt_resolve.go:174-179 tells callers not to collapse back into a single
// full-record read.
//
// Pass 1 folds SUMMARIES — id, status, severity, ts, file, line, category, est —
// and applies the whole selection: filter, then sort. Pass 2 hydrates full
// Records only for the ids that survived, in pass 1's order. Peak retained memory
// becomes O(distinct ids x Summary) + O(selected ids x Record) instead of
// O(all history x Record) twice over, which is what ReadAll + FoldRecords cost:
// measured at 327 MB RSS for `debt list --severity CRITICAL` on a 100k-record /
// 49 MB store against 145 MB for `debt resolve` on the same store.
//
// The filter therefore runs BEFORE materialization rather than after it, so a
// query returning 25 rows no longer holds all 100k records to produce them.
//
// Unlike the resolve worklist this keeps every selected id: `debt list` renders
// closed and location-less records too (that is what --status resolved is for).
func selectDebtForList(cmd *cobra.Command, f debtFilter, sortKey string) ([]localdebt.Record, error) {
	dir := debtStoreDir(cmd)
	opts := localdebt.ReadOpts{Writer: cmd.ErrOrStderr()}

	// A pass-through filter selects every id, so pass 1 would fold the whole store
	// only to hand pass 2 the whole store — paying for both projections instead of
	// one. Measured on a 47 MB / 50k-record store: 170 MB peak RSS for the direct
	// read against 198 MB for the two-pass read. Selection only pays when it
	// NARROWS, so an unfiltered list reads records directly.
	//
	// This is the same comparator either way (debtLess), so the two branches cannot
	// order differently.
	if f == (debtFilter{}) {
		recs, err := loadLocalDebt(cmd)
		if err != nil {
			return nil, err
		}
		if err := sortDebt(recs, sortKey); err != nil {
			return nil, usageError(err)
		}
		return recs, nil
	}

	sums, err := localdebt.ReadSummaries(dir, opts)
	if err != nil {
		return nil, fmt.Errorf("read local debt store: %w", err)
	}

	if f.Component != "" {
		// Normalized once, exactly as applyDebtFilter does — see its doc block for
		// why the empty guard is load-bearing.
		f.Component = filepath.ToSlash(filepath.Clean(f.Component))
	}
	selected := make([]localdebt.Summary, 0, len(sums))
	for _, s := range localdebt.FoldSummaries(sums) {
		if f.matchView(viewOfSummary(s)) {
			selected = append(selected, s)
		}
	}
	if err := sortDebtSummaries(selected, sortKey); err != nil {
		return nil, usageError(err) // a bad --sort value is a usage error (exit 2)
	}

	ids := make([]string, 0, len(selected))
	for _, s := range selected {
		ids = append(ids, s.ID)
	}
	// Drop pass 1's working set before pass 2 allocates. Both slices are dead once
	// the ids are extracted, but they stay REACHABLE until this function returns,
	// so without this the peak is summaries + records rather than the larger of the
	// two — which measured as a net regression for an UNFILTERED `debt list`, where
	// every id survives and the summary pass buys nothing.
	sums, selected = nil, nil
	_, _ = sums, selected

	recs, err := hydrateDebtIDs(dir, ids, opts, nil)
	if err != nil {
		return nil, fmt.Errorf("read local debt store: %w", err)
	}
	return recs, nil
}

func newDebtListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List technical-debt items as a table, with filtering and sorting",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runDebtList,
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().String("severity", "", "filter by severity (exact, case-insensitive: CRITICAL|HIGH|MEDIUM|LOW)")
	cmd.Flags().String("status", "", "filter by status (exact: open|deferred|resolved|wontfix)")
	cmd.Flags().String("category", "", "filter by category (substring match)")
	cmd.Flags().String("component", "", "filter by component (path prefix, e.g. internal/autofix)")
	cmd.Flags().String("origin", "", "filter by origin (exact: review|manual)")
	cmd.Flags().String("sort", sortKeySeverity, "sort key: severity|age|est|file")
	// Same flag name, type, and help string as `debt resolve --json`, so the two
	// machine-readable surfaces read identically to a caller.
	cmd.Flags().Bool("json", false, "emit the selected items as a JSON array")
	return cmd
}

// debtListStatuses is the accepted --status enum for `debt list`. It is the four
// buckets debtStatusBucket renders, NOT debt_add's narrower set: `wontfix` cannot
// be FILED by add (dismissing needs resolve's --reason) but a dismissed item is
// still viewable, so it must stay filterable.
var debtListStatuses = map[string]bool{"open": true, "deferred": true, "resolved": true, "wontfix": true}

// validateDebtListFilters rejects an unrecognized --severity or --status.
//
// Both used to be permissive: a typo matched nothing and exited 0, which is
// byte-for-byte what a genuinely empty backlog looks like, while
// `debt resolve --severity BOGUS` rejected the same value as a usage error. That
// is the same class of silent failure debtStoreDir's repo-root walk exists to
// prevent, and the enums are the ones resolve and add already validate against.
// An empty value stays a pass-through: it is the unset filter.
//
// BREAKING: a script passing an unrecognized value now exits 2 instead of 0.
func validateDebtListFilters(cmd *cobra.Command) error {
	if sev := strings.ToUpper(strings.TrimSpace(mustFlag(cmd, "severity"))); sev != "" && !resolveSeverities[sev] {
		return usageError(fmt.Errorf("invalid --severity %q: expected CRITICAL|HIGH|MEDIUM|LOW", mustFlag(cmd, "severity")))
	}
	if st := strings.ToLower(strings.TrimSpace(mustFlag(cmd, "status"))); st != "" && !debtListStatuses[st] {
		return usageError(fmt.Errorf("invalid --status %q: expected open|deferred|resolved|wontfix", mustFlag(cmd, "status")))
	}
	if o := strings.ToLower(strings.TrimSpace(mustFlag(cmd, "origin"))); o != "" &&
		o != localdebt.OriginReview && o != localdebt.OriginManual {
		return usageError(fmt.Errorf("invalid --origin %q: expected review|manual", mustFlag(cmd, "origin")))
	}
	return nil
}

func runDebtList(cmd *cobra.Command, _ []string) error {
	if err := validateDebtListFilters(cmd); err != nil {
		return err
	}

	recs, err := selectDebtForList(cmd, debtFilter{
		Severity:  mustFlag(cmd, "severity"),
		Status:    mustFlag(cmd, "status"),
		Category:  mustFlag(cmd, "category"),
		Component: mustFlag(cmd, "component"),
		Origin:    mustFlag(cmd, "origin"),
	}, mustFlag(cmd, "sort"))
	if err != nil {
		return err
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
	Origin    string // exact, case-insensitive (review|manual); matches the EFFECTIVE origin
}

// debtView is the projection filtering and sorting act on. A full
// localdebt.Record and the localdebt.Summary the streaming path decodes both
// convert to it, so `debt list`'s SUMMARY-stage selection and any record-stage
// filtering are one implementation rather than two that can drift — a filter
// that disagreed between the passes would select an id in pass 1 and render
// something else in pass 2.
type debtView struct {
	Severity   string
	Status     string
	Category   string
	File       string
	Line       int
	EstMinutes int
	Date       string // YYYY-MM-DD, the sort key's date component
	Origin     string // EFFECTIVE origin (localdebt.EffectiveOrigin normalization)
}

func viewOfRecord(r localdebt.Record) debtView {
	return debtView{
		Severity: r.Severity, Status: r.Status, Category: r.Category,
		File: r.File, Line: r.Line, EstMinutes: r.EstMinutes, Date: debtRecordDate(r),
		Origin: r.EffectiveOrigin(),
	}
}

func viewOfSummary(s localdebt.Summary) debtView {
	date := ""
	if len(s.Timestamp) >= 10 {
		date = s.Timestamp[:10]
	}
	return debtView{
		Severity: s.Severity, Status: s.Status, Category: s.Category,
		File: s.File, Line: s.Line, EstMinutes: s.EstMinutes, Date: date,
		Origin: s.EffectiveOrigin(),
	}
}

// match reports whether r satisfies every non-empty field of f.
func (f debtFilter) match(r localdebt.Record) bool { return f.matchView(viewOfRecord(r)) }

func (f debtFilter) matchView(r debtView) bool {
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
	// The view already carries the EFFECTIVE origin (absent/unrecognized means
	// review), so the filter compares against exactly what the ORIGIN column
	// renders — a v1/v2 record matches --origin review without carrying the key.
	if f.Origin != "" && r.Origin != strings.ToLower(strings.TrimSpace(f.Origin)) {
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
	less, err := debtLess(key)
	if err != nil {
		return err
	}
	sort.SliceStable(recs, func(i, j int) bool {
		return less(viewOfRecord(recs[i]), viewOfRecord(recs[j]))
	})
	return nil
}

// sortDebtSummaries is sortDebt over the SUMMARY projection, so `debt list` can
// order its selection before it hydrates anything. Both call debtLess, so pass 1
// and pass 2 cannot order differently.
func sortDebtSummaries(sums []localdebt.Summary, key string) error {
	less, err := debtLess(key)
	if err != nil {
		return err
	}
	sort.SliceStable(sums, func(i, j int) bool {
		return less(viewOfSummary(sums[i]), viewOfSummary(sums[j]))
	})
	return nil
}

// debtLess returns the comparator for a sort key, or an error for an unknown one.
func debtLess(key string) (func(a, b debtView) bool, error) {
	byLocation := func(a, b debtView) (bool, bool) {
		if a.File != b.File {
			return a.File < b.File, true
		}
		if a.Line != b.Line {
			return a.Line < b.Line, true
		}
		return false, false
	}

	switch key {
	case sortKeySeverity:
		return func(a, b debtView) bool {
			// severityRank is most-severe-HIGHEST (cli/debt_resolve.go), so the
			// most-severe-first ordering is a descending compare.
			if ra, rb := severityRank(a.Severity), severityRank(b.Severity); ra != rb {
				return ra > rb
			}
			if a.Date != b.Date {
				return a.Date < b.Date // older first within a severity
			}
			ok, decided := byLocation(a, b)
			return decided && ok
		}, nil
	case sortKeyAge:
		return func(a, b debtView) bool {
			if a.Date != b.Date {
				return a.Date < b.Date // older first
			}
			ok, decided := byLocation(a, b)
			return decided && ok
		}, nil
	case sortKeyEst:
		return func(a, b debtView) bool {
			if a.EstMinutes != b.EstMinutes {
				return a.EstMinutes > b.EstMinutes // largest first
			}
			ok, decided := byLocation(a, b)
			return decided && ok
		}, nil
	case sortKeyFile:
		return func(a, b debtView) bool {
			if ok, decided := byLocation(a, b); decided {
				return ok
			}
			return a.Date < b.Date
		}, nil
	default:
		return nil, fmt.Errorf("unknown sort key %q (want severity|age|est|file)", key)
	}
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
	if _, err := fmt.Fprintln(tw, "ID\tSEVERITY\tSTATUS\tORIGIN\tEST\tFILE\tCATEGORY\tPROBLEM"); err != nil {
		return err
	}
	for _, r := range recs {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
			debtIDCell(r.ID), cell(r.Severity), debtStatusBucket(r.Status), cell(r.EffectiveOrigin()), r.EstMinutes,
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
//
// The remaining control characters are then STRIPPED through sanitizeCell
// (cli/scorecard.go), not collapsed: ESC, the other C0 bytes, DEL, the C1 range
// and U+2028/U+2029 are not word separators, they are cursor commands. Store
// content is model-generated by the reconcile fan-out, so a finding carrying an
// ANSI sequence would otherwise emit it verbatim and erase or overwrite the row
// above it in `debt list` — and in `debt resolve`, which renders through
// this same helper. Whitespace collapse runs FIRST so a newline still separates
// words instead of joining them.
func cell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return sanitizeCell(s)
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
