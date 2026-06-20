package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/gitrange"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/metrics"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/validation"
	"github.com/samestrin/atcr/internal/verify"
	"github.com/spf13/cobra"
)

// newReviewCmd builds `atcr review`: resolve the git range, build payloads,
// create the review directory, and fan out to the persona pool.
func newReviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Fan a code change out to the reviewer pool",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runReview,
	}
	cmd.Flags().String("id", "", "review id (default: <YYYY-MM-DD>_<branch-slug>)")
	cmd.Flags().String("output-dir", "", "write the review tree to this path instead of .atcr/reviews/<id>/ (mutually exclusive with --id; does not update .atcr/latest)")
	cmd.Flags().String("payload", "", "payload mode override: diff, blocks, or files")
	cmd.Flags().Int("timeout", 0, "global timeout in seconds (overrides config)")
	cmd.Flags().Int64("byte-budget", 0, "per-payload byte budget, 0 = unlimited (overrides config)")
	cmd.Flags().Int("max-parallel", 0, "max concurrent parallel-lane agent calls, 0 = unbounded (default when unset: 10 from config, not unbounded)")
	cmd.Flags().String("fail-on", "", "one-shot: review + reconcile, then exit 1 if any finding at/above this severity survives")
	cmd.Flags().Bool("verify", false, "one-shot: chain review -> reconcile -> verify (adversarial skeptics) in a single run")
	cmd.Flags().Bool("require-verified", false, "with --verify and --fail-on: gate counts only skeptic-confirmed (VERIFIED) findings — the strictest gate")
	cmd.Flags().Bool("fresh", false, "with --verify: re-verify findings that already carry a verdict")
	cmd.Flags().Bool("thorough", false, "with --verify: use 3 skeptics per finding with majority rule")
	cmd.Flags().String("min-severity", "", "with --verify: skip findings below this severity floor (default MEDIUM)")
	cmd.Flags().String("resume", "", "resume an interrupted/failed review (latest | <id> | <path>): run only pending agents into the existing directory, then reconcile")
	cmd.Flags().Bool("force", false, "overwrite an existing review directory, backing it up to <dir>.bak first (applies to --id and --output-dir collisions; mutually exclusive with --resume)")
	addRangeFlags(cmd)
	return cmd
}

// outputDirFromFlags resolves the --output-dir override for `atcr review`. It
// returns "" when the flag is unset (the default .atcr/reviews/<id>/ layout). An
// explicit value is mutually exclusive with --id (the id is meaningless when the
// path is explicit) and must be non-empty; a relative path is resolved against
// CWD here, at flag-parse time, so PrepareReview receives an absolute path.
// Every rejection is a usageError so it maps to exit 2.
func outputDirFromFlags(cmd *cobra.Command) (string, error) {
	if !cmd.Flags().Changed("output-dir") {
		return "", nil
	}
	if cmd.Flags().Changed("id") {
		return "", usageError(errors.New("--output-dir and --id are mutually exclusive"))
	}
	dir, _ := cmd.Flags().GetString("output-dir")
	if strings.TrimSpace(dir) == "" {
		return "", usageError(errors.New("--output-dir must not be empty"))
	}
	abs, err := filepath.Abs(strings.TrimSpace(dir))
	if err != nil {
		return "", usageError(fmt.Errorf("resolving --output-dir: %w", err))
	}
	// Validate the resolved (absolute, cleaned) path, not the raw value: a
	// legitimate relative path like ../reviews resolves clean and passes, while
	// a path under a system directory (/etc, /proc, /sys) is rejected by the
	// input-validation layer (exit 2) before any review work begins.
	if err := validation.FilePath(abs); err != nil {
		return "", usageError(err)
	}
	return abs, nil
}

