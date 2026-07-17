package main

import (
	"errors"
	"fmt"
	reclib "github.com/samestrin/atcr/reconcile"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/samestrin/atcr/internal/telemetry"
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
	cmd.Flags().String("repo", ".", "repo root to validate finding file paths against (default: current directory)")
	cmd.Flags().String("fail-on", "", "exit 1 if any finding at/above this severity survives (CRITICAL, HIGH, MEDIUM, LOW)")
	cmd.Flags().Bool("require-verified", false, "with --fail-on: count only skeptic-confirmed (VERIFIED) findings — the strictest gate")
	cmd.Flags().StringSlice("sources", nil, "restrict reconcile to these source directories (default: all)")
	cmd.Flags().Bool("no-scorecard", false, "skip writing scorecard records to the local store")
	cmd.Flags().Bool("no-local-debt", false, "skip writing reconciled findings to the local TD store")
	addSyncCloudFlags(cmd)
	addQualitySignalFlags(cmd)
	return cmd
}

// normalizeRepoFlag reads the shared --repo flag for the commands that thread a
// reviewed-repo root (`reconcile` and `verify`), defaults an empty or
// whitespace-only value to "." (the CWD == repo-root operating assumption), and
// verifies the result is an existing directory. A nonexistent or non-directory
// --repo is a usage error (exit 2) so a bad root fails loudly instead of silently
// degrading path validation (reconcile) or the skeptic snapshot/redaction base
// (verify), where every finding degrades to unverifiable while the command still
// exits 0. Shared by both handlers so their normalization cannot drift (Epic 22.1).
func normalizeRepoFlag(cmd *cobra.Command) (string, error) {
	repoRoot, _ := cmd.Flags().GetString("repo")
	if strings.TrimSpace(repoRoot) == "" {
		repoRoot = "."
	}
	if info, err := os.Stat(repoRoot); err != nil || !info.IsDir() {
		return "", usageError(fmt.Errorf("--repo %q does not exist or is not a directory", repoRoot))
	}
	return repoRoot, nil
}

