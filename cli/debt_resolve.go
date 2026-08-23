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
	"time"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/reconcile"
)

// isRecordedRationale reports whether a justification can stand in for --reason on a
// wontfix — the terminal, backlog-suppressing status whose record localdebt then
// preserves through compaction precisely because it holds reasoning that exists
// nowhere else in the tree.
//
// Non-emptiness is not the bar. A reconcile-enriched justification is an EXCERPT of
// the reviewer's review.md, and every fenced quote inside that excerpt is replaced by
// reconcile.ElidedQuotePlaceholder — so an excerpt whose whole block was fenced
// reduces to nothing but placeholders: tool-generated text carrying zero reviewer
// content, which the old `!= ""` guard accepted as a permanent dismissal's entire
// audit trail. Reviewer prose anywhere beside a placeholder still qualifies; the
// placeholder is only ever discounted, never treated as disqualifying.
//
// The placeholder is only HALF the quote problem, because extractSection elides only
// a TERMINATED fence. A DANGLING opener's marker deliberately stays and its whole
// quoted tail is released as prose (justification.go:519-520), so that excerpt is
// 100% tool-visible quoted example text carrying no more reviewer reasoning than a
// placeholder does. Fence markers and fenced content are therefore discounted on the
// same terms: whether a permanent dismissal is auditable must not depend on whether
// the reviewing model remembered to close its fence.
func isRecordedRationale(justification string) bool {
	inFence := false
	for _, line := range strings.Split(justification, "\n") {
		// Mirrors reconcile's isFenceMarker (justification.go:702) — the producer of
		// the text being read. An unterminated opener leaves inFence set for the rest
		// of the excerpt, which is exactly the released tail that must not count.
		if isFenceMarkerLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if line = strings.TrimSpace(line); line != "" && line != reconcile.ElidedQuotePlaceholder {
			return true
		}
	}
	return false
}

// isFenceMarkerLine reports whether a justification line is a Markdown code-fence
// marker, matching reconcile.extractSection's own isFenceMarker so the reader of an
// excerpt agrees with its writer about where the quotes are.
func isFenceMarkerLine(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), "```")
}

// defaultDebtResolveDir is the .atcr/-scoped local TD store, rooted at the current
// working directory (localdebt's Root: "." convention). Since Plan 35.13 it is the
// shared --dir default for ALL FIVE debt subcommands, not just resolve: list, add,
// and dashboard used to read a separate .planning/-scoped store, and no atcr code
// reads that store any more.
var defaultDebtResolveDir = localdebt.DefaultDir(".")

// resolveSeverities is the validated --severity enum, matching the set used
// elsewhere in cli/debt.go.
var resolveSeverities = map[string]bool{"CRITICAL": true, "HIGH": true, "MEDIUM": true, "LOW": true}

// resolveStatuses is the validated --status enum for a mark action. Both values are
// terminal (isClosedStatus folds them out): "resolved" means the code was actually
// fixed; "wontfix" (Epic 24.0) dismisses a false-positive/accepted pattern so agents
// stop re-surfacing it. "deferred" is intentionally excluded — it is written by other
// paths, not by an explicit resolve.
var resolveStatuses = map[string]bool{"resolved": true, "wontfix": true}

// newDebtResolveCmd builds `atcr debt resolve [id]`: the .atcr/-scoped resolver surface
// the debt-resolve skill route shells out to. It lists open items from the local TD
// store (deterministically sorted for the skill's selection rule) and records
// resolution outcomes as append-only status records. The actual fix cycle
// (RED→GREEN→ADVERSARIAL→REFACTOR) is agent-driven in skills/atcr/debt-resolve.md;
// this subcommand is the store's read/mark-resolved contract, never a code editor.
func newDebtResolveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve [id]",
		Short: "List and mark-resolve items in the local TD store",
		Long: "atcr debt resolve reads the local technical-debt store (.atcr/debt/,\n" +
			"populated by atcr reconcile and atcr debt add) and lists open items for the\n" +
			"debt-resolve skill route to fix — the same store list, add, dashboard, and\n" +
			"compact read. Pass the id positionally to record a resolution\n" +
			"(`atcr debt resolve <id>`); the id is the leading column of `atcr debt list`,\n" +
			"which is also the command to use for browsing the full backlog.",
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: runDebtResolve,
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().Bool("json", false, "emit the selected items as a JSON array")
	cmd.Flags().String("severity", "", "filter by severity (exact, case-insensitive: CRITICAL|HIGH|MEDIUM|LOW)")
	cmd.Flags().Int("max", 10, "action cap: how many items are selected to act on (0 = no cap). Distinct from debt dashboard --top, which is a ranked-display cutoff over a full aggregation — and where 0 suppresses the list rather than removing the cap")
	cmd.Flags().String("status", "resolved", "terminal status to record for the positional id (resolved|wontfix)")
	cmd.Flags().String("reason", "", "justification recorded on the resolution record; replaces any existing justification (e.g. why a finding is wontfix)")
	// The retired flags fail with guidance, not a bare pflag "unknown flag":
	// --resolve <id> stuttered against the subcommand name and is now the
	// positional form; --list duplicated `atcr debt list`.
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "unknown flag: --resolve"):
			return usageError(fmt.Errorf("--resolve was removed: pass the id positionally — `atcr debt resolve <id>`"))
		case strings.Contains(msg, "unknown flag: --list"):
			return usageError(fmt.Errorf("--list was removed: browse the backlog with `atcr debt list`"))
		default:
			return usageError(err)
		}
	})
	return cmd
}

