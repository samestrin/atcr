package main

import (
	"io"

	"github.com/spf13/cobra"
)

// quickstartOpts carries the inputs for an `atcr quickstart` run. Streams and
// the browser-open hook are injectable so the interactive flow is unit-testable
// without a TTY or a real browser.
type quickstartOpts struct {
	dir    string
	force  bool
	open   bool
	in     io.Reader
	out    io.Writer
	errOut io.Writer
	openFn func(string) error
}

// newQuickstartCmd builds `atcr quickstart`: the interactive onboarding wizard.
// It scaffolds the same .atcr/ workspace as `atcr init` (reusing its writers),
// then layers on an interactive synthetic-provider + key-env setup and a CI
// workflow scaffold so a new user reaches their first review quickly.
func newQuickstartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quickstart",
		Short: "Interactive onboarding: scaffold config, provider, and a CI workflow",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			force, err := cmd.Flags().GetBool("force")
			if err != nil {
				return err
			}
			open, err := cmd.Flags().GetBool("open")
			if err != nil {
				return err
			}
			return runQuickstart(quickstartOpts{
				dir:    ".",
				force:  force,
				open:   open,
				in:     cmd.InOrStdin(),
				out:    cmd.OutOrStdout(),
				errOut: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().Bool("force", false, "overwrite existing configuration and workflow files")
	cmd.Flags().Bool("open", false, "open the provider signup page in a browser")
	return cmd
}

// runQuickstart orchestrates the onboarding wizard. It first reuses `atcr init`'s
// writers to lay down .atcr/config.yaml and the editable personas, then (in later
// steps) sets up the synthetic provider, guides the user through the API-key env
// var, and scaffolds a CI workflow.
func runQuickstart(o quickstartOpts) error {
	if err := runInit(o.dir, o.force, o.out, o.errOut); err != nil {
		return err
	}
	return nil
}
