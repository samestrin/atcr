package main

import (
	"testing"

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
