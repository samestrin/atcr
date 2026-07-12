package main

import (
	"errors"
	"fmt"
	reclib "github.com/samestrin/atcr/reconcile"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/log"
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
		Long: `Merge findings from all sources into reconciled artifacts.

Discovers per-source findings under <review>/sources, then clusters, dedupes, and
confidence-scores them into <review>/reconciled.

Clustering uses AST-isomorphism grouping by default: each finding is keyed by the
smallest covering AST block of its source line, so findings group together even
when line numbers drift, with line proximity as the per-finding fallback when no
parser is available or the source is missing. Set ATCR_DISABLE_AST_GROUPING to a
truthy value (1, true) to revert to legacy line-proximity-only clustering; a
falsy, unparseable, or unset value keeps AST grouping on.`,
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: runReconcile,
	}
	cmd.Flags().String("fail-on", "", "exit 1 if any finding at/above this severity survives (CRITICAL, HIGH, MEDIUM, LOW)")
	cmd.Flags().Bool("require-verified", false, "with --fail-on: count only skeptic-confirmed (VERIFIED) findings — the strictest gate")
	cmd.Flags().StringSlice("sources", nil, "restrict reconcile to these source directories (default: all)")
	cmd.Flags().Bool("no-scorecard", false, "skip writing scorecard records to the local store")
	cmd.Flags().Bool("no-local-debt", false, "skip writing reconciled findings to the local TD store")
	return cmd
}

