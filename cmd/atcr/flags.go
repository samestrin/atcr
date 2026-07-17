package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
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

// defaultCloudEndpoint is the compiled-in --sync-cloud destination. It is a
// placeholder URL only: the real production ingest contract is owned by the
// atcr.dev backend and is not operational until ATCR_API_KEY issuance is live.
// Tests point --cloud-endpoint at an httptest server instead (loopback http is
// permitted for that; see scorecard.ValidateCloudEndpoint). When a user invokes
// --sync-cloud without overriding the default, a warning is emitted so the
// placeholder default is visible rather than silently POSTing to an inactive URL.
//
// Migration path: the URL is deliberately a compiled-in constant — if the
// backend endpoint ever moves, update this constant and cut a release; already
// deployed binaries can be redirected to the new destination without a rebuild
// via the --cloud-endpoint flag.
const defaultCloudEndpoint = "https://atcr.dev/dashboard"

// addSyncCloudFlags declares the --sync-cloud opt-in and its --cloud-endpoint
// override (Story 4) on cmd. --cloud-endpoint's well-formedness and the presence
// of ATCR_API_KEY are validated at run time (resolveSyncCloud), not at flag-parse
// time, to keep this helper narrowly scoped to wiring (AC 04-01). The PreRunE is
// chained prev-first (not assigned), matching addRangeFlags, so a prior hook
// (review and reconcile both also register range flags) is never silently
// overwritten, hooks fire in installation order (earlier-installed first), and a
// future --sync-cloud precondition can slot in without clobbering it.
func addSyncCloudFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("sync-cloud", false, "after the run, push the anonymized scorecard to the cloud dashboard (requires ATCR_API_KEY)")
	cmd.Flags().String("cloud-endpoint", defaultCloudEndpoint, "override the --sync-cloud destination (https://, or loopback http:// for local testing)")
	prev := cmd.PreRunE
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if prev != nil {
			if err := prev(cmd, args); err != nil {
				return err
			}
		}
		if boolFlag(cmd, "sync-cloud") {
			endpoint, _ := cmd.Flags().GetString("cloud-endpoint")
			if strings.TrimSpace(endpoint) == defaultCloudEndpoint {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: --cloud-endpoint default %q is a placeholder; --sync-cloud will not work until a real endpoint and ATCR_API_KEY are configured\n", defaultCloudEndpoint)
			}
		}
		return nil
	}
}

// addQualitySignalFlags declares the --preview flag on cmd (the review and
// reconcile host commands — Story 6's two Send call sites). --preview renders the
// exact content-free quality-signal payload locally and sends nothing (Story 3);
// its run-path short-circuit lives in maybePreviewQualitySignal, invoked before any
// opt-in gate or transport work. The PreRunE is chained prev-first (not assigned),
// matching addRangeFlags/addSyncCloudFlags, so the range/sync-cloud hooks installed
// earlier are never silently overwritten and a future --preview precondition can
// slot in without clobbering them.
func addQualitySignalFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("preview", false, "print the exact content-free quality-signal payload that would be transmitted, then exit without sending anything (needs no opt-in and makes no network call)")
	prev := cmd.PreRunE
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if prev != nil {
			if err := prev(cmd, args); err != nil {
				return err
			}
		}
		return nil
	}
}

// validateRangeFlags checks the declared relationships between --base,
// --head, and --merge-commit.
func validateRangeFlags(cmd *cobra.Command) error {
	base := cmd.Flags().Changed("base")
	head := cmd.Flags().Changed("head")
	mergeCommit := cmd.Flags().Changed("merge-commit")

	if head && !base {
		return usageError(errors.New("--head requires --base"))
	}
	if (base || head) && mergeCommit {
		return usageError(errors.New("--merge-commit cannot be combined with --base/--head"))
	}
	return nil
}
