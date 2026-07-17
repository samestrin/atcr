package main

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
	cmd.Flags().String("dir", defaultDebtResolveDir, "path to the local TD store (.atcr/debt)")
	return cmd
}

func runDebtCompact(cmd *cobra.Command, _ []string) error {
	dir := mustFlag(cmd, "dir")

	opts := localdebt.ReadOpts{Writer: cmd.ErrOrStderr()}
	if err := localdebt.Compact(dir, opts); err != nil {
		return fmt.Errorf("compact: %w", err)
	}

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Compaction complete.")
	return nil
}
