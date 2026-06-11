package main

import "github.com/spf13/cobra"

// newReconcileCmd builds `atcr reconcile`: discover sources, normalize,
// cluster, dedupe, compute confidence, and write reconciled artifacts.
func newReconcileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reconcile [id-or-path]",
		Short: "Merge findings from all sources into reconciled artifacts",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented("reconcile")
		},
	}
	return cmd
}
