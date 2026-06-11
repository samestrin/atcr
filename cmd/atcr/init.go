package main

import "github.com/spf13/cobra"

// newInitCmd builds `atcr init`: write the project config and editable
// persona files from embedded defaults.
func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write .atcr/config.yaml and editable persona files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented("init")
		},
	}
	cmd.Flags().Bool("force", false, "overwrite existing configuration and persona files")
	return cmd
}
