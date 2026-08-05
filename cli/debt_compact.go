package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/localdebt"
)

func newDebtCompactCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compact",
		Short: "Compact the public, .atcr/-scoped local TD store (drop superseded records)",
		Long: "atcr debt compact reads the public local technical-debt store (.atcr/debt/)\n" +
			"and folds it by id, dropping superseded records atomically so that\n" +
			"on-disk store size tracks live findings, not total history.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runDebtCompact,
	}
	addDebtStoreFlag(cmd)
	return cmd
}

func runDebtCompact(cmd *cobra.Command, _ []string) error {
	dir := debtStoreDir(cmd)

	opts := localdebt.ReadOpts{Writer: cmd.ErrOrStderr()}
	res, err := localdebt.Compact(dir, opts)
	if err != nil {
		return fmt.Errorf("compact: %w", err)
	}

	// The three no-op shapes are distinguished by the two counts, not by
	// StoreFound: it now answers only "is there a store here", so "no store" can no
	// longer be printed over records that exist (localdebt.CompactResult).
	if !res.StoreFound {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No local TD store to compact.")
		return nil
	}
	if res.RecordsBefore == 0 {
		// Records written by a newer atcr are kept verbatim rather than folded or
		// dropped. Say so, or the store looks stubbornly larger than the fold counts
		// imply and the natural next step is to go delete something.
		if res.Preserved > 0 {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"Nothing to compact: all %d record(s) were written by a newer atcr version and were left untouched. Upgrade to fold them.\n",
				res.Preserved)
			return nil
		}
		// A store whose shards hold nothing this binary can read at all: not "no
		// store", and not a fold either.
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Nothing to compact: the store holds no readable records.")
		return nil
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Compacted %d records into %d (%d superseded dropped).\n",
		res.RecordsBefore, res.RecordsAfter, res.Dropped)
	if res.Preserved > 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(),
			"Kept %d record(s) written by a newer atcr version untouched; upgrade to fold them.\n",
			res.Preserved)
	}
	return nil
}
