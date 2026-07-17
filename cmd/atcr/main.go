// Command atcr is the Agent Team Code Review CLI: it fans a code change out
// to a panel of LLM reviewer personas and reconciles their findings into a
// single deduplicated, confidence-scored deliverable.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/telemetry"
	"github.com/spf13/cobra"
)

// gracefulShutdownTimeout bounds how long the process waits for cooperative
// shutdown after the first interrupt signal before forcing exit. Hardcoded per
// epic 4.1 (no flag — a --shutdown-timeout would collide with review's
// --timeout); a package var only so tests can shrink it.
var gracefulShutdownTimeout = 10 * time.Second

// forceExit terminates the process when the grace period elapses. A package var
// so tests can substitute a capture and assert the exit code without the test
// binary actually exiting.
var forceExit = os.Exit

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Intercept SIGINT/SIGTERM and cancel the root context so the fanout engine
	// drains cooperatively (no new agents start; in-flight ones finish or time
	// out) and partial results are preserved. Buffer 1 so the signal is never
	// dropped if it arrives before the goroutine blocks on the channel.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	handleSignals(sigCh, cancel, os.Stderr)

	root := newRootCmd()
	if err := root.ExecuteContext(ctx); err != nil {
		code := exitCode(err)
		if code != 0 {
			fmt.Fprintln(os.Stderr, "atcr:", err)
		}
		os.Exit(code)
	}
}

// handleSignals starts a goroutine that, on the first SIGINT/SIGTERM, prints a
// graceful-shutdown notice to out, cancels the root context, and then force-exits
// with code 1 if cooperative shutdown overruns gracefulShutdownTimeout. SIGTERM
// and SIGINT are treated identically — both mean "shut down".
//
// Only the first signal is handled and a buffered second signal is intentionally
// never drained: forcing exit on a second Ctrl-C would race the in-flight
// partial-result write (driven cooperatively by cancel() deep inside the fanout
// engine), aborting exactly the partial results epic 4.1 exists to preserve.
// main() has no flush-done seam to gate such a force-quit on, so the grace timer
// is the bounded backstop against a genuine hang.
func handleSignals(sigCh <-chan os.Signal, cancel context.CancelFunc, out io.Writer) {
	go func() {
		<-sigCh
		// Notices go to out (os.Stderr in production), not the structured logger, by
		// design: that logger is request-scoped (built per-invocation in cobra's
		// PersistentPreRunE) and is absent on the early --help/--version signal paths
		// where PersistentPreRunE never runs, so the signal handler must not depend on
		// it. These are plain interactive UX strings, not structured events.
		_, _ = fmt.Fprintln(out, "\nReceived interrupt, shutting down gracefully...")
		cancel()
		<-time.After(gracefulShutdownTimeout)
		_, _ = fmt.Fprintln(out, "Graceful shutdown timed out, forcing exit")
		forceExit(1)
	}()
}

