package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// homeUserDir is a seam over os.UserHomeDir so tests can pin the home directory
// and make ~-relativization deterministic.
var homeUserDir = os.UserHomeDir

// homeState is the resolved live review state the home view renders: whether any
// review has run yet and, if so, its id and status.
type homeState struct {
	hasReview bool
	reviewID  string
	status    string
}

// relHome renders path with the user's home-directory prefix replaced by "~"
// (axi.md Principle 8's example). STUB (T2 RED): returns the path unchanged.
func relHome(path string) string { return path }

// resolveHomeState resolves the current review's id/status. STUB (T2 RED):
// always reports the no-review state.
func resolveHomeState() homeState { return homeState{} }

// renderHomeView writes the non-axi home view. STUB (T2 RED): writes nothing.
func renderHomeView(w io.Writer, execPath, description string, st homeState) error { return nil }

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
