package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterTool_RejectsWriteNames(t *testing.T) {
	d := NewDispatcher(prefixResolver{t.TempDir()}, DefaultLimits())
	noop := func(_ context.Context, _ *Dispatcher, _ json.RawMessage, _ string) (ToolResult, error) {
		return ToolResult{}, nil
	}
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"write_file", true},
		{"delete_file", true},
		{"file_modifier", true},
		{"Write_File", true},
		{"remover", true},
		{"update_index", true},
		{"patch_apply", true},
		{"read_file", false},
		{"grep", false},
		{"list_files", false},
		{"git_log", false},
	}
	for _, c := range cases {
		err := d.RegisterTool(c.name, noop)
		if c.wantErr {
			assert.Error(t, err, c.name)
		} else {
			assert.NoError(t, err, c.name)
		}
	}
}

// TestRegisterTool_RejectsExecNames asserts the public RegisterTool API refuses
// execution-verb names (run/exec/eval/shell), mirroring the write-verb rejection.
// This is the registration-side half of the structural exec boundary (Epic 11.2):
// the only sanctioned way to register an exec handler is EnableExecution, which
// routes through the trusted registerExec path and co-sets the execTools gate.
func TestRegisterTool_RejectsExecNames(t *testing.T) {
	d := NewDispatcher(prefixResolver{t.TempDir()}, DefaultLimits())
	noop := func(_ context.Context, _ *Dispatcher, _ json.RawMessage, _ string) (ToolResult, error) {
		return ToolResult{}, nil
	}
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"run_eval", true},
		{"run_tests", true}, // even the built-in exec name is rejected via the PUBLIC API
		{"exec_cmd", true},
		{"shell_out", true},
		{"eval_expr", true},
		{"Run_Script", true},
		// read-only names remain registrable.
		{"read_file", false},
		{"grep", false},
		{"list_files", false},
		{"git_log", false},
	}
	for _, c := range cases {
		err := d.RegisterTool(c.name, noop)
		if c.wantErr {
			assert.Error(t, err, c.name)
		} else {
			assert.NoError(t, err, c.name)
		}
	}
}

// TestRegisterTool_ExecVerbNeverSilentlyUngated is the behavioral guard for the
// Epic 11.2 acceptance criterion: an exec-capable handler offered to the PUBLIC
// API must be refused outright — never registered ungated. After a rejected
// RegisterTool the name must be absent from the registry and unreachable through
// Execute (an "unknown tool" error), so it can never become a fully ungated exec
// tool the dispatch-time eligibility gate is never consulted for.
func TestRegisterTool_ExecVerbNeverSilentlyUngated(t *testing.T) {
	d := NewDispatcher(prefixResolver{t.TempDir()}, DefaultLimits())
	execish := func(_ context.Context, _ *Dispatcher, _ json.RawMessage, _ string) (ToolResult, error) {
		return ToolResult{}, nil // stands in for a handler whose body would reach the sandbox
	}
	err := d.RegisterTool("run_eval", execish)
	require.Error(t, err, "public API must reject an exec-verb tool name")
	assert.NotContains(t, d.RegisteredTools(), "run_eval", "rejected exec tool must not be registered")

	_, execErr := d.Execute(context.Background(), "run_eval", json.RawMessage(`{}`))
	require.Error(t, execErr)
	assert.Contains(t, execErr.Error(), "unknown tool", "a rejected exec tool must be unreachable, not ungated")
}

func TestTools_ContainNoWriteTools(t *testing.T) {
	for _, d := range Tools() {
		assert.NoError(t, guardToolName(d.Name), d.Name)
	}
}

func TestRegisteredTools_Completeness(t *testing.T) {
	d := NewDispatcher(prefixResolver{t.TempDir()}, DefaultLimits())
	assert.ElementsMatch(t, []string{"read_file", "grep", "list_files"}, d.RegisteredTools())
}
