// Command atcr is the Agent Team Code Review CLI: it fans a code change out
// to a panel of LLM reviewer personas and reconciles their findings into a
// single deduplicated, confidence-scored deliverable.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/samestrin/atcr/internal/log"
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
		Use:   "atcr",
		Short: "Agent Team Code Review — a review panel, not a reviewer",
		Long: "atcr fans a code change out to a panel of heterogeneous LLM reviewer personas,\n" +
			"then deterministically reconciles their findings into a single deduplicated,\n" +
			"confidence-scored deliverable.\n\n" +
			"Logging:\n" +
			"  LOG_LEVEL      environment variable: debug, info, warn, error (default info).\n" +
			"                 Set LOG_LEVEL=debug to diagnose a failing review.\n" +
			"  --log-format   log output format: text or json (default text).\n" +
			"                 Use json for machine-readable, newline-delimited logs in CI.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// An unknown subcommand is a usage error (exit 2), not the generic
		// failure code: in CI, exit 1 specifically means "findings at/above
		// threshold". Setting Args bypasses cobra's legacyArgs path (which
		// returns an uncoded error from Find), and the RunE keeps bare `atcr`
		// printing help with exit 0.
		Args: usageArgs(cobra.NoArgs),
		// PersistentPreRunE is inherited by every subcommand, so it is the single
		// point where the root logger is constructed (from LOG_LEVEL and
		// --log-format) and stored in the command context. No subcommand builds
		// its own logger after this; they retrieve it via log.FromContext.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return setupLogger(cmd)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	// --log-format is a persistent flag so every subcommand inherits it; LOG_LEVEL
	// is read from the environment (see logLevelFromEnv). Both feed setupLogger.
	root.PersistentFlags().String("log-format", "text", "log output format: text or json")

	// Flag-parse errors (unknown flags, bad values, violated flag groups)
	// are usage errors: exit 2.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError(err)
	})

	root.AddCommand(
		newReviewCmd(),
		newReconcileCmd(),
		newVerifyCmd(),
		newReportCmd(),
		newRangeCmd(),
		newStatusCmd(),
		newInitCmd(),
		newServeCmd(),
		newDoctorCmd(),
		newTrustCmd(),
		newScorecardCmd(),
		newLeaderboardCmd(),
	)
	return root
}

// logLevelFromEnv returns the configured LOG_LEVEL, defaulting to "info" when the
// variable is unset or blank. LOG_LEVEL is read from the environment (not a flag)
// so operators can raise verbosity per-invocation without changing the command
// line; log.LevelFromString validates the value in setupLogger.
func logLevelFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("LOG_LEVEL")); v != "" {
		return v
	}
	return "info"
}

// setupLogger constructs the single root logger from LOG_LEVEL and --log-format
// and stores it in the command context, where every subcommand retrieves it via
// log.FromContext. The sink is cmd.ErrOrStderr() — os.Stderr in production, so
// MCP serve mode keeps stdout protocol-only, while tests can capture output by
// redirecting the command's error writer. An invalid level or format is a usage
// error (exit 2) returned before any subcommand handler runs.
func setupLogger(cmd *cobra.Command) error {
	format, _ := cmd.Flags().GetString("log-format")
	logger, err := log.New(logLevelFromEnv(), format, cmd.ErrOrStderr())
	if err != nil {
		return usageError(err)
	}
	cmd.SetContext(log.NewContext(cmd.Context(), logger))
	return nil
}
