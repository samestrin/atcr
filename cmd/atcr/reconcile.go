package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
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
	cmd.Flags().StringSlice("sources", nil, "restrict reconcile to these source directories (default: all)")
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

	arg := ""
	if len(args) == 1 {
		arg = args[0]
	}
	reviewDir, err := resolveReviewDir(arg)
	if err != nil {
		return usageError(err) // missing/incomplete review → exit 2
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

	return gateFindings(res, threshold)
}

// failOnThreshold reads and validates the --fail-on flag, returning the
// canonical threshold ("" when the flag is unset). An invalid value is a usage
// error (exit 2). Used by the one-shot review path, where the flag presence is
// itself the trigger.
func failOnThreshold(cmd *cobra.Command) (string, error) {
	v, _ := cmd.Flags().GetString("fail-on")
	if v == "" {
		return "", nil
	}
	t, err := reconcile.ParseSeverity(v)
	if err != nil {
		return "", usageError(err)
	}
	return t, nil
}

// resolveGateThreshold resolves the reconcile gate severity honoring the
// documented file-tier precedence (original-requirements): --fail-on flag >
// project config > registry. The embedded default is deliberately NOT applied —
// an unconfigured project stays opt-in (no gate) rather than spuriously failing
// on the default HIGH. The chosen value is enum-validated here because config
// fail_on is not validated at load time. Error handling: a present-but-broken
// project config is a usage error (exit 2, the repo's own config); a missing
// config is skipped; a broken user-global registry is skipped best-effort so it
// never blocks reconcile.
func resolveGateThreshold(cmd *cobra.Command) (string, error) {
	if v, _ := cmd.Flags().GetString("fail-on"); strings.TrimSpace(v) != "" {
		return validateGate(v)
	}

	// Project config tier (primary): exists+broken → exit 2; missing → skip.
	projPath := registry.DefaultProjectConfigPath(".")
	if fileExists(projPath) {
		proj, err := registry.LoadProjectConfig(projPath)
		if err != nil {
			return "", usageError(err)
		}
		if v := strings.TrimSpace(proj.FailOn); v != "" {
			return validateGate(v)
		}
	}

	// Registry tier (user-global, lower precedence): best-effort — a broken
	// registry should not block a reconcile that does not otherwise need it.
	if regPath, err := registry.DefaultRegistryPath(); err == nil && fileExists(regPath) {
		if reg, err := registry.LoadRegistry(regPath); err == nil {
			if v := strings.TrimSpace(reg.FailOn); v != "" {
				return validateGate(v)
			}
		}
	}
	return "", nil // no configured gate → opt-in no-op
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

// fileExists reports whether path exists (any non-stat-error).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// gateFindings returns a plain error (exit 1) when any finding at/above the
// threshold survives, else nil. A "" threshold is a no-op.
func gateFindings(res reconcile.Result, threshold string) error {
	if threshold == "" {
		return nil
	}
	if n := reconcile.CountAtOrAbove(res.Findings, threshold); n > 0 {
		return fmt.Errorf("%d finding(s) at or above %s survived reconciliation", n, threshold)
	}
	return nil
}
