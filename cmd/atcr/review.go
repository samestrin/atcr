package main

import "github.com/spf13/cobra"

// newReviewCmd builds `atcr review`: resolve the git range, build payloads,
// create the review directory, and fan out to the persona pool.
func newReviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Fan a code change out to the reviewer pool",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented("review")
		},
	}
	cmd.Flags().String("id", "", "review id (default: <YYYY-MM-DD>_<branch-slug>)")
	addRangeFlags(cmd)
	return cmd
}