func runReconcile(cmd *cobra.Command, args []string) error {
	// Diagnostics route through the shared context logger so they honor LOG_LEVEL,
	// redaction, and correlation; the discard fallback keeps this nil-safe when no
	// logger was wired (no reliance on slog.Default). User-facing summary output
	// still goes to stdout (OutOrStdout) unchanged.
	logger := log.FromContext(cmd.Context())

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
		// Trim for parity with runScorecard (scorecard.go): a trailing-whitespace
		// or quoted-blank arg becomes the empty default-anchor path rather than a
		// raw value. anchorDir trims too, so this is belt-and-suspenders that keeps
		// the two command handlers visibly consistent.
		arg = strings.TrimSpace(args[0])
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
	res, err := reconcile.RunReconcile(cmd.Context(), reviewDir, sources, reclib.Options{
		ReconciledAt: time.Now(),
		Partial:      fanout.ReadManifestPartial(reviewDir),
		Root:         ".", // repo root = CWD; validate finding file paths (Epic 5.0)
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
	scorecard.EmitForReconcile(reviewDir, res, scorecard.EmitOpts{NoScorecard: noScorecard, Diag: cmd.ErrOrStderr()})

	// Persist the run's reconciled findings into the .atcr/-scoped local TD store
	// (Epic 20.1 Story 2) so standalone/public users accumulate a durable backlog
	// across runs, not just one review's directory. Best-effort and non-fatal —
	// mirroring the scorecard emit above: a persistence failure is logged to the
	// diagnostics channel and never changes runReconcile's return value or the
	// reconcile gate's exit code. --no-local-debt suppresses this for the run.
	noLocalDebt, _ := cmd.Flags().GetBool("no-local-debt")
	persistLocalDebt(reviewDir, res, noLocalDebt, cmd.ErrOrStderr())

	// TD-004: warn when verify never ran — the gate would trivially pass
	// everything. Routed through the context logger so it honors LOG_LEVEL and is
	// correlated; visible at the default info level.
	if requireVerified {
		if verr := reconcile.ValidateRequireVerified(reviewDir); verr != nil {
			logger.Warn("--require-verified set but verify never ran", "detail", verr)
		}
	}

	return gateFindings(res, threshold, requireVerified)
}

// persistLocalDebt appends the run's reconciled findings to the .atcr/-scoped
// local TD store (Epic 20.1 Story 2). It is a best-effort, non-fatal side effect
// modeled on scorecard.EmitForReconcile: every failure is logged to diag and the
// function always returns cleanly, so the reconcile's own return value and the
// gate's exit code are never affected by a persistence problem.
//
// It reads from res.JSONFindings() (NOT res.Findings) so the Epic 18.3
// Justification/SourceReport enrichment is carried through — those fields live
// only on the cached JSONFinding layer. A zero-finding run does no I/O (no store
// directory is created).
//
// Dedup is write-time by finding id (history.FindingID(file,line,problem), via
// Record.StampID): a single full-history ReadAll seeds the set of ids already in
// the store, and each new record is appended only if its id is unseen — the same
// set also collapses in-run duplicates (two findings that hash to one id write
// once). A dedup-read failure fails OPEN toward append (log + treat store as
// empty) rather than silently dropping the whole run's backlog.
func persistLocalDebt(reviewDir string, res reconcile.Result, noLocalDebt bool, diag io.Writer) {
	if noLocalDebt {
		return
	}
	findings := res.JSONFindings()
	if len(findings) == 0 {
		return // no-op on an empty result; never a zero-length write
	}

	dir := localdebt.DefaultDir(".")
	seen := make(map[string]bool)
	if existing, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: diag}); err != nil {
		// Fail open: append anyway so a corrupt/unreadable store does not drop this
		// run's findings from the backlog. The error is already path-scrubbed by
		// ReadAll (basePathErr).
		_, _ = fmt.Fprintf(diag, "localdebt: dedup read failed, appending without dedup: %v\n", err)
	} else {
		for _, r := range existing {
			seen[r.ID] = true
		}
	}

	// run_id mirrors scorecard.EmitForReconcile verbatim so a finding's shard and
	// provenance line up across the two ledgers; the ts is the same reconcile
	// timestamp (deterministic, no second clock read).
	runID := res.Summary.ReconciledAt + "-" + filepath.Base(reviewDir)
	for _, f := range findings {
		// Apply the same exclusions the gate uses (internal/reconcile/gate.go
		// IsFailing): out-of-scope findings and refuted verdicts never persist,
		// so the local TD backlog matches what the gate considers real.
		// Path-warned findings are also skipped: a file that did not resolve under
		// the repo root is treated as a hallucinated path (Epic 5.0).
		if strings.ToLower(strings.TrimSpace(f.Category)) == reconcile.CategoryOutOfScope {
			continue
		}
		if f.Verification != nil && strings.ToLower(strings.TrimSpace(f.Verification.Verdict)) == reconcile.VerdictRefuted {
			continue
		}
		if f.PathWarning != "" {
			continue
		}

		rec := localdebt.Record{
			SchemaVersion: localdebt.SchemaVersion,
			RunID:         runID,
			Timestamp:     res.Summary.ReconciledAt,
			Severity:      f.Severity,
			File:          f.File,
			Line:          f.Line,
			Problem:       f.Problem,
			Fix:           f.Fix,
			Category:      f.Category,
			EstMinutes:    f.EstMinutes,
			Evidence:      f.Evidence,
			Reviewers:     f.Reviewers,
			Confidence:    f.Confidence,
			Justification: f.Justification,
		}
		if f.SourceReport != nil {
			rec.SourceReport = &localdebt.SourceReport{
				Path:    f.SourceReport.Path,
				Line:    f.SourceReport.Line,
				Section: f.SourceReport.Section,
			}
		}
		rec.StampID()
		if seen[rec.ID] {
			continue // already persisted (cross-run) or an in-run duplicate id
		}
		seen[rec.ID] = true
		if err := localdebt.Append(dir, rec); err != nil {
			// Non-fatal: log (already path-scrubbed by Append) and continue with the
			// remaining findings.
			_, _ = fmt.Fprintf(diag, "localdebt: append failed: %v\n", err)
		}
	}
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
// error (exit 2). Delegates validation to validateGate to share one code path
// with resolveGateThreshold and prevent semantic drift.
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
