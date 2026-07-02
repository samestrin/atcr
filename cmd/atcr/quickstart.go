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
func newQuickstartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quickstart",
		Short: "Interactive onboarding: scaffold config, provider, and a CI workflow",
	}
}

// runQuickstart orchestrates the onboarding wizard. Stub — implemented in GREEN.
func runQuickstart(o quickstartOpts) error {
	return nil
}
