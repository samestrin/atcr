package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/history"
	"github.com/spf13/cobra"
)

// defaultHistorySince is the query window used when --since is omitted: wide
// enough (90 days) to be useful by default, while still bounding the table.
const defaultHistorySince = 90 * 24 * time.Hour

// newHistoryCmd builds `atcr history`: read the append-only finding history —
// the monthly shards under .atcr/history plus the legacy pre-19.4 flat ledger
// .atcr/findings-history.jsonl (Epic 35.14) — filter it by a time window
// (--since) and package prefix
// (--package), and print a markdown table of counts by severity per package. An
// absent or fully-filtered history is not an error — it exits 0 with a "no
// history" notice (Epic 19.0 AC3).
//
// --prune <horizon> additionally deletes whole monthly shards older than the
// horizon before reporting. It is the only destructive thing this command can
// do, and it is strictly opt-in: there is no default horizon and no implicit
// pruning anywhere else — a review run appends to the ledger but never trims it,
// so history is only ever deleted by someone asking for it.
func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show finding history over time as a markdown table",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runHistory,
	}
	cmd.Flags().String("since", "", "only include findings within this window: h/m/s or d/w units (e.g. 30d, 2w, 48h); default 90d")
	cmd.Flags().String("package", "", "only include findings whose package is at or under this path prefix (e.g. internal/registry)")
	cmd.Flags().String("prune", "", "DELETE monthly shards older than this retention horizon before reporting (e.g. 24w, 365d); minimum 28d, no default. Note: h/m/s units mean hours/MINUTES/seconds")
	return cmd
}

// minPruneHorizon is the shortest retention horizon --prune accepts. ParseSince
// falls back to time.ParseDuration for h/m/s units, where "m" means MINUTES —
// so `--prune 6m` ("six months") would compute a 6-minute cutoff and
// irreversibly delete every shard but the current UTC month. A horizon shorter
// than a month can never be a sane retention policy for monthly shards, so
// anything below this floor is rejected as a usage error.
const minPruneHorizon = 28 * 24 * time.Hour

func runHistory(cmd *cobra.Command, _ []string) error {
	since := defaultHistorySince
	if raw, _ := cmd.Flags().GetString("since"); strings.TrimSpace(raw) != "" {
		d, err := history.ParseSince(raw)
		if err != nil {
			return usageError(err) // bad --since is a usage error (exit 2)
		}
		since = d
	}
	// Parsed before anything is read or deleted: a bad horizon must fail the
	// command outright rather than after a partial prune.
	var horizon time.Duration
	if raw, _ := cmd.Flags().GetString("prune"); strings.TrimSpace(raw) != "" {
		d, err := history.ParseSince(raw)
		if err != nil {
			return usageError(fmt.Errorf("--prune: %w", err)) // exit 2, nothing deleted
		}
		if d < minPruneHorizon {
			return usageError(fmt.Errorf("--prune: retention horizon %q is below the 28-day minimum (units: d/w = days/weeks, h/m/s = hours/MINUTES/seconds — '6m' is 6 minutes, not 6 months)", raw)) // exit 2, nothing deleted
		}
		horizon = d
	}
	pkg, _ := cmd.Flags().GetString("package")

	// A shard holds every package's records for its month, so pruning cannot be
	// scoped to one package — it is file-granular by construction. Silently
	// ignoring --package here would let a user delete every package's history
	// believing the deletion was scoped, so the combination is rejected before
	// anything is removed.
	if horizon > 0 && strings.TrimSpace(pkg) != "" {
		return usageError(fmt.Errorf("--prune cannot be combined with --package: pruning removes whole monthly shards, which hold every package's records"))
	}

	root, err := repoRoot()
	if err != nil {
		return usageError(fmt.Errorf("resolving repo root: %w", err))
	}
	// One `now` for both the shard selection and the record-level Filter below:
	// two separate time.Now() calls could straddle a month boundary and drop a
	// shard the filter then expects records from.
	now := time.Now()

	out := cmd.OutOrStdout()
	// Prune notices go to stderr, not stdout: stdout carries the markdown table
	// and nothing else, so `atcr history --prune 365d > report.md` still produces
	// a valid document.
	diag := cmd.ErrOrStderr()

	// Prune first, so the report that follows describes what is actually left on
	// disk. Removals are always named — including on the error path, since a
	// failed prune is not an unwound one: shards removed before the failure are
	// already gone, and a deletion the user is never told about is exactly how a
	// later query reads as data loss.
	pruned := false
	if horizon > 0 {
		res, perr := history.PruneShards(history.ShardDir(root), horizon, now)
		pruned = true
		if len(res.Removed) > 0 {
			_, _ = fmt.Fprintf(diag, "pruned %d shard(s) past the retention horizon: %s (%d file(s) kept)\n",
				len(res.Removed), strings.Join(res.Removed, ", "), res.Kept)
		}
		if perr != nil {
			// A filesystem failure, not a bad invocation: exit 1, not the usage
			// code CI scripts read as "misconfigured command".
			return fmt.Errorf("pruning history: %w", perr)
		}
		if len(res.Removed) == 0 {
			_, _ = fmt.Fprintf(diag, "no shards older than the retention horizon — nothing pruned (%d file(s) kept)\n", res.Kept)
		}
	}

	recs, err := history.LoadAllSince(history.ShardDir(root), history.LegacyLedgerPath(root), since, now)
	if err != nil {
		return usageError(err) // corrupt/unreadable ledger (exit 2)
	}

	// An empty result no longer implies an empty ledger: since Epic 35.14 the
	// shards outside the --since window are never opened, so a repo with years of
	// out-of-window history also loads zero records. Only a genuinely empty store
	// earns the first-run hint — telling a user to "run atcr review first" when
	// they have history that simply predates the window would read as data loss.
	// After a prune the hint is suppressed outright: "run atcr review first"
	// directly under "pruned 4 shard(s)" would contradict the line above it.
	if len(recs) == 0 && !pruned && !history.HasAny(history.ShardDir(root), history.LegacyLedgerPath(root)) {
		_, _ = fmt.Fprintln(out, "no history recorded yet — run 'atcr review' first")
		return nil
	}

	filtered := history.Filter(recs, since, pkg, now)
	if len(filtered) == 0 {
		scope := "the selected window"
		if strings.TrimSpace(pkg) != "" {
			scope = fmt.Sprintf("package %q within the selected window", strings.TrimSpace(pkg))
		}
		_, _ = fmt.Fprintf(out, "no history for %s\n", scope)
		return nil
	}

	_, _ = fmt.Fprint(out, history.RenderTable(filtered))
	return nil
}