func runReconcile(cmd *cobra.Command, args []string) error {
	// Diagnostics route through the shared context logger so they honor LOG_LEVEL,
	// redaction, and correlation; the discard fallback keeps this nil-safe when no
	// logger was wired (no reliance on slog.Default). User-facing summary output
	// still goes to stdout (OutOrStdout) unchanged.
	logger := log.FromContext(cmd.Context())

	// --preview renders the outbound quality-signal payload locally and sends
	// nothing (Story 3). It short-circuits at the top of RunE — before the
	// --sync-cloud precondition, the opt-in gate, review-dir resolution, and any
	// transport/credential resolution — so it works for an undecided user with no
	// ATCR_API_KEY and never runs a reconcile (AC 03-01/03-02). (Cobra's pure
	// flag-relationship PreRunE still runs before RunE but does no I/O, network,
	// gate, or credential access.) Its output is the marshal of the shared
	// buildQualitySignalPayload, identical to a real send.
	if handled, perr := maybePreviewQualitySignal(cmd); handled {
		return perr
	}

	// Resolve --sync-cloud preconditions FIRST so a missing/empty ATCR_API_KEY
	// (exit 3) or a bad --cloud-endpoint (exit 2) fails fast — before any reconcile
	// I/O and before any network call (Story 4, AC 04-02/04-03).
	syncPlan, err := resolveSyncCloud(cmd)
	if err != nil {
		return err
	}

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

	// The reviewed-repo root that finding file-path validation resolves against
	// (Epic 22.1). Defaults to "." (the CWD == repo-root operating assumption),
	// preserving pre-22.1 behavior; --repo <other-repo> lets reconcile validate
	// findings against a repo other than the CWD, or from a non-repo-root CWD,
	// instead of falsely flagging every path as "file not found". An explicit
	// empty --repo normalizes to "." (never Root="", which would silently disable
	// path validation AND AST grouping); a nonexistent root fails loudly. Shared
	// with `atcr verify` via normalizeRepoFlag so the two commands cannot diverge.
	repoRoot, err := normalizeRepoFlag(cmd)
	if err != nil {
		return err
	}

	sources, _ := cmd.Flags().GetStringSlice("sources")
	res, err := reconcile.RunReconcile(cmd.Context(), reviewDir, sources, reclib.Options{
		ReconciledAt: time.Now(),
		Partial:      fanout.ReadManifestPartial(reviewDir),
		Root:         repoRoot, // validate finding file paths against --repo (Epic 22.1; default ".")
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

	gateErr := gateFindings(res, threshold, requireVerified)

	// Fire the anonymous usage ping on reconcile completion — a fire-and-forget
	// side effect alongside the scorecard/local-debt writes above, never blocking
	// or changing this command's outcome (Story 1). Sent AFTER the gate is
	// resolved so the event status reflects the run's actual outcome (TD-009).
	// The opt-out gate (Story 2) is checked BEFORE Send so a disabled run spawns
	// no goroutine; a nil client no-ops.
	if telemetryGate() {
		status := "success"
		if gateErr != nil {
			status = "failure"
		}
		telemetry.FromContext(cmd.Context()).Send(cmd.Context(), reconcileTelemetryEvent(status))
	}

	// --sync-cloud push (Story 4): an explicit, user-invoked action fired AFTER the
	// reconcile outcome is finalized. An auth rejection overrides the outcome with
	// exit 3 (AC 04-04); any other push failure is a non-fatal warning that
	// preserves the gate's own exit code (AC 04-02).
	if syncPlan.enabled {
		outcome := "success"
		if gateErr != nil {
			outcome = "failure"
		}
		// Symmetric with review.go: an auth rejection overrides the findings gate
		// (exit 1) but never an already-coded failure — though at this point gateErr
		// is only ever nil or the plain exit-1 gate error (infra errors returned above).
		return resolveSyncCloudOutcome(gateErr, runSyncCloud(cmd.Context(), cmd.ErrOrStderr(), syncPlan, reviewDir, outcome))
	}
	return gateErr
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
		// ReadAll (basePathErr). Because this also loses the set of previously
		// dismissed (wontfix) ids, warn loudly that those dismissals may resurface.
		_, _ = fmt.Fprintf(diag, "localdebt: dedup read failed, appending without dedup; previously dismissed/wontfix findings may be re-surfaced: %v\n", err)
	} else {
		for _, r := range existing {
			seen[r.ID] = true
		}
	}

	// run_id mirrors scorecard.EmitForReconcile verbatim so a finding's shard and
	// provenance line up across the two ledgers; the ts is the same reconcile
	// timestamp (deterministic, no second clock read).
	runID := res.Summary.ReconciledAt + "-" + filepath.Base(reviewDir)

	// Resolve each finding's model from the fan-out pool summary's per-agent
	// AgentStatus.Model (schema v2, Sprint 30.0), mirroring EmitForReconcile's
	// reviewer->model mapping. Best-effort: a missing/unreadable summary leaves the
	// map empty and records persist with Model == "" (attribution-incomplete), which
	// the quality-signal aggregation excludes from per-model rows rather than
	// mis-bucketing under an empty model.
	modelByReviewer := map[string]string{}
	if ps, err := fanout.ReadPoolSummary(reviewDir); err == nil {
		for _, a := range ps.Agents {
			if a.Model != "" {
				modelByReviewer[a.Agent] = a.Model
			}
		}
	}

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
			Model:         resolveRecordModel(f.Reviewers, modelByReviewer),
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

// resolveRecordModel picks the single model to attribute to a persisted
// local-debt record from its reviewers and the fan-out's reviewer->model map
// (Sprint 30.0, schema v2). A record carries one Model field, so a model can only
// be attributed when it is unambiguous: it returns the shared model when the
// record's resolvable reviewers all agree on one model (a single reviewer, or a
// multi-persona merge that ran on the same model). It returns "" —
// attribution-incomplete, which AggregateQualitySignal excludes from per-model
// rows — when no reviewer has a resolvable model, OR when reviewers span two or
// more DISTINCT models (a cross-model merged finding). Returning the first
// reviewer's model in the cross-model case would mis-credit the other personas'
// dismissal signal to a model they never ran on, so it is deliberately excluded
// rather than guessed.
func resolveRecordModel(reviewers []string, modelByReviewer map[string]string) string {
	model := ""
	for _, rev := range reviewers {
		m := modelByReviewer[rev]
		if m == "" {
			continue
		}
		if model == "" {
			model = m
		} else if m != model {
			return "" // reviewers disagree on model → attribution-incomplete
		}
	}
	return model
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
