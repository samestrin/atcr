package main

import (
	"github.com/spf13/cobra"
)

// newAuditReportCmd is stubbed for the RED stage: the flag exists so tests can
// invoke it, but it renders nothing yet.
func newAuditReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit-report",
		Short: "Render a one-page compliance report for a PR's review runs",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return nil
		},
	}
	cmd.Flags().Int("pr", 0, "pull-request number to report on")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}