func runDebtResolve(cmd *cobra.Command, args []string) error {
	dir := debtStoreDir(cmd)

	var id string
	if len(args) > 0 {
		id = strings.TrimSpace(args[0])
		// An empty positional (`atcr debt resolve ""`) is a mark attempt with no
		// id, not a list request: reject it rather than silently falling through
		// to the list view.
		if id == "" {
			return usageError(fmt.Errorf("atcr debt resolve requires a non-empty id"))
		}
	}
	// --status/--reason only mean something for a mark action; without an id they
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
			return usageError(fmt.Errorf("%s requires an id: atcr debt resolve <id>", provided[0]))
		}
		if len(provided) > 1 {
			return usageError(fmt.Errorf("%s require an id: atcr debt resolve <id>", strings.Join(provided, ", ")))
		}
	}
	if id != "" {
		// --json/--severity/--max only affect the list renderer, which the mark branch
		// never reaches; combined with an id they would be silently ignored. Reject
		// the combination, symmetric with --status/--reason without an id above.
		var listOnly []string
		for _, name := range []string{"json", "severity", "max"} {
			if cmd.Flags().Changed(name) {
				listOnly = append(listOnly, "--"+name)
			}
		}
		if len(listOnly) > 0 {
			return usageError(fmt.Errorf("%s cannot be used when marking an item (atcr debt resolve <id>)", strings.Join(listOnly, ", ")))
		}
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
	limit, _ := cmd.Flags().GetInt("max")
	// --max documents only "0 = no cap"; a negative value would otherwise fall
	// through the `limit > 0` cap check as a silent no-cap. Reject it the way
	// debt add rejects a negative --est.
	if limit < 0 {
		return usageError(fmt.Errorf("invalid --max %d: expected a non-negative number", limit))
	}

	open, err := selectOpenDebt(dir, sev, limit, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
	if err != nil {
		return fmt.Errorf("atcr debt resolve: failed to read local debt store: %w", err)
	}

	if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
		return renderDebtJSON(cmd.OutOrStdout(), open)
	}
	return renderResolveList(cmd.OutOrStdout(), dir, open)
}

// isClosedStatus reports whether a record CARRIES a terminal status marker. The
// reconcile hook writes records with an empty status (open); a
// resolution/deferral/dismissal record carries an explicit terminal status.
// Applied to the record FoldRecords selected for an id, it answers whether the
// ITEM is currently closed — which is not the same as closed forever, since only
// wontfix survives a re-detection (localdebt.IsSuppressingStatus).
func isClosedStatus(status string) bool {
	return localdebt.IsClosedStatus(status)
}

