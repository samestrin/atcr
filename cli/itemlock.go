package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/localdebt"
)

// itemLockPublishedNote is the warning `lock --help` and `unlock --help` carry:
// the holder name leaves this machine.
const itemLockPublishedNote = "The holder name is stored in a ref on the remote, and is readable by\n" +
	"anyone who can read that remote. The holder is --holder, else $" + localdebt.ItemLockHolderEnv + ",\n" +
	"else the OS user name alone (never user@host)."

// newDebtLockCmd builds `atcr debt lock <id>...` (Epic 93). Each lock is one
// annotated ref, refs/atcr/debt-locks/<hex id>, pushed to the store repository's
// remote; the push is the mutex, so of two clones locking one item exactly one
// wins. Locks are advisory: only a second lock of the same item checks them.
func newDebtLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock <id>...",
		Short: "Lock debt items on the store repository's remote so only one clone works each",
		Long: "atcr debt lock pushes one ref per item, refs/atcr/debt-locks/<hex id>, to the\n" +
			"remote of the git repository that holds the store. A push of a ref that\n" +
			"already exists fails, so of two clones locking one item exactly one wins.\n" +
			"Several ids lock in one atomic push: if any is already locked, none is taken.\n" +
			"A store outside a git repository, a repository with no remote, or a remote\n" +
			"that does not answer within 30s is an error; there is no local fallback.\n" +
			"Locks are advisory: debt resolve and debt add do not check them.\n\n" +
			itemLockPublishedNote,
		Args: usageArgs(cobra.MinimumNArgs(1)),
		RunE: runDebtLock,
	}
	addDebtStoreFlag(cmd)
	addItemLockHolderFlag(cmd)
	return cmd
}

// newDebtUnlockCmd builds `atcr debt unlock <id>...`, which deletes the refs
// lock pushed, only for the holder recorded in each.
func newDebtUnlockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlock <id>...",
		Short: "Release debt item locks taken by atcr debt lock",
		Long: "atcr debt unlock deletes each item's ref, refs/atcr/debt-locks/<hex id>, from\n" +
			"the store repository's remote, in one atomic push. An item that is not\n" +
			"locked, or is locked under a different holder, is an error and nothing is\n" +
			"released; pass that holder's name with --holder to release their lock.\n\n" +
			itemLockPublishedNote,
		Args: usageArgs(cobra.MinimumNArgs(1)),
		RunE: runDebtUnlock,
	}
	addDebtStoreFlag(cmd)
	addItemLockHolderFlag(cmd)
	return cmd
}

func addItemLockHolderFlag(cmd *cobra.Command) {
	cmd.Flags().String("holder", "", "name recorded in the lock, stored on the remote (default $"+localdebt.ItemLockHolderEnv+", else the OS user name)")
}

// itemLocker builds the locker for a lock or unlock invocation.
func itemLocker(cmd *cobra.Command) (localdebt.ItemLocker, error) {
	dir, err := debtStoreDir(cmd)
	if err != nil {
		return localdebt.ItemLocker{}, err
	}
	holder := localdebt.ResolveItemLockHolder(mustFlag(cmd, "holder"))
	return localdebt.ItemLocker{Dir: dir, Holder: holder}, nil
}

func runDebtLock(cmd *cobra.Command, ids []string) error {
	l, err := itemLocker(cmd)
	if err != nil {
		return err
	}
	// An id the store does not hold is refused before any push: a typo would
	// otherwise take a lock that protects nothing and report success.
	recs, err := collectDebtIDRecords(l.Dir, ids, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
	if err != nil {
		return fmt.Errorf("atcr debt lock: failed to read local debt store: %w", err)
	}
	known := make(map[string]bool, len(recs))
	for _, r := range recs {
		known[r.ID] = true
	}
	for _, id := range ids {
		if !known[id] {
			return usageError(fmt.Errorf("atcr debt lock: no item %q in the store at %s", id, l.Dir))
		}
	}
	locks, err := l.Lock(cmd.Context(), ids)
	if err != nil {
		return itemLockError("lock", err)
	}
	for _, lk := range locks {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Locked %s (holder %s, at %s, %s on %s).\n",
			lk.ID, lk.Holder, lk.At.Format(time.RFC3339), lk.Ref, lk.Remote)
	}
	return nil
}

func runDebtUnlock(cmd *cobra.Command, ids []string) error {
	l, err := itemLocker(cmd)
	if err != nil {
		return err
	}
	locks, err := l.Unlock(cmd.Context(), ids)
	if err != nil {
		return itemLockError("unlock", err)
	}
	for _, lk := range locks {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Unlocked %s (holder %s, locked at %s).\n",
			lk.ID, lk.Holder, lk.At.Format(time.RFC3339))
	}
	return nil
}

// itemLockError prefixes the command name, and marks invalid input as a usage
// error, keeping the sentinel in the chain.
func itemLockError(verb string, err error) error {
	err = fmt.Errorf("atcr debt %s: %w", verb, err)
	if errors.Is(err, localdebt.ErrItemLockInput) {
		return usageError(err)
	}
	return err
}
