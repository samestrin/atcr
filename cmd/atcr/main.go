// Command atcr is the Agent Team Code Review CLI: it fans a code change out
// to a panel of LLM reviewer personas and reconciles their findings into a
// single deduplicated, confidence-scored deliverable.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := newRootCmd()
	if err := root.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "atcr:", err)
		os.Exit(exitCode(err))
	}
}

// exitCode maps an error returned by a subcommand to a process exit code.
// Exit-code semantics are centralized here: 0 success, 1 failure (including
// --fail-on threshold violations), 2 usage or configuration errors.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var coded interface{ ExitCode() int }
	if ok := asExitCoder(err, &coded); ok {
		return coded.ExitCode()
	}
	return 1
}

// asExitCoder reports whether err (or any error in its chain) carries an
// explicit exit code.
func asExitCoder(err error, target *interface{ ExitCode() int }) bool {
	for err != nil {
		if c, ok := err.(interface{ ExitCode() int }); ok {
			*target = c
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// newRootCmd constructs the atcr command tree. All subcommands use RunE so
// errors bubble up to main() for centralized exit-code mapping.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "atcr",
		Short:         "Agent Team Code Review — a review panel, not a reviewer",
		Long:          "atcr fans a code change out to a panel of heterogeneous LLM reviewer personas,\nthen deterministically reconciles their findings into a single deduplicated,\nconfidence-scored deliverable.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		newReviewCmd(),
		newReconcileCmd(),
		newReportCmd(),
		newRangeCmd(),
		newInitCmd(),
		newServeCmd(),
	)
	return root
}

// errNotImplemented marks subcommands whose engine wiring lands in a later
// sprint phase.
func errNotImplemented(name string) error {
	return fmt.Errorf("%s: not implemented yet", name)
}
