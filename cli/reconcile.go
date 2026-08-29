package cli

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
falsy, unparseable, or unset value keeps AST grouping on.

Once a panel has 3 or more distinct reviewers, the consensus filter routes
uncorroborated singleton findings to the ambiguous sidecar instead of
findings.json, on the theory that a real issue would likely be caught by more
than one reviewer. --consensus selects how tolerant that filter is:

  strict   (default) sidecar every singleton below HIGH confidence — the
           behavior shipped in epic 14.2
  lenient  keep MEDIUM-confidence singletons; sidecar only LOW-confidence ones
  off      consensus filter inert; every singleton reaches findings.json. This
           is not a return to pre-14.2 output: epic 35.9's trust demotion still
           applies, so off output can contain LOW-confidence findings pre-14.2
           output never did

Only the corroboration bar moves. The 3-reviewer panel floor and every
exemption (security-related, HIGH/CRITICAL severity, out-of-scope, and
high-trust-reviewer singletons) apply identically at all three levels, and a
filtered finding is always recoverable from the sidecar. Below the panel floor
the filter is inert regardless of level.

The level also resolves from a consensus: key in .atcr/config.yaml or
~/.config/atcr/registry.yaml; the flag overrides both. Passing the flag with an
empty value is a usage error rather than a fallback to those tiers — unlike
--fail-on, an inherited consensus level can be WEAKER than the flag's default,
so --consensus "$LEVEL" with an unset shell variable must not silently disable
the filter. Note that --consensus is a flag on this command only — atcr review
and atcr review --resume honor the config/registry tiers but take no flag.`,
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: runReconcile,
	}
	cmd.Flags().String("repo-root", ".", "repo root to validate finding file paths against, and whose .atcr/debt store findings persist to — overrides the root recorded in the review manifest (default: current directory)")
	cmd.Flags().String("repo", ".", "deprecated alias for --repo-root")
	_ = cmd.Flags().MarkDeprecated("repo", "use --repo-root instead")
	cmd.Flags().String("fail-on", "", "exit 1 if any finding at/above this severity survives (CRITICAL, HIGH, MEDIUM, LOW)")
	cmd.Flags().Bool("require-verified", false, "with --fail-on: count only skeptic-confirmed (VERIFIED) findings — the strictest gate")
	cmd.Flags().String("consensus", "", "consensus filter level: strict (default), lenient, or off")
	cmd.Flags().StringSlice("sources", nil, "restrict reconcile to these source directories (default: all)")
	cmd.Flags().Bool("no-scorecard", false, "skip writing scorecard records to the local store")
	cmd.Flags().Bool("no-local-debt", false, "skip writing reconciled findings to the local TD store")
	addSyncCloudFlags(cmd)
	addQualitySignalFlags(cmd)
	return cmd
}

// normalizeRepoFlag reads the shared --repo-root flag for the commands that
// thread a reviewed-repo root (`reconcile`, `verify`, and `diff-smell`),
// defaults an empty or whitespace-only value to "." (the CWD == repo-root
// operating assumption), and verifies the result is an existing directory. A
// nonexistent or non-directory --repo-root is a usage error (exit 2) so a bad
// root fails loudly instead of silently degrading path validation (reconcile)
// or the skeptic snapshot/redaction base (verify), where every finding degrades
// to unverifiable while the command still exits 0. Shared by all handlers so
// their normalization cannot drift (Epic 22.1). --repo is a deprecated alias:
// the canonical --repo-root wins when both are set.
func normalizeRepoFlag(cmd *cobra.Command) (string, error) {
	repoRoot, _ := cmd.Flags().GetString("repo-root")
	if !cmd.Flags().Changed("repo-root") && cmd.Flags().Changed("repo") {
		repoRoot, _ = cmd.Flags().GetString("repo")
	}
	if strings.TrimSpace(repoRoot) == "" {
		repoRoot = "."
	}
	if info, err := os.Stat(repoRoot); err != nil || !info.IsDir() {
		return "", usageError(fmt.Errorf("--repo-root %q does not exist or is not a directory", repoRoot))
	}
	return repoRoot, nil
}

func runReconcile(cmd *cobra.Command, args []string) error {
	// Diagnostics route through the shared context logger so they honor LOG_LEVEL,
	// redaction, and correlation; the discard fallback keeps this nil-safe when no
	// logger was wired (no reliance on slog.Default). User-facing summary output
	// still goes to stdout (OutOrStdout) unchanged.
	logger := log.FromContext(cmd.Context())

	// --dry-run renders the outbound quality-signal payload locally and sends
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

	// The --consensus flag's own shape is validated first: it is pure argument
	// checking with no I/O, and argument errors precede config errors.
	explicitConsensus, err := consensusFlagValue(cmd)
	if err != nil {
		return err
	}

	// Resolve the gate threshold and the consensus filter level (both validated
	// against their closed enums) BEFORE any I/O so a bad value fails fast as a
	// usage error (exit 2) rather than after a reconcile has already written
	// artifacts. The --fail-on/--consensus flags win; absent them the project
	// config, then the user-global registry, decide. Resolved together from ONE
	// tier load so the two settings can never come from different tiers.
	threshold, consensusLevel, err := resolveGateAndConsensus(gateFlagValue(cmd), explicitConsensus)
	if err != nil {
		return err
	}

	// Record the effective consensus level: it can come from ~/.config/atcr/registry.yaml
	// with nothing local naming it, and ConsensusFiltered == 0 alone cannot distinguish
	// "off" from "strict with nothing to filter" — so without this a non-default level
	// changes findings.json with no trace in the run output. Logged at resolve time,
	// not post-run, so the level is still recorded when the reconcile below fails —
	// the run where an operator most needs to know which configuration was in effect.
	logger.Info("consensus filter level resolved", "consensus", consensusLevel)

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

	// Resolve the store root BEFORE RunReconcile (TD-024): finding-path
	// validation must run against the SAME root the findings persist under.
	// Validating against --repo-root (default ".", the CWD) while persistence
	// independently resolved explicit > manifest > CWD meant a reconcile from a
	// non-repo-root CWD stamped every finding PathWarning and the bridge dropped
	// all of them — the manifest-resolved store received ZERO records. The
	// explicit tier is keyed off the RAW --repo-root flag (or its deprecated
	// --repo alias), not the normalized value: both carry a "." default, so the
	// normalized value is non-empty on every run and using it here would make
	// the explicit tier always win and the manifest tier dead code, with the
	// whole feature inert and every test still green. The TrimSpace guard keeps
	// `--repo-root ""`'s documented "normalizes to the default" behavior: a
	// blank flag asserts nothing, so the manifest still speaks rather than the
	// run being pinned to the CWD.
	explicitRepo := ""
	if raw, _ := cmd.Flags().GetString("repo-root"); cmd.Flags().Changed("repo-root") && strings.TrimSpace(raw) != "" {
		explicitRepo = repoRoot // the normalizeRepoFlag-validated value
	} else if raw, _ := cmd.Flags().GetString("repo"); cmd.Flags().Changed("repo") && strings.TrimSpace(raw) != "" {
		explicitRepo = repoRoot // deprecated --repo alias, same validated value
	}
	storeRoot, storeOK := localdebt.ResolveStoreRoot(localdebt.RootOpts{
		Explicit:  explicitRepo,
		ReviewDir: reviewDir,
		AllowCWD:  true,
		Diag:      cmd.ErrOrStderr(),
	})
	// An unresolved root (the stale-manifest stop signal) keeps validation on
	// the --repo value: the reconcile still runs, persistence just stays off —
	// never Root="", which would silently disable path validation AND AST
	// grouping.
	validationRoot := repoRoot
	if storeOK && storeRoot != "" {
		validationRoot = storeRoot
	}

	res, err := reconcile.RunReconcile(cmd.Context(), reviewDir, sources, reclib.Options{
		ReconciledAt: time.Now(),
		Partial:      fanout.ReadManifestPartial(reviewDir),
		Root:         validationRoot, // validate finding paths against the store's root (TD-024)
		TrustPriors:  scorecard.ResolveTrustPriors(),
		Consensus:    consensusLevel, // epic 35.9.1: strict (default) | lenient | off
	})
	if err != nil {
		// An I/O failure is an infrastructure/usage error (exit 2), never the
		// gate's exit 1 — and consistent with the one-shot review path.
		return usageError(err)
	}

	// The filtered count is only known post-run; it rides its own line now that
	// the level itself is recorded at resolve time above.
	logger.Info("consensus filter applied", "filtered", res.Summary.ConsensusFiltered)

	// Same contract as the consensus line above: the Tier 4 routing count was
	// visible only inside report.md and summary.json, so `atcr reconcile`
	// printed a quietly smaller finding count with no stated cause.
	logger.Info("tier-4 content resolution applied", "unresolved_filtered", res.Summary.UnresolvedFiltered)

	// The trust priors are read through a 180d window (scorecard.ResolveTrustPriors,
	// epic 35.11), so a reviewer with no runs inside it silently drops out of the
	// map and loses trust exemption/demotion. Nothing else surfaces that: the
	// scorecard read discards its diagnostics and `atcr personas list --scores`
	// still reads all history. A drop in this count between runs is the signal.
	logger.Info("trust priors resolved", "reviewers", res.Summary.TrustPriorsResolved)

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "reconciled %d finding(s) from %d source(s) -> %s\n",
		res.Summary.TotalFindings, len(res.Summary.SourcesScanned),
		filepath.Join(reviewDir, "reconciled"))

	// Emit the per-run scorecard (Epic 3.3) via the shared bridge both reconcile
	// entry points call (CLI here, MCP atcr_reconcile handler), so the two never
	// diverge. Best-effort: a scorecard failure is logged but never fails the
	// reconcile (AC 01-01). --no-scorecard suppresses emission for this run
	// (Story 5); Emit gates on it before any I/O.
	noScorecard, _ := cmd.Flags().GetBool("no-scorecard")
	// docs/scorecard.md caution: a non-strict run's scorecard records are computed
	// from the post-filter finding set, durably depressing the reviewer trust
	// priors that later strict runs read. Surface the documented mitigation
	// in-run when the user hasn't already taken it.
	if consensusLevel != reclib.ConsensusStrict && !noScorecard {
		logger.Warn("non-strict consensus run depresses reviewer trust priors for later strict runs; pass --no-scorecard to keep it out of scorecard history",
			"consensus", consensusLevel)
	}
	scorecard.EmitForReconcile(reviewDir, res, scorecard.EmitOpts{NoScorecard: noScorecard, Diag: cmd.ErrOrStderr()})

	// Persist the run's reconciled findings into the .atcr/-scoped local TD store
	// (Epic 20.1 Story 2) so standalone/public users accumulate a durable backlog
	// across runs, not just one review's directory. Best-effort and non-fatal —
	// mirroring the scorecard emit above: a persistence failure is logged to the
	// diagnostics channel and never changes runReconcile's return value or the
	// reconcile gate's exit code. --no-local-debt suppresses this for the run.
	// The root was resolved BEFORE RunReconcile (above) so validation and
	// persistence agree on which store this run's findings belong to (TD-024).
	noLocalDebt, _ := cmd.Flags().GetBool("no-local-debt")
	persistedRoot := persistLocalDebt(reviewDir, res, storeRoot, storeOK, noLocalDebt, cmd.ErrOrStderr())

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
	if telemetryGate(cmd.ErrOrStderr()) {
		status := "success"
		if gateErr != nil {
			status = "failure"
		}
		telemetry.FromContext(cmd.Context()).Send(cmd.Context(), reconcileTelemetryEvent(status))
	}

	// Fire the gated, content-free community prompt quality signal (Story 6) adjacent
	// to the passive ping above. Its own independent opt-in gate is resolved fresh
	// inside — short-circuiting before any payload construction when disabled — and
	// it is fail-open: a transport failure never changes this command's outcome.
	// --dry-run (Story 3) short-circuits at the top of runReconcile, so it is never
	// reached on the preview path. The gate's unrecognized-env-value warning goes to
	// this command's stderr. storeRoot threads the SAME root the persistence hook
	// resolved, so the signal aggregates the store this run actually wrote.
	maybeSendQualitySignal(cmd.Context(), cmd.ErrOrStderr(), persistedRoot)

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

// persistLocalDebt is the CLI half of the local-TD persistence hook: delegate to
// the one shared implementation with the store root the CALLER resolved.
// runReconcile resolves that root BEFORE RunReconcile so finding-path
// validation runs against the same root the findings persist under (TD-024);
// ok=false (an unresolvable recorded root — the stale-manifest stop signal) and
// --no-local-debt are both no-ops, and "" is returned in either case so the
// quality-signal send falls back to repoRoot() discovery. All record-building,
// dedup-seeding, appending, and compaction logic lives in
// localdebt.PersistForReconcile, which the MCP atcr_reconcile handler calls too —
// keeping the two entry points on one implementation is the whole point of the
// split, so nothing that shapes a record belongs here (Plan 35.13 T6).
//
// Only two things remain genuinely CLI-side. --no-local-debt is a cobra flag the
// MCP path has no equivalent of (mirroring the scorecard emit's same asymmetry).
// And the CWD tier is CLI-only: `atcr reconcile` running from the repo root is a
// long-standing convention, whereas the MCP server's CWD is whatever launched it.
func persistLocalDebt(reviewDir string, res reconcile.Result, root string, ok bool, noLocalDebt bool, diag io.Writer) string {
	if noLocalDebt || !ok {
		return ""
	}
	// autoCompactPolicy is threaded through as a value rather than read inside the
	// bridge: a cli package var is unreachable from internal/localdebt, so passing it
	// is what keeps this test seam live after the extraction.
	localdebt.PersistForReconcile(reviewDir, res, localdebt.PersistOpts{
		Root:        root,
		Diag:        diag,
		AutoCompact: autoCompactPolicy,
	})
	return root
}

// autoCompactPolicy is the automatic-compaction threshold the reconcile persistence
// hook applies. The zero value means localdebt's production defaults (100k records
// / 100 MiB); it is a var so a test can shrink it to a handful of records and
// restore it with t.Cleanup, rather than writing 100k records to exercise the
// trigger. Same seam precedent as localdebt's lockWait/lockStale.
var autoCompactPolicy = localdebt.CompactPolicy{}

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
// with the tiered gate readers and prevent semantic drift.
func failOnThreshold(cmd *cobra.Command) (string, error) {
	v := gateFlagValue(cmd)
	if v == "" {
		return "", nil
	}
	return validateGate(v)
}

// finishGate applies the gate's call-site rules to an already-resolved raw
// value: nothing configured is an opt-in no-op, anything else is enum-validated
// here because config fail_on is not validated at load time. Shared by both
// tiered readers below so neither can phrase the gate differently.
func finishGate(raw string) (string, error) {
	if raw == "" {
		return "", nil // no configured gate → opt-in no-op
	}
	return validateGate(raw)
}

// resolveGateAndRawConsensus resolves both shared settings from a single tier
// load for the surfaces that carry no --consensus flag (`review`), returning the
// gate validated and the consensus level RAW. The enum check is left to the
// caller because the consensus value is consumed only on a reconciling run: a
// plain `atcr review` must not abort on a configured level it cannot influence,
// while a broken project config still fails here through the gate.
func resolveGateAndRawConsensus(cmd *cobra.Command) (threshold, rawConsensus string, err error) {
	rawGate, rawConsensus, err := registry.ResolveSharedSettings(".", gateFlagValue(cmd), "")
	if err != nil {
		return "", "", usageError(err)
	}
	if threshold, err = finishGate(rawGate); err != nil {
		return "", "", err
	}
	return threshold, rawConsensus, nil
}

// resolveGateAndConsensus resolves BOTH shared settings from a single load of
// the config tiers, then applies each setting's own call-site validation in the
// same order the two standalone resolvers would. A run that needs both — every
// reconcile write path does — must use this rather than calling the resolvers
// back to back: two independent resolvers parse .atcr/config.yaml twice and,
// under ATCR_REGISTRY_URL, issue two HTTP GETs, and since the registry tier is
// swallowed best-effort one fetch can succeed while the other fails, resolving
// the gate and the consensus level from different tiers within one run.
func resolveGateAndConsensus(explicitGate, explicitConsensus string) (threshold, consensus string, err error) {
	rawGate, rawConsensus, err := registry.ResolveSharedSettings(".", explicitGate, explicitConsensus)
	if err != nil {
		return "", "", usageError(err)
	}
	if threshold, err = finishGate(rawGate); err != nil {
		return "", "", err
	}
	if consensus, err = validateConsensus(rawConsensus); err != nil {
		return "", "", err
	}
	return threshold, consensus, nil
}

// consensusFlagValue reads the --consensus flag and trims it. An ABSENT flag is
// unset — it falls through to the config chain — but an explicitly set empty or
// whitespace-only value is a usage error (exit 2) naming the valid levels,
// mirroring outputDirFromFlags' "--output-dir must not be empty".
//
// This deliberately does NOT mirror gateFlagValue, because the two are not
// symmetric: an empty --fail-on inherits a gate that can only be stricter,
// whereas an empty --consensus can inherit a WEAKER filter (a checked-in
// .atcr/config.yaml or a machine-wide ~/.config/atcr/registry.yaml may say
// off). Treating it as unset would let `atcr reconcile --consensus "$LEVEL"`
// with an unset shell variable silently disable corroboration.
func consensusFlagValue(cmd *cobra.Command) (string, error) {
	if !cmd.Flags().Changed("consensus") {
		return "", nil
	}
	v, _ := cmd.Flags().GetString("consensus")
	if v = strings.TrimSpace(v); v == "" {
		return "", usageError(fmt.Errorf("--consensus must not be empty: must be one of %s",
			strings.Join(reclib.ConsensusLevels(), ", ")))
	}
	return v, nil
}

// resolveConsensusLevel resolves the consensus filter level via the shared
// registry.ResolveConsensus precedence chain (explicit > project config >
// user-global registry; no embedded default inside the resolver), then maps the
// resolver's "" to strict and enum-validates here — config consensus, like
// config fail_on, is not validated at load time. A broken project config is a
// usage error (exit 2, the repo's own config). The same resolver backs the MCP
// atcr_reconcile handler and the review/resume reconcile call sites, so the
// layers cannot fork.
//
// It takes the explicit value rather than a *cobra.Command because only `atcr
// reconcile` registers a --consensus flag; review and resume resolve the same
// chain with an empty explicit value (config/registry tiers only).
func resolveConsensusLevel(explicit string) (string, error) {
	raw, err := registry.ResolveConsensus(".", explicit)
	if err != nil {
		return "", usageError(err)
	}
	return validateConsensus(raw)
}

// validateConsensus canonicalizes and enum-validates a consensus level; an
// invalid value is a usage error (exit 2) naming the valid levels, mirroring
// validateGate. An empty value canonicalizes to strict (NormalizeConsensus),
// so nothing configured anywhere keeps today's behavior.
func validateConsensus(v string) (string, error) {
	c, ok := reclib.NormalizeConsensus(v)
	if !ok {
		return "", usageError(reclib.InvalidConsensusError(v))
	}
	return c, nil
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
