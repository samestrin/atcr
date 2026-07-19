package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// runHome renders the Content-First home view (axi.md Principle 8) for a bare
// `atcr` invocation — the case where the root command's RunE fires because no
// subcommand was given. It replaces the former cmd.Help() call. Cobra's
// -h/--help and --version short-circuit before RunE, so they are structurally
// unaffected; every subcommand keeps its own RunE. This is the T1 branch scaffold
// — the full renderer (T2) and the --axi payload (T3) build on it.
func runHome(cmd *cobra.Command) error {
	_, err := fmt.Fprintln(cmd.OutOrStdout(), cmd.Short)
	return err
}