// selectOpenDebt folds the append-only record stream by id into the open backlog.
// An id is open unless its effective record carries a terminal status; the displayed
// record is the effective occurrence FoldRecords keeps. It reuses
// localdebt.FoldRecords so this resolve list, Compact, and AggregateQualitySignal
// share ONE precedence rule — a finding re-raised across runs under the same id can
// no longer rank/filter on the first occurrence here while the quality signal
// aggregates the last.
//
// Closure is NOT permanent, and this function needs no special case to express
// that: FoldRecords is recency-aware, so a re-detection appended after a
// `resolved` or `deferred` record IS the effective record and passes the filter
// below as an ordinary open item. Only `wontfix` survives re-detection, because
// only it suppresses. The behavior therefore lives entirely in the fold, and the
// coupled write-side half is persistLocalDebt's dedup seed — without it no
// re-detection is ever appended and this filter has nothing new to admit.
//
// Records whose effective occurrence has no File are skipped (nothing to
// display/act on). Results are sorted severity DESC then ts ASC (oldest first) and
// capped at max — the deterministic selection rule the skill route documents.
//
// # Two passes, deliberately
//
// Pass 1 (selectOpenIDs) folds minimal localdebt.Summary values to decide WHICH
// ids are open, in what order, and where the --max cap falls. Pass 2
// (hydrateOpenDebt) re-reads the store retaining full Records only for those ids.
// Peak retained memory becomes O(distinct ids x Summary) + O(selected ids x
// Record) instead of O(all history x Record).
//
// This doubles read I/O, and that is an accepted trade rather than an oversight:
// Plan 35.13 Decision 5 establishes that the store's ceiling is MEMORY, not CPU —
// encoding/json sustains ~100 MB/s on records this size, so two streaming passes
// at the 100 MB auto-compaction threshold cost well under a second, while peak
// resident drops by roughly an order of magnitude. Do not "optimize" this back
// into a single full-record read.
func selectOpenDebt(dir string, severity string, limit int, opts localdebt.ReadOpts) ([]localdebt.Record, error) {
	sums, err := localdebt.ReadSummaries(dir, opts)
	if err != nil {
		return nil, err
	}
	return hydrateOpenDebt(dir, selectOpenIDs(sums, severity, limit), opts)
}

// selectOpenIDs is pass 1: the whole selection rule, pure and unit-testable. It
// returns the ids of the open backlog in final display order, so the sort applied
// here — not pass 2's read order — is authoritative.
//
// The closed test is isClosedStatus, NOT IsSettledStatus: the no-arg `debt resolve` list is
// the fix WORKLIST, and a deferred item is deliberately off it until something
// re-detects it — that is what deferring means. A deferred item remains live in
// `debt list`/`dashboard` and closeable by id; those are different questions,
// answered by different predicates (localdebt/record.go).
func selectOpenIDs(sums []localdebt.Summary, severity string, limit int) []string {
	folded := localdebt.FoldSummaries(sums)
	open := make([]localdebt.Summary, 0, len(folded))
	for _, s := range folded {
		if isClosedStatus(s.Status) || !s.HasFile {
			continue
		}
		if severity != "" && strings.ToUpper(s.Severity) != severity {
			continue
		}
		open = append(open, s)
	}

	sort.SliceStable(open, func(i, j int) bool {
		ri, rj := severityRank(open[i].Severity), severityRank(open[j].Severity)
		if ri != rj {
			return ri > rj
		}
		return open[i].Timestamp < open[j].Timestamp
	})
	if limit > 0 && len(open) > limit {
		open = open[:limit]
	}

	ids := make([]string, 0, len(open))
	for _, s := range open {
		ids = append(ids, s.ID)
	}
	return ids
}

// hydrateOpenDebt is pass 2: walk the store shard by shard and retain full records
// ONLY for the selected ids, then return them in the order pass 1 chose.
//
// Shards are enumerated with the same os.ReadDir + .jsonl filter, in the same
// lexical order, that ReadAll uses, so pass 2 sees exactly the shard set pass 1
// did. The retained records are re-folded, which is what preserves the documented
// display rule: the record shown is the effective occurrence FoldRecords keeps
// (the last open one for the id), not the first one encountered.
//
// A missing store directory is empty, not an error, matching ReadAll.
//
// When pass 2 materializes fewer records than pass 1 selected — a concurrent
// `debt resolve <id>` or `debt compact` landed between the two reads — it
// notes the shortfall count on opts.Writer (when set), so a short worklist is
// distinguishable from a short backlog.
func hydrateOpenDebt(dir string, ids []string, opts localdebt.ReadOpts) ([]localdebt.Record, error) {
	// Re-assert pass 1's predicate against pass 2's fold. Normally a no-op — both
	// folds see the same records and agree. It matters when they do NOT: the two
	// passes are separate reads, so a concurrent `debt resolve <id>` or
	// `reconcile` appending between them can make an id's effective record terminal
	// after pass 1 selected it, and without this the closed item would still be
	// printed as an open worklist row.
	return hydrateDebtIDs(dir, ids, opts, func(r localdebt.Record) bool {
		return !isClosedStatus(r.Status) && r.File != ""
	})
}

