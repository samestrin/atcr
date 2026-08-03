package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/validation"
)

// addRangeFlags declares the shared review-range flags on cmd and installs a
// PreRunE enforcing their relationships: --base may appear alone (head
// defaults to HEAD — the natural CI-gate invocation), --head requires --base,
// and neither may combine with --merge-commit. Validation lives here (not in
// cobra flag groups) so violations surface as coded usage errors (exit 2).
func addRangeFlags(cmd *cobra.Command) {
	cmd.Flags().String("base", "", "base ref for the review range")
	cmd.Flags().String("head", "", "head ref for the review range")
	cmd.Flags().String("merge-commit", "", "merge commit SHA (base = SHA^ first parent, head = SHA)")
	// Chain rather than assign: a later phase may install its own PreRunE,
	// and neither hook may silently vanish. The invariant is prev-first —
	// a previously-installed hook runs before this helper's own validation,
	// matching addSyncCloudFlags, so a command that installs both helpers
	// sees hooks fire in installation order (earlier-installed first).
	prev := cmd.PreRunE
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if prev != nil {
			if err := prev(cmd, args); err != nil {
				return err
			}
		}
		return validateRangeFlags(cmd)
	}
}

// addBaselineFlags declares the Sprint 35.0 baseline-scan flag (`--all`) on cmd.
// It installs NO PreRunE of its own: the mutual-exclusion validation (--all/--dir
// vs a diff range) is owned by the shared validateRangeFlags installed via
// addRangeFlags (Sprint 35.0 task 3.8 consolidated it there so the baseline message
// wins over the range checks — TD-001). Adding a pass-through PreRunE wrapper here
// would be dead indirection, matching addQualitySignalFlags' convention. The --dir
// string flag is registered directly on the review command (it is review-only,
// unlike --all's shared-helper registration).
func addBaselineFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("all", false, "review every non-ignored, git-tracked file as a full-repository baseline scan (no diff range; mutually exclusive with --base/--head/--merge-commit)")
}

// defaultCloudEndpoint is the compiled-in --sync-cloud destination. It is
// deliberately EMPTY: this build has no default destination at all.
//
// The scorecard ingest contract is owned by atcr-enterprise, NOT by the atcr.dev
// backend. --sync-cloud authenticates with Authorization: Bearer <ATCR_API_KEY>,
// and there is no API-key issuance flow for a community OSS user — no signup, no
// billing account. The population that holds keys is the paid product, so the
// endpoint answers to that repository on its own schedule.
//
// Empty is the honest value, but it cannot be left to fall through: an empty
// endpoint reaching scorecard.ValidateCloudEndpoint fails with "--cloud-endpoint
// must be a valid https:// URL", which blames the user for a flag they never set.
// So addSyncCloudFlags' PreRunE refuses up front and names the real cause. The
// previous value (a live atcr.dev HTML page) was worse still: with ATCR_API_KEY
// exported, a --sync-cloud run POSTed a scorecard at a web page.
//
// Tests point --cloud-endpoint at an httptest server instead (loopback http is
// permitted for that; see scorecard.ValidateCloudEndpoint) — a non-empty explicit
// override bypasses the refusal; an explicitly empty one is refused with its own
// message.
//
// Migration path unchanged: the URL is deliberately a compiled-in constant — when
// the enterprise endpoint lands, set this constant and cut a release; already
// deployed binaries can still be redirected via --cloud-endpoint without a rebuild.
const defaultCloudEndpoint = ""

