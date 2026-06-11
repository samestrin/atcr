package main

import "github.com/spf13/cobra"

// newReportCmd builds `atcr report`: render views over reconciled findings.
func newReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report [id-or-path]",
		Short: "Render md, json, or checklist views over reconciled findings",
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented("report")
		},
	}
	cmd.Flags().String("format", "md", "output format: md, json, or checklist")
	return cmd
}