// runReview resolves the range, loads config, and runs the full review flow.
// Range/config problems are usage errors (exit 2); an all-agents-failed review
// is a plain failure (exit 1) with the artifacts preserved on disk.
func runReview(cmd *cobra.Command, _ []string) error {
	// --resume targets an existing review directory and runs only its pending
	// agents (epic 4.1.1); it is a distinct flow from a fresh review, so branch
	// before any new-review flag handling.
	if cmd.Flags().Changed("resume") {
		// --resume (continue an existing review non-destructively) and --force
		// (back up and overwrite it) are opposite collision resolutions; passing
		// both is a usage error (Epic 4.7 AC1b). Checked here, before the resume
		// short-circuit, so the conflict fires regardless of flag order.
		if cmd.Flags().Changed("force") {
			return usageError(errors.New("--resume and --force are mutually exclusive"))
		}
		anchor, _ := cmd.Flags().GetString("resume")
		return runResume(cmd, anchor)
	}

	ctx := cmd.Context()
	base, _ := cmd.Flags().GetString("base")
	head, _ := cmd.Flags().GetString("head")
	mergeCommit, _ := cmd.Flags().GetString("merge-commit")
	idOverride, _ := cmd.Flags().GetString("id")

	// Resolve --output-dir (mutually exclusive with --id, relative→absolute)
	// before any review work, so a bad flag combination is a usage error (exit 2)
	// with no wasted API calls.
	outputDir, err := outputDirFromFlags(cmd)
	if err != nil {
		return err
	}

	// Resolve the gate threshold (--fail-on flag > project config > registry)
	// before any review work; a bad configured value is a usage error (exit 2).
	threshold, err := resolveGateThreshold(cmd)
	if err != nil {
		return err
	}

	// --verify chains review -> reconcile -> verify (AC 04-02). Validate its
	// --min-severity here too, before any API calls, so a bad value fails fast.
	verifyFlag, _ := cmd.Flags().GetBool("verify")
	verifyMinSev := ""
	if verifyFlag {
		if verifyMinSev, err = verifyMinSeverity(cmd); err != nil {
			return err
		}
	}

	// --require-verified hardens the one-shot gate to count only VERIFIED findings.
	// It is meaningless without both a gate (--fail-on) and the verify stage that
	// produces verdicts (--verify); a strict gate with no verdicts would silently
	// pass everything. Fail fast as a usage error (parity with `atcr reconcile`).
	requireVerified, _ := cmd.Flags().GetBool("require-verified")
	if requireVerified && (threshold == "" || !verifyFlag) {
		return usageError(errors.New("--require-verified requires --fail-on and --verify"))
	}

	res, err := gitrange.Resolve(ctx, ".", gitrange.Options{Base: base, Head: head, MergeCommit: mergeCommit})
	if err != nil {
		// A SIGINT/SIGTERM during range resolution surfaces as context.Canceled
		// here; route it to the graceful interrupt path (exit 1 + notice) rather
		// than a confusing "review failed: context canceled" usage error (exit 2).
		if errors.Is(ctx.Err(), context.Canceled) {
			return interruptedBeforeFanout(cmd)
		}
		// A range failure aborts the pipeline before any agent runs — a usage
		// error (exit 2), per AC 03-02 Error Scenario 2 ("review failed: ...").
		return usageError(fmt.Errorf("review failed: %w", err))
	}

	cfg, err := fanout.LoadReviewConfig(".", cliOverrides(cmd))
	if err != nil {
		return usageError(err) // missing/invalid config → exit 2
	}
	if banner := cfg.Registry.ProjectProviderBanner(); banner != "" {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), banner)
	}

	now := time.Now()
	req := fanout.ReviewRequest{
		Repo: ".",
		Root: ".",
		Range: fanout.ReviewRange{
			Base:          res.Base,
			Head:          res.Head,
			DetectionMode: res.DetectionMode,
			DefaultBranch: res.DefaultBranch,
			CommitCount:   res.CommitCount,
		},
		Branch:     gitrange.CurrentBranch(ctx, "."),
		Date:       now.Format("2006-01-02"),
		TimeSuffix: now.Format("150405"),
		StartedAt:  now,
		IDOverride: idOverride,
		OutputDir:  outputDir,
		Force:      boolFlag(cmd, "force"),
	}

	// Run the two review phases separately so build-phase failures (persona
	// resolution, unknown provider, prompt render — configuration errors per
	// AC 03-02) map to exit 2, while an all-agents-failed execution stays the
	// plain exit 1 with artifacts preserved on disk.
	prep, err := fanout.PrepareReview(ctx, cfg, req)
	if err != nil {
		// An interrupt during payload build / scaffolding cancels the context; no
		// review directory exists yet, so route to the graceful no-results path
		// instead of a usage error.
		if errors.Is(ctx.Err(), context.Canceled) {
			return interruptedBeforeFanout(cmd)
		}
		return usageError(err)
	}

	// The review id is the earliest correlation anchor (it exists only after
	// PrepareReview). correlateAndRedact attaches it to the context logger so every
	// downstream stage — execute, reconcile, verify — emits log lines greppable by
	// review_id (AC9), and installs sink-level redaction (AC5/AC6) for the whole
	// review. Shared with runResume so the contract can't drift between paths.
	// From here on use this correlated ctx, never cmd.Context() again.
	ctx = correlateAndRedact(ctx, prep.ID, prep.Repo)

	if err := preflightAPIKeys(prep.Slots); err != nil {
		return err // no slot can authenticate → exit 2 before any provider call
	}

	// Snapshot the summary counters before fan-out so the end-of-review summary can
	// diff against this baseline and report only this review's contribution, not the
	// process-cumulative registry totals (correct even if a future process runs more
	// than one review against the shared DefaultRegistry).
	metricsBaseline := snapshotSummaryMetrics(metrics.DefaultRegistry)

	result, err := fanout.ExecuteReview(ctx, llmclient.New(), prep)

	// Graceful interrupt (SIGINT/SIGTERM cancelled the root context): completed
	// agents are already persisted by WritePool and the manifest is marked
	// interrupted, so report what was saved and stop — running reconcile/verify on
	// a cancelled context would only fail. The check is on the parent ctx, the one
	// signal that survives the engine's per-agent timeout classification. Exit 1
	// (consistent with the 10s force-exit path); skips the normal success line so
	// an interrupted run is never reported as "succeeded".
	if errors.Is(ctx.Err(), context.Canceled) {
		return reportInterrupt(cmd, ctx, result, prep)
	}

	// End-of-review status + metrics summary (Epic 4.4 AC3): the one-line outcome
	// plus duration, agent outcome, API calls, and findings. Both are emitted before
	// the all-agents-failed error guard below, so an exit-1 review (every agent
	// failed, artifacts preserved) still prints the breakdown an operator most needs
	// on a failure. The summary diffs the post-review registry against metricsBaseline
	// so the counts reflect this review alone — the same metrics the MCP server exports.
	// Note: time.Since(now) is CLI-level elapsed time (includes range resolution,
	// config load, and scaffolding overhead); atcr_review_duration_seconds is
	// engine-level (starts inside fanout.ExecuteReview). The two values measure
	// slightly different spans and will rarely agree exactly — this is intentional.
	if result != nil {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "review %s: %d/%d agents succeeded (%s)\n",
			result.ID, result.Summary.Succeeded, result.Summary.Total, result.Dir)
		summaryDelta := snapshotSummaryMetrics(metrics.DefaultRegistry).sub(metricsBaseline)
		writeReviewSummary(cmd.OutOrStdout(), summaryDelta, time.Since(now))
	}
	if err != nil {
		return err // all-agents-failed → exit 1, artifacts preserved
	}

	// One-shot mode: reconcile in-process and gate on the threshold. Review
	// artifacts are already on disk, so a reconcile failure (exit 2) preserves
	// them for inspection (AC 03-02 Error Scenario 3).
	//
	// result.Summary.Partial is used directly (not ReadManifestPartial) because
	// the `if err != nil { return err }` guard above ensures this block is only
	// reached when ExecuteReview succeeded — a WritePool fault returns a non-nil
	// error and short-circuits before this line. The FailureMarker correction in
	// ReadManifestPartial is only needed by the out-of-process `atcr reconcile`
	// path that runs after the fact against the on-disk summary.json.
	if threshold != "" || verifyFlag {
		rec, rerr := reconcile.RunReconcile(ctx, result.Dir, nil, reconcile.Options{
			ReconciledAt: time.Now(),
			Partial:      result.Summary.Partial,
		})
		if rerr != nil {
			return usageError(fmt.Errorf("review failed: %w", rerr))
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "reconciled %d finding(s)\n", rec.Summary.TotalFindings)

		// --verify implies the reconcile stage (run exactly once, above) and then
		// chains the adversarial verify stage in the same process (AC 04-02).
		if verifyFlag {
			vres, verr := verify.Verify(ctx, ".", result.Dir, cfg.Registry, verify.Options{
				Fresh:       boolFlag(cmd, "fresh"),
				Thorough:    boolFlag(cmd, "thorough"),
				MinSeverity: verifyMinSev,
			})
			if verr != nil {
				return verifyFailureError(verr)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"verified %d finding(s): %d confirmed, %d refuted, %d unverifiable\n",
				vres.FindingsProcessed, vres.VerdictCounts.Confirmed, vres.VerdictCounts.Refuted,
				vres.VerdictCounts.Unverifiable)
			// Gate on the post-verify findings so a refuted finding never blocks the
			// one-shot gate (the whole point of the verify stage).
			if threshold != "" {
				findings, ferr := reconcile.ReadReconciledFindings(result.Dir)
				if ferr != nil {
					return usageError(ferr)
				}
				if n := reconcile.CountFailingJSON(findings, threshold, requireVerified); n > 0 {
					return fmt.Errorf("%d finding(s) at or above %s survived verification", n, threshold)
				}
			}
			return nil
		}
		return gateFindings(rec, threshold, false)
	}
	return nil
}