// addSyncCloudFlags declares the --sync-cloud opt-in and its --cloud-endpoint
// override (Story 4) on cmd. An absent or explicitly empty --cloud-endpoint is
// refused here at flag-parse time (PreRunE) — this build ships no default
// destination, so there is nothing to fall back on; a supplied endpoint's
// well-formedness and the presence of ATCR_API_KEY are validated at run time
// (resolveSyncCloud) (AC 04-01). The PreRunE is chained prev-first (not
// assigned), matching addRangeFlags, so a prior hook is
// never silently overwritten, hooks fire in installation order
// (earlier-installed first), and a future --sync-cloud precondition can slot in
// without clobbering it. On review (and range) the prior hook is the
// range-flag validation those commands register; reconcile registers no range
// flags, so there prev is nil and this helper's hook is the only one.
func addSyncCloudFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("sync-cloud", false, "after the run, push the anonymized scorecard to the cloud dashboard (requires --cloud-endpoint and ATCR_API_KEY)")
	cmd.Flags().String("cloud-endpoint", defaultCloudEndpoint, "the --sync-cloud destination (required — this build ships no default; https://, or loopback http:// for local testing)")
	prev := cmd.PreRunE
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if prev != nil {
			if err := prev(cmd, args); err != nil {
				return err
			}
		}
		// --preview is a pure, side-effect-free local render that pushes nothing, so
		// the --sync-cloud destination is irrelevant on that path (the preview
		// short-circuit in RunE bypasses sync entirely). Suppress the refusal when
		// --preview is set — preview overrides sync-cloud (AC 03-01 EC2).
		//
		// --resume is the same shape and gets the same suppression: runReview
		// returns via runResume BEFORE resolveSyncCloud is reached, so --sync-cloud
		// is a documented no-op there. Refusing would break an invocation that
		// previously worked, over a destination the resume path never consults. The
		// no-op is not left silent — the resume branch in runReview says so on
		// stderr, for the with-endpoint case as much as the absent-destination one.
		if boolFlag(cmd, "sync-cloud") && !previewFlagSet(cmd) && !resumeFlagSet(cmd) {
			endpoint, _ := cmd.Flags().GetString("cloud-endpoint")
			// Refuse rather than warn-and-proceed: this build ships no default
			// destination, so there is nothing to push to. Reported as a usageError
			// (exit 2), matching validateRangeFlags — an absent destination is a
			// configuration fault, not the exit-3 auth rejection that a bad
			// ATCR_API_KEY earns. ATCR_API_KEY remains separately required once a
			// destination IS supplied; resolveSyncCloud still enforces it.
			if strings.TrimSpace(endpoint) == "" {
				// Distinguish "never supplied" from "supplied empty": telling a user
				// who just passed --cloud-endpoint "" that the build has no default
				// destination blames the default for a value they chose, and a bare
				// "must not be empty" misdescribes a whitespace-only value — the
				// supplied-empty branch names the trim and states availability.
				if cmd.Flags().Changed("cloud-endpoint") {
					return usageError(errors.New("--cloud-endpoint was supplied but is empty after trimming whitespace; pass a non-empty endpoint only if you operate your own compatible ingest (the scorecard ingest endpoint is not yet available in this build)"))
				}
				return usageError(errors.New("--sync-cloud has no destination in this build (the scorecard ingest endpoint is not yet available); pass --cloud-endpoint only if you operate your own compatible ingest"))
			}
		}
		return nil
	}
}

// previewFlagSet reports whether the --preview flag is registered on cmd AND set.
// It uses a nil-safe Lookup (not boolFlag) because addSyncCloudFlags may in
// principle be installed on a command that does not register --preview, and this
// runs in a PreRunE where a panic would abort an unrelated invocation.
func previewFlagSet(cmd *cobra.Command) bool {
	if cmd.Flags().Lookup("preview") == nil {
		return false
	}
	v, _ := cmd.Flags().GetBool("preview")
	return v
}

// resumeFlagSet reports whether the --resume flag is registered on cmd AND
// supplied. It keys on Changed (not the value) because `--resume ""` is still a
// resume invocation, and it uses a nil-safe Lookup for the same reason
// previewFlagSet does: reconcile hosts addSyncCloudFlags but registers no
// --resume, and a panic here would abort an unrelated invocation.
func resumeFlagSet(cmd *cobra.Command) bool {
	if cmd.Flags().Lookup("resume") == nil {
		return false
	}
	return cmd.Flags().Changed("resume")
}

// addQualitySignalFlags declares the --preview flag on cmd (the review and
// reconcile host commands — Story 6's two Send call sites). --preview renders the
// exact content-free quality-signal payload locally and sends nothing (Story 3);
// its run-path short-circuit lives in maybePreviewQualitySignal, invoked before any
// opt-in gate or transport work. It installs no PreRunE: --preview has no
// precondition of its own, so the range/sync-cloud hooks installed earlier on cmd
// already run untouched. Add chaining here only when a precondition genuinely
// appears — a pass-through wrapper kept "for a future precondition" is dead
// indirection.
func addQualitySignalFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("preview", false, "print the exact content-free quality-signal payload that would be transmitted, then exit without sending anything (needs no opt-in and makes no network call)")
}

