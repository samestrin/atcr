package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCompleter is a stub LLM completer for tests: it returns canned content (or
// an error) without any network call, so handler tests are deterministic.
type fakeCompleter struct {
	resp string
	err  error
}

func (f fakeCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return f.resp, f.err
}

// connectTest wires a server (rooted at root, driven by completer) to a client
// over an in-memory transport — never stdio — and returns the client session
// (AC 04-01 Scenario 3: InMemoryTransport enables in-process testing).
func connectTest(t *testing.T, root string, completer fakeCompleter) *mcpsdk.ClientSession {
	t.Helper()
	srv, err := NewServer(root, completer, nil)
	require.NoError(t, err)

	clientT, serverT := mcpsdk.NewInMemoryTransports()
	ctx := context.Background()
	// Servers must be connected before clients (the client initializes the session).
	if _, err := srv.Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	c := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "test"}, nil)
	cs, err := c.Connect(ctx, clientT, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// callOK invokes a tool, asserts it did not error, and decodes the typed result.
func callOK[T any](t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) T {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool %s returned error: %s", name, contentText(res))
	var out T
	b, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}

// callErr invokes a tool expecting a tool-level error result, returning its text.
func callErr(t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err, "transport-level error for %s", name)
	require.True(t, res.IsError, "tool %s unexpectedly succeeded", name)
	return contentText(res)
}

func contentText(res *mcpsdk.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestNewServer_NoError(t *testing.T) {
	srv, err := NewServer(t.TempDir(), fakeCompleter{}, nil)
	require.NoError(t, err)
	require.NotNil(t, srv)
}

// TestServe_InitializeHandshake verifies the server completes the MCP
// initialize handshake (implicit in Connect) and advertises its tools.
func TestServe_InitializeHandshake(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, res.Tools, 6, "server advertises exactly six tools after initialize")
}

// TestServe_InMemoryTransport verifies a tool is callable in-process with no
// stdio (AC 04-01 Scenario 3) — atcr_status on an empty root errors cleanly.
func TestServe_InMemoryTransport(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	msg := callErr(t, cs, ToolStatus, map[string]any{})
	assert.Contains(t, msg, "no reviews found")
}

// TestServe_NoStdoutLeak verifies no tool call writes to stdout: stdout is owned
// by the MCP protocol, so a leak would corrupt the stream (AC 04-01 Security /
// Scenario 3). The in-memory transport uses an internal pipe, so any os.Stdout
// write here is a discipline violation.
func TestServe_NoStdoutLeak(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = old })

	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	_, _ = cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: ToolStatus, Arguments: map[string]any{}})
	_, _ = cs.ListTools(context.Background(), nil)

	require.NoError(t, w.Close())
	leaked, _ := io.ReadAll(r)
	assert.Empty(t, string(leaked), "no MCP tool call may write to stdout")
}

// TestServe_StdinClosed verifies the session closes cleanly (no panic/hang) when
// the client disconnects — the stdio analogue of stdin closing (Edge Case 1/3).
func TestServe_StdinClosed(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	assert.NoError(t, cs.Close())
}
