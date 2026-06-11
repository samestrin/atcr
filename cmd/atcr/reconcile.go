package main

import (
	"fmt"
	"path/filepath"
	"time"

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
	res, err := reconcile.RunReconcile(reviewDir, sources, reconcile.Options{
		ReconciledAt: time.Now(),
		Partial:      readManifestPartial(reviewDir),
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

// resolveGateThreshold resolves the reconcile gate threshold honoring the
// AC 03-02 precedence: the explicit --fail-on flag overrides the project
// config's fail_on (the project-level default gate). When neither is set — an
// unconfigured project with no flag — the gate is a no-op (""), so reconcile
// stays opt-in rather than spuriously failing on the embedded default. An
// invalid value at either source is a usage error (exit 2).
func resolveGateThreshold(cmd *cobra.Command) (string, error) {
	if v, _ := cmd.Flags().GetString("fail-on"); v != "" {
		t, err := reconcile.ParseSeverity(v)
		if err != nil {
			return "", usageError(err)
		}
		return t, nil
	}
	proj, err := registry.LoadProjectConfig(registry.DefaultProjectConfigPath("."))
	if err != nil || proj.FailOn == "" {
		return "", nil // no project config or no configured default → no gate
	}
	t, err := reconcile.ParseSeverity(proj.FailOn)
	if err != nil {
		return "", usageError(err)
	}
	return t, nil
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
