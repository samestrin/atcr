package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/verify"
	"github.com/spf13/cobra"
)

// newVerifyCmd builds `atcr verify`: run skeptics over a review's reconciled,
// deduplicated findings and re-emit the artifacts with verdicts and confidence
// v2. It is the standalone counterpart to `atcr review --verify` and shares the
// same internal/verify.Verify orchestration as the atcr_verify MCP tool.
func newVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify [id-or-path]",
		Short: "Run adversarial skeptics over reconciled findings",
		Long: "Run adversarial verification over a review's reconciled findings: a skeptic " +
			"(a different model than any reviewer credited on the finding) tries to refute " +
			"each finding using the read-only tool loop, then verdicts are written back as " +
			"confidence-v2 tiers. Runs after `atcr reconcile`; reads reconciled/findings.json.",
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: runVerify,
	}
	cmd.Flags().Bool("fresh", false, "re-verify findings that already carry a verdict")
	cmd.Flags().Bool("thorough", false, "use 3 skeptics per finding with majority rule (default 1)")
	cmd.Flags().String("min-severity", "", "skip findings below this severity floor: CRITICAL, HIGH, MEDIUM, LOW (default MEDIUM)")
	return cmd
}

func runVerify(cmd *cobra.Command, args []string) error {
	// Validate --min-severity against the closed enum BEFORE any I/O so a bad
	// value fails fast as a usage error (exit 2), per AC 04-01 Error Scenario 2.
	minSev, err := verifyMinSeverity(cmd)
	if err != nil {
		return err
	}

	arg := ""
	if len(args) == 1 {
		arg = args[0]
	}
	reviewDir, err := resolveReviewDir(arg)
	if err != nil {
		return verifyFailureError(err) // missing/incomplete review → exit 2
	}

	cfg, err := fanout.LoadReviewConfig(".", registry.CLIOverrides{})
	if err != nil {
		return usageError(err) // missing/invalid registry → exit 2 (AC 04-01 Error Scenario 3)
	}

	fresh, _ := cmd.Flags().GetBool("fresh")
	thorough, _ := cmd.Flags().GetBool("thorough")
	res, err := verify.Verify(cmd.Context(), ".", reviewDir, cfg.Registry, verify.Options{
		Fresh:             fresh,
		Thorough:          thorough,
		MinSeverity:       minSev,
		SharedTimeoutSecs: cfg.Settings.TimeoutSecs,
	})
	if err != nil {
		if errors.Is(err, verify.ErrNoReconciledFindings) {
			// Plain error (exit 1) with the cross-entry-point guidance (AC 04-04
			// Error Scenario 1): no stack trace, names the path, suggests reconcile.
			return fmt.Errorf("no reconciled findings found in %s — run 'atcr reconcile' first", reviewDir)
		}
		return usageError(err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"verified %d finding(s): %d confirmed, %d refuted, %d unverifiable -> %s\n",
		res.FindingsProcessed, res.VerdictCounts.Confirmed, res.VerdictCounts.Refuted,
		res.VerdictCounts.Unverifiable, filepath.Join(reviewDir, "reconciled"))
	return nil
}

// verifyMinSeverity reads and validates the --min-severity flag, returning the
// canonical threshold or "" when unset (the pipeline then applies the registry
// verify.min_severity, defaulting to MEDIUM). An invalid value is a usage error
// (exit 2). Whitespace-only is treated as unset, matching the --fail-on readers.
func verifyMinSeverity(cmd *cobra.Command) (string, error) {
	v, _ := cmd.Flags().GetString("min-severity")
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	t, err := reconcile.ParseSeverity(v)
	if err != nil {
		return "", usageError(err)
	}
	return t, nil
}

// verifyFailureError wraps a non-ErrNoReconciledFindings error from verify.Verify
// with a consistent "verify failed:" prefix so that `atcr verify` and
// `atcr review --verify` produce identical stderr shapes for scripts keying on text.
// Both still map to exit 2 via usageError.
func verifyFailureError(err error) error {
	return usageError(fmt.Errorf("verify failed: %w", err))
}
