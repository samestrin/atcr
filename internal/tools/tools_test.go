package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegisterTool_RejectsWriteNames(t *testing.T) {
	d := NewDispatcher(prefixResolver{t.TempDir()}, t.TempDir(), DefaultLimits())
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

func TestTools_ContainNoWriteTools(t *testing.T) {
	for _, d := range Tools() {
		assert.NoError(t, guardToolName(d.Name), d.Name)
	}
}

func TestRegisteredTools_Completeness(t *testing.T) {
	d := NewDispatcher(prefixResolver{t.TempDir()}, t.TempDir(), DefaultLimits())
	assert.ElementsMatch(t, []string{"read_file", "grep", "list_files"}, d.RegisteredTools())
}