// collectDebtIDRecords is the shared shard-walk behind the two-pass debt reads:
// it streams every shard in dir one file at a time and retains the RAW
// (unfolded) records for the wanted ids, so peak memory is one shard plus the
// wanted ids' own history rather than the whole store. hydrateDebtIDs folds
// this for the worklist; markDebtResolved calls it directly for its single
// target id.
//
// Every error leaving this function is redacted, the way localdebt.ReadAll
// redacts the read path this replaced: an EACCES here carries the absolute,
// username-bearing shard path, and reaching the user unredacted would be a
// regression against the store package's SECURITY contract.
func collectDebtIDRecords(dir string, ids []string, opts localdebt.ReadOpts) ([]localdebt.Record, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading localdebt dir: %w", localdebt.RedactPathErr(err))
	}
	var retained []localdebt.Record
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		recs, err := localdebt.ReadRecords(filepath.Join(dir, e.Name()), opts)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, localdebt.RedactPathErr(err)
		}
		for _, r := range recs {
			if wanted[r.ID] {
				retained = append(retained, r)
			}
		}
	}
	return retained, nil
}

// hydrateDebtIDs is the shared pass 2 for every two-pass debt read: the resolve
// worklist (hydrateOpenDebt) and `debt list` (selectDebtForList).
//
// keep re-asserts the caller's pass-1 predicate against pass 2's fold; nil keeps
// every hydrated id, which is what `debt list` needs — it renders closed and
// location-less records, so dropping them here would silently shorten the table.
func hydrateDebtIDs(dir string, ids []string, opts localdebt.ReadOpts, keep func(localdebt.Record) bool) ([]localdebt.Record, error) {
	retained, err := collectDebtIDRecords(dir, ids, opts)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]localdebt.Record, len(ids))
	for _, r := range localdebt.FoldRecords(retained) {
		if keep != nil && !keep(r) {
			continue
		}
		byID[r.ID] = r
	}
	out := make([]localdebt.Record, 0, len(ids))
	for _, id := range ids {
		// An id selected by pass 1 and absent here was closed or removed between
		// the passes; dropping it is correct — the worklist must not offer it.
		if r, ok := byID[id]; ok {
			out = append(out, r)
		}
	}
	// But it must not be dropped SILENTLY: a worklist shortened by a concurrent
	// close/compact between the passes reads exactly like a short backlog, with
	// exit code 0 either way. Say on stderr how many selected ids could not be
	// hydrated. Best-effort: a nil Writer (unit callers) skips the note.
	if missing := len(ids) - len(out); missing > 0 && opts.Writer != nil {
		_, _ = fmt.Fprintf(opts.Writer,
			"note: %d selected item(s) were not found when re-reading the store (closed or compacted between passes)\n",
			missing)
	}
	return out, nil
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

