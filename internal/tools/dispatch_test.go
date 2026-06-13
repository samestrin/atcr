package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prefixResolver is a test stub Jail that joins rel onto root without any
// escape checks. Real jail behavior is exercised in jail_test.go.
type prefixResolver struct{ root string }

func (r prefixResolver) Resolve(rel string) (string, error) {
	return filepath.Join(r.root, rel), nil
}

func (r prefixResolver) Root() string { return r.root }

// rejectResolver always rejects, simulating a jail violation.
type rejectResolver struct{}

func (rejectResolver) Resolve(rel string) (string, error) {
	return "", fmt.Errorf("path jail: path escapes snapshot root: %s", rel)
}

func (rejectResolver) Root() string { return "" }

func newTestDispatcher(t *testing.T, root string) *Dispatcher {
	t.Helper()
	return NewDispatcher(prefixResolver{root}, DefaultLimits())
}

func TestDispatcher_RoutesReadFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package main\n")
	d := newTestDispatcher(t, root)

	res, err := d.Execute(context.Background(), "read_file", json.RawMessage(`{"path":"a.go"}`))
	require.NoError(t, err)
	assert.Equal(t, "1: package main\n", res.Content)
}

func TestDispatcher_RoutesGrep(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "func Foo() {}\n")
	d := newTestDispatcher(t, root)

	res, err := d.Execute(context.Background(), "grep", json.RawMessage(`{"pattern":"func"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "a.go:1:")
}

func TestDispatcher_RoutesListFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "x\n")
	d := newTestDispatcher(t, root)

	res, err := d.Execute(context.Background(), "list_files", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "f a.go")
}

func TestDispatcher_UnknownToolReturnsToolError(t *testing.T) {
	d := newTestDispatcher(t, t.TempDir())
	_, err := d.Execute(context.Background(), "unknown_tool", json.RawMessage(`{}`))
	require.Error(t, err)
	var te *ToolError
	require.ErrorAs(t, err, &te)
	assert.Equal(t, "unknown tool: unknown_tool", te.Error())
}

func TestDispatcher_MalformedArgumentsReturnsToolError(t *testing.T) {
	d := newTestDispatcher(t, t.TempDir())
	_, err := d.Execute(context.Background(), "read_file", json.RawMessage(`not json`))
	require.Error(t, err)
	var te *ToolError
	require.ErrorAs(t, err, &te)
	assert.Contains(t, te.Error(), "invalid arguments")
}

func TestDispatcher_PerCallByteCapTruncates(t *testing.T) {
	d := newTestDispatcher(t, t.TempDir())
	d.SetLimits(Limits{MaxResultBytes: 256})
	require.NoError(t, d.RegisterTool("big", func(_ context.Context, _ *Dispatcher, _ json.RawMessage, _ string) (ToolResult, error) {
		return ToolResult{Content: strings.Repeat("x", 1000)}, nil
	}))

	res, err := d.Execute(context.Background(), "big", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.True(t, res.Truncated)
	assert.LessOrEqual(t, len(res.Content), 256)
	assert.Equal(t, 1000, res.OriginalBytes)
}

func TestDispatcher_EmptyHandlerResult(t *testing.T) {
	d := newTestDispatcher(t, t.TempDir())
	require.NoError(t, d.RegisterTool("empty", func(_ context.Context, _ *Dispatcher, _ json.RawMessage, _ string) (ToolResult, error) {
		return ToolResult{Content: ""}, nil
	}))

	res, err := d.Execute(context.Background(), "empty", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, ToolResult{Content: "", Truncated: false, OriginalBytes: 0}, res)
}

func TestDispatcher_ResultExactlyAtCapNotTruncated(t *testing.T) {
	d := newTestDispatcher(t, t.TempDir())
	d.SetLimits(Limits{MaxResultBytes: 10})
	require.NoError(t, d.RegisterTool("exact", func(_ context.Context, _ *Dispatcher, _ json.RawMessage, _ string) (ToolResult, error) {
		return ToolResult{Content: strings.Repeat("y", 10)}, nil
	}))

	res, err := d.Execute(context.Background(), "exact", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.False(t, res.Truncated)
	assert.Equal(t, strings.Repeat("y", 10), res.Content)
}

func TestDispatcher_JailRejectionReturnedAsError(t *testing.T) {
	d := NewDispatcher(rejectResolver{}, DefaultLimits())
	_, err := d.Execute(context.Background(), "read_file", json.RawMessage(`{"path":"../escape"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path jail")
}

func TestDispatcher_HandlerPanicRecovered(t *testing.T) {
	d := newTestDispatcher(t, t.TempDir())
	require.NoError(t, d.RegisterTool("panic_tool", func(_ context.Context, _ *Dispatcher, _ json.RawMessage, _ string) (ToolResult, error) {
		panic("boom")
	}))

	_, err := d.Execute(context.Background(), "panic_tool", json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool execution failed")
}

func TestDispatcher_RegisteredToolsAreTheThreeBuiltins(t *testing.T) {
	d := newTestDispatcher(t, t.TempDir())
	assert.ElementsMatch(t, []string{"read_file", "grep", "list_files"}, d.RegisteredTools())
}

// TestTruncate_LimitSmallerThanMarker verifies that truncate never returns a
// string longer than limit, even when limit < len(truncMarker).
func TestTruncate_LimitSmallerThanMarker(t *testing.T) {
	for _, limit := range []int{1, 5, len(truncMarker) - 1} {
		result := truncate(strings.Repeat("x", 100), limit)
		assert.LessOrEqual(t, len(result), limit,
			"truncate(100 chars, %d) returned %d bytes — must be ≤ limit", limit, len(result))
	}
}