// Exit-code semantics, centralized: 0 success, 1 failure (including
// --fail-on threshold violations), 2 usage or configuration errors, 3 a
// --sync-cloud authentication failure (missing/empty key or a remote 401/403),
// distinct from exitUsage so scripts/CI can detect an auth failure specifically.
const (
	exitFailure = 1
	exitUsage   = 2
	exitAuth    = 3
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

// authError marks err as a --sync-cloud authentication failure (exit 3), the
// dedicated code distinct from exitUsage/exitFailure so scripts can detect a
// missing/invalid ATCR_API_KEY specifically. Resolved through the same errors.As
// dispatch in exitCode as every other coded error.
func authError(err error) error {
	return &codedError{code: exitAuth, err: err}
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
	// A single opt-in telemetry client, constructed once here and injected into
	// every subcommand's context via PersistentPreRunE (deliberately not a
	// package-level singleton). The compiled-in endpoint is empty until a real
	// ingestion backend lands, so Send is a no-op in dev, CI, and production for
	// now (see defaultTelemetryEndpoint).
	telemetryClient := telemetry.New(defaultTelemetryEndpoint)

	root := &cobra.Command{
		Use:   "atcr",
		Short: "Agent Team Code Review — a review panel, not a reviewer",
		// Setting Version makes cobra auto-register the --version flag, which
		// short-circuits before PersistentPreRunE (matching the comments on that
		// hook below). A peer `version` subcommand is also registered for the
		// `atcr version` convention; both render the same string.
		Version: atcrVersion(),
		Long: "atcr fans a code change out to a panel of heterogeneous LLM reviewer personas,\n" +
			"then deterministically reconciles their findings into a single deduplicated,\n" +
			"confidence-scored deliverable.\n\n" +
			"Working directory:\n" +
			"  Run atcr from the repository root. Subcommands resolve project config\n" +
			"  (.atcr/config.yaml), the git range, and the history/audit ledgers (.atcr/)\n" +
			"  relative to the current working directory; running from a subdirectory can\n" +
			"  write ledger records that `atcr audit-report` and `atcr history` (which walk\n" +
			"  up to the repo root) will not find.\n\n" +
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
		// Note: cobra's --help/-h and --version flags short-circuit before
		// PersistentPreRunE runs, so no logger is stored in context on those
		// paths. All consumers must use log.FromContext, which falls back to a
		// shared discard logger on a miss — never assert logger presence directly.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := setupLogger(cmd); err != nil {
				return err
			}
			// Inject the single process telemetry client into the command context
			// alongside the logger, so runReview/runReconcile retrieve it via
			// telemetry.FromContext without a signature change.
			cmd.SetContext(telemetry.NewContext(cmd.Context(), telemetryClient))
			return nil
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
		newDebateCmd(),
		newReportCmd(),
		newQualityReportCmd(),
		newGithubCmd(),
		newRangeCmd(),
		newStatusCmd(),
		newInitCmd(),
		newQuickstartCmd(),
		newServeCmd(),
		newDoctorCmd(),
		newTrustCmd(),
		newScorecardCmd(),
		newLeaderboardCmd(),
		newBenchmarkCmd(),
		newPersonasCmd(),
		newModelsCmd(),
		newDebtCmd(),
		newHistoryCmd(),
		newAuditReportCmd(),
		newConfigCmd(),
		newVersionCmd(),
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

// telemetryEnabledFromEnv reports whether the ATCR_TELEMETRY env var permits the
// anonymous usage ping. Read once per emitting run (via telemetryGate); the
// value is process-stable so every subcommand resolves it identically.
//
// IMPORTANT — inverse boolean direction: ATCR_TELEMETRY names the ENABLED state
// directly, the opposite of ATCR_DISABLE_AST_GROUPING (which names a DISABLE
// flag). A recognized falsy value (0, false, f, F, False, FALSE) disables the
// ping; unset, blank, or any unparseable value fails OPEN to enabled — matching
// the documented default-on posture. Parsing is strict via strconv.ParseBool and
// never errors: an invalid value is the "default enabled" case, not a usage
// error. This footgun is called out in `atcr config set`'s help and docs/telemetry.md.
func telemetryEnabledFromEnv() bool {
	v := strings.TrimSpace(os.Getenv("ATCR_TELEMETRY"))
	if v == "" {
		return true
	}
	enabled, err := strconv.ParseBool(v)
	if err != nil {
		// Unparseable values fail open to the documented default (enabled), but
		// warn once so a misspelled opt-out (e.g. "flase") is visible rather than
		// silently ignored. This function is read once per run via telemetryGate,
		// so the warning is inherently one-time.
		_, _ = fmt.Fprintf(os.Stderr, "warning: unrecognized ATCR_TELEMETRY value %q; treating as enabled\n", v)
		return true
	}
	return enabled
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
	// Scrub secret-shaped tokens (bearer/sk-) at the single root-logger
	// construction point so EVERY command's log lines — CLI, serve, MCP — are
	// covered by AC5 without each call site opting in. NewRedactor("") applies
	// only the token regexes (empty root = no path work); per-review AC6 path
	// relativization stays layered in review.go/handlers.go where the review root
	// is known. Wrapping here (not in serve) preserves AC3 — serve still forwards
	// the context logger unchanged.
	logger = log.WithRedactor(logger, log.NewRedactor(""))
	cmd.SetContext(log.NewContext(cmd.Context(), logger))
	return nil
}
