package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// version is the atcr release version. It is overridable at build time via
// -ldflags "-X github.com/samestrin/atcr/cli.version=v1.2.3"; left
// empty for ordinary `go build`/`go install` so atcrVersion can derive a
// meaningful value from the embedded build info instead. The fully-qualified
// -X target must track this var's package (it moved out of package main in
// Sprint 34.0 Task 03) or release binaries silently report "dev".
var version = ""

// atcrVersion resolves the version string reported by `atcr --version` and
// `atcr version`. Precedence: an ldflags-injected value wins; otherwise the
// module version recorded by `go install <module>@<v>` is used; otherwise the
// VCS revision embedded in a local build; falling back to "dev" when nothing
// is available (e.g. `go run`).
func atcrVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				rev := s.Value
				if len(rev) > 12 {
					rev = rev[:12]
				}
				return "dev+" + rev
			}
		}
	}
	return "dev"
}

// initVersionFlags wires the root's version-flag surface (epic 165 T3, axi.md
// Principle 10): -v, -V and --version each print the BARE version on stdout,
// exit 0, in both output modes. The `version` subcommand keeps its conventional
// `atcr version <ver>` line (newVersionCmd).
//
// Cobra registers --version (shorthand -v) from root.Version and short-circuits
// on it in execute() BEFORE PersistentPreRunE. Two pieces extend that without
// losing the short-circuit:
//
//   - SetVersionTemplate replaces cobra's `atcr version <ver>` rendering with
//     the bare version plus a newline.
//   - pflag allows only one shorthand per flag, so -V cannot be a second
//     shorthand on --version. Instead a HIDDEN alias flag carries the shorthand
//     and SHARES the version flag's pflag.Value: parsing -V sets the same bool
//     cobra reads via GetBool("version"), so -V takes the identical early-exit
//     path — no logger, no telemetry, no mode resolution. The alias is hidden
//     so help and the flag list keep showing only -v/--version; validFlags
//     (axi_error.go) folds its shorthand into the error document's help[].
//
// InitDefaultVersionFlag is called here (cobra would otherwise call it lazily
// during Execute) so the alias can share the flag's value at construction time;
// cobra's later call is a no-op because Lookup("version") already finds it.
func initVersionFlags(root *cobra.Command) {
	root.InitDefaultVersionFlag()
	root.SetVersionTemplate("{{.Version}}\n")
	vf := root.Flags().Lookup("version")
	if vf == nil {
		return // root.Version empty: no version flag to alias
	}
	root.Flags().AddFlag(&pflag.Flag{
		Name:      "version-upper",
		Shorthand: "V",
		Usage:     "version for atcr (alias of --version)",
		Value:     vf.Value,
		DefValue:  vf.DefValue,
		// NoOptDefVal marks a bool-style flag that needs no argument; without
		// it pflag reads `-V` as "flag needs an argument".
		NoOptDefVal: vf.NoOptDefVal,
		Hidden:      true,
	})
}

// newVersionCmd prints the atcr version. It shares atcrVersion with the root's
// version flags so the subcommand and the flags never report different
// versions; since epic 165 T3 the flags print the bare version (axi.md
// Principle 10) while this subcommand keeps the conventional `atcr version
// <ver>` line. Provided alongside the flags because many tooling conventions
// invoke `atcr version` rather than `atcr --version`.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the atcr version",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "atcr version %s\n", atcrVersion())
			return err
		},
	}
}
