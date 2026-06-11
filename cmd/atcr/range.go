package main

import "github.com/spf13/cobra"

// newRangeCmd builds `atcr range`: pre-flight range resolution that prints
// the resolution JSON without invoking any provider.
func newRangeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "range",
		Short: "Resolve the review range and print resolution JSON",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented("range")
		},
	}
	addRangeFlags(cmd)
	return cmd
}
