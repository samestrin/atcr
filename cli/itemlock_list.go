package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/localdebt"
)

// defaultDebtLocksOlderThan is the age past which `atcr debt locks` flags a lock
// (Epic 93 decision Q2).
const defaultDebtLocksOlderThan = 12 * time.Hour

// The reasons `atcr debt locks` flags a lock, as they appear in --json.
const (
	debtLockFlagOld      = "older-than"
	debtLockFlagResolved = "item-resolved"
)

// debtLocksReport is the --json shape of `atcr debt locks`. Warnings is empty,
// never null, when every ref under the lock prefix reads as a lock.
type debtLocksReport struct {
	OlderThan string            `json:"older_than"`
	Locks     []debtLockRow     `json:"locks"`
	Warnings  []debtLockWarning `json:"warnings"`
}

// debtLockWarning is one ref under refs/atcr/debt-locks/ that could not be read
// as a lock (epic 122 T4): it is shown, never hidden, and never frees anything.
type debtLockWarning struct {
	Ref    string `json:"ref"`
	Remote string `json:"remote"`
	Reason string `json:"reason"`
}

// debtLockRow is one lock. Status is the item's folded status in the store, ""
// when the store does not hold the id. Flags is empty, never null, for a lock
// nothing flags.
type debtLockRow struct {
	ID       string   `json:"id"`
	Holder   string   `json:"holder"`
	LockedAt string   `json:"locked_at"`
	Status   string   `json:"status"`
	Flags    []string `json:"flags"`
	Ref      string   `json:"ref"`
	Remote   string   `json:"remote"`
}

// newDebtLocksCmd builds `atcr debt locks` (Epic 93 T2): it lists every item
// lock on the store remote and flags the ones worth a look. It only reads.
func newDebtLocksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "locks",
		Short: "List debt item locks and flag old ones and ones on resolved items",
		Long: "atcr debt locks lists every lock atcr debt lock pushed to the store\n" +
			"repository's remote: item, holder, and when it was taken. It flags a lock\n" +
			"older than --older-than (default 12h) and a lock on an item whose status in\n" +
			"the store is resolved. A flag is a prompt to ask the holder, nothing more:\n" +
			"there is no liveness check, and the command never frees a lock. Release one\n" +
			"with atcr debt unlock --holder <holder> <id>. A ref under the lock namespace\n" +
			"that cannot be read as a lock is shown as a warning, never hidden, and does\n" +
			"not fail the listing.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runDebtLocks,
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().Duration("older-than", defaultDebtLocksOlderThan, "flag a lock taken longer ago than this")
	addJSONFlag(cmd)
	return cmd
}

func runDebtLocks(cmd *cobra.Command, _ []string) error {
	olderThan, err := cmd.Flags().GetDuration("older-than")
	if err != nil {
		return usageError(err)
	}
	if olderThan <= 0 {
		return usageError(fmt.Errorf("atcr debt locks: --older-than must be positive, got %s", olderThan))
	}
	dir, err := debtStoreDir(cmd)
	if err != nil {
		return err
	}
	locks, lockWarnings, err := localdebt.ItemLocker{Dir: dir}.List(cmd.Context())
	if err != nil {
		return fmt.Errorf("atcr debt locks: %w", err)
	}
	sums, err := localdebt.ReadSummaries(dir, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
	if err != nil {
		return fmt.Errorf("atcr debt locks: failed to read local debt store: %w", localdebt.RedactPathErr(err))
	}
	status := make(map[string]string)
	for _, s := range localdebt.FoldSummaries(sums) {
		status[s.ID] = s.Status
	}
	now := time.Now()
	rep := debtLocksReport{
		OlderThan: olderThan.String(),
		Locks:     make([]debtLockRow, 0, len(locks)),
		Warnings:  make([]debtLockWarning, 0, len(lockWarnings)),
	}
	for _, w := range lockWarnings {
		rep.Warnings = append(rep.Warnings, debtLockWarning{Ref: w.Ref, Remote: w.Remote, Reason: w.Reason})
	}
	for _, lk := range locks {
		row := debtLockRow{
			ID: lk.ID, Holder: lk.Holder, LockedAt: lk.At.Format(time.RFC3339),
			Status: status[lk.ID], Flags: []string{}, Ref: lk.Ref, Remote: lk.Remote,
		}
		if now.Sub(lk.At) > olderThan {
			row.Flags = append(row.Flags, debtLockFlagOld)
		}
		if strings.EqualFold(strings.TrimSpace(row.Status), localdebt.StatusResolved) {
			row.Flags = append(row.Flags, debtLockFlagResolved)
		}
		rep.Locks = append(rep.Locks, row)
	}
	if boolFlag(cmd, "json") {
		return writeJSON(cmd.OutOrStdout(), rep)
	}
	return renderDebtLocks(cmd.OutOrStdout(), rep, olderThan, now)
}

// renderDebtLocks prints the human table: one row per lock, its age, and why it
// is flagged, then one line per warning for a ref that could not be read.
func renderDebtLocks(w io.Writer, rep debtLocksReport, olderThan time.Duration, now time.Time) error {
	if len(rep.Locks) == 0 {
		if _, err := fmt.Fprintln(w, "No item locks."); err != nil {
			return err
		}
		return renderDebtLockWarnings(w, rep.Warnings)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tHOLDER\tLOCKED AT\tAGE\tSTATUS\tFLAGGED")
	flagged := 0
	for _, r := range rep.Locks {
		age := "-"
		if at, err := time.Parse(time.RFC3339, r.LockedAt); err == nil {
			age = now.Sub(at).Truncate(time.Minute).String()
		}
		var why []string
		if slices.Contains(r.Flags, debtLockFlagOld) {
			why = append(why, "older than "+olderThan.String())
		}
		if slices.Contains(r.Flags, debtLockFlagResolved) {
			why = append(why, "item resolved")
		}
		if len(why) > 0 {
			flagged++
		}
		st := r.Status
		if st == "" {
			st = "-"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.Holder, r.LockedAt, age, st, strings.Join(why, ", "))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\n%d of %d locks flagged. Nothing was freed; release a lock with atcr debt unlock.\n", flagged, len(rep.Locks)); err != nil {
		return err
	}
	return renderDebtLockWarnings(w, rep.Warnings)
}

// renderDebtLockWarnings prints one line per bad ref, so a ref that cannot be
// read as a lock is shown and never hidden (epic 122 T4). Each line names the
// remote beside the ref, so the same bad ref on two remotes is distinguishable
// (epic 127 T2, row f111ddd891700632).
func renderDebtLockWarnings(w io.Writer, warnings []debtLockWarning) error {
	if len(warnings) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "\n%d warning(s): these refs under %s could not be read as locks; inspect or delete them with git:\n", len(warnings), localdebt.ItemLockRefPrefix); err != nil {
		return err
	}
	for _, wn := range warnings {
		if _, err := fmt.Fprintf(w, "  %s on %s: %s\n", wn.Ref, wn.Remote, wn.Reason); err != nil {
			return err
		}
	}
	return nil
}