// interruptedBeforeFanout handles a SIGINT/SIGTERM that arrived before the
// fan-out started (during range resolution, config load, or scaffolding). No
// review directory exists yet, so there are no partial results to preserve — the
// notice just confirms the interrupt and exits 1, consistent with the post-fan-out
// interrupt path (and never the misleading exit-2 "review failed" usage error).
func interruptedBeforeFanout(cmd *cobra.Command) error {
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "\n⚠️ Review interrupted before it started; no partial results to save.")
	return &codedError{code: exitFailure, err: errors.New("review interrupted")}
}

// interruptMessage renders the user-facing notice for a signal-interrupted
// review (epic 4.1 AC5/AC6). result may be nil when ExecuteReview was cancelled
// before producing one, so the review id and directory fall back to the
// PreparedReview, which is always available before the fan-out starts. It points
// at `atcr status <id>` to inspect and `atcr review --resume <id>` to finish the
// remaining agents (epic 4.1.1 makes --resume real, completing the partial run
// without re-spending tokens on the already-completed agents).
func interruptMessage(result *fanout.ReviewResult, prep *fanout.PreparedReview) string {
	done, total, dir := 0, 0, prep.Dir
	if result != nil {
		done, total, dir = result.Summary.Succeeded, result.Summary.Total, result.Dir
	}
	return fmt.Sprintf(
		"\n⚠️ Review interrupted. %d/%d agents completed; partial results saved to %s.\n"+
			"   Run 'atcr status %s' to inspect, or 'atcr review --resume %s' to finish the remaining agents.\n",
		done, total, dir, prep.ID, prep.ID)
}

