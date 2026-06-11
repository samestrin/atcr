package main

import "github.com/spf13/cobra"

// newServeCmd builds `atcr serve`: an MCP stdio server wrapping the same
// engine as the CLI.
func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP stdio server over the review engine",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented("serve")
		},
	}
	return cmd
}
