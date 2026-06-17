package main

import (
	"fmt"
	"os"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/mcp"
	"github.com/spf13/cobra"
)

// serveFn is the MCP engine entrypoint, exposed as a package var so tests can
// substitute a non-blocking stub: the real StdioTransport owns os.Stdin and
// os.Stdout and would block a unit test on the pipe.
var serveFn = mcp.Serve

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

			// Stdout is owned by the protocol; logs go to stderr. Reuse the root
			// logger from context (AC3) — serve constructs none of its own — so
			// LOG_LEVEL/--log-format and redaction apply uniformly. Serve blocks
			// until the client disconnects (stdin EOF) or ctx is cancelled, drains
			// in-flight background reviews, then returns nil for a clean exit 0.
			logger := log.FromContext(cmd.Context())
			return serveFn(cmd.Context(), ".", llmclient.New(), logger)
		},
	}
	return cmd
}
