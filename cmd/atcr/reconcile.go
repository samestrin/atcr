package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/spf13/cobra"
)

// newReconcileCmd builds `atcr reconcile`: discover sources, normalize,
// cluster, dedupe, compute confidence, and write reconciled artifacts. With
// --fail-on it gates the exit code on surviving findings at/above a severity.
func newReconcileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reconcile [id-or-path]",
		Short: "Merge findings from all sources into reconciled artifacts",
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE:  runReconcile,
	}
	cmd.Flags().String("fail-on", "", "exit 1 if any finding at/above this severity survives (CRITICAL, HIGH, MEDIUM, LOW)")
	cmd.Flags().Bool("require-verified", false, "with --fail-on: count only skeptic-confirmed (VERIFIED) findings — the strictest gate")
	cmd.Flags().StringSlice("sources", nil, "restrict reconcile to these source directories (default: all)")
	cmd.Flags().Bool("no-scorecard", false, "skip writing scorecard records to the local store")
	return cmd
}

func runReconcile(cmd *cobra.Command, args []string) error {
	// Resolve the gate threshold (validated against the closed enum) BEFORE any
	// I/O so a bad value fails fast as a usage error (exit 2). The --fail-on flag
	// wins; absent it, the project config's fail_on is the default gate.
	threshold, err := resolveGateThreshold(cmd)
	if err != nil {
		return err
	}

	// --require-verified is meaningless without a gate: a strict gate that never
	// runs gives false confidence (the "gate that catches nothing" failure mode
	// Epic 3.0 exists to eliminate). Fail fast as a usage error (AC 05-01 EC3).
	requireVerified, _ := cmd.Flags().GetBool("require-verified")
	if requireVerified && threshold == "" {
		return usageError(errors.New("--require-verified requires --fail-on"))
	}

	arg := ""
	if len(args) == 1 {
		arg = args[0]
	}
	reviewDir, err := resolveReviewDir(arg)
	if err != nil {
		return usageError(err) // missing/incomplete review → exit 2
	}

	// A fan-out-managed review that has not written its completion signal is a
	// usage error: reconciling mid-run would silently read a partial agent set.
	if err := fanout.EnsureReviewComplete(reviewDir, filepath.Base(reviewDir)); err != nil {
		return usageError(err)
	}

	sources, _ := cmd.Flags().GetStringSlice("sources")
	res, err := reconcile.RunReconcile(cmd.Context(), reviewDir, sources, reconcile.Options{
		ReconciledAt: time.Now(),
		Partial:      fanout.ReadManifestPartial(reviewDir),
	})
	if err != nil {
		// An I/O failure is an infrastructure/usage error (exit 2), never the
		// gate's exit 1 — and consistent with the one-shot review path.
		return usageError(err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "reconciled %d finding(s) from %d source(s) -> %s\n",
		res.Summary.TotalFindings, len(res.Summary.SourcesScanned),
		filepath.Join(reviewDir, "reconciled"))

	// Emit the per-run scorecard (Epic 3.3) via the shared bridge both reconcile
	// entry points call (CLI here, MCP atcr_reconcile handler), so the two never
	// diverge. Best-effort: a scorecard failure is logged but never fails the
	// reconcile (AC 01-01). --no-scorecard suppresses emission for this run
	// (Story 5); Emit gates on it before any I/O.
	noScorecard, _ := cmd.Flags().GetBool("no-scorecard")
	scorecard.EmitForReconcile(reviewDir, res, scorecard.EmitOpts{NoScorecard: noScorecard})

	// TD-004: warn when verify never ran — the gate would trivially pass everything.
	if requireVerified {
		if verr := reconcile.ValidateRequireVerified(reviewDir); verr != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "atcr: warning: --require-verified set but", verr)
		}
	}

	return gateFindings(res, threshold, requireVerified)
}

// gateFlagValue reads the --fail-on flag and trims it, so both threshold
// readers share one semantic: a whitespace-only value is unset, never a usage
// error in one command and a config fallback in the other.
func gateFlagValue(cmd *cobra.Command) string {
	v, _ := cmd.Flags().GetString("fail-on")
	return strings.TrimSpace(v)
}

// failOnThreshold reads and validates the --fail-on flag, returning the
// canonical threshold ("" when the flag is unset). An invalid value is a usage
// error (exit 2). Used by the one-shot review path, where the flag presence is
// itself the trigger.
func failOnThreshold(cmd *cobra.Command) (string, error) {
	v := gateFlagValue(cmd)
	if v == "" {
		return "", nil
	}
	return validateGate(v)
}

// resolveGateThreshold resolves the reconcile gate severity via the shared
// registry.ResolveGateThreshold precedence chain (--fail-on flag > project
// config > registry; no embedded default), then enum-validates the chosen
// value here because config fail_on is not validated at load time. A broken
// project config is a usage error (exit 2, the repo's own config). The same
// resolver backs the MCP atcr_reconcile handler so the two layers cannot fork.
func resolveGateThreshold(cmd *cobra.Command) (string, error) {
	raw, err := registry.ResolveGateThreshold(".", gateFlagValue(cmd))
	if err != nil {
		return "", usageError(err)
	}
	if raw == "" {
		return "", nil // no configured gate → opt-in no-op
	}
	return validateGate(raw)
}

// validateGate canonicalizes and enum-validates a gate severity; an invalid
// value is a usage error (exit 2).
func validateGate(v string) (string, error) {
	t, err := reconcile.ParseSeverity(v)
	if err != nil {
		return "", usageError(err)
	}
	return t, nil
}

// gateFindings returns a plain error (exit 1) when any finding at/above the
// threshold survives, else nil. A "" threshold is a no-op. requireVerified
// restricts the count to skeptic-confirmed (VERIFIED) findings — the strictest
// gate; refuted findings are always excluded regardless.
func gateFindings(res reconcile.Result, threshold string, requireVerified bool) error {
	if threshold == "" {
		return nil
	}
	if n := reconcile.CountAtOrAbove(res.Findings, threshold, requireVerified); n > 0 {
		return fmt.Errorf("%d finding(s) at or above %s survived reconciliation", n, threshold)
	}
	return nil
}
