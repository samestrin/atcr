package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/spf13/cobra"
)

// newStatusCmd builds `atcr status [id-or-path]`: print a review's fan-out
// progress as JSON so the Skill's orchestration loop can poll it. Defaults to
// .atcr/latest. The same engine reader backs the atcr_status MCP tool.
func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [id-or-path]",
		Short: "Print a review's fan-out progress as JSON",
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE:  runStatus,
	}
	return cmd
}

func runStatus(cmd *cobra.Command, args []string) error {
	arg := ""
	if len(args) == 1 {
		arg = args[0]
	}
	dir, err := anchorDir(arg)
	if err != nil {
		return usageError(err) // no review specified and no latest pointer
	}

	st, err := fanout.ReadReviewStatus(dir, filepath.Base(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return usageError(fmt.Errorf("review not found: %s", filepath.Base(dir)))
		}
		return usageError(err) // corrupt manifest, etc.
	}

	out, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding status: %w", err)
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(out)); err != nil {
		return fmt.Errorf("writing status: %w", err)
	}
	return nil
}