// absFn resolves a path to absolute form. It is a package var so a test can
// substitute a failing resolver — filepath.Abs only fails when os.Getwd fails on
// a relative path, which cannot be forced in-process (mirrors the serveFn seam).
var absFn = filepath.Abs

// evalSymlinksFn resolves symlinks in a path. It is a package var so a test can
// stub it to exercise the fail-open branch.
var evalSymlinksFn = filepath.EvalSymlinks

// resolveRedactRoot returns root in absolute, symlink-resolved form for AC6 path
// relativization (relativizePaths no-ops on the CLI default "."). Symlink
// resolution matters on macOS, where the temp/repo root is reached through a
// symlink (e.g. /tmp -> /private/var/...) but paths are logged in their real
// form — without resolving, the real-form path lacks the un-resolved root prefix
// and leaks. When absolute resolution fails it returns root unchanged; when only
// symlink resolution fails it falls open to the absolute form.
func resolveRedactRoot(ctx context.Context, root string) string {
	abs, err := absFn(root)
	if err != nil {
		// Fail open (keep redacting with the relative root) but make the silent
		// loss of path relativization observable instead of swallowing the error.
		log.FromContext(ctx).Warn("path redaction may be incomplete: could not resolve absolute repo root",
			"root", root, "error", err)
		return root
	}
	resolved, err := evalSymlinksFn(abs)
	if err != nil {
		// The path may not be on disk yet; relativizing the absolute (un-resolved)
		// form still works, so fall open to abs rather than dropping relativization.
		return abs
	}
	return resolved
}

