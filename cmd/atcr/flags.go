package main

import (
	"errors"

	"github.com/spf13/cobra"
)

// addRangeFlags declares the shared review-range flags on cmd and installs a
// PreRunE enforcing their relationships: --base/--head travel together, and
// neither may combine with --merge-commit. Validation lives here (not in
// cobra flag groups) so violations surface as coded usage errors (exit 2).
func addRangeFlags(cmd *cobra.Command) {
	cmd.Flags().String("base", "", "base ref for the review range")
	cmd.Flags().String("head", "", "head ref for the review range")
	cmd.Flags().String("merge-commit", "", "merge commit SHA (base = SHA^, head = SHA)")
	cmd.PreRunE = func(cmd *cobra.Command, _ []string) error {
		return validateRangeFlags(cmd)
	}
}

// validateRangeFlags checks the declared relationships between --base,
// --head, and --merge-commit.
func validateRangeFlags(cmd *cobra.Command) error {
	base := cmd.Flags().Changed("base")
	head := cmd.Flags().Changed("head")
	mergeCommit := cmd.Flags().Changed("merge-commit")

	if base != head {
		return usageError(errors.New("--base and --head must be used together"))
	}
	if (base || head) && mergeCommit {
		return usageError(errors.New("--merge-commit cannot be combined with --base/--head"))
	}
	return nil
}
