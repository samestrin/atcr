package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/mcp"
	"github.com/spf13/cobra"
)

// newServeCmd builds `atcr serve`: an MCP stdio server wrapping the same engine
// as the CLI. Stdout is owned by the MCP protocol; every log and diagnostic
// goes to stderr (cmd.ErrOrStderr()), because a single byte of human-readable
// output on stdout corrupts the protocol and disconnects the client.
func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP stdio server over the review engine",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A stdio MCP server speaks JSON-RPC over a pipe. An interactive
			// terminal on stdin means the caller meant a different command.
			if fi, err := os.Stdin.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
				return fmt.Errorf("atcr serve requires stdin/stdout pipe; use atcr review for interactive mode")
			}

			// Stdout is owned by the protocol; logs go to stderr. Serve blocks until
			// the client disconnects (stdin EOF) or ctx is cancelled, drains
			// in-flight background reviews, then returns nil for a clean exit 0.
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil))
			return mcp.Serve(cmd.Context(), ".", llmclient.New(), logger)
		},
	}
	return cmd
}