// correlateReviewID returns ctx carrying a logger tagged with the review id, so
// every downstream review stage (execute, reconcile, verify) emits log lines a
// reviewer can grep by review_id (AC9). It builds on the context logger (never a
// freshly constructed one), preserving any attributes already attached upstream.
func correlateReviewID(ctx context.Context, reviewID string) context.Context {
	logger := log.WithReviewID(log.FromContext(ctx), reviewID)
	return log.NewContext(ctx, logger)
}

// boolFlag reads a bool flag, panicking on lookup error (an undefined flag is a
// programming error that must fail loudly, not silently return false).
func boolFlag(cmd *cobra.Command, name string) bool {
	v, err := cmd.Flags().GetBool(name)
	if err != nil {
		panic(fmt.Sprintf("boolFlag: undefined flag %q: %v", name, err))
	}
	return v
}

// preflightAPIKeys fails fast (exit 2, per AC 03-02 Error Scenario 1) when no
// slot's chain — primary plus fallbacks — has its API key env var set: the
// fan-out cannot possibly produce a single success. Any one keyed agent
// anywhere lets the run proceed, because keys resolve per-invocation and
// partial success (≥1 agent) is a binding exit-0 contract. Runs after
// PrepareReview (slots carry the resolved chains), so a doomed run leaves its
// scaffolded review dir behind — consistent with the artifacts-preserved
// contract, and reconcile/report reject in-progress reviews.
func preflightAPIKeys(slots []fanout.Slot) error {
	seen := map[string]bool{}
	var missing []string
	for _, s := range slots {
		for _, a := range append([]fanout.Agent{s.Primary}, s.Fallbacks...) {
			env := a.Invocation.APIKeyEnv
			if os.Getenv(env) != "" {
				return nil
			}
			if !seen[env] {
				seen[env] = true
				missing = append(missing, env)
			}
		}
	}
	if len(missing) == 0 {
		return nil // empty roster is rejected earlier by PrepareReview
	}
	return usageError(fmt.Errorf("API key env var not set: %s", strings.Join(missing, ", ")))
}

// cliOverrides reads the shared-settings flags actually set on cmd.
func cliOverrides(cmd *cobra.Command) registry.CLIOverrides {
	var o registry.CLIOverrides
	if cmd.Flags().Changed("payload") {
		v, _ := cmd.Flags().GetString("payload")
		o.PayloadMode = &v
	}
	if cmd.Flags().Changed("timeout") {
		v, _ := cmd.Flags().GetInt("timeout")
		o.TimeoutSecs = &v
	}
	if cmd.Flags().Changed("byte-budget") {
		v, _ := cmd.Flags().GetInt64("byte-budget")
		o.PayloadByteBudget = &v
	}
	if cmd.Flags().Changed("max-parallel") {
		v, _ := cmd.Flags().GetInt("max-parallel")
		o.MaxParallel = &v
	}
	return o
}