// renderResolveList writes an aligned table of open items for the skill route to
// select from. An empty selection prints a clear "no items" line naming the
// store dir it read — the same hint `debt add` prints on a write — so reading
// the wrong store is distinguishable from an empty backlog. Exits 0 either way.
//
// Every column passes through the same sanitizers renderDebtTable uses
// (debtIDCell, cell, truncate): a stray tab or CR in a store field must not tear
// a tabwriter row, and an empty id (a hand-edited store is the only source)
// renders as "-" rather than a blank leading column.
func renderResolveList(w io.Writer, dir string, recs []localdebt.Record) error {
	if len(recs) == 0 {
		_, err := fmt.Fprintf(w, "No items to resolve (no open items in %s).\n", dir)
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tSEVERITY\tFILE\tLINE\tPROBLEM"); err != nil {
		return err
	}
	for _, r := range recs {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n",
			debtIDCell(r.ID), cell(r.Severity), cell(r.File), r.Line, cell(truncate(r.Problem, 60))); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// maxReasonBytes bounds a --reason justification. It sits well under the store's 1 MiB
// per-line read cap (internal/localdebt maxLineBytes) so a justification can never push
// a record over the limit and be silently dropped on read.
const maxReasonBytes = 4 << 10 // 4 KiB

// markDebtResolved records an append-only resolution for id: it copies the item's
// first open record, unions the Reviewers attribution of every open record for the
// id (and picks the most recent non-empty Model) so AggregateQualitySignal credits
// every persona that raised the finding, stamps a terminal status/timestamp, and
// appends it so the fold in selectOpenDebt drops the item from the open list. The
// stable id is preserved (never re-stamped) so the resolution lines up with the
// original finding.
func markDebtResolved(cmd *cobra.Command, dir, id, status, reason string) error {
	// A --reason is stored verbatim as the record's Justification. The store bounds a
	// single JSONL line at maxLineBytes (1 MiB) on read and silently drops any line
	// over that limit, so an unbounded reason could make a finding unreadable. Reject
	// oversized justifications up front, well under the read cap, before touching the
	// store.
	if len(reason) > maxReasonBytes {
		return usageError(fmt.Errorf("--reason too long: %d bytes exceeds the %d-byte limit", len(reason), maxReasonBytes))
	}
	// Hydrate only the target id's own records: collectDebtIDRecords streams the
	// shards one file at a time and retains just this id's history, so peak memory
	// stays one shard plus one id — the same MEMORY ceiling selectOpenDebt's
	// two-pass selection argues for — instead of ReadAll materializing the entire
	// store to answer a single-id question. Indexed or streaming ID lookup is
	// tracked separately by the compaction/GC work at localdebt.Compact
	// (internal/localdebt/store.go:1066).
	recs, err := collectDebtIDRecords(dir, []string{id}, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
	if err != nil {
		return fmt.Errorf("atcr debt resolve: failed to read local debt store: %w", err)
	}

	// The already-closed guard tests the FOLDED effective status, not the presence
	// of a terminal record somewhere in history. Scanning all history would refuse
	// to close a regressed id a second time — permanently, since a resolution
	// record for it always exists — so a finding that came back could never be
	// closed again.
	// FoldRecords emits exactly one record per id, so this both decides the guard
	// and supplies the record to copy. Divergent terminal records can coexist for
	// one id (the no-lock TD-004 window below); the fold already resolves them by
	// precedence rather than shard read order, so the reported status is
	// deterministic — wontfix outranks resolved/deferred.
	var effective *localdebt.Record
	for _, f := range localdebt.FoldRecords(recs) {
		if f.ID == id {
			r := f
			effective = &r
			break
		}
	}
	// Refusal is gated on SETTLED, not merely closed. `deferred` carries a terminal
	// marker but means "not now", and every other view treats it as live debt — so
	// gating on isClosedStatus would leave a deferred item permanently unactionable:
	// refused here as already closed, while `debt list` and the dashboard kept
	// showing it as outstanding work. Deciding to fix or dismiss a deferred item
	// later is exactly what deferring is for.
	var alreadyClosed bool
	var closedStatus string
	if effective != nil && localdebt.IsSettledStatus(effective.Status) {
		alreadyClosed = true
		// Print the status the gate actually MATCHED, not the raw store text:
		// IsSettledStatus lowercases and trims before comparing, so a stored
		// "  ReSoLvEd  " settles the item — and the store is the world-appendable
		// .atcr/debt/, so its text is untrusted input that must never be echoed
		// verbatim to the terminal.
		closedStatus = strings.ToLower(strings.TrimSpace(effective.Status))
	}

	// The resolution copies the EFFECTIVE record — the same one the list rendered
	// and the user acted on — not the first open record in read order. After a
	// regression those differ: StampID excludes severity so a re-settled severity
	// keeps the id, and copying the original would stamp the resolution with the
	// superseded severity, fix, estimate and evidence. Compaction then makes that
	// stale record the id's live one. The copy happens below, once the id is
	// known to name a live, located item.
	var reviewers []string
	seenReviewer := make(map[string]bool)
	var model string
	for i := range recs {
		if recs[i].ID != id {
			continue
		}
		// SETTLED, not merely closed, matching the guard above: a `deferred` record is
		// live debt and its attribution counts. Skipping it here would resolve a
		// deferred item with an EMPTY reviewer union, denying every persona that
		// raised the finding its confirmed credit in the quality signal.
		if localdebt.IsSettledStatus(recs[i].Status) {
			continue
		}
		// Union attribution across every live record for the id: the same finding can
		// be re-raised by later reconcile runs as distinct open records under one
		// stable id, and AggregateQualitySignal reads only the terminal record
		// (foldTerminalByID) — stamping just the first open record's Reviewers would
		// deny the later runs' personas their dismissed/confirmed credit. Dedupe
		// first-seen so the order is deterministic.
		for _, rv := range recs[i].Reviewers {
			if !seenReviewer[rv] {
				seenReviewer[rv] = true
				reviewers = append(reviewers, rv)
			}
		}
		// The terminal record carries a single Model, and AggregateQualitySignal
		// buckets every credited persona by it — so the choice materially affects
		// attribution. Prefer the most recent open record's non-empty Model (stream
		// order is append order, so last non-empty wins): it reflects the latest
		// run's attribution, while a later attribution-less record never blanks an
		// earlier model (an empty Model is excluded from every signal row).
		if strings.TrimSpace(recs[i].Model) != "" {
			model = recs[i].Model
		}
	}
	// Concurrency-tolerant, not lock-protected: the item is already settled, so this
	// invocation reports and no-ops instead of appending another terminal record.
	// Two concurrent invocations can each pass this check before either appends (the
	// accepted TD-004 no-lock stance). Since epic 24.0 those two records need not be
	// identical duplicates: one may be --status resolved and the other --status
	// wontfix, so the durable trail can carry divergent terminal claims for one id.
	// The reported status comes from localdebt.FoldRecords, which resolves divergent
	// terminals by precedence (wontfix outranks resolved) rather than by shard read
	// order — so it is well-defined, not order-dependent.
	if alreadyClosed {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is already closed as %s; nothing to do.\n", id, closedStatus)
		return nil
	}
	// A nil effective record is the true "no such id" case. An id that DOES
	// resolve but whose effective record carries no File is a different defect:
	// the item renders as live work in `debt list` and the dashboard, so
	// reporting it as nonexistent would send the operator hunting for an id
	// that is right there — name the actual problem instead.
	if effective == nil {
		return fmt.Errorf("no open technical-debt item with id %q in the local store", id)
	}
	if effective.File == "" {
		return fmt.Errorf("id %q has no file location and cannot be resolved; it must be corrected in the store", id)
	}
	orig := *effective
	if status == "wontfix" && strings.TrimSpace(reason) == "" && !isRecordedRationale(orig.Justification) {
		return usageError(fmt.Errorf("--status wontfix requires --reason <justification>"))
	}

	now := time.Now().UTC().Format(time.RFC3339)
	rec := orig
	// Assign only a non-empty union, symmetric with Model below: an empty union
	// means no live record carried attribution, and blanking the copied record's
	// Reviewers would destroy credit rather than decline to add any.
	if len(reviewers) > 0 {
		rec.Reviewers = reviewers
	}
	if model != "" {
		rec.Model = model
	}
	rec.RunID = now + "-" + status
	rec.Timestamp = now
	rec.Status = status
	rec.ResolvedAt = now
	// The copied record is the FOLDED one, so it carries the id's aggregate
	// counters — which this record must not duplicate. A resolution is a status
	// marker, not an occurrence: leaving them set would let the fold count the
	// carried total AND the still-present detection record it was derived from,
	// inflating Occurrences on every resolve. The counters live on the id's
	// effective record alone, the same rule retainForCompaction applies to the
	// resolution trail it retains (internal/localdebt/store.go).
	rec.Occurrences = 0
	rec.FirstSeen = ""
	rec.CountedThrough = ""
	// A supplied --reason records why the finding was dismissed/resolved and
	// replaces any justification the item already carried (e.g. reconcile
	// enrichment); an empty reason preserves the existing justification, never
	// blanking it.
	if r := strings.TrimSpace(reason); r != "" {
		rec.Justification = r
	}
	// Bound the ENCODED record before appending, the same rule `debt add` enforces
	// (cli/debt_add.go): the resolution copies the finding verbatim and adds the
	// terminal fields, so a finding whose own record sits just under the read cap
	// tips its resolution line OVER localdebt.MaxRecordBytes. Every read path skips
	// a line that long — the resolution would be written, invisible forever, and
	// the item would stay open while the command reported success. maxReasonBytes
	// bounds only the --reason slice of the record; this bounds the whole thing.
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("atcr debt resolve: failed to encode resolution: %w", err)
	}
	if len(line)+1 > localdebt.MaxRecordBytes {
		return usageError(fmt.Errorf(
			"resolution record is too large to store: %d bytes encoded, cap is %d (the store skips longer lines on read, so the item would stay open while reported resolved)",
			len(line)+1, localdebt.MaxRecordBytes))
	}
	if err := localdebt.Append(dir, rec); err != nil {
		return fmt.Errorf("atcr debt resolve: failed to record resolution: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Marked %s %s.\n", id, status)
	return nil
}