// validateDirFlag validates the Sprint 35.0 `--dir <path>` scoped-baseline flag
// against the repository root and returns the canonical scope: a slash-normalized,
// repo-root-relative, cleaned path (or "." for the whole-repo degenerate case).
// An unset --dir returns ("", nil) — not a directory scan. Every rejection is a
// usageError (exit 2) surfaced before any payload work begins (AC 02-01):
//   - empty value → "--dir must not be empty"
//   - resolves outside root (../ escape, an absolute path outside, OR a symlink
//     whose real target escapes) → "resolves outside the repository root"
//   - does-not-exist / not-a-directory → distinct messages via a single os.Stat
//
// root is the repository root the CLI runs against (".", resolved to CWD).
//
// SECURITY (AC 02-01, Story 2 Risk Analysis — path traversal/symlink escape): the
// containment guard resolves symlinks on BOTH the root and the candidate before
// comparing (filepath.EvalSymlinks), then filepath.Rel — os.Stat follows symlinks,
// so a purely lexical guard would accept a symlink inside root whose real target
// escapes (e.g. repo/link -> /etc). Resolving both operands also fixes the macOS
// false-rejection where root is reached through a symlink (/var -> /private/var)
// but an absolute --dir is supplied in resolved form. This genuinely mirrors
// --output-dir's defense: the same symlink-resolution discipline as resolveRedactRoot
// plus a validation.FilePath system-directory denylist (as outputDirFromFlags runs).
// A non-existent candidate cannot be symlink-resolved, so the guard falls back to a
// lexical Rel for that case (which still catches a ../ escape) before os.Stat
// reports "does not exist".
func validateDirFlag(cmd *cobra.Command, root string) (string, error) {
	if !cmd.Flags().Changed("dir") {
		return "", nil
	}
	raw, _ := cmd.Flags().GetString("dir")
	if strings.TrimSpace(raw) == "" {
		return "", usageError(errors.New("--dir must not be empty"))
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", usageError(fmt.Errorf("resolving repository root for --dir: %w", err))
	}
	cand := raw
	if !filepath.IsAbs(cand) {
		cand = filepath.Join(absRoot, cand)
	}
	cand = filepath.Clean(cand)

	// Containment: resolve symlinks on both operands (matching resolveRedactRoot's
	// discipline) so a symlink escape is caught and a symlinked root does not
	// falsely reject an in-root absolute path. Fall back to a lexical Rel when the
	// candidate does not resolve (does not exist yet) — that still catches a ../
	// escape, and os.Stat below surfaces the does-not-exist case with its own message.
	realRoot := absRoot
	if rr, rerr := filepath.EvalSymlinks(absRoot); rerr == nil {
		realRoot = rr
	}
	relBase, relTarget := realRoot, cand
	if rc, rcerr := filepath.EvalSymlinks(cand); rcerr == nil {
		relTarget = rc
	} else {
		relBase = absRoot // candidate unresolved → compare lexically against the unresolved root
	}
	rel, err := filepath.Rel(relBase, relTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", usageError(fmt.Errorf("--dir path %q resolves outside the repository root", raw))
	}

	fi, err := os.Stat(cand)
	if err != nil {
		if os.IsNotExist(err) {
			return "", usageError(fmt.Errorf("--dir path %q does not exist", raw))
		}
		return "", usageError(fmt.Errorf("--dir path %q: %w", raw, err))
	}
	if !fi.IsDir() {
		return "", usageError(fmt.Errorf("--dir path %q is not a directory", raw))
	}

	// Defense-in-depth system-directory denylist, mirroring outputDirFromFlags'
	// validation.FilePath(abs) guard. relTarget is the symlink-resolved candidate
	// (guaranteed to exist and to be inside realRoot by the containment check above),
	// so this only ever trips for a repo pathologically rooted under a system dir.
	if err := validation.FilePath(relTarget); err != nil {
		return "", usageError(fmt.Errorf("--dir path %q: %w", raw, err))
	}

	// Canonical scope: repo-root-relative, slash-normalized. At this point the
	// candidate exists and is a directory, so rel came from the resolved branch and
	// equals the logical subtree path git ls-files emits (symlinks affect only the
	// shared prefix). "." for the whole-repo degenerate case.
	return filepath.ToSlash(rel), nil
}

// validateRangeFlags checks the declared relationships between --base, --head, and
// --merge-commit, and owns the shared baseline mutual-exclusion (Sprint 35.0): a
// review has exactly one scoping mode — a diff range, a full-repo --all scan, or a
// scoped --dir scan. The baseline checks run FIRST, before the range-relationship
// checks, so when --all/--dir is present the accurate baseline message wins over
// the range validator's "--head requires --base" attribution (TD-001) — this is
// the shared exclusion check AC 02-03 specifies, replacing the separate
// validateBaselineFlags. Keying on Changed() (not GetBool/GetString) means an
// explicit --all=false still counts as supplied, the presence idiom Story 1
// mandates. Changed() on a flag a host command never registered (e.g. --all/--dir
// on `range`, which shares addRangeFlags but not review's baseline flags) safely
// returns false, so the baseline checks are inert there.
func validateRangeFlags(cmd *cobra.Command) error {
	base := cmd.Flags().Changed("base")
	head := cmd.Flags().Changed("head")
	mergeCommit := cmd.Flags().Changed("merge-commit")
	all := cmd.Flags().Changed("all")
	dir := cmd.Flags().Changed("dir")

	// Baseline exclusions first (TD-001: accurate message wins over range checks).
	if dir && (base || head || mergeCommit || all) {
		return usageError(errors.New("--dir cannot be combined with --base/--head/--merge-commit/--all: a scoped baseline scan has no diff range"))
	}
	if all && (base || head || mergeCommit) {
		return usageError(errors.New("--all cannot be combined with --base/--head/--merge-commit: a full-repository scan has no diff range"))
	}

	if head && !base {
		return usageError(errors.New("--head requires --base"))
	}
	if (base || head) && mergeCommit {
		return usageError(errors.New("--merge-commit cannot be combined with --base/--head"))
	}
	return nil
}
