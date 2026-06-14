package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defByName(t *testing.T, name string) ToolDef {
	t.Helper()
	for _, d := range Tools() {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("tool def %q not found", name)
	return ToolDef{}
}

func requiredOf(t *testing.T, d ToolDef) []string {
	t.Helper()
	raw, ok := d.Parameters["required"]
	if !ok {
		return nil
	}
	vals, ok := raw.([]string)
	require.True(t, ok, "required must be []string")
	return vals
}

func TestTools_ReturnsThreeReadOnlyDefs(t *testing.T) {
	defs := Tools()
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	assert.ElementsMatch(t, []string{"read_file", "grep", "list_files"}, names)
}

func TestToolDef_MarshalsToOpenAIFunctionShape(t *testing.T) {
	for _, d := range Tools() {
		raw, err := json.Marshal(d)
		require.NoError(t, err)

		var env map[string]any
		require.NoError(t, json.Unmarshal(raw, &env))
		assert.Equal(t, "function", env["type"])

		fn, ok := env["function"].(map[string]any)
		require.True(t, ok, "function envelope present")
		assert.Equal(t, d.Name, fn["name"])
		assert.NotEmpty(t, fn["description"])

		params, ok := fn["parameters"].(map[string]any)
		require.True(t, ok, "parameters is a JSON Schema object")
		assert.Equal(t, "object", params["type"])
		_, hasProps := params["properties"]
		assert.True(t, hasProps, "schema declares properties")
	}
}

func TestReadFileDef_RequiresPath(t *testing.T) {
	assert.Contains(t, requiredOf(t, defByName(t, "read_file")), "path")
}

func TestGrepDef_RequiresPattern(t *testing.T) {
	assert.Contains(t, requiredOf(t, defByName(t, "grep")), "pattern")
}

func TestListFilesDef_RequiresNothing(t *testing.T) {
	assert.Empty(t, requiredOf(t, defByName(t, "list_files")))
}
