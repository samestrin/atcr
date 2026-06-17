package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServeCmd_Registered verifies `atcr serve` is wired into the command tree
// with the expected metadata. The MCP protocol behavior (handshake, stderr
// discipline, tool dispatch) is covered by internal/mcp using InMemoryTransport;
// the CLI layer is intentionally a thin adapter, so this test guards only the
// wiring (and that stdout stays clean is enforced by serve.go using
// cmd.ErrOrStderr()).
func TestServeCmd_Registered(t *testing.T) {
	root := newRootCmd()
	serve, _, err := root.Find([]string{"serve"})
	require.NoError(t, err)
	require.NotNil(t, serve)
	assert.Equal(t, "serve", serve.Name())
	assert.NotEmpty(t, serve.Short)
}

// TestServeCmd_UsesContextLogger verifies the serve command reuses the root
// logger from context (AC3) and constructs none of its own: the logger handed to
// mcp.Serve is exactly the one stored in the command context. The real
// StdioTransport blocks on os.Stdin, so serveFn is stubbed and stdin is pointed
// at a pipe to clear the char-device guard.
func TestServeCmd_UsesContextLogger(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	var got *slog.Logger
	orig := serveFn
	serveFn = func(_ context.Context, _ string, _ fanout.Completer, l *slog.Logger) error {
		got = l
		return nil
	}
	defer func() { serveFn = orig }()

	want, err := log.New("info", "text", io.Discard)
	require.NoError(t, err)

	cmd := newServeCmd()
	cmd.SetContext(log.NewContext(context.Background(), want))
	require.NoError(t, cmd.RunE(cmd, nil))

	require.Same(t, want, got, "serve must pass the context logger to mcp.Serve, not a locally constructed one")
}
