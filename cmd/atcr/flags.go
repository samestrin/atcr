package main

import (
	"errors"

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
	// and neither hook may silently vanish.
	prev := cmd.PreRunE
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if err := validateRangeFlags(cmd); err != nil {
			return err
		}
		if prev != nil {
			return prev(cmd, args)
		}
		return nil
	}
}

// defaultCloudEndpoint is the compiled-in --sync-cloud destination. Like
// defaultTelemetryEndpoint it names the documented atcr.dev dashboard ingest URL;
// the real production ingest contract is owned by the atcr.dev backend, so tests
// point --cloud-endpoint at an httptest server instead (loopback http is
// permitted for that; see scorecard.ValidateCloudEndpoint).
const defaultCloudEndpoint = "https://atcr.dev/dashboard"

// addSyncCloudFlags declares the --sync-cloud opt-in and its --cloud-endpoint
// override (Story 4) on cmd. --cloud-endpoint's well-formedness and the presence
// of ATCR_API_KEY are validated at run time (resolveSyncCloud), not at flag-parse
// time, to keep this helper narrowly scoped to wiring (AC 04-01). The PreRunE is
// chained (not assigned), matching addRangeFlags, so a prior hook (review and
// reconcile both also register range flags) is never silently overwritten and a
// future --sync-cloud precondition can slot in without clobbering it.
func addSyncCloudFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("sync-cloud", false, "after the run, push the anonymized scorecard to the cloud dashboard (requires ATCR_API_KEY)")
	cmd.Flags().String("cloud-endpoint", defaultCloudEndpoint, "override the --sync-cloud destination (https://, or loopback http:// for local testing)")
	prev := cmd.PreRunE
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if prev != nil {
			return prev(cmd, args)
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
