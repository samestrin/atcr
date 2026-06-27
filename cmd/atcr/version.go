package main

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// version is the atcr release version. It is overridable at build time via
// -ldflags "-X main.version=v1.2.3"; left empty for ordinary `go build`/`go
// install` so atcrVersion can derive a meaningful value from the embedded
// build info instead.
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

// newVersionCmd prints the atcr version. It mirrors the output of the cobra
// `--version` flag (set via root.Version) so the subcommand and the flag never
// diverge. Provided alongside the flag because many tooling conventions invoke
// `atcr version` rather than `atcr --version`.
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
