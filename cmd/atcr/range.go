package main

import (
	"encoding/json"
	"fmt"

	"github.com/samestrin/atcr/internal/gitrange"
	"github.com/spf13/cobra"
)

// newRangeCmd builds `atcr range`: pre-flight range resolution that prints
// the resolution JSON without invoking any provider. Flag-relationship
// violations surface as usage errors (exit 2) via addRangeFlags; resolution
// failures (empty range, shallow clone, invalid ref) bubble up as plain
// errors (exit 1) per AC 01-02.
func newRangeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "range",
		Short: "Resolve the review range and print resolution JSON",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			base, _ := cmd.Flags().GetString("base")
			head, _ := cmd.Flags().GetString("head")
			mergeCommit, _ := cmd.Flags().GetString("merge-commit")

			res, err := gitrange.Resolve(cmd.Context(), ".", gitrange.Options{
				Base:        base,
				Head:        head,
				MergeCommit: mergeCommit,
			})
			if err != nil {
				return err
			}

			out, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return fmt.Errorf("encoding resolution: %w", err)
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(out)); err != nil {
				return fmt.Errorf("writing resolution: %w", err)
			}
			return nil
		},
	}
	addRangeFlags(cmd)
	return cmd
}
