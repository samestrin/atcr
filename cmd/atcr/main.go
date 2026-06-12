// Command atcr is the Agent Team Code Review CLI: it fans a code change out
// to a panel of LLM reviewer personas and reconciles their findings into a
// single deduplicated, confidence-scored deliverable.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := newRootCmd()
	if err := root.ExecuteContext(context.Background()); err != nil {
		code := exitCode(err)
		if code != 0 {
			fmt.Fprintln(os.Stderr, "atcr:", err)
		}
		os.Exit(code)
	}
}

// Exit-code semantics, centralized: 0 success, 1 failure (including
// --fail-on threshold violations), 2 usage or configuration errors.
const (
	exitFailure = 1
	exitUsage   = 2
)

// codedError carries an explicit process exit code through the error chain.
type codedError struct {
	code int
	err  error
}

func (e *codedError) Error() string { return e.err.Error() }
func (e *codedError) Unwrap() error { return e.err }
func (e *codedError) ExitCode() int { return e.code }

// usageError marks err as a usage/configuration error (exit 2).
func usageError(err error) error {
	return &codedError{code: exitUsage, err: err}
}

// exitCode maps an error returned by a subcommand to a process exit code.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return exitFailure
}

// usageArgs wraps a cobra positional-args validator so violations map to
// exit code 2 instead of the generic failure code.
func usageArgs(v cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := v(cmd, args); err != nil {
			return usageError(err)
		}
		return nil
	}
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
		// An unknown subcommand is a usage error (exit 2), not the generic
		// failure code: in CI, exit 1 specifically means "findings at/above
		// threshold". Setting Args bypasses cobra's legacyArgs path (which
		// returns an uncoded error from Find), and the RunE keeps bare `atcr`
		// printing help with exit 0.
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	// Flag-parse errors (unknown flags, bad values, violated flag groups)
	// are usage errors: exit 2.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError(err)
	})

	root.AddCommand(
		newReviewCmd(),
		newReconcileCmd(),
		newReportCmd(),
		newRangeCmd(),
		newStatusCmd(),
		newInitCmd(),
		newServeCmd(),
		newDoctorCmd(),
		newTrustCmd(),
	)
	return root
}
