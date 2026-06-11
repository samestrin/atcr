package main

import "github.com/spf13/cobra"

// newRangeCmd builds `atcr range`: pre-flight range resolution that prints
// the resolution JSON without invoking any provider.
func newRangeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "range",
		Short: "Resolve the review range and print resolution JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented("range")
		},
	}
	cmd.Flags().String("base", "", "base ref for the review range")
	cmd.Flags().String("head", "", "head ref for the review range")
	cmd.Flags().String("merge-commit", "", "merge commit SHA (base = SHA^, head = SHA)")
	cmd.MarkFlagsRequiredTogether("base", "head")
	cmd.MarkFlagsMutuallyExclusive("base", "merge-commit")
	return cmd
}
